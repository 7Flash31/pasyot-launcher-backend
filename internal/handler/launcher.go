package handler

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"pasyot-launcher/internal/blob"
	"pasyot-launcher/internal/domain"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Object(w http.ResponseWriter, r *http.Request) {
	sha := chi.URLParam(r, "sha")
	if !blob.ValidSHA(sha) {
		badRequest(w, "invalid hash")
		return
	}
	f, info, err := h.Blobs.Open(sha)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			notFound(w)
			return
		}
		internalError(w, err)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+sha+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, sha, info.ModTime(), f)
}

func (h *Handler) LauncherLatest(w http.ResponseWriter, r *http.Request) {
	if h.LauncherURL != "" {
		writeJSON(w, http.StatusOK, domain.LauncherBuild{
			Version:  h.LauncherVersion,
			Filename: path.Base(h.LauncherURL),
			URL:      h.baseURL(r) + "/launcher/download",
		})
		return
	}
	if h.LauncherFile != "" {
		info, err := os.Stat(h.LauncherFile)
		if err != nil {
			log.Printf("[ERROR] LAUNCHER_FILE %s: %v", h.LauncherFile, err)
			notFound(w)
			return
		}
		at := info.ModTime().UTC().Truncate(time.Second)
		writeJSON(w, http.StatusOK, domain.LauncherBuild{
			Version:   h.LauncherVersion,
			Filename:  filepath.Base(h.LauncherFile),
			Size:      info.Size(),
			URL:       h.baseURL(r) + "/launcher/download",
			CreatedAt: &at,
		})
		return
	}

	b, err := h.Store.LatestLauncherBuild(r.Context())
	if err != nil {
		h.storeError(w, err)
		return
	}
	b.URL = h.baseURL(r) + "/launcher/download"
	writeJSON(w, http.StatusOK, b)
}

func (h *Handler) LauncherDownload(w http.ResponseWriter, r *http.Request) {
	if h.LauncherURL != "" {
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.Redirect(w, r, h.LauncherURL, http.StatusFound)
		return
	}
	if h.LauncherFile != "" {
		f, info, err := openRegular(h.LauncherFile)
		if err != nil {
			log.Printf("[ERROR] LAUNCHER_FILE %s: %v", h.LauncherFile, err)
			notFound(w)
			return
		}
		defer f.Close()
		h.serveDownload(w, r, filepath.Base(h.LauncherFile), info.ModTime(), f)
		return
	}

	b, err := h.Store.LatestLauncherBuild(r.Context())
	if err != nil {
		h.storeError(w, err)
		return
	}
	f, info, err := h.Blobs.Open(b.SHA256)
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()
	h.serveDownload(w, r, b.Filename, info.ModTime(), f)
}

func (h *Handler) serveDownload(w http.ResponseWriter, r *http.Request, filename string, modtime time.Time, content io.ReadSeeker) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, filename, modtime, content)
}

func openRegular(name string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, errors.New("not a regular file")
	}
	return f, info, nil
}

func (h *Handler) UploadLauncher(w http.ResponseWriter, r *http.Request) {
	if h.LauncherURL != "" || h.LauncherFile != "" {
		http.Error(w, "launcher source is set in the environment (LAUNCHER_URL / LAUNCHER_FILE), uploads are ignored", http.StatusConflict)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxUploadBytes)

	up, err := h.receiveFile(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	defer os.Remove(up.Path)

	version := strings.TrimSpace(up.Fields["version"])
	if version == "" || len(version) > 32 {
		badRequest(w, "version: 1-32 characters")
		return
	}
	filename := up.Filename
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "pasyot-launcher-" + version
	}

	sha, size, err := h.Blobs.PutFile(up.Path)
	if err != nil {
		internalError(w, err)
		return
	}
	build := domain.LauncherBuild{Version: version, Filename: filename, Size: size, SHA256: sha}
	if err := h.Store.SaveLauncherBuild(r.Context(), &build); err != nil {
		internalError(w, err)
		return
	}
	build.URL = h.baseURL(r) + "/launcher/download"
	writeJSON(w, http.StatusCreated, build)
}

func (h *Handler) MinecraftVersions(w http.ResponseWriter, r *http.Request) {
	versions, source := h.Minecraft.Versions(r.Context())
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions, "source": source})
}

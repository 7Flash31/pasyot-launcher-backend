package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	b, err := h.Store.LatestLauncherBuild(r.Context())
	if err != nil {
		h.storeError(w, err)
		return
	}
	b.URL = h.baseURL(r) + "/launcher/download"
	writeJSON(w, http.StatusOK, b)
}

func (h *Handler) LauncherDownload(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+b.Filename+`"`)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, b.Filename, info.ModTime(), f)
}

func (h *Handler) UploadLauncher(w http.ResponseWriter, r *http.Request) {
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

package handler

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pasyot-launcher/internal/domain"
	"pasyot-launcher/internal/pack"
	"pasyot-launcher/internal/store"

	"github.com/go-chi/chi/v5"
)

const manifestFormat = 1

func (h *Handler) ListModpacks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.Modpacks(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) GetModpack(w http.ResponseWriter, r *http.Request) {
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		h.storeError(w, err)
		return
	}
	versions, err := h.Store.Versions(r.Context(), m.Name)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*domain.Modpack
		Versions []domain.Version `json:"versions"`
	}{m, versions})
}

func (h *Handler) CreateModpack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Loader      string `json:"loader"`
		Minecraft   string `json:"minecraft"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, "bad json")
		return
	}
	name := domain.NormalizeName(body.Name)
	if !domain.ValidName(name) {
		badRequest(w, "name: latin letters, digits, hyphen and underscore, 1-64 characters")
		return
	}
	loader := strings.ToLower(strings.TrimSpace(body.Loader))
	if !domain.ValidLoader(loader) {
		badRequest(w, "loader: one of "+strings.Join(domain.Loaders, ", "))
		return
	}
	minecraft := strings.TrimSpace(body.Minecraft)
	if !domain.ValidMinecraft(minecraft) {
		badRequest(w, "minecraft: version like 1.20.1, up to 16 characters")
		return
	}
	if _, err := h.Store.Modpack(r.Context(), name); err == nil {
		http.Error(w, "modpack with this name already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		internalError(w, err)
		return
	}

	m, err := h.Store.CreateModpack(r.Context(), name, strings.TrimSpace(body.Description), loader, minecraft)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (h *Handler) UpdateModpack(w http.ResponseWriter, r *http.Request) {
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		h.storeError(w, err)
		return
	}
	var body struct {
		Description *string `json:"description"`
		Loader      *string `json:"loader"`
		Minecraft   *string `json:"minecraft"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, "bad json")
		return
	}

	description, loader, minecraft := m.Description, m.Loader, m.Minecraft
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
	}
	if body.Loader != nil {
		loader = strings.ToLower(strings.TrimSpace(*body.Loader))
		if !domain.ValidLoader(loader) {
			badRequest(w, "loader: one of "+strings.Join(domain.Loaders, ", "))
			return
		}
	}
	if body.Minecraft != nil {
		minecraft = strings.TrimSpace(*body.Minecraft)
		if !domain.ValidMinecraft(minecraft) {
			badRequest(w, "minecraft: version like 1.20.1, up to 16 characters")
			return
		}
	}

	if err := h.Store.UpdateModpack(r.Context(), m.Name, description, loader, minecraft); err != nil {
		h.storeError(w, err)
		return
	}
	updated, err := h.Store.Modpack(r.Context(), m.Name)
	if err != nil {
		h.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteModpack(w http.ResponseWriter, r *http.Request) {
	orphans, err := h.Store.DeleteModpack(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		h.storeError(w, err)
		return
	}
	h.removeBlobs(orphans)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UploadVersion(w http.ResponseWriter, r *http.Request) {
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		h.storeError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.MaxUploadBytes)

	up, err := h.receiveFile(r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	defer os.Remove(up.Path)

	files, err := pack.Extract(up.Path, h.Blobs, pack.Options{
		Include:  splitList(up.Fields["include"]),
		Optional: splitList(up.Fields["optional"]),
	})
	if err != nil {
		badRequest(w, "archive: "+err.Error())
		return
	}

	fingerprint := pack.Fingerprint(files)
	prev, prevNumber, err := h.Store.LatestFingerprint(r.Context(), m.Name)
	if err != nil {
		internalError(w, err)
		return
	}
	if prev != "" && prev == fingerprint {
		http.Error(w, fmt.Sprintf("files are identical to version %d, no new version needed", prevNumber),
			http.StatusConflict)
		return
	}

	user := userFrom(r)
	version, orphans, err := h.Store.ReplaceVersion(r.Context(), m.Name, files,
		strings.TrimSpace(up.Fields["notes"]), user.Username, fingerprint)
	if err != nil {
		internalError(w, err)
		return
	}
	h.removeBlobs(orphans)

	m.LatestVersion = version.Version
	base := h.baseURL(r)
	writeJSON(w, http.StatusCreated, map[string]any{
		"modpack":      m,
		"version":      version,
		"groups":       pack.Groups(files),
		"manifest_url": manifestURL(base, m.Name),
		"pack_url":     fmt.Sprintf("%s/modpacks/%s/pack", base, m.Name),
		"pack":         packDescriptor(base, m, version.Version),
	})
}

func (h *Handler) Manifest(w http.ResponseWriter, r *http.Request) {
	m, version, files, err := h.loadVersion(r)
	if err != nil {
		h.storeError(w, err)
		return
	}
	base := h.baseURL(r)
	for i := range files {
		files[i].URL = base + "/objects/" + files[i].SHA256
	}
	manifest := domain.Manifest{
		Format:     manifestFormat,
		Name:       m.Name,
		Loader:     m.Loader,
		Minecraft:  m.Minecraft,
		Version:    version.Version,
		Notes:      version.Notes,
		Groups:     pack.Groups(files),
		FileCount:  version.FileCount,
		TotalBytes: version.TotalBytes,
		CreatedAt:  version.CreatedAt,
		Files:      files,
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, manifest)
}

func (h *Handler) PackFile(w http.ResponseWriter, r *http.Request) {
	m, version, _, err := h.loadVersion(r)
	if err != nil {
		h.storeError(w, err)
		return
	}
	filename := fmt.Sprintf("%s-v%d.pasyotpack", m.Name, version.Version)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	writeJSON(w, http.StatusOK, packDescriptor(h.baseURL(r), m, version.Version))
}

func (h *Handler) VersionArchive(w http.ResponseWriter, r *http.Request) {
	m, version, files, err := h.loadVersion(r)
	if err != nil {
		h.storeError(w, err)
		return
	}

	filename := fmt.Sprintf("%s-v%d.zip", m.Name, version.Version)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	zw := zip.NewWriter(w)
	for _, f := range files {
		src, _, err := h.Blobs.Open(f.SHA256)
		if err != nil {
			log.Printf("[ERROR] archive %s: %s: %v", filename, f.Path, err)
			return
		}
		dst, err := zw.CreateHeader(&zip.FileHeader{
			Name:     f.Path,
			Method:   compressionFor(f.Path),
			Modified: version.CreatedAt,
		})
		if err == nil {
			_, err = io.Copy(dst, src)
		}
		src.Close()
		if err != nil {
			log.Printf("[ERROR] archive %s: %s: %v", filename, f.Path, err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		log.Printf("[ERROR] archive %s: %v", filename, err)
	}
}

func compressionFor(path string) uint16 {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jar", ".zip", ".png", ".ogg", ".jpg", ".jpeg", ".gz", ".webp":
		return zip.Store
	}
	return zip.Deflate
}

func (h *Handler) loadVersion(r *http.Request) (*domain.Modpack, *domain.Version, []domain.File, error) {
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		return nil, nil, nil, err
	}
	version, err := h.Store.Version(r.Context(), m.Name, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	files, err := h.Store.VersionFiles(r.Context(), m.Name, version.Version)
	if err != nil {
		return nil, nil, nil, err
	}
	return m, version, files, nil
}

type upload struct {
	Path     string
	Filename string
	Fields   map[string]string
}

func (h *Handler) receiveFile(r *http.Request) (u upload, err error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return u, errors.New("expected multipart/form-data")
	}
	u.Fields = map[string]string{}
	defer func() {
		if err != nil && u.Path != "" {
			os.Remove(u.Path)
		}
	}()

	for {
		part, perr := mr.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		if perr != nil {
			return u, fmt.Errorf("reading form: %w", perr)
		}
		name := part.FormName()

		if name == "file" && part.FileName() != "" {
			if u.Path != "" {
				part.Close()
				return u, errors.New("multiple file fields")
			}
			tmp, terr := h.Blobs.TempFile("upload-*")
			if terr != nil {
				part.Close()
				return u, terr
			}
			u.Path = tmp.Name()
			u.Filename = filepath.Base(filepath.FromSlash(part.FileName()))
			_, cerr := io.Copy(tmp, part)
			part.Close()
			tmp.Close()
			if cerr != nil {
				return u, fmt.Errorf("upload interrupted: %w", cerr)
			}
			continue
		}

		value, verr := io.ReadAll(io.LimitReader(part, 4<<10))
		part.Close()
		if verr != nil {
			return u, fmt.Errorf("reading field %s: %w", name, verr)
		}
		u.Fields[name] = string(value)
	}

	if u.Path == "" {
		return u, errors.New("missing file field")
	}
	return u, nil
}

func packDescriptor(base string, m *domain.Modpack, version int) domain.Pack {
	return domain.Pack{
		Format:    manifestFormat,
		Server:    base,
		Name:      m.Name,
		Loader:    m.Loader,
		Minecraft: m.Minecraft,
		Version:   version,
		Manifest:  manifestURL(base, m.Name),
	}
}

func manifestURL(base, name string) string {
	return fmt.Sprintf("%s/modpacks/%s/manifest", base, name)
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *Handler) removeBlobs(shas []string) {
	freed := 0
	for _, sha := range shas {
		if err := h.Blobs.Remove(sha); err != nil {
			log.Printf("[ERROR] removing object %s: %v", sha, err)
			continue
		}
		freed++
	}
	if freed > 0 {
		log.Printf("storage: removed %d unused objects", freed)
	}
}

func (h *Handler) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		notFound(w)
		return
	}
	internalError(w, err)
}

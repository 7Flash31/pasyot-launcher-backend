package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pasyot-launcher/internal/domain"
	"pasyot-launcher/internal/pack"
	"pasyot-launcher/internal/slug"
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
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		h.storeError(w, err)
		return
	}
	versions, err := h.Store.Versions(r.Context(), m.Slug)
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
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, "bad json")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len([]rune(body.Name)) > 64 {
		badRequest(w, "name: 1-64 characters")
		return
	}
	s := body.Slug
	if s == "" {
		s = slug.Make(body.Name)
	}
	if !slug.Valid(s) {
		badRequest(w, "slug: only a-z, 0-9, hyphen and underscore; set it explicitly")
		return
	}

	if _, err := h.Store.Modpack(r.Context(), s); err == nil {
		http.Error(w, "modpack with this slug already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		internalError(w, err)
		return
	}

	m, err := h.Store.CreateModpack(r.Context(), s, body.Name, strings.TrimSpace(body.Description))
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (h *Handler) DeleteModpack(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteModpack(r.Context(), chi.URLParam(r, "slug")); err != nil {
		h.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UploadVersion(w http.ResponseWriter, r *http.Request) {
	packSlug := chi.URLParam(r, "slug")
	m, err := h.Store.Modpack(r.Context(), packSlug)
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
	prev, prevNumber, err := h.Store.LatestFingerprint(r.Context(), m.Slug)
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
	version, err := h.Store.CreateVersion(r.Context(), m.Slug, files,
		strings.TrimSpace(up.Fields["notes"]), user.Username, fingerprint)
	if err != nil {
		internalError(w, err)
		return
	}

	m.LatestVersion = version.Version
	base := h.baseURL(r)
	writeJSON(w, http.StatusCreated, map[string]any{
		"modpack":      m,
		"version":      version,
		"groups":       pack.Groups(files),
		"manifest_url": manifestURL(base, m.Slug, version.Version),
		"pack_url":     fmt.Sprintf("%s/modpacks/%s/versions/%d/pack", base, m.Slug, version.Version),
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
		Modpack:    m.Slug,
		Name:       m.Name,
		Version:    version.Version,
		Notes:      version.Notes,
		Groups:     pack.Groups(files),
		FileCount:  version.FileCount,
		TotalBytes: version.TotalBytes,
		CreatedAt:  version.CreatedAt,
		Files:      files,
	}
	if chi.URLParam(r, "version") != "" {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *Handler) PackFile(w http.ResponseWriter, r *http.Request) {
	m, version, _, err := h.loadVersion(r)
	if err != nil {
		h.storeError(w, err)
		return
	}
	filename := fmt.Sprintf("%s-v%d.pasyotpack", m.Slug, version.Version)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	writeJSON(w, http.StatusOK, packDescriptor(h.baseURL(r), m, version.Version))
}

func (h *Handler) loadVersion(r *http.Request) (*domain.Modpack, *domain.Version, []domain.File, error) {
	m, err := h.Store.Modpack(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		return nil, nil, nil, err
	}
	number := 0
	if raw := chi.URLParam(r, "version"); raw != "" {
		number, err = strconv.Atoi(raw)
		if err != nil || number <= 0 {
			return nil, nil, nil, store.ErrNotFound
		}
	}
	version, err := h.Store.Version(r.Context(), m.Slug, number)
	if err != nil {
		return nil, nil, nil, err
	}
	files, err := h.Store.VersionFiles(r.Context(), m.Slug, version.Version)
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
		Format:      manifestFormat,
		Server:      base,
		Modpack:     m.Slug,
		Name:        m.Name,
		Version:     version,
		ManifestURL: manifestURL(base, m.Slug, version),
	}
}

func manifestURL(base, slug string, version int) string {
	return fmt.Sprintf("%s/modpacks/%s/versions/%d/manifest", base, slug, version)
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

func (h *Handler) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		notFound(w)
		return
	}
	internalError(w, err)
}

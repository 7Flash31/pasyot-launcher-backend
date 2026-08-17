package handler

import (
	"net/http"

	"pasyot-launcher/internal/blob"
	"pasyot-launcher/internal/store"
	"pasyot-launcher/internal/vedrow"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	Store  *store.Store
	Blobs  *blob.Store
	Vedrow *vedrow.Client

	Admins []string

	LauncherURL     string
	LauncherFile    string
	LauncherVersion string

	PublicBaseURL  string
	PublicWebURL   string
	WebDir         string
	SecureCookies  bool
	MaxUploadBytes int64
}

func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Logger, middleware.Recoverer)

	r.Get("/health", h.Health)

	r.Get("/auth/vedrow/start", h.LoginStart)
	r.Get("/auth/vedrow/callback", h.LoginCallback)
	r.With(h.withUser, h.requireAuth).Get("/auth/me", h.Me)
	r.Post("/auth/logout", h.Logout)

	r.Get("/objects/{sha}", h.Object)

	r.Get("/launcher/latest", h.LauncherLatest)
	r.Get("/launcher/download", h.LauncherDownload)
	r.With(h.withUser, h.requireAdmin).Post("/launcher/builds", h.UploadLauncher)

	r.Route("/modpacks", func(r chi.Router) {
		r.Get("/", h.ListModpacks)
		r.Get("/{slug}", h.GetModpack)
		r.Get("/{slug}/manifest", h.Manifest)
		r.Get("/{slug}/versions/{version}/manifest", h.Manifest)
		r.Get("/{slug}/pack", h.PackFile)
		r.Get("/{slug}/versions/{version}/pack", h.PackFile)

		r.Group(func(r chi.Router) {
			r.Use(h.withUser, h.requireAdmin)
			r.Post("/", h.CreateModpack)
			r.Delete("/{slug}", h.DeleteModpack)
			r.Post("/{slug}/versions", h.UploadVersion)
		})
	})

	if h.WebDir != "" {
		r.Handle("/*", http.FileServer(http.Dir(h.WebDir)))
	}

	return r
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.Ping(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

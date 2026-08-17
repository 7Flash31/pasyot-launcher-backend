package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pasyot-launcher/internal/blob"
	"pasyot-launcher/internal/handler"
	"pasyot-launcher/internal/store"
	"pasyot-launcher/internal/vedrow"

	"github.com/joho/godotenv"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check that the server is alive and exit")
	flag.Parse()

	_ = godotenv.Load()

	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	dataDir := env("DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("cannot create DATA_DIR %s: %v", dataDir, err)
	}

	st, err := store.Open(env("DB_PATH", filepath.Join(dataDir, "pasyot.db")))
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()

	blobs, err := blob.New(dataDir)
	if err != nil {
		log.Fatalf("file store: %v", err)
	}

	publicBase := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	publicWeb := strings.TrimRight(os.Getenv("PUBLIC_WEB_URL"), "/")

	webDir := env("WEB_DIR", "./web")
	if _, err := os.Stat(webDir); err != nil {
		webDir = ""
	}

	redirect := os.Getenv("VEDROW_REDIRECT_URI")
	if redirect == "" {
		redirect = publicBase + "/auth/vedrow/callback"
	}
	vd := vedrow.New(
		os.Getenv("VEDROW_API_URL"),
		os.Getenv("VEDROW_WEB_URL"),
		os.Getenv("VEDROW_CLIENT_ID"),
		os.Getenv("VEDROW_CLIENT_SECRET"),
		redirect,
	)
	if !vd.Configured() {
		log.Print("[WARN] vedrow login is not configured (VEDROW_*): /auth/vedrow/start will answer 503")
	}

	h := &handler.Handler{
		Store:          st,
		Blobs:          blobs,
		Vedrow:         vd,
		Admins:         splitEnv("ADMINS"),
		PublicBaseURL:  publicBase,
		PublicWebURL:   publicWeb,
		WebDir:         webDir,
		SecureCookies:  strings.HasPrefix(publicBase, "https://"),
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_MB", 4096)) << 20,
	}
	if len(h.Admins) == 0 {
		log.Print("[WARN] ADMINS is empty: nobody will be able to create a modpack")
	}

	addr := ":" + env("PORT", "8081")
	srv := &http.Server{
		Addr:              addr,
		Handler:           corsMiddleware(publicWeb, handler.NewRouter(h)),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       2 * time.Minute,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Print("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[ERROR] shutdown: %v", err)
		}
	}()

	if webDir != "" {
		log.Printf("serving site from %s", webDir)
	}
	log.Printf("listening on %s, data in %s", addr, dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}

func corsMiddleware(origin string, next http.Handler) http.Handler {
	if v := os.Getenv("CORS_ORIGIN"); v != "" {
		origin = v
	}
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if origin != "*" {
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func runHealthcheck() int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + env("PORT", "8081") + "/health")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

func splitEnv(key string) []string {
	var out []string
	for _, part := range strings.Split(os.Getenv(key), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

package handler

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pasyot-launcher/internal/store"
	"pasyot-launcher/internal/vedrow"

	"github.com/google/uuid"
)

const (
	sessionTTL = 30 * 24 * time.Hour
	loginTTL   = 10 * time.Minute
)

func (h *Handler) LoginStart(w http.ResponseWriter, r *http.Request) {
	if !h.Vedrow.Configured() {
		http.Error(w, "vedrow login is not configured", http.StatusServiceUnavailable)
		return
	}
	state, verifier := vedrow.NewState(), vedrow.NewVerifier()
	next := safeNext(r.URL.Query().Get("next"))
	if err := h.Store.SaveLoginState(r.Context(), state, verifier, next, loginTTL); err != nil {
		internalError(w, err)
		return
	}
	http.Redirect(w, r, h.Vedrow.AuthorizeURL(state, verifier), http.StatusFound)
}

func (h *Handler) LoginCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if e := q.Get("error"); e != "" {
		h.redirectWithError(w, r, e)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		h.redirectWithError(w, r, "invalid_response")
		return
	}

	verifier, next, err := h.Store.TakeLoginState(r.Context(), state)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.redirectWithError(w, r, "expired_state")
			return
		}
		internalError(w, err)
		return
	}

	info, err := h.Vedrow.Login(r.Context(), code, verifier)
	if err != nil {
		log.Printf("[ERROR] vedrow login: %v", err)
		h.redirectWithError(w, r, "vedrow_unavailable")
		return
	}

	username := info.PreferredUsername
	if username == "" {
		username = info.Name
	}
	user, err := h.Store.UpsertUser(r.Context(), uuid.NewString(), info.Sub,
		username, info.Email, info.Picture, h.isAdmin(info))
	if err != nil {
		internalError(w, err)
		return
	}

	token := newSessionToken()
	if err := h.Store.CreateSession(r.Context(), hashToken(token), user.ID, sessionTTL); err != nil {
		internalError(w, err)
		return
	}
	h.setSessionCookie(w, token)

	http.Redirect(w, r, strings.TrimRight(h.PublicWebURL, "/")+next, http.StatusFound)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r))
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		if err := h.Store.DeleteSession(r.Context(), hashToken(token)); err != nil {
			internalError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
		Secure:   h.SecureCookies,
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (h *Handler) isAdmin(info *vedrow.UserInfo) bool {
	for _, a := range h.Admins {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if a == strings.ToLower(info.PreferredUsername) ||
			a == strings.ToLower(info.Email) || a == info.Sub {
			return true
		}
	}
	return false
}

func (h *Handler) redirectWithError(w http.ResponseWriter, r *http.Request, reason string) {
	target := strings.TrimRight(h.PublicWebURL, "/") + "/?login_error=" + url.QueryEscape(reason)
	http.Redirect(w, r, target, http.StatusFound)
}

func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	if u, err := url.Parse(next); err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return next
}

package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const refreshCookieName = "er_refresh"

type Handler struct {
	service      *Service
	cookieSecure bool
}

func NewHandler(service *Service, cookieSecure bool) *Handler {
	return &Handler{service: service, cookieSecure: cookieSecure}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/signup", h.signup)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.HandleFunc("GET /api/v1/auth/me", h.me)
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	meta := requestMeta(r, req.DeviceName, req.Platform)
	pair, err := h.service.Signup(r.Context(), req, meta)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}
	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusCreated, authResponse(pair))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	meta := requestMeta(r, req.DeviceName, req.Platform)
	pair, err := h.service.Login(r.Context(), req, meta)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}
	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusOK, authResponse(pair))
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	refreshToken := refreshTokenFromRequest(r)
	pair, err := h.service.Refresh(r.Context(), refreshToken, requestMeta(r, "", "web"))
	if err != nil {
		if errors.Is(err, ErrSessionRevoked) {
			h.clearRefreshCookie(w)
		}
		h.writeAuthError(w, err)
		return
	}
	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusOK, authResponse(pair))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	refreshToken := refreshTokenFromRequest(r)
	if err := h.service.Logout(r.Context(), refreshToken, requestMeta(r, "", "web")); err != nil {
		writeError(w, http.StatusInternalServerError, "logout_failed")
		return
	}
	h.clearRefreshCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	accessToken, err := h.service.TokenFromAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_bearer_token")
		return
	}
	user, err := h.service.CurrentUser(r.Context(), accessToken)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]User{"user": user})
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/api/v1/auth",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_input")
	case errors.Is(err, ErrEmailAlreadyExists):
		writeError(w, http.StatusConflict, "email_already_exists")
	case errors.Is(err, ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
	case errors.Is(err, ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "invalid_token")
	case errors.Is(err, ErrSessionRevoked):
		writeError(w, http.StatusUnauthorized, "session_revoked")
	default:
		slog.Error("auth request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

func authResponse(pair TokenPair) AuthResponse {
	return AuthResponse{
		AccessToken: pair.AccessToken,
		ExpiresIn:   int64(pair.AccessTTL.Seconds()),
		TokenType:   "Bearer",
		User:        pair.User,
	}
}

func refreshTokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return strings.TrimSpace(body.RefreshToken)
}

func requestMeta(r *http.Request, deviceName, platform string) SessionMeta {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		ip = strings.TrimSpace(strings.Split(ip, ",")[0])
	} else {
		var err error
		ip, _, err = net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
	}
	return SessionMeta{
		UserAgent:  r.UserAgent(),
		IPAddress:  ip,
		DeviceName: strings.TrimSpace(deviceName),
		Platform:   strings.TrimSpace(platform),
	}
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

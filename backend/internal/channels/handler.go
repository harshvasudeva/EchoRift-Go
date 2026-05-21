package channels

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"echorift/backend/internal/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db   *pgxpool.Pool
	auth *auth.Service
}

type Channel struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Private     bool      `json:"is_private"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

func NewHandler(db *pgxpool.Pool, authService *auth.Service) *Handler {
	return &Handler{db: db, auth: authService}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceID}/channels", h.list)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing_workspace_id")
		return
	}
	if err := h.requireMembership(r, workspaceID, user.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusForbidden, "workspace_access_denied")
			return
		}
		writeError(w, http.StatusInternalServerError, "membership_check_failed")
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT id::text, workspace_id::text, name, COALESCE(description, ''), type, is_private, position, created_at
		FROM channels
		WHERE workspace_id = $1 AND archived_at IS NULL
		ORDER BY position ASC, created_at ASC
	`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "channel_list_failed")
		return
	}
	defer rows.Close()

	channels := make([]Channel, 0)
	for rows.Next() {
		var channel Channel
		if err := rows.Scan(&channel.ID, &channel.WorkspaceID, &channel.Name, &channel.Description, &channel.Type, &channel.Private, &channel.Position, &channel.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "channel_scan_failed")
			return
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "channel_list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]Channel{"channels": channels})
}

func (h *Handler) requireMembership(r *http.Request, workspaceID, userID string) error {
	var membershipID string
	return h.db.QueryRow(r.Context(), `
		SELECT id::text
		FROM memberships
		WHERE workspace_id = $1 AND user_id = $2 AND disabled_at IS NULL
	`, workspaceID, userID).Scan(&membershipID)
}

func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	token, err := h.auth.TokenFromAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_bearer_token")
		return auth.User{}, false
	}
	user, err := h.auth.CurrentUser(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return auth.User{}, false
	}
	return user, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

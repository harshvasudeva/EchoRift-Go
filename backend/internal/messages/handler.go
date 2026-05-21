package messages

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"echorift/backend/internal/auth"
	realtime "echorift/backend/internal/websocket"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Broadcaster interface {
	BroadcastWorkspace(workspaceID string, event any)
}

type Handler struct {
	db          *pgxpool.Pool
	auth        *auth.Service
	broadcaster Broadcaster
}

type Message struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	ChannelID   string     `json:"channel_id"`
	AuthorID    string     `json:"author_user_id"`
	AuthorName  string     `json:"author_display_name"`
	Body        string     `json:"body"`
	CreatedAt   time.Time  `json:"created_at"`
	EditedAt    *time.Time `json:"edited_at"`
}

type SendMessageRequest struct {
	Body string `json:"body"`
}

type SendMessageResponse struct {
	Message Message `json:"message"`
}

func NewHandler(db *pgxpool.Pool, authService *auth.Service, broadcaster Broadcaster) *Handler {
	return &Handler{db: db, auth: authService, broadcaster: broadcaster}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceID}/channels/{channelID}/messages", h.list)
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceID}/channels/{channelID}/messages", h.send)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	channelID := r.PathValue("channelID")
	if err := h.requireChannelAccess(r, workspaceID, channelID, user.ID); err != nil {
		h.writeAccessError(w, err)
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 50, 100)
	rows, err := h.db.Query(r.Context(), `
		SELECT * FROM (
		    SELECT m.id::text, m.workspace_id::text, m.channel_id::text,
		           COALESCE(m.author_user_id::text, ''), COALESCE(u.display_name, 'Deleted User'),
		           COALESCE(m.body, ''), m.created_at, m.edited_at
		    FROM messages m
		    LEFT JOIN users u ON u.id = m.author_user_id
		    WHERE m.workspace_id = $1
		      AND m.channel_id = $2
		      AND m.deleted_at IS NULL
		    ORDER BY m.created_at DESC
		    LIMIT $3
		) recent
		ORDER BY created_at ASC
	`, workspaceID, channelID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "message_list_failed")
		return
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.WorkspaceID, &message.ChannelID, &message.AuthorID, &message.AuthorName, &message.Body, &message.CreatedAt, &message.EditedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "message_scan_failed")
			return
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "message_list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]Message{"messages": messages})
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	channelID := r.PathValue("channelID")
	if err := h.requireChannelAccess(r, workspaceID, channelID, user.ID); err != nil {
		h.writeAccessError(w, err)
		return
	}

	var req SendMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 4000 {
		writeError(w, http.StatusBadRequest, "invalid_message_body")
		return
	}

	var message Message
	if err := h.db.QueryRow(r.Context(), `
		INSERT INTO messages (workspace_id, channel_id, author_user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, workspace_id::text, channel_id::text, author_user_id::text, $5::text, body, created_at, edited_at
	`, workspaceID, channelID, user.ID, body, user.Name).Scan(&message.ID, &message.WorkspaceID, &message.ChannelID, &message.AuthorID, &message.AuthorName, &message.Body, &message.CreatedAt, &message.EditedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "message_send_failed")
		return
	}

	if h.broadcaster != nil {
		h.broadcaster.BroadcastWorkspace(workspaceID, realtime.Event{
			Type:        "message.created",
			WorkspaceID: workspaceID,
			ChannelID:   channelID,
			Payload:     map[string]any{"message": message},
			CreatedAt:   time.Now().UTC(),
		})
	}

	writeJSON(w, http.StatusCreated, SendMessageResponse{Message: message})
}

func (h *Handler) requireChannelAccess(r *http.Request, workspaceID, channelID, userID string) error {
	var id string
	return h.db.QueryRow(r.Context(), `
		SELECT c.id::text
		FROM channels c
		JOIN memberships m ON m.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1
		  AND c.id = $2
		  AND m.user_id = $3
		  AND m.disabled_at IS NULL
		  AND c.archived_at IS NULL
	`, workspaceID, channelID, userID).Scan(&id)
}

func (h *Handler) writeAccessError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusForbidden, "channel_access_denied")
		return
	}
	writeError(w, http.StatusInternalServerError, "channel_access_check_failed")
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

func parseLimit(raw string, fallback, max int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
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

package websocket

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"echorift/backend/internal/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	writeTimeout = 5 * time.Second
	readLimit    = 64 * 1024
)

type Handler struct {
	db   *pgxpool.Pool
	auth *auth.Service
	hub  *Hub
	log  *slog.Logger
}

type Connection struct {
	id            string
	user          auth.User
	socket        *websocket.Conn
	send          chan Event
	subscriptions map[string]struct{}
}

type ClientEvent struct {
	ID          string         `json:"id,omitempty"`
	Type        string         `json:"type"`
	WorkspaceID string         `json:"workspace_id,omitempty"`
	ChannelID   string         `json:"channel_id,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

func NewHandler(db *pgxpool.Pool, authService *auth.Service, hub *Hub, log *slog.Logger) *Handler {
	return &Handler{db: db, auth: authService, hub: hub, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ws", h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	accessToken := strings.TrimSpace(r.URL.Query().Get("access_token"))
	if accessToken == "" {
		var err error
		accessToken, err = h.auth.TokenFromAuthorization(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
	}

	user, err := h.auth.CurrentUser(r.Context(), accessToken)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		h.log.Warn("websocket accept failed", "error", err)
		return
	}
	ws.SetReadLimit(readLimit)

	conn := &Connection{
		id:            randomID(),
		user:          user,
		socket:        ws,
		send:          make(chan Event, 64),
		subscriptions: make(map[string]struct{}),
	}

	h.log.Info("websocket connected", "conn_id", conn.id, "user_id", user.ID)
	defer func() {
		subscriptions := make([]string, 0, len(conn.subscriptions))
		for workspaceID := range conn.subscriptions {
			subscriptions = append(subscriptions, workspaceID)
		}
		h.hub.Remove(conn)
		for _, workspaceID := range subscriptions {
			if h.hub.CountUserWorkspace(workspaceID, conn.user.ID) == 0 {
				h.broadcastPresence(workspaceID, conn.user, "offline")
			}
		}
		_ = ws.Close(websocket.StatusNormalClosure, "closed")
		h.log.Info("websocket disconnected", "conn_id", conn.id, "user_id", user.ID)
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go h.writeLoop(ctx, conn, cancel)
	h.readLoop(ctx, conn, cancel)
}

func (h *Handler) readLoop(ctx context.Context, conn *Connection, cancel context.CancelFunc) {
	defer cancel()
	for {
		var event ClientEvent
		if err := wsjson.Read(ctx, conn.socket, &event); err != nil {
			if !isExpectedClose(err) && !contextDone(ctx) {
				h.log.Debug("websocket read failed", "conn_id", conn.id, "error", err)
			}
			return
		}

		switch event.Type {
		case "ping":
			conn.enqueue(Event{ID: randomID(), Type: "pong", CreatedAt: time.Now().UTC()})
		case "workspace.subscribe":
			h.handleSubscribe(ctx, conn, event)
		case "workspace.unsubscribe":
			if event.WorkspaceID != "" {
				h.hub.UnsubscribeWorkspace(conn, event.WorkspaceID)
				conn.enqueue(Event{ID: randomID(), Type: "workspace.unsubscribed", WorkspaceID: event.WorkspaceID, CreatedAt: time.Now().UTC()})
			}
		case "typing.start", "typing.stop":
			h.handleEphemeralWorkspaceEvent(ctx, conn, event)
		default:
			conn.enqueue(Event{ID: randomID(), Type: "error", Payload: map[string]string{"code": "unknown_event_type"}, CreatedAt: time.Now().UTC()})
		}
	}
}

func (h *Handler) writeLoop(ctx context.Context, conn *Connection, cancel context.CancelFunc) {
	defer cancel()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-conn.send:
			if !ok {
				return
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(writeCtx, conn.socket, event)
			writeCancel()
			if err != nil {
				h.log.Debug("websocket write failed", "conn_id", conn.id, "error", err)
				return
			}
		case <-ticker.C:
			writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(writeCtx, conn.socket, Event{ID: randomID(), Type: "ping", CreatedAt: time.Now().UTC()})
			writeCancel()
			if err != nil {
				h.log.Debug("websocket heartbeat failed", "conn_id", conn.id, "error", err)
				return
			}
		}
	}
}

func (h *Handler) handleSubscribe(ctx context.Context, conn *Connection, event ClientEvent) {
	if event.WorkspaceID == "" {
		conn.enqueue(Event{ID: randomID(), Type: "error", Payload: map[string]string{"code": "missing_workspace_id"}, CreatedAt: time.Now().UTC()})
		return
	}
	if err := h.requireWorkspaceMembership(ctx, event.WorkspaceID, conn.user.ID); err != nil {
		conn.enqueue(Event{ID: randomID(), Type: "error", WorkspaceID: event.WorkspaceID, Payload: map[string]string{"code": "workspace_access_denied"}, CreatedAt: time.Now().UTC()})
		return
	}
	h.hub.SubscribeWorkspace(conn, event.WorkspaceID)
	conn.enqueue(Event{ID: randomID(), Type: "workspace.subscribed", WorkspaceID: event.WorkspaceID, Payload: map[string]any{"connections": h.hub.CountWorkspace(event.WorkspaceID), "presence": h.hub.WorkspacePresence(event.WorkspaceID)}, CreatedAt: time.Now().UTC()})
	h.broadcastPresence(event.WorkspaceID, conn.user, "online")
}

func (h *Handler) handleEphemeralWorkspaceEvent(ctx context.Context, conn *Connection, event ClientEvent) {
	if event.WorkspaceID == "" {
		return
	}
	if _, ok := conn.subscriptions[event.WorkspaceID]; !ok {
		return
	}
	if err := h.requireWorkspaceMembership(ctx, event.WorkspaceID, conn.user.ID); err != nil {
		return
	}
	h.hub.BroadcastWorkspace(event.WorkspaceID, Event{
		ID:          randomID(),
		Type:        event.Type,
		WorkspaceID: event.WorkspaceID,
		ChannelID:   event.ChannelID,
		Payload: map[string]any{
			"user_id":      conn.user.ID,
			"display_name": conn.user.Name,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (h *Handler) broadcastPresence(workspaceID string, user auth.User, status string) {
	h.hub.BroadcastWorkspace(workspaceID, Event{
		ID:          randomID(),
		Type:        "presence.updated",
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"user_id":      user.ID,
			"display_name": user.Name,
			"status":       status,
		},
		CreatedAt: time.Now().UTC(),
	})
}

func (h *Handler) requireWorkspaceMembership(ctx context.Context, workspaceID, userID string) error {
	var membershipID string
	return h.db.QueryRow(ctx, `
		SELECT id::text
		FROM memberships
		WHERE workspace_id = $1 AND user_id = $2 AND disabled_at IS NULL
	`, workspaceID, userID).Scan(&membershipID)
}

func (c *Connection) enqueue(event Event) {
	select {
	case c.send <- event:
	default:
	}
}

func isExpectedClose(err error) bool {
	return websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, pgx.ErrNoRows)
}

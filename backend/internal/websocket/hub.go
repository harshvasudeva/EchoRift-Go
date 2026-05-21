package websocket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Event struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	ChannelID   string    `json:"channel_id,omitempty"`
	RoomID      string    `json:"room_id,omitempty"`
	Payload     any       `json:"payload,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Hub struct {
	mu        sync.RWMutex
	workspace map[string]map[*Connection]struct{}
}

func NewHub() *Hub {
	return &Hub{workspace: make(map[string]map[*Connection]struct{})}
}

func (h *Hub) SubscribeWorkspace(conn *Connection, workspaceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.workspace[workspaceID] == nil {
		h.workspace[workspaceID] = make(map[*Connection]struct{})
	}
	h.workspace[workspaceID][conn] = struct{}{}
	conn.subscriptions[workspaceID] = struct{}{}
}

func (h *Hub) UnsubscribeWorkspace(conn *Connection, workspaceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(conn.subscriptions, workspaceID)
	if h.workspace[workspaceID] == nil {
		return
	}
	delete(h.workspace[workspaceID], conn)
	if len(h.workspace[workspaceID]) == 0 {
		delete(h.workspace, workspaceID)
	}
}

func (h *Hub) Remove(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for workspaceID := range conn.subscriptions {
		if h.workspace[workspaceID] != nil {
			delete(h.workspace[workspaceID], conn)
			if len(h.workspace[workspaceID]) == 0 {
				delete(h.workspace, workspaceID)
			}
		}
		delete(conn.subscriptions, workspaceID)
	}
}

func (h *Hub) BroadcastWorkspace(workspaceID string, event any) {
	normalized := normalizeEvent(workspaceID, event)

	h.mu.RLock()
	connections := make([]*Connection, 0, len(h.workspace[workspaceID]))
	for conn := range h.workspace[workspaceID] {
		connections = append(connections, conn)
	}
	h.mu.RUnlock()

	for _, conn := range connections {
		select {
		case conn.send <- normalized:
		default:
			// Slow consumers can recover durable state over REST.
		}
	}
}

func (h *Hub) CountWorkspace(workspaceID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.workspace[workspaceID])
}

func (h *Hub) CountUserWorkspace(workspaceID, userID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for conn := range h.workspace[workspaceID] {
		if conn.user.ID == userID {
			count++
		}
	}
	return count
}

func (h *Hub) WorkspacePresence(workspaceID string) []map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	seen := make(map[string]map[string]any)
	for conn := range h.workspace[workspaceID] {
		seen[conn.user.ID] = map[string]any{
			"user_id":      conn.user.ID,
			"display_name": conn.user.Name,
			"status":       "online",
		}
	}
	presence := make([]map[string]any, 0, len(seen))
	for _, value := range seen {
		presence = append(presence, value)
	}
	return presence
}

func normalizeEvent(workspaceID string, event any) Event {
	if existing, ok := event.(Event); ok {
		if existing.ID == "" {
			existing.ID = randomID()
		}
		if existing.CreatedAt.IsZero() {
			existing.CreatedAt = time.Now().UTC()
		}
		if existing.WorkspaceID == "" {
			existing.WorkspaceID = workspaceID
		}
		return existing
	}
	return Event{ID: randomID(), Type: "event", WorkspaceID: workspaceID, Payload: event, CreatedAt: time.Now().UTC()}
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

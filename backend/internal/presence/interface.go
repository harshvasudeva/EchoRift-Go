package presence

import (
	"context"
	"time"
)

type Status string

const (
	StatusOnline  Status = "online"
	StatusAway    Status = "away"
	StatusBusy    Status = "busy"
	StatusOffline Status = "offline"
)

type State struct {
	WorkspaceID      string
	UserID           string
	Status           Status
	ConnectionCount  int
	ActiveSessionIDs []string
	LastSeenAt       time.Time
	LastHeartbeatAt  time.Time
	ActiveRoomID     string
}

type Presence interface {
	SetStatus(ctx context.Context, workspaceID, userID string, status Status) error
	GetStatus(ctx context.Context, workspaceID, userID string) (State, error)
	GetWorkspacePresence(ctx context.Context, workspaceID string) ([]State, error)
	AddConnection(ctx context.Context, workspaceID, userID, sessionID, connID string) error
	RemoveConnection(ctx context.Context, workspaceID, userID, sessionID, connID string) error
	Heartbeat(ctx context.Context, workspaceID, userID, connID string) error
	SetTyping(ctx context.Context, workspaceID, channelID, userID string, ttl time.Duration) error
	ClearTyping(ctx context.Context, workspaceID, channelID, userID string) error
}

package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"echorift/backend/internal/auth"
	"echorift/backend/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db   *pgxpool.Pool
	auth *auth.Service
	cfg  config.Config
}

type JoinVoiceResponse struct {
	LiveKitURL string `json:"livekit_url"`
	Token      string `json:"token"`
	RoomName   string `json:"room_name"`
	ChannelID  string `json:"channel_id"`
}

type liveKitClaims struct {
	Issuer    string       `json:"iss"`
	Subject   string       `json:"sub"`
	Name      string       `json:"name,omitempty"`
	IssuedAt  int64        `json:"iat"`
	NotBefore int64        `json:"nbf"`
	ExpiresAt int64        `json:"exp"`
	Video     liveKitVideo `json:"video"`
	Metadata  string       `json:"metadata,omitempty"`
}

type liveKitVideo struct {
	RoomJoin       bool   `json:"roomJoin"`
	Room           string `json:"room"`
	CanPublish     bool   `json:"canPublish"`
	CanSubscribe   bool   `json:"canSubscribe"`
	CanPublishData bool   `json:"canPublishData"`
}

func NewHandler(db *pgxpool.Pool, authService *auth.Service, cfg config.Config) *Handler {
	return &Handler{db: db, auth: authService, cfg: cfg}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/workspaces/{workspaceID}/voice/{channelID}/join", h.joinVoice)
}

func (h *Handler) joinVoice(w http.ResponseWriter, r *http.Request) {
	if h.cfg.LiveKitURL == "" || h.cfg.LiveKitKey == "" || h.cfg.LiveKitSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "livekit_not_configured")
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	channelID := r.PathValue("channelID")

	channelName, err := h.requireVoiceChannelAccess(r, workspaceID, channelID, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusForbidden, "voice_access_denied")
			return
		}
		writeError(w, http.StatusInternalServerError, "voice_access_check_failed")
		return
	}

	roomName := fmt.Sprintf("workspace_%s_channel_%s", workspaceID, channelID)
	if err := h.ensureRoom(r, workspaceID, channelID, roomName, channelName, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "room_create_failed")
		return
	}

	token, err := h.issueToken(user, workspaceID, channelID, roomName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "livekit_token_failed")
		return
	}

	writeJSON(w, http.StatusOK, JoinVoiceResponse{
		LiveKitURL: normalizeLiveKitURL(h.cfg.LiveKitURL),
		Token:      token,
		RoomName:   roomName,
		ChannelID:  channelID,
	})
}

func (h *Handler) requireVoiceChannelAccess(r *http.Request, workspaceID, channelID, userID string) (string, error) {
	var channelName string
	return channelName, h.db.QueryRow(r.Context(), `
		SELECT c.name
		FROM channels c
		JOIN memberships m ON m.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1
		  AND c.id = $2
		  AND c.type = 'voice'
		  AND c.archived_at IS NULL
		  AND m.user_id = $3
		  AND m.disabled_at IS NULL
	`, workspaceID, channelID, userID).Scan(&channelName)
}

func (h *Handler) ensureRoom(r *http.Request, workspaceID, channelID, livekitRoomName, channelName, userID string) error {
	_, err := h.db.Exec(r.Context(), `
		INSERT INTO rooms (workspace_id, channel_id, livekit_room_name, name, type, created_by)
		VALUES ($1, $2, $3, $4, 'voice', $5)
		ON CONFLICT (livekit_room_name) DO UPDATE
		SET ended_at = NULL
	`, workspaceID, channelID, livekitRoomName, channelName, userID)
	return err
}

func (h *Handler) issueToken(user auth.User, workspaceID, channelID, roomName string) (string, error) {
	now := time.Now().UTC()
	metadata, _ := json.Marshal(map[string]string{"workspace_id": workspaceID, "channel_id": channelID})
	claims := liveKitClaims{
		Issuer:    h.cfg.LiveKitKey,
		Subject:   user.ID,
		Name:      user.Name,
		IssuedAt:  now.Unix(),
		NotBefore: now.Add(-5 * time.Second).Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
		Metadata:  string(metadata),
		Video: liveKitVideo{
			RoomJoin:       true,
			Room:           roomName,
			CanPublish:     true,
			CanSubscribe:   true,
			CanPublishData: true,
		},
	}

	headerJSON, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, []byte(h.cfg.LiveKitSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
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

func normalizeLiveKitURL(raw string) string {
	return strings.TrimRight(raw, "/")
}

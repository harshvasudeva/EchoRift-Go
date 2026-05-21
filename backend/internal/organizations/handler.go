package organizations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"echorift/backend/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db   *pgxpool.Pool
	auth *auth.Service
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	OwnerID   string    `json:"owner_user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

type CreateWorkspaceResponse struct {
	Workspace Workspace `json:"workspace"`
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func NewHandler(db *pgxpool.Pool, authService *auth.Service) *Handler {
	return &Handler{db: db, auth: authService}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspaces", h.list)
	mux.HandleFunc("POST /api/v1/workspaces", h.create)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT w.id::text, w.name, w.slug, w.owner_user_id::text, w.created_at
		FROM workspaces w
		JOIN memberships m ON m.workspace_id = w.id
		WHERE m.user_id = $1 AND m.disabled_at IS NULL AND w.archived_at IS NULL
		ORDER BY w.created_at ASC
	`, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_list_failed")
		return
	}
	defer rows.Close()

	workspaces := make([]Workspace, 0)
	for rows.Next() {
		var workspace Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.OwnerID, &workspace.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "workspace_scan_failed")
			return
		}
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_list_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string][]Workspace{"workspaces": workspaces})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	var req CreateWorkspaceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) < 2 || len(name) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_workspace_name")
		return
	}

	slug, err := uniqueSlug(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "slug_generation_failed")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_create_failed")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var workspace Workspace
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO workspaces (name, slug, owner_user_id)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, slug, owner_user_id::text, created_at
	`, name, slug, user.ID).Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.OwnerID, &workspace.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_create_failed")
		return
	}

	var membershipID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO memberships (workspace_id, user_id)
		VALUES ($1, $2)
		RETURNING id::text
	`, workspace.ID, user.ID).Scan(&membershipID); err != nil {
		writeError(w, http.StatusInternalServerError, "membership_create_failed")
		return
	}

	var adminRoleID, memberRoleID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO roles (workspace_id, name, position, is_admin)
		VALUES ($1, 'Admin', 100, true)
		RETURNING id::text
	`, workspace.ID).Scan(&adminRoleID); err != nil {
		writeError(w, http.StatusInternalServerError, "role_create_failed")
		return
	}
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO roles (workspace_id, name, position, is_default)
		VALUES ($1, 'Member', 0, true)
		RETURNING id::text
	`, workspace.ID).Scan(&memberRoleID); err != nil {
		writeError(w, http.StatusInternalServerError, "role_create_failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO membership_roles (membership_id, role_id)
		VALUES ($1, $2), ($1, $3)
	`, membershipID, adminRoleID, memberRoleID); err != nil {
		writeError(w, http.StatusInternalServerError, "membership_role_create_failed")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO role_permissions (role_id, permission_id, effect)
		SELECT $1, id, 'allow' FROM permissions
	`, adminRoleID); err != nil {
		writeError(w, http.StatusInternalServerError, "admin_permissions_create_failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO role_permissions (role_id, permission_id, effect)
		SELECT $1, id, 'allow'
		FROM permissions
		WHERE code = ANY($2::text[])
	`, memberRoleID, []string{
		"workspace.view", "channels.view", "messages.read", "messages.send",
		"messages.edit_own", "messages.delete_own", "reactions.add", "files.upload",
		"rooms.join", "rooms.speak", "rooms.screen_share",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "member_permissions_create_failed")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO channels (workspace_id, name, description, type, created_by, position)
		VALUES
		    ($1, 'general', 'Default team chat', 'text', $2, 0),
		    ($1, 'team-room', 'Default voice room', 'voice', $2, 1)
	`, workspace.ID, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "default_channels_create_failed")
		return
	}

	metadataJSON, err := json.Marshal(map[string]any{"name": workspace.Name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit_create_failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO audit_logs (workspace_id, actor_user_id, action, entity_type, entity_id, metadata)
		VALUES ($1, $2, 'workspace.create', 'workspace', $1, $3::jsonb)
	`, workspace.ID, user.ID, string(metadataJSON)); err != nil {
		writeError(w, http.StatusInternalServerError, "audit_create_failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_create_failed")
		return
	}

	writeJSON(w, http.StatusCreated, CreateWorkspaceResponse{Workspace: workspace})
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

func uniqueSlug(name string) (string, error) {
	base := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if base == "" {
		base = "workspace"
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", base, suffix), nil
}

func randomSuffix() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
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

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"echorift/backend/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

type Service struct {
	db  *pgxpool.Pool
	jwt *JWTManager
}

func NewService(db *pgxpool.Pool, cfg config.Config) *Service {
	return &Service{
		db:  db,
		jwt: NewJWTManager(cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTSecret, accessTokenTTL),
	}
}

func (s *Service) Signup(ctx context.Context, req SignupRequest, meta SessionMeta) (TokenPair, error) {
	email := normalizeEmail(req.Email)
	name := strings.TrimSpace(req.DisplayName)
	if email == "" || !strings.Contains(email, "@") || len(req.Password) < 8 {
		return TokenPair{}, ErrInvalidInput
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	passwordHash, err := HashPassword(req.Password, DefaultPasswordParams)
	if err != nil {
		return TokenPair{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	defer rollback(ctx, tx)

	var user User
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, $2)
		RETURNING id::text, email::text, display_name, created_at
	`, email, name).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return TokenPair{}, ErrEmailAlreadyExists
		}
		return TokenPair{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO identities (user_id, provider, provider_subject, provider_email, password_hash)
		VALUES ($1, 'local', $2::text, $2::citext, $3)
	`, user.ID, email, passwordHash); err != nil {
		if isUniqueViolation(err) {
			return TokenPair{}, ErrEmailAlreadyExists
		}
		return TokenPair{}, err
	}

	sessionID, refreshToken, err := s.createSession(ctx, tx, user.ID, meta)
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.audit(ctx, tx, user.ID, "auth.signup", meta, map[string]any{"provider": "local"}); err != nil {
		return TokenPair{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, err
	}

	accessToken, accessTTL, err := s.jwt.Issue(user.ID, sessionID)
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, AccessTTL: accessTTL, User: user}, nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest, meta SessionMeta) (TokenPair, error) {
	email := normalizeEmail(req.Email)
	if email == "" || req.Password == "" {
		return TokenPair{}, ErrInvalidCredentials
	}

	var user User
	var passwordHash string
	if err := s.db.QueryRow(ctx, `
		SELECT u.id::text, u.email::text, u.display_name, u.created_at, i.password_hash
		FROM identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider = 'local'
		  AND i.provider_subject = $1
		  AND u.disabled_at IS NULL
	`, email).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt, &passwordHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenPair{}, ErrInvalidCredentials
		}
		return TokenPair{}, err
	}

	ok, err := VerifyPassword(req.Password, passwordHash)
	if err != nil || !ok {
		return TokenPair{}, ErrInvalidCredentials
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	defer rollback(ctx, tx)

	sessionID, refreshToken, err := s.createSession(ctx, tx, user.ID, meta)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.audit(ctx, tx, user.ID, "auth.login", meta, map[string]any{"provider": "local"}); err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, err
	}

	accessToken, accessTTL, err := s.jwt.Issue(user.ID, sessionID)
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, AccessTTL: accessTTL, User: user}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string, meta SessionMeta) (TokenPair, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return TokenPair{}, ErrInvalidToken
	}
	oldHash := hashToken(refreshToken)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenPair{}, err
	}
	defer rollback(ctx, tx)

	var sessionID, userID, familyID string
	var sessionExpiresAt time.Time
	var user User
	err = tx.QueryRow(ctx, `
		SELECT s.id::text, s.user_id::text, s.refresh_token_family_id::text, s.expires_at,
		       u.id::text, u.email::text, u.display_name, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.refresh_token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
		  AND u.disabled_at IS NULL
		FOR UPDATE OF s
	`, oldHash).Scan(&sessionID, &userID, &familyID, &sessionExpiresAt, &user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return s.handleRefreshReuse(ctx, oldHash, meta)
		}
		return TokenPair{}, err
	}

	newRefreshToken, err := newOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	newHash := hashToken(newRefreshToken)

	if _, err := tx.Exec(ctx, `
		INSERT INTO consumed_refresh_tokens (token_hash, session_id, refresh_token_family_id, expires_at)
		VALUES ($1, $2, $3, $4)
	`, oldHash, sessionID, familyID, sessionExpiresAt); err != nil {
		if isUniqueViolation(err) {
			_ = s.revokeFamily(ctx, familyID, "refresh_token_reuse")
			return TokenPair{}, ErrSessionRevoked
		}
		return TokenPair{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE sessions
		SET refresh_token_hash = $1,
		    last_seen_at = now(),
		    user_agent = COALESCE(NULLIF($2, ''), user_agent),
		    ip_address = COALESCE(NULLIF($3, '')::inet, ip_address)
		WHERE id = $4
	`, newHash, meta.UserAgent, meta.IPAddress, sessionID); err != nil {
		return TokenPair{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TokenPair{}, err
	}

	accessToken, accessTTL, err := s.jwt.Issue(userID, sessionID)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: accessToken, RefreshToken: newRefreshToken, AccessTTL: accessTTL, User: user}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string, meta SessionMeta) error {
	_ = meta
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now(), revoke_reason = 'logout'
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL
	`, hashToken(refreshToken))
	return err
}

func (s *Service) CurrentUser(ctx context.Context, accessToken string) (User, error) {
	claims, err := s.jwt.Validate(accessToken)
	if err != nil {
		return User{}, err
	}

	var user User
	if err := s.db.QueryRow(ctx, `
		SELECT u.id::text, u.email::text, u.display_name, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = $1
		  AND u.id = $2
		  AND s.revoked_at IS NULL
		  AND u.disabled_at IS NULL
	`, claims.SessionID, claims.Subject).Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvalidToken
		}
		return User{}, err
	}
	return user, nil
}

func (s *Service) TokenFromAuthorization(header string) (string, error) {
	return bearerToken(header)
}

func (s *Service) createSession(ctx context.Context, tx pgx.Tx, userID string, meta SessionMeta) (sessionID string, refreshToken string, err error) {
	refreshToken, err = newOpaqueToken()
	if err != nil {
		return "", "", err
	}
	refreshHash := hashToken(refreshToken)
	expiresAt := time.Now().UTC().Add(refreshTokenTTL)

	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions (
		    user_id, refresh_token_hash, refresh_token_family_id,
		    user_agent, ip_address, device_name, platform, expires_at
		)
		VALUES ($1, $2, gen_random_uuid(), $3, NULLIF($4, '')::inet, $5, $6, $7)
		RETURNING id::text
	`, userID, refreshHash, meta.UserAgent, meta.IPAddress, meta.DeviceName, meta.Platform, expiresAt).Scan(&sessionID); err != nil {
		return "", "", err
	}
	return sessionID, refreshToken, nil
}

func (s *Service) handleRefreshReuse(ctx context.Context, tokenHash string, meta SessionMeta) (TokenPair, error) {
	_ = meta
	var familyID string
	var consumedAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT refresh_token_family_id::text, consumed_at
		FROM consumed_refresh_tokens
		WHERE token_hash = $1 AND expires_at > now()
	`, tokenHash).Scan(&familyID, &consumedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenPair{}, ErrInvalidToken
		}
		return TokenPair{}, err
	}

	// Browser dev tooling, React StrictMode, or multiple tabs can occasionally
	// issue the same refresh request concurrently. Treat an immediate duplicate
	// as stale rather than malicious so the successful request can keep the new
	// refresh cookie. Older reuse still revokes the family.
	if time.Since(consumedAt) < 10*time.Second {
		return TokenPair{}, ErrInvalidToken
	}

	if err := s.revokeFamily(ctx, familyID, "refresh_token_reuse"); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{}, ErrSessionRevoked
}

func (s *Service) revokeFamily(ctx context.Context, familyID, reason string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = COALESCE(revoked_at, now()), revoke_reason = $2
		WHERE refresh_token_family_id = $1
	`, familyID, reason)
	return err
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, actorUserID string, action string, meta SessionMeta, metadata map[string]any) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (actor_user_id, action, ip_address, user_agent, metadata)
		VALUES ($1, $2, NULLIF($3, '')::inet, $4, $5::jsonb)
	`, actorUserID, action, meta.IPAddress, meta.UserAgent, string(metadataJSON))
	return err
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

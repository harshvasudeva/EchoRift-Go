package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type JWTManager struct {
	issuer   string
	audience string
	secret   []byte
	ttl      time.Duration
}

type AccessClaims struct {
	Subject   string `json:"sub"`
	SessionID string `json:"sid"`
	JTI       string `json:"jti"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func NewJWTManager(issuer, audience, secret string, ttl time.Duration) *JWTManager {
	return &JWTManager{issuer: issuer, audience: audience, secret: []byte(secret), ttl: ttl}
}

func (m *JWTManager) Issue(userID, sessionID string) (string, time.Duration, error) {
	jti, err := newTokenID()
	if err != nil {
		return "", 0, err
	}
	now := time.Now().UTC()
	claims := AccessClaims{
		Subject:   userID,
		SessionID: sessionID,
		JTI:       jti,
		Issuer:    m.issuer,
		Audience:  m.audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.ttl).Unix(),
	}

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", 0, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", 0, err
	}

	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := m.sign(unsigned)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), m.ttl, nil
}

func (m *JWTManager) Validate(token string) (AccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, ErrInvalidToken
	}

	unsigned := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	if !hmac.Equal(signature, m.sign(unsigned)) {
		return AccessClaims{}, ErrInvalidToken
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		return AccessClaims{}, ErrInvalidToken
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	var claims AccessClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return AccessClaims{}, ErrInvalidToken
	}
	if claims.Issuer != m.issuer || claims.Audience != m.audience {
		return AccessClaims{}, ErrInvalidToken
	}
	if claims.Subject == "" || claims.SessionID == "" || claims.JTI == "" {
		return AccessClaims{}, ErrInvalidToken
	}
	if time.Now().UTC().Unix() >= claims.ExpiresAt {
		return AccessClaims{}, fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	if claims.IssuedAt > time.Now().UTC().Add(30*time.Second).Unix() {
		return AccessClaims{}, fmt.Errorf("%w: issued_in_future", ErrInvalidToken)
	}

	return claims, nil
}

func (m *JWTManager) sign(unsigned string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	return mac.Sum(nil)
}

func bearerToken(authHeader string) (string, error) {
	if authHeader == "" {
		return "", errors.New("missing authorization header")
	}
	scheme, token, ok := strings.Cut(authHeader, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", errors.New("invalid authorization header")
	}
	return strings.TrimSpace(token), nil
}

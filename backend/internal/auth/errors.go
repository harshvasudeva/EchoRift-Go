package auth

import "errors"

var (
	ErrInvalidInput       = errors.New("invalid_input")
	ErrEmailAlreadyExists = errors.New("email_already_exists")
	ErrInvalidCredentials = errors.New("invalid_credentials")
	ErrInvalidToken       = errors.New("invalid_token")
	ErrSessionRevoked     = errors.New("session_revoked")
)

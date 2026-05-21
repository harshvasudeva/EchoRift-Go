CREATE TABLE consumed_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    refresh_token_family_id UUID NOT NULL,
    consumed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_consumed_refresh_tokens_family ON consumed_refresh_tokens(refresh_token_family_id);
CREATE INDEX idx_consumed_refresh_tokens_expires ON consumed_refresh_tokens(expires_at);

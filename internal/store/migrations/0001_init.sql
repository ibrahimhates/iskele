-- Initial schema: identity, sessions, API tokens, audit and settings.
--
-- Timestamps are RFC3339Nano UTC strings so lexical ordering matches
-- chronological ordering and indexes on them work as expected.

CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    username        TEXT NOT NULL,
    -- Lowercased username, so logins are case-insensitive while the display
    -- form the operator chose is preserved.
    username_lower  TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    -- Encrypted with the key in /etc/iskele/secret.key; never stored plainly.
    totp_secret_enc TEXT NOT NULL DEFAULT '',
    totp_enabled    INTEGER NOT NULL DEFAULT 0 CHECK (totp_enabled IN (0, 1)),
    disabled        INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    last_login_at   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 of the refresh token. The token itself is shown once and never
    -- stored, so a database leak does not hand over live sessions.
    refresh_hash  TEXT NOT NULL UNIQUE,
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    revoked_at    TEXT NOT NULL DEFAULT '',
    last_used_at  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE api_tokens (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    -- Public prefix, shown in the UI so a token can be identified without
    -- revealing it.
    prefix        TEXT NOT NULL UNIQUE,
    token_hash    TEXT NOT NULL UNIQUE,
    scopes        TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL DEFAULT '',
    last_used_at  TEXT NOT NULL DEFAULT '',
    revoked_at    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE login_attempts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ip         TEXT NOT NULL,
    username   TEXT NOT NULL DEFAULT '',
    success    INTEGER NOT NULL CHECK (success IN (0, 1)),
    created_at TEXT NOT NULL
);

-- Brute-force checks count failures for one IP inside a time window.
CREATE INDEX idx_login_attempts_ip_time ON login_attempts(ip, created_at);
CREATE INDEX idx_login_attempts_time ON login_attempts(created_at);

CREATE TABLE audit_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       TEXT NOT NULL DEFAULT '',
    username      TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL CHECK (result IN ('ok', 'error')),
    -- JSON, with secret-looking values masked before it is written.
    detail        TEXT NOT NULL DEFAULT '{}',
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL
);

CREATE INDEX idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_user ON audit_logs(user_id);
CREATE INDEX idx_audit_action ON audit_logs(action);
CREATE INDEX idx_audit_resource ON audit_logs(resource_type, resource_id);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

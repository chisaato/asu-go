-- +goose Up
CREATE TABLE admin_users (
    id TEXT PRIMARY KEY NOT NULL,
    username TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    password_hash TEXT,
    role TEXT NOT NULL DEFAULT 'admin',
    disabled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    password_changed_at TIMESTAMP NOT NULL
);

CREATE TABLE admin_user_identities (
    id TEXT PRIMARY KEY NOT NULL,
    user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (issuer, subject)
);
CREATE INDEX admin_user_identities_user_id ON admin_user_identities(user_id);

CREATE TABLE admin_auth_sessions (
    id TEXT PRIMARY KEY NOT NULL,
    user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP,
    revoke_reason TEXT
);
CREATE INDEX admin_auth_sessions_user_id ON admin_auth_sessions(user_id);
CREATE INDEX admin_auth_sessions_family_id ON admin_auth_sessions(family_id);

CREATE TABLE admin_refresh_tokens (
    id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL REFERENCES admin_auth_sessions(id) ON DELETE CASCADE,
    token_hash TEXT UNIQUE NOT NULL,
    issued_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    absolute_expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    replaced_by TEXT
);
CREATE INDEX admin_refresh_tokens_token_hash ON admin_refresh_tokens(token_hash);
CREATE INDEX admin_refresh_tokens_session_id ON admin_refresh_tokens(session_id);

-- +goose Down
DROP TABLE admin_refresh_tokens;
DROP TABLE admin_auth_sessions;
DROP TABLE admin_user_identities;
DROP TABLE admin_users;

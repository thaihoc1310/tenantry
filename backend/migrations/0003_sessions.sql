-- +goose Up

-- One portal login (architecture/06-security-and-identity.md §3.2). The row
-- lives as long as its Keycloak refresh token. The browser holds a random
-- cookie and the table only its sha256, so reading the table does not give
-- away a usable cookie. The refresh token is encrypted with the session key
-- and bound to this row; access and ID tokens are never stored.
CREATE TABLE sessions (
    id_hash           bytea PRIMARY KEY CHECK (length(id_hash) = 32),
    user_id           text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    realm_roles       text[] NOT NULL DEFAULT '{}', -- Keycloak realm roles at the last token refresh
    refresh_token_enc bytea NOT NULL,
    access_expires_at timestamptz NOT NULL,         -- refresh the roles after this
    expires_at        timestamptz NOT NULL,         -- the refresh token's expiry; the session ends here
    created_at        timestamptz NOT NULL DEFAULT now()
);
-- Ending every session of a user (disable, logout everywhere).
CREATE INDEX sessions_user_idx ON sessions (user_id);
-- Housekeeping deletes expired rows.
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;

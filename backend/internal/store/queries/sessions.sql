-- name: CreateSession :exec
INSERT INTO sessions (id_hash, user_id, realm_roles, refresh_token_enc, access_expires_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSession :one
-- An expired row is treated as absent; housekeeping deletes it later.
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions JOIN users ON users.id = sessions.user_id
WHERE sessions.id_hash = $1 AND sessions.expires_at > now();

-- name: LockSession :one
-- Held while the session's access token is refreshed, so concurrent requests
-- of one session wait and then see the renewed row instead of refreshing again.
SELECT * FROM sessions WHERE id_hash = $1 AND expires_at > now() FOR UPDATE;

-- name: RenewSession :exec
UPDATE sessions
SET realm_roles = $2, refresh_token_enc = $3, access_expires_at = $4, expires_at = $5
WHERE id_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = $1;

-- name: EndSession :one
-- Logout: the row goes, and its refresh token comes back to end the SSO session.
DELETE FROM sessions WHERE id_hash = $1 RETURNING refresh_token_enc;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < now();

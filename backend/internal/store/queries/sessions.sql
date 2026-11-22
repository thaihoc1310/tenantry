-- name: CreateSession :exec
INSERT INTO sessions (id_hash, user_id, realm_roles, refresh_token_enc, access_expires_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSession :one
-- An expired row is treated as absent; housekeeping deletes it later.
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions JOIN users ON users.id = sessions.user_id
WHERE sessions.id_hash = $1 AND sessions.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = $1;

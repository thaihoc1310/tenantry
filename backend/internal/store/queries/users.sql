-- name: GetActiveUser :one
-- A Disabled account is treated as unknown, so it loses access on its next request.
SELECT * FROM users WHERE id = $1 AND status = 'Active';

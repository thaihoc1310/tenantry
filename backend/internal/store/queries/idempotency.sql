-- name: ClaimIdempotencyKey :execrows
-- 0 rows: the key is taken. A concurrent request holding it uncommitted makes
-- this wait until that request commits (0 rows) or rolls back (1 row).
INSERT INTO idempotency_keys (user_id, key, method, path, request_hash)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, key) DO NOTHING;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE user_id = $1 AND key = $2;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET response_status = $3, response_location = $4, response_body = $5
WHERE user_id = $1 AND key = $2;

-- name: DeleteOldIdempotencyKeys :execrows
DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours';

-- name: EnqueueEvent :one
INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version)
VALUES (@operation_id, @ordering_key, @target_cluster_id, @type, @payload, @version)
RETURNING *;

-- name: ClaimDueEvents :many
-- Locks a batch of due events for one cluster's worker; SKIP LOCKED lets
-- several workers run side by side without taking the same event. An event
-- waits while an older undelivered event shares its ordering key, even one
-- that is backing off or locked by another worker. Events of an Unavailable
-- cluster wait without spending attempts, so their operations stay Pending
-- (04-state-machines §6).
SELECT e.* FROM outbox_events e
JOIN clusters c ON c.id = e.target_cluster_id
WHERE e.processed_at IS NULL
  AND e.next_attempt_at <= now()
  AND e.target_cluster_id = @cluster_id
  AND c.status <> 'Unavailable'
  AND NOT EXISTS (
    SELECT 1 FROM outbox_events older
    WHERE older.ordering_key = e.ordering_key AND older.processed_at IS NULL AND older.id < e.id
  )
ORDER BY e.id
LIMIT @batch_size
FOR UPDATE OF e SKIP LOCKED;

-- name: MarkEventDelivered :exec
UPDATE outbox_events SET processed_at = now(), last_error = '' WHERE id = $1;

-- name: MarkEventFailed :one
UPDATE outbox_events
SET attempts        = attempts + 1,
    next_attempt_at = now() + make_interval(secs => sqlc.arg(retry_in_seconds)::float8),
    last_error      = sqlc.arg(last_error)::text
WHERE id = @id
RETURNING attempts;

-- name: ResetOperationEvents :execrows
-- A user retry: deliver the operation's undelivered events again right away.
UPDATE outbox_events
SET attempts = 0, next_attempt_at = now(), last_error = ''
WHERE operation_id = $1 AND processed_at IS NULL;

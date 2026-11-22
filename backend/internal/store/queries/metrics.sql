-- Numbers read when Prometheus scrapes (internal/metrics).

-- name: OutboxBacklog :one
-- Undelivered events of a cluster and the age of the oldest: the queue lag.
SELECT count(*)::bigint AS pending,
       coalesce(extract(epoch FROM now() - min(created_at)), 0)::float8 AS oldest_seconds
FROM outbox_events WHERE target_cluster_id = $1 AND processed_at IS NULL;

-- name: GroupSyncBacklog :many
-- Rows Keycloak has not caught up with, by kind, and how long the oldest has waited.
SELECT 'group'::text AS kind, count(*)::bigint AS pending,
       coalesce(extract(epoch FROM now() - min(updated_at)), 0)::float8 AS oldest_seconds
FROM groups WHERE sync_status <> 'Synced'
UNION ALL
SELECT 'member', count(*), coalesce(extract(epoch FROM now() - min(updated_at)), 0)::float8
FROM group_members WHERE sync_status <> 'Synced'
UNION ALL
SELECT 'user', count(*), coalesce(extract(epoch FROM now() - min(updated_at)), 0)::float8
FROM users WHERE sync_status <> 'Synced';

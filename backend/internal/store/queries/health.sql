-- The Platform health page (PRODUCT-PLAN §19, /admin/health): what the
-- database says of the workers and the work in hand.

-- name: HealthCounts :one
SELECT
  (SELECT count(*) FROM outbox_events e JOIN clusters c ON c.id = e.target_cluster_id
    WHERE e.processed_at IS NULL AND c.status <> 'Unavailable')::int AS events_waiting,
  coalesce((SELECT extract(epoch FROM now() - min(e.created_at)) FROM outbox_events e JOIN clusters c ON c.id = e.target_cluster_id
    WHERE e.processed_at IS NULL AND c.status <> 'Unavailable'), 0)::float8 AS oldest_event_seconds,
  ((SELECT count(*) FROM groups WHERE sync_status <> 'Synced')
    + (SELECT count(*) FROM group_members WHERE sync_status <> 'Synced')
    + (SELECT count(*) FROM users WHERE sync_status <> 'Synced'))::int AS sync_waiting,
  coalesce((SELECT extract(epoch FROM now() - min(t)) FROM (
    SELECT min(updated_at) AS t FROM groups WHERE sync_status <> 'Synced'
    UNION ALL SELECT min(updated_at) FROM group_members WHERE sync_status <> 'Synced'
    UNION ALL SELECT min(updated_at) FROM users WHERE sync_status <> 'Synced') w), 0)::float8 AS oldest_sync_seconds,
  coalesce((SELECT last_error FROM (
    SELECT last_error, updated_at FROM groups WHERE sync_status = 'Error'
    UNION ALL SELECT last_error, updated_at FROM group_members WHERE sync_status = 'Error'
    UNION ALL SELECT last_error, updated_at FROM users WHERE sync_status = 'Error') e
    ORDER BY updated_at DESC LIMIT 1), '')::text AS last_sync_error,
  (SELECT count(*) FROM operations WHERE state IN ('Pending', 'Running'))::int AS operations_running,
  (SELECT count(*) FROM operations WHERE state IN ('Failed', 'Blocked'))::int AS operations_attention,
  (SELECT count(*) FROM organizations WHERE status = 'Pending')::int AS requests_waiting;

-- The group sync worker's queue (04-state-machines §9.1). Rows are claimed
-- FOR UPDATE SKIP LOCKED and held until the batch commits, so the API waits
-- for the worker to record what it did before changing a row again, and
-- several workers never take the same row.

-- name: ClaimDueGroups :many
SELECT * FROM groups
WHERE sync_status <> 'Synced' AND next_sync_at <= now()
ORDER BY next_sync_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkGroupSynced :exec
UPDATE groups SET sync_status = 'Synced', keycloak_group_id = $2, sync_attempts = 0, last_error = '', updated_at = now()
WHERE id = $1;

-- name: MarkGroupFailed :exec
UPDATE groups
SET sync_status = 'Error', sync_attempts = sync_attempts + 1, last_error = @last_error,
    next_sync_at = now() + make_interval(secs => @retry_in_seconds::float8), updated_at = now()
WHERE id = @id;

-- name: DeleteGroupRow :exec
-- A tombstone Keycloak has caught up with; its memberships go with it.
DELETE FROM groups WHERE id = $1;

-- name: ClaimDueMembers :many
-- Memberships of groups Keycloak knows (keycloak_group_id set): a member of a
-- group not created yet waits for it. Memberships of a deleted group go with
-- the group.
SELECT gm.group_id, gm.user_id, gm.deleted_at, gm.sync_attempts, g.keycloak_group_id, u.subject
FROM group_members gm
JOIN groups g ON g.id = gm.group_id
JOIN users u ON u.id = gm.user_id
WHERE gm.sync_status <> 'Synced' AND gm.next_sync_at <= now()
  AND g.deleted_at IS NULL AND g.keycloak_group_id IS NOT NULL
ORDER BY gm.next_sync_at
LIMIT $1
FOR UPDATE OF gm SKIP LOCKED;

-- name: MarkMemberSynced :exec
UPDATE group_members SET sync_status = 'Synced', sync_attempts = 0, last_error = '', updated_at = now()
WHERE group_id = $1 AND user_id = $2;

-- name: MarkMemberFailed :exec
UPDATE group_members
SET sync_status = 'Error', sync_attempts = sync_attempts + 1, last_error = @last_error,
    next_sync_at = now() + make_interval(secs => @retry_in_seconds::float8), updated_at = now()
WHERE group_id = @group_id AND user_id = @user_id;

-- name: DeleteMemberRow :exec
-- A membership tombstone Keycloak has caught up with.
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ClaimDueUsers :many
-- Accounts an Admin disabled or enabled, for Keycloak's enabled flag.
SELECT * FROM users
WHERE sync_status <> 'Synced' AND next_sync_at <= now()
ORDER BY next_sync_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkUserSynced :exec
UPDATE users SET sync_status = 'Synced', sync_attempts = 0, last_error = '', updated_at = now() WHERE id = $1;

-- name: MarkUserFailed :exec
UPDATE users
SET sync_status = 'Error', sync_attempts = sync_attempts + 1, last_error = @last_error,
    next_sync_at = now() + make_interval(secs => @retry_in_seconds::float8), updated_at = now()
WHERE id = @id;

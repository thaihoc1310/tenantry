-- name: CreateGroup :one
-- Starts Pending: the group sync worker creates it in Keycloak.
INSERT INTO groups (id, org_id, display_name, machine_name) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id) VALUES ($1, $2);

-- name: ListGroups :many
-- The organization's live groups by name. Bounded by maxGroups, so not paged.
SELECT * FROM groups WHERE org_id = $1 AND deleted_at IS NULL ORDER BY display_name, id;

-- name: ListGroupMembers :many
-- Live memberships of the organization's live groups, by member name.
SELECT gm.group_id, gm.user_id, gm.sync_status, u.display_name, u.email
FROM group_members gm
JOIN groups g ON g.id = gm.group_id
JOIN users u ON u.id = gm.user_id
WHERE g.org_id = $1 AND g.deleted_at IS NULL AND gm.deleted_at IS NULL
ORDER BY u.display_name, u.id;

-- name: CountGroups :one
-- Live groups of the organization: tombstones waiting for Keycloak do not count.
SELECT count(*)::int FROM groups WHERE org_id = $1 AND deleted_at IS NULL;

-- name: GetGroup :one
-- A live group; a tombstone is gone as far as people are concerned.
SELECT * FROM groups WHERE id = $1 AND deleted_at IS NULL;

-- name: LockGroup :one
SELECT * FROM groups WHERE id = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: RenameGroup :one
-- The display name only: the machine name, the group's name in Keycloak and
-- in Kubernetes RBAC, never changes.
UPDATE groups SET display_name = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: TombstoneGroup :exec
-- The worker deletes it in Keycloak, then deletes the row and its memberships.
UPDATE groups
SET deleted_at = now(), sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: ShareLockMembership :one
-- Held while someone is added to a group, so they cannot leave the
-- organization half-way (leaving deletes this row first; 02-data-model §2).
SELECT m.role FROM organization_members m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1 AND m.user_id = $2
FOR SHARE OF m, u;

-- name: JoinGroup :execrows
-- Adds a member to a group; a tombstone left by an earlier removal comes
-- back to life. 0 rows: already a live member.
INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
ON CONFLICT (group_id, user_id) DO UPDATE
SET deleted_at = NULL, sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
WHERE group_members.deleted_at IS NOT NULL;

-- name: LeaveGroup :execrows
-- Removal is a tombstone: the row goes once Keycloak has caught up.
UPDATE group_members
SET deleted_at = now(), sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
WHERE group_id = $1 AND user_id = $2 AND deleted_at IS NULL;

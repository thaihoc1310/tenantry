-- name: ListMembers :many
-- Members by user id (ULIDs sort by sign-up), each with the live groups of
-- the organization they are in, both arrays in group name order.
SELECT m.user_id, m.role, m.created_at, u.email, u.display_name,
  array(SELECT g.id FROM group_members gm JOIN groups g ON g.id = gm.group_id
        WHERE gm.user_id = m.user_id AND g.org_id = m.org_id AND gm.deleted_at IS NULL AND g.deleted_at IS NULL
        ORDER BY g.display_name, g.id)::text[] AS group_ids,
  array(SELECT g.display_name FROM group_members gm JOIN groups g ON g.id = gm.group_id
        WHERE gm.user_id = m.user_id AND g.org_id = m.org_id AND gm.deleted_at IS NULL AND g.deleted_at IS NULL
        ORDER BY g.display_name, g.id)::text[] AS group_names
FROM organization_members m JOIN users u ON u.id = m.user_id
WHERE m.org_id = @org_id
  AND (sqlc.narg(cursor)::text IS NULL OR m.user_id > sqlc.narg(cursor))
ORDER BY m.user_id
LIMIT @page_size;

-- name: UpdateMemberRole :exec
UPDATE organization_members SET role = $3, updated_at = now() WHERE org_id = $1 AND user_id = $2;

-- name: DeleteMember :one
-- First when someone leaves or is removed: the deleted row is what a
-- concurrent "add to group" checks under FOR SHARE (02-data-model §2).
DELETE FROM organization_members WHERE org_id = $1 AND user_id = $2 RETURNING role;

-- name: TombstoneMemberGroups :execrows
-- Then, in a statement of its own, the person's group memberships in the
-- organization become tombstones for the group sync worker to undo in Keycloak.
UPDATE group_members gm
SET deleted_at = now(), sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now()
FROM groups g
WHERE gm.group_id = g.id AND g.org_id = $1 AND gm.user_id = $2 AND gm.deleted_at IS NULL;

-- name: SetOrganizationOwner :exec
UPDATE organizations SET owner_user_id = $2, updated_at = now() WHERE id = $1;

-- name: CancelTransfersTo :execrows
-- A recipient who stops being an OrgAdmin cannot become the owner.
UPDATE ownership_transfers SET state = 'Cancelled', updated_at = now()
WHERE org_id = $1 AND to_user_id = $2 AND state = 'Proposed';

-- name: CancelProposedTransfer :execrows
-- A new offer replaces the one waiting.
UPDATE ownership_transfers SET state = 'Cancelled', updated_at = now()
WHERE org_id = $1 AND state = 'Proposed';

-- name: CreateTransfer :one
INSERT INTO ownership_transfers (id, org_id, from_user_id, to_user_id, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetProposedTransfer :one
SELECT sqlc.embed(t),
  f.email AS from_email, f.display_name AS from_name,
  r.email AS to_email, r.display_name AS to_name
FROM ownership_transfers t
JOIN users f ON f.id = t.from_user_id
JOIN users r ON r.id = t.to_user_id
WHERE t.org_id = $1 AND t.state = 'Proposed';

-- name: GetTransfer :one
SELECT * FROM ownership_transfers WHERE id = $1;

-- name: LockTransfer :one
SELECT * FROM ownership_transfers WHERE id = $1 FOR UPDATE;

-- name: SetTransferState :exec
UPDATE ownership_transfers SET state = $2, updated_at = now() WHERE id = $1;

-- name: GetMember :one
SELECT m.user_id, m.role, m.created_at, u.email, u.display_name,
  array(SELECT g.id FROM group_members gm JOIN groups g ON g.id = gm.group_id
        WHERE gm.user_id = m.user_id AND g.org_id = m.org_id AND gm.deleted_at IS NULL AND g.deleted_at IS NULL
        ORDER BY g.display_name, g.id)::text[] AS group_ids,
  array(SELECT g.display_name FROM group_members gm JOIN groups g ON g.id = gm.group_id
        WHERE gm.user_id = m.user_id AND g.org_id = m.org_id AND gm.deleted_at IS NULL AND g.deleted_at IS NULL
        ORDER BY g.display_name, g.id)::text[] AS group_names
FROM organization_members m JOIN users u ON u.id = m.user_id
WHERE m.org_id = @org_id AND m.user_id = @user_id;

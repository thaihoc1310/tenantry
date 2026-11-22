-- name: LockEntitlement :one
-- Held while a limit of the entitlement is checked and used, so two requests
-- cannot both take the last place (02-data-model §3).
SELECT * FROM organization_entitlements WHERE org_id = $1 FOR UPDATE;

-- name: CountMembers :one
SELECT count(*)::int FROM organization_members WHERE org_id = $1;

-- name: MemberByEmail :one
-- Whether someone with this email is already a member; emails are compared lowercase.
SELECT u.id FROM organization_members m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1 AND lower(u.email) = @email::text
LIMIT 1;

-- name: CountLiveGroups :one
-- Groups of the organization among ids, not waiting for removal.
SELECT count(*)::int FROM groups WHERE org_id = $1 AND id = ANY(@ids::text[]) AND deleted_at IS NULL;

-- name: RevokePendingInvitationsFor :execrows
-- Inviting an email again replaces its open invitation.
UPDATE invitations SET state = 'Revoked', updated_at = now()
WHERE org_id = $1 AND email = $2 AND state = 'Pending';

-- name: CreateInvitation :one
INSERT INTO invitations (id, org_id, email, token_hash, role, group_ids, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListInvitations :many
-- Open invitations of an organization, newest first; an expired one shows
-- until it is replaced or revoked, so the inviter sees why it stopped working.
SELECT sqlc.embed(i), u.display_name AS inviter_name, u.email AS inviter_email
FROM invitations i JOIN users u ON u.id = i.invited_by
WHERE i.org_id = $1 AND i.state = 'Pending'
  AND (sqlc.narg(cursor)::text IS NULL OR i.id < sqlc.narg(cursor))
ORDER BY i.id DESC
LIMIT @page_size;

-- name: GetInvitation :one
SELECT * FROM invitations WHERE id = $1;

-- name: RevokeInvitation :one
UPDATE invitations SET state = 'Revoked', updated_at = now()
WHERE id = $1 AND state = 'Pending'
RETURNING *;

-- name: PreviewInvitation :one
-- What the invited person sees before accepting.
SELECT sqlc.embed(i), o.display_name AS org_name, o.slug AS org_slug, o.status AS org_status,
       u.display_name AS inviter_name,
       array(SELECT g.display_name FROM groups g
             WHERE g.id = ANY(i.group_ids) AND g.deleted_at IS NULL ORDER BY g.display_name)::text[] AS group_names
FROM invitations i
JOIN organizations o ON o.id = i.org_id
JOIN users u ON u.id = i.invited_by
WHERE i.token_hash = $1;

-- name: LockInvitation :one
SELECT * FROM invitations WHERE id = $1 FOR UPDATE;

-- name: ExpireInvitation :exec
UPDATE invitations SET state = 'Expired', updated_at = now() WHERE id = $1 AND state = 'Pending';

-- name: AcceptInvitation :exec
UPDATE invitations SET state = 'Accepted', accepted_by = $2, updated_at = now() WHERE id = $1 AND state = 'Pending';

-- name: JoinGroups :exec
-- Adds the user to the live groups of the organization among ids. A
-- tombstone left by an earlier removal comes back to life: the group sync
-- worker then makes sure the membership exists in Keycloak (02-data-model §2).
INSERT INTO group_members (group_id, user_id)
SELECT g.id, @user_id FROM groups g
WHERE g.org_id = @org_id AND g.id = ANY(@ids::text[]) AND g.deleted_at IS NULL
ON CONFLICT (group_id, user_id) DO UPDATE
SET deleted_at = NULL, sync_status = 'Pending', sync_attempts = 0, next_sync_at = now(), last_error = '', updated_at = now();

-- name: GetMembership :one
SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2;

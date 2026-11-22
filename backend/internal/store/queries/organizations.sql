-- name: GetOrganizationForMember :one
-- Joining on the caller's membership means a non-member gets no row, so a
-- missing org and someone else's org look the same (404, threat case §20.3).
-- idOrSlug is either an id (30 chars) or a slug (≤ 20), never ambiguous. A
-- user may hold a Rejected request and a newer one under the same slug; the
-- live one wins.
SELECT sqlc.embed(o), m.role
FROM organizations o
JOIN organization_members m ON m.org_id = o.id AND m.user_id = @user_id
WHERE (o.id = @id_or_slug OR o.slug = @id_or_slug)
  AND o.status <> 'Deleted'
ORDER BY o.status = 'Rejected', o.created_at DESC
LIMIT 1;

-- name: LockOrganization :one
-- Read before a change, held until it commits, so the audit trail's "before"
-- is what the change replaced.
SELECT * FROM organizations WHERE id = $1 FOR UPDATE;

-- name: UpdateOrganization :one
-- A NULL argument keeps the column. The status guard closes the gap between
-- the caller's read and this write: a request rejected in between stays untouched.
UPDATE organizations
SET display_name = COALESCE(sqlc.narg(display_name), display_name),
    description  = COALESCE(sqlc.narg(description), description),
    updated_at   = now()
WHERE id = @id AND status IN ('Pending', 'Active')
RETURNING *;

-- name: ListOrganizationsForMember :many
-- Keyset pagination on the id: ULIDs sort by creation time, so id DESC is
-- newest first and a page boundary stays stable while rows are added.
SELECT sqlc.embed(o), m.role
FROM organizations o
JOIN organization_members m ON m.org_id = o.id AND m.user_id = @user_id
WHERE o.status <> 'Deleted'
  AND (sqlc.narg(cursor)::text IS NULL OR o.id < sqlc.narg(cursor))
ORDER BY o.id DESC
LIMIT @page_size;

-- name: CreateOrganizationRequest :one
-- The partial unique indexes enforce "slug free" and "one Pending request per
-- user" even under concurrent requests; the service maps their violations.
INSERT INTO organizations (id, slug, display_name, description, owner_user_id, status)
VALUES (@id, @slug, @display_name, @description, @owner_user_id, 'Pending')
RETURNING *;

-- name: AddOrganizationMember :exec
INSERT INTO organization_members (org_id, user_id, role) VALUES (@org_id, @user_id, @role);

-- name: GetActiveUserID :one
SELECT id FROM users WHERE id = $1 AND status = 'Active';

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

-- name: UpdateOrganization :one
-- A NULL argument keeps the column. The status guard closes the gap between
-- the caller's read and this write: a request rejected in between stays untouched.
UPDATE organizations
SET display_name = COALESCE(sqlc.narg(display_name), display_name),
    description  = COALESCE(sqlc.narg(description), description),
    updated_at   = now()
WHERE id = @id AND status IN ('Pending', 'Active')
RETURNING *;

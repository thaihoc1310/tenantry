-- +goose Up

-- A deleted organization gives its slug back, as a rejected request does
-- (architecture/02-data-model §2, 04-state-machines §11). Deleting one waits
-- until no Workspace row of it is left, quarantined ones included, so no
-- Kubernetes name <org>-<workspace> of the old one is still in use; groups
-- and every other name use ids, not the slug.
DROP INDEX organizations_slug_uq;
CREATE UNIQUE INDEX organizations_slug_uq ON organizations (slug) WHERE status NOT IN ('Rejected', 'Deleted');

-- +goose Down
DROP INDEX organizations_slug_uq;
CREATE UNIQUE INDEX organizations_slug_uq ON organizations (slug) WHERE status <> 'Rejected';

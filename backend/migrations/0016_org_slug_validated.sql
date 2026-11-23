-- +goose Up

-- 0006 added the hyphen-free slug check NOT VALID, so rows from before it
-- were not checked. None is left (the lab had none on 2026-11-23, Deleted
-- and Rejected included): the check now holds for every row. VALIDATE fails
-- here, naming the constraint, if a database still has such a slug.
ALTER TABLE organizations VALIDATE CONSTRAINT organizations_slug_check;

-- +goose Down
ALTER TABLE organizations DROP CONSTRAINT organizations_slug_check;
ALTER TABLE organizations
    ADD CONSTRAINT organizations_slug_check CHECK (slug ~ '^[a-z][a-z0-9]*$' AND length(slug) BETWEEN 3 AND 20) NOT VALID;

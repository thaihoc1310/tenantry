-- +goose Up

-- Account projection. Keycloak owns the account; identity is (issuer, subject)
-- because email and name can change (PRODUCT-PLAN §4.1).
CREATE TABLE users (
    id           text PRIMARY KEY,
    issuer       text NOT NULL,
    subject      text NOT NULL,
    email        text,
    display_name text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active', 'Disabled')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

-- The slug prefixes every Kubernetes name the org owns, so it must be a valid
-- DNS label fragment and never change (§4.2). Reserved words are checked in Go.
CREATE TABLE organizations (
    id            text PRIMARY KEY,
    slug          text NOT NULL CHECK (slug ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND length(slug) BETWEEN 3 AND 20),
    display_name  text NOT NULL,
    description   text NOT NULL DEFAULT '',
    owner_user_id text NOT NULL REFERENCES users(id),
    status        text NOT NULL CHECK (status IN ('Pending', 'Active', 'Rejected', 'Deleted')),
    review_note   text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- A rejected request gives its slug back.
CREATE UNIQUE INDEX organizations_slug_uq ON organizations (slug) WHERE status <> 'Rejected';
-- At most one open request per user.
CREATE UNIQUE INDEX organizations_one_pending_per_user_uq ON organizations (owner_user_id) WHERE status = 'Pending';

-- Removing a member deletes the row; history lives in audit_events.
CREATE TABLE organization_members (
    org_id     text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    text NOT NULL REFERENCES users(id),
    role       text NOT NULL CHECK (role IN ('OrgOwner', 'OrgAdmin', 'OrgMember')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
-- With organizations.owner_user_id NOT NULL this makes "exactly one owner" (§15):
-- the column guarantees at least one, this index at most one.
CREATE UNIQUE INDEX organization_members_one_owner_uq ON organization_members (org_id) WHERE role = 'OrgOwner';
CREATE INDEX organization_members_user_idx ON organization_members (user_id);

-- +goose Down
DROP TABLE organization_members;
DROP TABLE organizations;
DROP TABLE users;

-- +goose Up

-- An invitation link an OrgOwner or OrgAdmin sends by hand (PRODUCT-PLAN §9.1).
-- Only the sha256 of the token is stored: the token itself appears once, in
-- the response that creates the invitation. Accepting it needs a verified
-- email equal to this one (04-state-machines §7).
CREATE TABLE invitations (
    id          text PRIMARY KEY,
    org_id      text NOT NULL REFERENCES organizations(id),
    email       text NOT NULL CHECK (email = lower(email) AND email LIKE '%_@_%'),
    token_hash  bytea NOT NULL CHECK (length(token_hash) = 32),
    role        text NOT NULL CHECK (role IN ('OrgAdmin', 'OrgMember')), -- never OrgOwner
    group_ids   text[] NOT NULL DEFAULT '{}',                           -- groups joined on accept
    invited_by  text NOT NULL REFERENCES users(id),
    expires_at  timestamptz NOT NULL,
    state       text NOT NULL DEFAULT 'Pending' CHECK (state IN ('Pending', 'Accepted', 'Revoked', 'Expired')),
    accepted_by text REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invitations_token_hash_uq UNIQUE (token_hash),
    CHECK ((state = 'Accepted') = (accepted_by IS NOT NULL))
);
-- One pending invitation per email in an organization: inviting again revokes
-- the older one in the same transaction; this index is the safety net.
CREATE UNIQUE INDEX invitations_one_pending_per_email_uq ON invitations (org_id, email) WHERE state = 'Pending';
-- An organization's open invitations, newest first.
CREATE INDEX invitations_org_idx ON invitations (org_id, id DESC);

-- An OrgOwner offers the organization to one of its OrgAdmins, who accepts in
-- the portal; no token or link (PRODUCT-PLAN §9.2, 04-state-machines §8).
CREATE TABLE ownership_transfers (
    id           text PRIMARY KEY,
    org_id       text NOT NULL REFERENCES organizations(id),
    from_user_id text NOT NULL REFERENCES users(id),
    to_user_id   text NOT NULL REFERENCES users(id),
    expires_at   timestamptz NOT NULL,
    state        text NOT NULL DEFAULT 'Proposed' CHECK (state IN ('Proposed', 'Accepted', 'Cancelled', 'Expired')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (from_user_id <> to_user_id)
);
-- At most one offer waiting per organization: a new one cancels the older.
CREATE UNIQUE INDEX ownership_transfers_one_proposed_uq ON ownership_transfers (org_id) WHERE state = 'Proposed';

-- +goose Down
DROP TABLE ownership_transfers;
DROP TABLE invitations;

-- +goose Up

-- Responses of requests sent with an Idempotency-Key (IMPLEMENTATION-PLAN
-- §3.4), so a retried request gets the first answer instead of doing the
-- work twice. The row is claimed at the start of the request's transaction
-- (response_status 0) and filled in before it commits; a failed request
-- rolls it back, so a committed row always holds a 2xx response.
CREATE TABLE idempotency_keys (
    user_id           text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key               text NOT NULL,
    method            text NOT NULL,
    path              text NOT NULL,
    request_hash      bytea NOT NULL CHECK (length(request_hash) = 32), -- sha256 of the body
    response_status   int NOT NULL DEFAULT 0 CHECK (response_status = 0 OR response_status BETWEEN 200 AND 299),
    response_location text,  -- the Location header, when the response had one
    -- json, not jsonb: kept byte for byte, so a replay is the very response
    -- (jsonb would reorder keys). Never queried into. NULL: no body.
    response_body     json,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);
-- Housekeeping deletes rows after 24 hours.
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

-- Who changed what (PRODUCT-PLAN §21.3). Written in the transaction of the
-- change it records, so the trail has exactly the changes that happened.
-- No foreign keys: the trail outlives what it names.
CREATE TABLE audit_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id text, -- NULL when the platform acted on its own
    org_id        text, -- NULL for events outside any organization (login)
    action        text NOT NULL, -- e.g. organization.update
    target_type   text NOT NULL,
    target_id     text NOT NULL,
    result        text NOT NULL,
    detail        jsonb NOT NULL DEFAULT '{}',
    at            timestamptz NOT NULL DEFAULT now()
);
-- An organization's recent activity, newest first.
CREATE INDEX audit_events_org_idx ON audit_events (org_id, id DESC) WHERE org_id IS NOT NULL;

-- +goose Down
DROP TABLE audit_events;
DROP TABLE idempotency_keys;

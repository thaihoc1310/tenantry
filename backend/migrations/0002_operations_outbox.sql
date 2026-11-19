-- +goose Up

-- One piece of asynchronous work a user can follow, e.g. creating a Workspace
-- (PRODUCT-PLAN §15). The state machine lives in internal/operation.
CREATE TABLE operations (
    id             text PRIMARY KEY,
    org_id         text NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    resource_type  text NOT NULL,              -- e.g. Workspace
    resource_id    text NOT NULL,
    type           text NOT NULL,              -- e.g. workspace.create
    state          text NOT NULL DEFAULT 'Pending'
                   CHECK (state IN ('Pending', 'Running', 'Succeeded', 'Failed', 'Blocked')),
    progress       smallint NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    error_code     text NOT NULL DEFAULT '',
    error_message  text NOT NULL DEFAULT '',
    correlation_id text NOT NULL DEFAULT '',   -- request id of the call that started it
    created_by     text REFERENCES users(id),  -- NULL when the platform started it
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz                 -- set on Succeeded, Failed or Blocked
);
CREATE INDEX operations_org_idx ON operations (org_id, id DESC);
CREATE INDEX operations_resource_idx ON operations (resource_type, resource_id);

-- Changes to push to a cluster, written in the same transaction as the change
-- itself and delivered at least once by tenantry-worker (IMPLEMENTATION-PLAN §5.1).
CREATE TABLE outbox_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, -- delivery order
    operation_id      text NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    -- Events with the same key are delivered strictly in id order, so a
    -- delete can never overtake the create of the same Workspace.
    ordering_key      text NOT NULL,
    target_cluster_id text NOT NULL,           -- REFERENCES clusters(id) once that table exists (M3)
    type              text NOT NULL,           -- e.g. workspace.apply
    payload           jsonb NOT NULL,
    version           bigint NOT NULL,         -- desired generation carried by the payload
    attempts          integer NOT NULL DEFAULT 0, -- consecutive failed deliveries
    next_attempt_at   timestamptz NOT NULL DEFAULT now(),
    last_error        text NOT NULL DEFAULT '',
    processed_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now()
);
-- The worker only ever looks at undelivered events.
CREATE INDEX outbox_events_due_idx ON outbox_events (next_attempt_at, id) WHERE processed_at IS NULL;
CREATE INDEX outbox_events_ordering_idx ON outbox_events (ordering_key, id) WHERE processed_at IS NULL;
CREATE INDEX outbox_events_operation_idx ON outbox_events (operation_id) WHERE processed_at IS NULL;

-- +goose Down
DROP TABLE outbox_events;
DROP TABLE operations;

-- +goose Up

-- Routes publish a Service of a Workspace on its cluster's apps domain
-- (IMPLEMENTATION-PLAN M11 step 4; PRODUCT-PLAN §7.6; architecture/02 §4,
-- 04-state-machines §9.3). The API puts the hostname together; one hostname
-- is held by one Route on the whole platform (ROUTE_HOSTNAME_TAKEN).
CREATE TABLE workspace_routes (
    id           text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    namespace    text NOT NULL,
    name         text NOT NULL CHECK (name ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'),
    hostname     text NOT NULL,
    service_name text NOT NULL,
    service_port integer NOT NULL CHECK (service_port BETWEEN 1 AND 65535),
    state        text NOT NULL DEFAULT 'Desired' CHECK (state IN ('Desired', 'Creating', 'Active', 'Deleting', 'Deleted')),
    -- Why the controller could not build its Ingress (status.routes[].message).
    message      text NOT NULL DEFAULT '',
    created_by   text REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX workspace_routes_hostname_uq ON workspace_routes (hostname) WHERE state <> 'Deleted';
CREATE INDEX workspace_routes_workspace_idx ON workspace_routes (workspace_id) WHERE state <> 'Deleted';

-- +goose Down
DROP TABLE workspace_routes;

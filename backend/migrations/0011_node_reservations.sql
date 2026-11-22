-- +goose Up

-- What a Dedicated Workspace holds of its node (IMPLEMENTATION-PLAN M8 step
-- 1; architecture/02-data-model §4): requests = limits, a hard limit, so the
-- sum on a node never passes what it can hold (checked under the node
-- grant's lock). Lifecycle: 04-state-machines §3.
CREATE TABLE workspace_node_reservations (
    id                    text PRIMARY KEY,
    workspace_id          text NOT NULL,
    org_id                text NOT NULL,
    cluster_id            text NOT NULL,
    node_grant_id         text NOT NULL,
    reserved_cpu_milli    bigint NOT NULL CHECK (reserved_cpu_milli >= 0),
    reserved_memory_bytes bigint NOT NULL CHECK (reserved_memory_bytes >= 0),
    reserved_pods         int NOT NULL CHECK (reserved_pods >= 0),
    state                 text NOT NULL DEFAULT 'Pending' CHECK (state IN ('Pending', 'Active', 'Releasing', 'Released')),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    -- one per Workspace: a resize changes this row
    CONSTRAINT workspace_node_reservations_workspace_uq UNIQUE (workspace_id),
    FOREIGN KEY (workspace_id, org_id, cluster_id) REFERENCES workspaces (id, org_id, cluster_id) ON DELETE CASCADE,
    -- a node grant of the same organization, on the same cluster
    FOREIGN KEY (node_grant_id, org_id, cluster_id) REFERENCES dedicated_node_grants (id, org_id, cluster_id)
);
CREATE INDEX workspace_node_reservations_grant_idx ON workspace_node_reservations (node_grant_id) WHERE state <> 'Released';

-- +goose Down
DROP TABLE workspace_node_reservations;

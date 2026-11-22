-- +goose Up

-- The Pod address block of each Workspace (IMPLEMENTATION-PLAN M9 step 1;
-- architecture/02-data-model §4, 04-state-machines §4). The IPAM service
-- picks a free block under the network pool's lock; the exclusion
-- constraint is the last line: no two blocks not Released overlap in a
-- cluster (SQLSTATE 23P01 → CIDR_OVERLAP).
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE network_allocations (
    id               text PRIMARY KEY,
    workspace_id     text NOT NULL,
    cluster_id       text NOT NULL,
    pool_id          text NOT NULL REFERENCES network_pools(id),
    cidr             cidr NOT NULL,
    state            text NOT NULL DEFAULT 'Allocated' CHECK (state IN ('Allocated', 'Releasing', 'Quarantined', 'Released')),
    quarantine_until timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT network_allocations_workspace_uq UNIQUE (workspace_id),
    FOREIGN KEY (workspace_id, cluster_id) REFERENCES workspaces (id, cluster_id) ON DELETE CASCADE,
    CHECK ((state = 'Quarantined') = (quarantine_until IS NOT NULL)),
    CONSTRAINT network_allocations_no_overlap EXCLUDE USING gist (cluster_id WITH =, cidr inet_ops WITH &&) WHERE (state <> 'Released')
);
CREATE INDEX network_allocations_pool_idx ON network_allocations (pool_id) WHERE state <> 'Released';
CREATE INDEX network_allocations_quarantine_idx ON network_allocations (quarantine_until) WHERE state = 'Quarantined';

-- +goose Down
DROP TABLE network_allocations;

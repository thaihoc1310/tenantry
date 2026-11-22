-- +goose Up

-- What a Platform Admin manages from M5 (IMPLEMENTATION-PLAN M5 step 1;
-- architecture/02-data-model.md §2–3): the nodes of each cluster, nodes
-- granted to one organization, the address ranges a cluster already uses, and
-- the Keycloak side of disabling a user.

-- Pod CIDR (and Calico's default pool), Service CIDR, node CIDR: network pools
-- must not overlap them (lab: 192.168.0.0/16, 10.96.0.0/12).
ALTER TABLE clusters ADD COLUMN reserved_cidrs cidr[] NOT NULL DEFAULT '{}';

-- The per-node system reserve of a shared pool, as columns like those of a
-- dedicated node (02 §3 had one jsonb): the capacity math reads them and the
-- database keeps them non-negative.
ALTER TABLE shared_compute_pools
    DROP COLUMN system_reserve,
    ADD COLUMN system_reserve_cpu_milli    bigint NOT NULL DEFAULT 0 CHECK (system_reserve_cpu_milli >= 0),
    ADD COLUMN system_reserve_memory_bytes bigint NOT NULL DEFAULT 0 CHECK (system_reserve_memory_bytes >= 0),
    ADD COLUMN system_reserve_pods         int NOT NULL DEFAULT 0 CHECK (system_reserve_pods >= 0),
    -- the target of cluster_nodes' composite key: a node's pool is on its cluster
    ADD CONSTRAINT shared_compute_pools_id_cluster_uq UNIQUE (id, cluster_id);

-- The nodes of a cluster, as the operator reports them (PlatformNodeInventory
-- from M6; the seed writes them before that). shared_pool_id is the pool an
-- Admin put the node in: a node is in at most one pool, of its own cluster.
CREATE TABLE cluster_nodes (
    cluster_id               text NOT NULL REFERENCES clusters(id),
    node_name                text NOT NULL CHECK (node_name ~ '^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$'),
    kind                     text NOT NULL CHECK (kind IN ('ControlPlane', 'System', 'Worker')),
    ready                    boolean NOT NULL DEFAULT false,
    schedulable              boolean NOT NULL DEFAULT false,
    allocatable_cpu_milli    bigint NOT NULL DEFAULT 0 CHECK (allocatable_cpu_milli >= 0),
    allocatable_memory_bytes bigint NOT NULL DEFAULT 0 CHECK (allocatable_memory_bytes >= 0),
    allocatable_pods         int NOT NULL DEFAULT 0 CHECK (allocatable_pods >= 0),
    shared_pool_id           text,
    observed_at              timestamptz NOT NULL DEFAULT now(),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, node_name),
    FOREIGN KEY (shared_pool_id, cluster_id) REFERENCES shared_compute_pools (id, cluster_id),
    -- only a worker node carries tenant Pods
    CHECK (shared_pool_id IS NULL OR kind = 'Worker')
);
CREATE INDEX cluster_nodes_pool_idx ON cluster_nodes (shared_pool_id) WHERE shared_pool_id IS NOT NULL;

-- A node of a cluster given to one organization (PRODUCT-PLAN §6.3), under
-- its cluster grant. Revoking sets revoked_at: the row stays for the
-- reservations that point at it (M8). Capacity is per node: reservable =
-- allocatable - system_reserve, nothing overcommitted.
CREATE TABLE dedicated_node_grants (
    id                          text PRIMARY KEY,
    org_id                      text NOT NULL REFERENCES organizations(id),
    cluster_id                  text NOT NULL REFERENCES clusters(id),
    node_name                   text NOT NULL,
    allocatable_cpu_milli       bigint NOT NULL DEFAULT 0 CHECK (allocatable_cpu_milli >= 0),
    allocatable_memory_bytes    bigint NOT NULL DEFAULT 0 CHECK (allocatable_memory_bytes >= 0),
    allocatable_pods            int NOT NULL DEFAULT 0 CHECK (allocatable_pods >= 0),
    system_reserve_cpu_milli    bigint NOT NULL DEFAULT 0 CHECK (system_reserve_cpu_milli >= 0),
    system_reserve_memory_bytes bigint NOT NULL DEFAULT 0 CHECK (system_reserve_memory_bytes >= 0),
    system_reserve_pods         int NOT NULL DEFAULT 0 CHECK (system_reserve_pods >= 0),
    node_state                  text NOT NULL DEFAULT 'NotReady' CHECK (node_state IN ('Ready', 'NotReady', 'Cordoned')),
    capacity_state              text NOT NULL DEFAULT 'Available'
                                CHECK (capacity_state IN ('Available', 'PartiallyAllocated', 'FullyAllocated', 'Overcommitted')),
    revoked_at                  timestamptz,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    -- A live node grant needs the organization's grant on that cluster: the
    -- service checks it, and refuses to delete a grant with live nodes. No
    -- foreign key: revoked rows outlive the grant, as history for the
    -- reservations that point at them.
    -- the target of workspace_node_reservations' composite key (M8)
    CONSTRAINT dedicated_node_grants_id_org_cluster_uq UNIQUE (id, org_id, cluster_id)
);
-- A node is dedicated to one organization at a time.
CREATE UNIQUE INDEX dedicated_node_grants_one_live_uq ON dedicated_node_grants (cluster_id, node_name) WHERE revoked_at IS NULL;
CREATE INDEX dedicated_node_grants_org_idx ON dedicated_node_grants (org_id, cluster_id) WHERE revoked_at IS NULL;

-- Disabling or enabling a user reaches Keycloak through the group sync worker,
-- with the same retry columns as groups (02 §2).
ALTER TABLE users
    ADD COLUMN sync_status   text NOT NULL DEFAULT 'Synced' CHECK (sync_status IN ('Pending', 'Synced', 'Error')),
    ADD COLUMN sync_attempts int NOT NULL DEFAULT 0,
    ADD COLUMN next_sync_at  timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN last_error    text NOT NULL DEFAULT '';
CREATE INDEX users_sync_due_idx ON users (next_sync_at) WHERE sync_status <> 'Synced';

-- +goose Down
DROP INDEX users_sync_due_idx;
ALTER TABLE users DROP COLUMN last_error, DROP COLUMN next_sync_at, DROP COLUMN sync_attempts, DROP COLUMN sync_status;
DROP TABLE dedicated_node_grants;
DROP TABLE cluster_nodes;
ALTER TABLE shared_compute_pools
    DROP CONSTRAINT shared_compute_pools_id_cluster_uq,
    DROP COLUMN system_reserve_pods,
    DROP COLUMN system_reserve_memory_bytes,
    DROP COLUMN system_reserve_cpu_milli,
    ADD COLUMN system_reserve jsonb NOT NULL DEFAULT '{}';
ALTER TABLE clusters DROP COLUMN reserved_cidrs;

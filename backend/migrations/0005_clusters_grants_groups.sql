-- +goose Up

-- Everything approving an organization writes or points at
-- (IMPLEMENTATION-PLAN M3 step 4; architecture/02-data-model.md §2–3).
-- Clusters and pools only have their tables and seed data in M3; managing
-- them from the portal is M5.

-- Ids an Admin picks (cluster-a, shared-general, onboarding-pod-network) end
-- up as Kubernetes label values and object names, so they are DNS labels.

CREATE TABLE clusters (
    id                text PRIMARY KEY CHECK (id ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    display_name      text NOT NULL,
    status            text NOT NULL DEFAULT 'Registered'
                      CHECK (status IN ('Registered', 'Ready', 'Maintenance', 'Unavailable')),
    proxy_endpoint    text NOT NULL, -- Capsule Proxy, the server of tenant kubeconfigs
    console_url       text NOT NULL, -- Headlamp
    apps_domain       text NOT NULL, -- tenant Routes are <name>-<workspace>.<apps_domain>
    capabilities      jsonb NOT NULL DEFAULT '{}',
    last_health_check timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Shared compute: nodes many organizations share, sized with an overcommit
-- ratio (PRODUCT-PLAN §5.3). Its nodes carry tenantry.io/compute-pool=<id>.
CREATE TABLE shared_compute_pools (
    id                          text PRIMARY KEY CHECK (id ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    cluster_id                  text NOT NULL REFERENCES clusters(id),
    name                        text NOT NULL,
    allocation_overcommit_ratio numeric(4, 2) NOT NULL CHECK (allocation_overcommit_ratio >= 1),
    system_reserve              jsonb NOT NULL DEFAULT '{}',
    capacity_snapshot           jsonb NOT NULL DEFAULT '{}', -- copied from PlatformSharedPool status (M5)
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now()
);

-- Address space Workspaces get their CIDR from (PRODUCT-PLAN §7). org_id NULL:
-- a pool any organization's grant may list; otherwise only that one's.
CREATE TABLE network_pools (
    id                     text PRIMARY KEY CHECK (id ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'),
    cluster_id             text NOT NULL REFERENCES clusters(id),
    org_id                 text REFERENCES organizations(id),
    cidrs                  cidr[] NOT NULL CHECK (cardinality(cidrs) > 0),
    default_prefix_length  int NOT NULL,
    allowed_prefix_lengths int[] NOT NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),
    CHECK (default_prefix_length = ANY (allowed_prefix_lengths))
);

-- What an approval proposes (PRODUCT-PLAN §5.2.1): the Admin approves it as
-- is or edits it first. A granted grant is a copy, untouched by later
-- policy changes. The templates are documents in the API's own shape.
CREATE TABLE onboarding_policies (
    id                   text PRIMARY KEY,
    default_cluster_id   text NOT NULL REFERENCES clusters(id),
    entitlement_template jsonb NOT NULL,
    grant_template       jsonb NOT NULL,
    active               boolean NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX onboarding_policies_one_active_uq ON onboarding_policies ((true)) WHERE active;

-- Limits that do not depend on a cluster (PRODUCT-PLAN §5.1).
CREATE TABLE organization_entitlements (
    org_id               text PRIMARY KEY REFERENCES organizations(id),
    max_members          int NOT NULL CHECK (max_members >= 1),
    max_groups           int NOT NULL CHECK (max_groups >= 0),
    max_workspaces_total int NOT NULL CHECK (max_workspaces_total >= 0),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- What an organization may reserve on one cluster (PRODUCT-PLAN §5.2).
-- CPU, memory and pods bound Shared Workspaces only; Workspaces, Namespaces
-- and storage bound every Workspace. Compute modes are not stored: Shared
-- when the grant lists a shared pool, Dedicated when the organization holds
-- dedicated nodes on the cluster (M5).
CREATE TABLE organization_cluster_grants (
    id                      text PRIMARY KEY,
    org_id                  text NOT NULL REFERENCES organizations(id),
    cluster_id              text NOT NULL REFERENCES clusters(id),
    is_default              boolean NOT NULL DEFAULT false,
    max_workspaces          int NOT NULL CHECK (max_workspaces >= 0),
    max_namespaces_total    int NOT NULL CHECK (max_namespaces_total >= 0),
    requests_cpu_milli      bigint NOT NULL CHECK (requests_cpu_milli >= 0),
    requests_memory_bytes   bigint NOT NULL CHECK (requests_memory_bytes >= 0),
    limits_cpu_milli        bigint NOT NULL CHECK (limits_cpu_milli >= requests_cpu_milli),
    limits_memory_bytes     bigint NOT NULL CHECK (limits_memory_bytes >= requests_memory_bytes),
    pods                    int NOT NULL CHECK (pods >= 0),
    requests_storage_bytes  bigint NOT NULL CHECK (requests_storage_bytes >= 0),
    allowed_storage_classes text[] NOT NULL DEFAULT '{}',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT organization_cluster_grants_org_cluster_uq UNIQUE (org_id, cluster_id)
);
CREATE UNIQUE INDEX organization_cluster_grants_one_default_uq ON organization_cluster_grants (org_id) WHERE is_default;

-- A pool and the grant listing it are on the same cluster; the service
-- checks it (02-data-model §3). Removing a grant removes its lists.
CREATE TABLE cluster_grant_shared_pools (
    grant_id text NOT NULL REFERENCES organization_cluster_grants(id) ON DELETE CASCADE,
    pool_id  text NOT NULL REFERENCES shared_compute_pools(id),
    PRIMARY KEY (grant_id, pool_id)
);
CREATE TABLE cluster_grant_network_pools (
    grant_id text NOT NULL REFERENCES organization_cluster_grants(id) ON DELETE CASCADE,
    pool_id  text NOT NULL REFERENCES network_pools(id),
    PRIMARY KEY (grant_id, pool_id)
);

-- Organization groups, mirrored to Keycloak by the group sync worker (M4,
-- 04-state-machines §9.1). A row says which way Keycloak must go
-- (deleted_at NULL: exist; set: be gone, a tombstone) and how far it got
-- (sync_status). A tombstone stays until Keycloak has caught up, then the
-- worker deletes the row.
CREATE TABLE groups (
    id                text PRIMARY KEY,
    org_id            text NOT NULL REFERENCES organizations(id),
    display_name      text NOT NULL,
    machine_name      text NOT NULL, -- org_<ulid>:group_<ulid>, the Keycloak group name
    keycloak_group_id text,
    deleted_at        timestamptz,
    sync_status       text NOT NULL DEFAULT 'Pending' CHECK (sync_status IN ('Pending', 'Synced', 'Error')),
    sync_attempts     int NOT NULL DEFAULT 0,
    next_sync_at      timestamptz NOT NULL DEFAULT now(),
    last_error        text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT groups_machine_name_uq UNIQUE (machine_name)
);
CREATE UNIQUE INDEX groups_display_name_uq ON groups (org_id, display_name) WHERE deleted_at IS NULL;
-- The sync worker's queue.
CREATE INDEX groups_sync_due_idx ON groups (next_sync_at) WHERE sync_status <> 'Synced';

-- No foreign key to organization_members: a member tombstone must outlive
-- the membership it undoes (02-data-model §2).
CREATE TABLE group_members (
    group_id      text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id       text NOT NULL REFERENCES users(id),
    deleted_at    timestamptz,
    sync_status   text NOT NULL DEFAULT 'Pending' CHECK (sync_status IN ('Pending', 'Synced', 'Error')),
    sync_attempts int NOT NULL DEFAULT 0,
    next_sync_at  timestamptz NOT NULL DEFAULT now(),
    last_error    text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_sync_due_idx ON group_members (next_sync_at) WHERE sync_status <> 'Synced';

-- +goose Down
DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE cluster_grant_network_pools;
DROP TABLE cluster_grant_shared_pools;
DROP TABLE organization_cluster_grants;
DROP TABLE organization_entitlements;
DROP TABLE onboarding_policies;
DROP TABLE network_pools;
DROP TABLE shared_compute_pools;
DROP TABLE clusters;

-- +goose Up

-- Workspaces and what hangs off them in M6 (IMPLEMENTATION-PLAN M6 step 9;
-- architecture/02-data-model.md §4). Node reservations come in M8, network
-- allocations in M9, routes in M11.

-- Targets of the composite keys below: a Workspace's grant, and a binding's
-- group, belong to the Workspace's own organization (02 §4).
ALTER TABLE organization_cluster_grants
    ADD CONSTRAINT organization_cluster_grants_id_org_cluster_uq UNIQUE (id, org_id, cluster_id);
ALTER TABLE groups ADD CONSTRAINT groups_id_org_uq UNIQUE (id, org_id);

-- One Capsule Tenant (PRODUCT-PLAN §4.4). Its resource columns are what it
-- holds of its grant: the grant's allocation is their SUM over Workspaces
-- not Deleted (CPU, memory and pods of Shared ones only). A row is first
-- written in Provisioning; Draft, Validating and Reserving only exist inside
-- the request (04-state-machines §1.3).
CREATE TABLE workspaces (
    id                     text PRIMARY KEY,
    org_id                 text NOT NULL REFERENCES organizations(id),
    cluster_id             text NOT NULL REFERENCES clusters(id),
    cluster_grant_id       text NOT NULL,
    slug                   text NOT NULL CHECK (slug ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND length(slug) BETWEEN 2 AND 20),
    -- <org slug>-<slug>: the Tenant, the CR, the Namespace prefix. Unique for
    -- good, also while a Deleted row waits for its CIDR's quarantine (M9).
    k8s_name               text NOT NULL,
    display_name           text NOT NULL,
    state                  text NOT NULL DEFAULT 'Provisioning'
                           CHECK (state IN ('Provisioning', 'Ready', 'Updating', 'Cordoned', 'Degraded', 'Failed', 'Deleting', 'Deleted')),
    requests_cpu_milli     bigint NOT NULL CHECK (requests_cpu_milli >= 0),
    requests_memory_bytes  bigint NOT NULL CHECK (requests_memory_bytes >= 0),
    limits_cpu_milli       bigint NOT NULL CHECK (limits_cpu_milli >= requests_cpu_milli),
    limits_memory_bytes    bigint NOT NULL CHECK (limits_memory_bytes >= requests_memory_bytes),
    pods                   int NOT NULL CHECK (pods >= 0),
    requests_storage_bytes bigint NOT NULL CHECK (requests_storage_bytes >= 0),
    namespace_quota        int NOT NULL CHECK (namespace_quota >= 1),
    compute_mode           text NOT NULL CHECK (compute_mode IN ('Shared', 'Dedicated')),
    shared_pool_id         text,
    -- desired_generation counts the CR specs the API has written; the
    -- worker sets observed_generation once the controller has reconciled one.
    desired_generation     bigint NOT NULL DEFAULT 1,
    observed_generation    bigint NOT NULL DEFAULT 0,
    conditions             jsonb NOT NULL DEFAULT '[]', -- copied from the CR's status
    deleted_at             timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workspaces_k8s_name_uq UNIQUE (k8s_name),
    -- the targets of the composite keys of bindings, namespaces and (M8) node reservations
    CONSTRAINT workspaces_id_org_uq UNIQUE (id, org_id),
    CONSTRAINT workspaces_id_cluster_uq UNIQUE (id, cluster_id),
    CONSTRAINT workspaces_id_org_cluster_uq UNIQUE (id, org_id, cluster_id),
    -- the grant of the organization, on the Workspace's cluster; RESTRICT:
    -- a grant goes only once no Workspace row points at it
    FOREIGN KEY (cluster_grant_id, org_id, cluster_id)
        REFERENCES organization_cluster_grants (id, org_id, cluster_id) ON DELETE RESTRICT,
    -- a pool of the Workspace's cluster, for Shared Workspaces only
    FOREIGN KEY (shared_pool_id, cluster_id) REFERENCES shared_compute_pools (id, cluster_id),
    CHECK ((compute_mode = 'Shared') = (shared_pool_id IS NOT NULL))
);
CREATE INDEX workspaces_org_idx ON workspaces (org_id, id);
CREATE INDEX workspaces_grant_idx ON workspaces (cluster_grant_id) WHERE state <> 'Deleted';
CREATE INDEX workspaces_pool_idx ON workspaces (shared_pool_id) WHERE state <> 'Deleted' AND shared_pool_id IS NOT NULL;

-- A role an organization group holds in a Workspace (PRODUCT-PLAN §10.4).
-- The group's foreign key RESTRICTs: a group with bindings is not deleted
-- (the service refuses first; this guards the sync worker's final delete).
CREATE TABLE workspace_group_bindings (
    workspace_id text NOT NULL,
    group_id     text NOT NULL,
    org_id       text NOT NULL,
    role         text NOT NULL CHECK (role IN ('TenantOwner', 'Developer', 'Viewer')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, group_id),
    FOREIGN KEY (workspace_id, org_id) REFERENCES workspaces (id, org_id) ON DELETE CASCADE,
    FOREIGN KEY (group_id, org_id) REFERENCES groups (id, org_id) ON DELETE RESTRICT
);
CREATE INDEX workspace_group_bindings_group_idx ON workspace_group_bindings (group_id);

-- The Namespaces of a Workspace as the portal sees them (04-state-machines
-- §9.2): asked for on the portal (source portal) or found by the controller
-- (source kubectl). name is the full name, <k8s_name>-<suffix>.
CREATE TABLE workspace_namespaces (
    id             text PRIMARY KEY,
    workspace_id   text NOT NULL,
    cluster_id     text NOT NULL,
    name           text NOT NULL CHECK (name ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'),
    source         text NOT NULL CHECK (source IN ('portal', 'kubectl')),
    state          text NOT NULL CHECK (state IN ('Desired', 'Creating', 'Active', 'Deleting', 'Deleted')),
    usage_snapshot jsonb NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, cluster_id) REFERENCES workspaces (id, cluster_id) ON DELETE CASCADE
);
-- A Namespace name is unique in its cluster.
CREATE UNIQUE INDEX workspace_namespaces_name_uq ON workspace_namespaces (cluster_id, name) WHERE state <> 'Deleted';
CREATE INDEX workspace_namespaces_workspace_idx ON workspace_namespaces (workspace_id);

-- An operation on a cluster itself (sharedpool.update) belongs to no
-- organization; only a Platform Admin sees it (02 §4).
ALTER TABLE operations ALTER COLUMN org_id DROP NOT NULL;

-- +goose Down
DELETE FROM operations WHERE org_id IS NULL;
ALTER TABLE operations ALTER COLUMN org_id SET NOT NULL;
DROP TABLE workspace_namespaces;
DROP TABLE workspace_group_bindings;
DROP TABLE workspaces;
ALTER TABLE groups DROP CONSTRAINT groups_id_org_uq;
ALTER TABLE organization_cluster_grants DROP CONSTRAINT organization_cluster_grants_id_org_cluster_uq;

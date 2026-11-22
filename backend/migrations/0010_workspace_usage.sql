-- +goose Up
-- (M7)

-- What a Workspace's Pods use of its quota, as the controller last reported
-- it (PlatformWorkspace status.usage; IMPLEMENTATION-PLAN M7): a resize may
-- not go below it, and GET /workspaces/{id}/usage shows it. Keys and
-- quantities as the CR has them (requests.cpu: 800m), like
-- workspace_namespaces.usage_snapshot.
ALTER TABLE workspaces ADD COLUMN usage_snapshot jsonb NOT NULL DEFAULT '{}';

-- Whether the Workspace is meant to be cordoned (spec.cordoned, PRODUCT-PLAN
-- §17.3); state Cordoned is the controller having done it.
ALTER TABLE workspaces ADD COLUMN cordoned boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN cordoned;
ALTER TABLE workspaces DROP COLUMN usage_snapshot;

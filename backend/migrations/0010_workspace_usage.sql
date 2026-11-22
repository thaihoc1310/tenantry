-- +goose Up

-- What a Workspace's Pods use of its quota, as the controller last reported
-- it (PlatformWorkspace status.usage; IMPLEMENTATION-PLAN M7): a resize may
-- not go below it, and GET /workspaces/{id}/usage shows it. Keys and
-- quantities as the CR has them (requests.cpu: 800m), like
-- workspace_namespaces.usage_snapshot.
ALTER TABLE workspaces ADD COLUMN usage_snapshot jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN usage_snapshot;

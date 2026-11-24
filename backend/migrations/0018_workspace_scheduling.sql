-- +goose Up

-- Pods the scheduler cannot place (PodScheduled False, Unschedulable), as
-- the controller reports them in status.scheduling and per Namespace. They
-- do not change the Workspace's state: it stays Ready, the portal warns.
ALTER TABLE workspaces
    ADD COLUMN unschedulable_pods int NOT NULL DEFAULT 0,
    ADD COLUMN unschedulable_message text NOT NULL DEFAULT '',
    ADD COLUMN unschedulable_since timestamptz;
ALTER TABLE workspace_namespaces
    ADD COLUMN unschedulable_pods int NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE workspace_namespaces DROP COLUMN unschedulable_pods;
ALTER TABLE workspaces
    DROP COLUMN unschedulable_pods,
    DROP COLUMN unschedulable_message,
    DROP COLUMN unschedulable_since;

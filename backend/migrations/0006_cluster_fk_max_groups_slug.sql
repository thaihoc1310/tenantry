-- +goose Up

-- An event goes to a registered cluster: the foreign key 0002 had to leave
-- out, because clusters only came with 0005.
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_target_cluster_fk FOREIGN KEY (target_cluster_id) REFERENCES clusters(id);

-- The three default groups count against max_groups (PRODUCT-PLAN §5.1), so
-- an entitlement below 3 would leave an organization over its limit from
-- its approval on. The service already refuses it; this is the safety net.
ALTER TABLE organization_entitlements DROP CONSTRAINT organization_entitlements_max_groups_check;
ALTER TABLE organization_entitlements
    ADD CONSTRAINT organization_entitlements_max_groups_check CHECK (max_groups >= 3);

-- An organization slug has no hyphen any more (PRODUCT-PLAN §4.2): <org>-<ws>
-- then names one Workspace of one organization. NOT VALID: new and changed
-- rows are checked, rows from before are not scanned (test organizations on
-- the lab may still carry a hyphen).
ALTER TABLE organizations DROP CONSTRAINT organizations_slug_check;
ALTER TABLE organizations
    ADD CONSTRAINT organizations_slug_check CHECK (slug ~ '^[a-z][a-z0-9]*$' AND length(slug) BETWEEN 3 AND 20) NOT VALID;

-- +goose Down
ALTER TABLE organizations DROP CONSTRAINT organizations_slug_check;
ALTER TABLE organizations
    ADD CONSTRAINT organizations_slug_check CHECK (slug ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$' AND length(slug) BETWEEN 3 AND 20);
ALTER TABLE organization_entitlements DROP CONSTRAINT organization_entitlements_max_groups_check;
ALTER TABLE organization_entitlements
    ADD CONSTRAINT organization_entitlements_max_groups_check CHECK (max_groups >= 0);
ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_target_cluster_fk;

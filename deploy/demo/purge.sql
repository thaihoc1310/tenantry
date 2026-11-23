-- Hard purge of test fixtures (deploy/demo/purge.py): organizations whose
-- slug starts with e2e or optest, in any status, and accounts whose email
-- does, with everything that belongs to them. Run inside a transaction the
-- caller opens and ends: ROLLBACK for the plan, COMMIT to purge. Every line
-- of output is tab-separated: "delete<TAB>table<TAB>rows", "refuse<TAB>what
-- <TAB>why", "keep<TAB>email" (a real account, never touched), "kc-group
-- <TAB>machine name" and "kc-user<TAB>email" (what Keycloak loses).
--
-- An organization is refused, and kept whole, while the cluster may still
-- hold something of it: a Workspace not Deleted, an address block still in
-- quarantine, a live dedicated node. An account is refused while a kept
-- organization (real or refused) still refers to it.

CREATE TEMP TABLE p_org ON COMMIT DROP AS
SELECT id, slug FROM organizations WHERE slug ~ '^(e2e|optest)';

CREATE TEMP TABLE p_refused_org ON COMMIT DROP AS
SELECT o.id, o.slug, 'a Workspace is not Deleted yet' AS why FROM p_org o
WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.org_id = o.id AND w.state <> 'Deleted')
UNION ALL
SELECT o.id, o.slug, coalesce('an address block is in quarantine until ' || max(a.quarantine_until)::text,
                                'an address block is not released yet') FROM p_org o
JOIN workspaces w ON w.org_id = o.id AND w.state = 'Deleted' JOIN network_allocations a ON a.workspace_id = w.id
WHERE a.state IN ('Allocated', 'Releasing') OR (a.state = 'Quarantined' AND a.quarantine_until > now())
GROUP BY o.id, o.slug
UNION ALL
SELECT o.id, o.slug, 'it holds a dedicated node' FROM p_org o
WHERE EXISTS (SELECT 1 FROM dedicated_node_grants g WHERE g.org_id = o.id AND g.revoked_at IS NULL);

DELETE FROM p_org WHERE id IN (SELECT id FROM p_refused_org);

CREATE TEMP TABLE p_user ON COMMIT DROP AS
SELECT id, email FROM users WHERE email ~ '^(e2e|optest)';

-- Accounts a kept organization refers to stay.
CREATE TEMP TABLE p_refused_user ON COMMIT DROP AS
SELECT DISTINCT u.id, u.email FROM p_user u WHERE
     EXISTS (SELECT 1 FROM organizations o WHERE o.owner_user_id = u.id AND o.id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM organization_members m WHERE m.user_id = u.id AND m.org_id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id WHERE gm.user_id = u.id AND g.org_id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM invitations i WHERE (i.invited_by = u.id OR i.accepted_by = u.id) AND i.org_id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM ownership_transfers t WHERE (t.from_user_id = u.id OR t.to_user_id = u.id) AND t.org_id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM operations op WHERE op.created_by = u.id AND op.org_id NOT IN (SELECT id FROM p_org))
  OR EXISTS (SELECT 1 FROM workspace_routes r JOIN workspaces w ON w.id = r.workspace_id WHERE r.created_by = u.id AND w.org_id NOT IN (SELECT id FROM p_org));

DELETE FROM p_user WHERE id IN (SELECT id FROM p_refused_user);

-- The demo accounts and the Platform Admin never match the prefixes; stop
-- outright if one ever did.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM p_user WHERE split_part(email, '@', 1) IN ('admin', 'alice', 'joe', 'bob', 'mallory')) THEN
    RAISE EXCEPTION 'purge would touch a demo account or the Platform Admin';
  END IF;
END $$;

SELECT 'refuse', 'organization ' || slug, why FROM p_refused_org ORDER BY slug;
SELECT 'refuse', 'account ' || email, 'a kept organization still refers to it' FROM p_refused_user ORDER BY email;
SELECT 'keep', coalesce(email, id) FROM users WHERE id NOT IN (SELECT id FROM p_user) AND id NOT IN (SELECT id FROM p_refused_user) ORDER BY 2;

CREATE TEMP TABLE p_ws ON COMMIT DROP AS SELECT id FROM workspaces WHERE org_id IN (SELECT id FROM p_org);
CREATE TEMP TABLE p_op ON COMMIT DROP AS SELECT id FROM operations WHERE org_id IN (SELECT id FROM p_org);
CREATE TEMP TABLE p_grant ON COMMIT DROP AS SELECT id FROM organization_cluster_grants WHERE org_id IN (SELECT id FROM p_org);
CREATE TEMP TABLE p_netpool ON COMMIT DROP AS SELECT id FROM network_pools WHERE org_id IN (SELECT id FROM p_org);
CREATE TEMP TABLE p_group ON COMMIT DROP AS SELECT id FROM groups WHERE org_id IN (SELECT id FROM p_org);
SELECT 'kc-group', machine_name FROM groups WHERE id IN (SELECT id FROM p_group) ORDER BY 2;

-- Children first, so every count is the rows that table loses.
WITH d AS (DELETE FROM outbox_events WHERE operation_id IN (SELECT id FROM p_op) RETURNING 1) SELECT 'delete', 'outbox_events', count(*) FROM d;
WITH d AS (DELETE FROM operations WHERE id IN (SELECT id FROM p_op) RETURNING 1) SELECT 'delete', 'operations', count(*) FROM d;
WITH d AS (DELETE FROM workspace_routes WHERE workspace_id IN (SELECT id FROM p_ws) RETURNING 1) SELECT 'delete', 'workspace_routes', count(*) FROM d;
WITH d AS (DELETE FROM workspace_namespaces WHERE workspace_id IN (SELECT id FROM p_ws) RETURNING 1) SELECT 'delete', 'workspace_namespaces', count(*) FROM d;
WITH d AS (DELETE FROM workspace_group_bindings WHERE workspace_id IN (SELECT id FROM p_ws) RETURNING 1) SELECT 'delete', 'workspace_group_bindings', count(*) FROM d;
WITH d AS (DELETE FROM workspace_node_reservations WHERE workspace_id IN (SELECT id FROM p_ws) RETURNING 1) SELECT 'delete', 'workspace_node_reservations', count(*) FROM d;
WITH d AS (DELETE FROM network_allocations WHERE workspace_id IN (SELECT id FROM p_ws) OR pool_id IN (SELECT id FROM p_netpool) RETURNING 1) SELECT 'delete', 'network_allocations', count(*) FROM d;
WITH d AS (DELETE FROM workspaces WHERE id IN (SELECT id FROM p_ws) RETURNING 1) SELECT 'delete', 'workspaces', count(*) FROM d;
WITH d AS (DELETE FROM dedicated_node_grants WHERE org_id IN (SELECT id FROM p_org) RETURNING 1) SELECT 'delete', 'dedicated_node_grants', count(*) FROM d;
WITH d AS (DELETE FROM cluster_grant_shared_pools WHERE grant_id IN (SELECT id FROM p_grant) RETURNING 1) SELECT 'delete', 'cluster_grant_shared_pools', count(*) FROM d;
WITH d AS (DELETE FROM cluster_grant_network_pools WHERE grant_id IN (SELECT id FROM p_grant) OR pool_id IN (SELECT id FROM p_netpool) RETURNING 1) SELECT 'delete', 'cluster_grant_network_pools', count(*) FROM d;
WITH d AS (DELETE FROM network_pools WHERE id IN (SELECT id FROM p_netpool) RETURNING 1) SELECT 'delete', 'network_pools', count(*) FROM d;
WITH d AS (DELETE FROM organization_cluster_grants WHERE id IN (SELECT id FROM p_grant) RETURNING 1) SELECT 'delete', 'organization_cluster_grants', count(*) FROM d;
WITH d AS (DELETE FROM group_members WHERE group_id IN (SELECT id FROM p_group) OR user_id IN (SELECT id FROM p_user) RETURNING 1) SELECT 'delete', 'group_members', count(*) FROM d;
WITH d AS (DELETE FROM groups WHERE id IN (SELECT id FROM p_group) RETURNING 1) SELECT 'delete', 'groups', count(*) FROM d;
WITH d AS (DELETE FROM invitations WHERE org_id IN (SELECT id FROM p_org) RETURNING 1) SELECT 'delete', 'invitations', count(*) FROM d;
WITH d AS (DELETE FROM ownership_transfers WHERE org_id IN (SELECT id FROM p_org) RETURNING 1) SELECT 'delete', 'ownership_transfers', count(*) FROM d;
WITH d AS (DELETE FROM organization_members WHERE org_id IN (SELECT id FROM p_org) OR user_id IN (SELECT id FROM p_user) RETURNING 1) SELECT 'delete', 'organization_members', count(*) FROM d;
WITH d AS (DELETE FROM organization_entitlements WHERE org_id IN (SELECT id FROM p_org) RETURNING 1) SELECT 'delete', 'organization_entitlements', count(*) FROM d;
WITH d AS (DELETE FROM audit_events WHERE org_id IN (SELECT id FROM p_org) OR actor_user_id IN (SELECT id FROM p_user)
  OR (target_type IN ('User', 'user') AND target_id IN (SELECT id FROM p_user)) RETURNING 1) SELECT 'delete', 'audit_events', count(*) FROM d;
WITH d AS (DELETE FROM organizations WHERE id IN (SELECT id FROM p_org) RETURNING 1) SELECT 'delete', 'organizations', count(*) FROM d;
WITH d AS (DELETE FROM sessions WHERE user_id IN (SELECT id FROM p_user) RETURNING 1) SELECT 'delete', 'sessions', count(*) FROM d;
WITH d AS (DELETE FROM idempotency_keys WHERE user_id IN (SELECT id FROM p_user) RETURNING 1) SELECT 'delete', 'idempotency_keys', count(*) FROM d;
WITH d AS (DELETE FROM users WHERE id IN (SELECT id FROM p_user) RETURNING 1) SELECT 'delete', 'users', count(*) FROM d;
-- For Keycloak: the accounts gone here, by email (groups are listed above).
SELECT 'kc-user', email FROM p_user ORDER BY email;

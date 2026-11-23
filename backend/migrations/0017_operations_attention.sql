-- +goose Up

-- What Operations → Needs attention lists, and the Health page counts: a
-- Failed or Blocked operation that is still the last word on its target. A
-- later operation on the same target supersedes it (a retry, a new change,
-- the delete); so does a target that is gone: a Deleted Workspace, a
-- revoked node grant, a Deleted organization. Those stay under All.
CREATE VIEW operations_attention AS
SELECT o.*
FROM operations o
WHERE o.state IN ('Failed', 'Blocked')
  AND NOT EXISTS (SELECT 1 FROM operations l
                  WHERE l.resource_type = o.resource_type AND l.resource_id = o.resource_id AND l.id > o.id)
  AND NOT (o.resource_type = 'Workspace' AND EXISTS (
          SELECT 1 FROM workspaces w WHERE w.id = o.resource_id AND w.state = 'Deleted'))
  AND NOT (o.resource_type = 'NodeGrant' AND EXISTS (
          SELECT 1 FROM dedicated_node_grants g WHERE g.id = o.resource_id AND g.revoked_at IS NOT NULL))
  AND NOT EXISTS (SELECT 1 FROM organizations r WHERE r.id = o.org_id AND r.status = 'Deleted');

-- +goose Down
DROP VIEW operations_attention;

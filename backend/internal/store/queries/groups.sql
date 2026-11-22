-- name: CreateGroup :exec
-- Starts Pending: the group sync worker creates it in Keycloak (M4).
INSERT INTO groups (id, org_id, display_name, machine_name) VALUES ($1, $2, $3, $4);

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id) VALUES ($1, $2);

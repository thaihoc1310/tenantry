-- name: UpsertUserByIdentity :one
-- First login creates the account, later ones refresh its snapshot of the
-- profile. (issuer, subject) is the identity: email and name can change
-- (PRODUCT-PLAN §4.1). ON CONFLICT makes two concurrent first logins
-- end with one row; status is never touched here.
INSERT INTO users (id, issuer, subject, email, display_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (issuer, subject) DO UPDATE
SET email = EXCLUDED.email, display_name = EXCLUDED.display_name, updated_at = now()
RETURNING *;

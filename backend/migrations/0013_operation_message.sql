-- +goose Up

-- What the cluster reports while an operation runs: the Conditions of the
-- CR that are not True yet (04-state-machines §1.1). A Degraded CR during
-- Provisioning or Updating keeps the operation Running; this says why it
-- is taking long, so nobody waits blind for the timeout.
ALTER TABLE operations ADD COLUMN message text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE operations DROP COLUMN message;

-- Commands are actionable for a bounded period. Persist the deadline so
-- polling and acknowledgement enforce the same terminal state across restarts
-- and concurrent writers.

ALTER TABLE device_runtime_commands
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Existing pending commands predate enforcement and have no safe deadline
-- evidence. Expire them once rather than allowing an unbounded stale action.
UPDATE device_runtime_commands
SET status = 'expired'
WHERE status IN ('pending', 'delivered')
  AND expires_at IS NULL;

ALTER TABLE device_runtime_commands
    ALTER COLUMN expires_at SET DEFAULT (NOW() + INTERVAL '15 minutes');

CREATE INDEX IF NOT EXISTS device_runtime_commands_pending_expiry_idx
    ON device_runtime_commands(device_id, created_at)
    WHERE status IN ('pending', 'delivered');

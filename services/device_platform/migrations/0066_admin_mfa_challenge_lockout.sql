-- Keep the shared failure budget on the existing challenge table so account
-- deletion and retention already cover it. A row with a NULL challenge_hash is
-- a synthetic per-account state row; real challenges always have a hash.
ALTER TABLE admin_mfa_challenges
    ALTER COLUMN challenge_hash DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS failed_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_at TIMESTAMPTZ;

ALTER TABLE admin_mfa_challenges
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_failed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE admin_mfa_challenges
SET updated_at = COALESCE(updated_at, created_at)
WHERE updated_at IS NULL;

-- The account state row is not an expiring challenge. Keeping it at infinity
-- prevents the privacy retention worker from deleting an active lock or a
-- partially consumed failure budget.
UPDATE admin_mfa_challenges
SET expires_at = 'infinity'
WHERE challenge_hash IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS admin_mfa_challenges_account_state_uidx
    ON admin_mfa_challenges(parent_account_id)
    WHERE challenge_hash IS NULL;

CREATE INDEX IF NOT EXISTS admin_mfa_challenges_account_lock_idx
    ON admin_mfa_challenges(parent_account_id, locked_until DESC)
    WHERE challenge_hash IS NULL AND locked_until IS NOT NULL;

ALTER TABLE admin_mfa_challenges
    DROP CONSTRAINT IF EXISTS admin_mfa_challenges_failed_attempts_non_negative;

ALTER TABLE admin_mfa_challenges
    ADD CONSTRAINT admin_mfa_challenges_failed_attempts_non_negative
    CHECK (failed_attempts >= 0);

CREATE INDEX IF NOT EXISTS admin_mfa_challenges_active_account_idx
    ON admin_mfa_challenges(parent_account_id, expires_at DESC)
    WHERE challenge_hash IS NOT NULL
      AND consumed_at IS NULL
      AND locked_at IS NULL;

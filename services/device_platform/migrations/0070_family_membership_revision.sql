-- Family membership and invitation mutations share one optimistic-concurrency
-- revision. The row is created lazily by the repository transaction, so old
-- families do not need a data backfill before the first management write.

CREATE TABLE IF NOT EXISTS family_membership_revisions (
    family_id UUID PRIMARY KEY
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT family_membership_revisions_positive
        CHECK (revision >= 1)
);

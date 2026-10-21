-- Growth reports summarise one child's week or month so a guardian does not
-- have to reconstruct progress from raw records.
--
-- The row holds only aggregates, never the underlying records: a report may be
-- pushed to a guardian's phone and shown next to a name, so it must not carry
-- conversation text, memory values or any other child content. The metrics
-- column keeps the numbers that the summary sentence was derived from, which
-- lets the console explain a number without recomputing it from live tables
-- whose contents have since changed.
CREATE TABLE IF NOT EXISTS growth_reports (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID NOT NULL,
    period_type TEXT NOT NULL,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    metrics JSONB NOT NULL DEFAULT '{}'::JSONB,
    summary TEXT NOT NULL DEFAULT '',
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT growth_reports_period_type_valid
        CHECK (period_type IN ('weekly', 'monthly')),
    CONSTRAINT growth_reports_period_order CHECK (period_end > period_start),
    CONSTRAINT growth_reports_summary_length CHECK (char_length(summary) <= 600),
    -- One report per child per period: the generator runs on a timer and may
    -- retry, and a duplicate would show the same week twice in the guardian's
    -- list.
    CONSTRAINT growth_reports_period_unique
        UNIQUE (child_id, period_type, period_start)
);

-- The guardian list always asks for one child newest-first, and the admin
-- console browses a whole family, so both orders need an index.
CREATE INDEX IF NOT EXISTS growth_reports_child_period_idx
    ON growth_reports (child_id, period_type, period_start DESC);

CREATE INDEX IF NOT EXISTS growth_reports_family_generated_idx
    ON growth_reports (family_id, generated_at DESC);

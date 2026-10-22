-- Learning assessment: one bounded run of English practice where the
-- difficulty follows the child's answers.
--
-- A session is an envelope: which lesson, at what level, how many answers were
-- right, and why it ended. The answers themselves live in assessment_answers.
-- Nothing here stores audio or a transcript of the child's voice; target_text is
-- the lesson content the child was asked to say, never what they said.
CREATE TABLE IF NOT EXISTS assessment_sessions (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts (id) ON DELETE CASCADE,
    -- A session without a child is a family-level session. The single-active
    -- rule below then applies to the family as a whole rather than to a child.
    child_id UUID REFERENCES child_profiles (id) ON DELETE SET NULL,
    lesson_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'completed', 'abandoned')),
    difficulty SMALLINT NOT NULL CHECK (difficulty BETWEEN 1 AND 5),
    item_index INT NOT NULL DEFAULT 0,
    correct_count INT NOT NULL DEFAULT 0,
    answered_count INT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL,
    -- The deadline is fixed at creation so the device and the worker enforce the
    -- same instant, and an answer arriving after it is refused even before the
    -- sweep runs.
    expires_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    finish_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- An answers record can never claim more correct answers than it has
    -- answers, and a negative count is meaningless.
    CONSTRAINT assessment_sessions_counts_non_negative CHECK (
        item_index >= 0
        AND correct_count >= 0
        AND answered_count >= 0
        AND correct_count <= answered_count
        AND answered_count <= item_index
    ),
    CONSTRAINT assessment_sessions_time_order CHECK (
        expires_at > started_at
        AND (finished_at IS NULL OR finished_at >= started_at)
    ),
    -- A terminal session must say when and why it ended; a live one must not.
    CONSTRAINT assessment_sessions_lifecycle CHECK (
        (state = 'active' AND finished_at IS NULL)
        OR (state IN ('completed', 'abandoned') AND finished_at IS NOT NULL)
    )
);

-- One family (and, when named, one child) can only run one assessment at a
-- time. COALESCE keeps the partial index usable for a family-level session,
-- because NULL values never compare equal in a unique index.
--
-- child_id is nullable rather than NOT NULL on purpose: a family may practise
-- before any child profile exists. The zero UUID is a sentinel for "no child",
-- never a real profile, so one household cannot end up with two live
-- family-level sessions while still allowing one live session per child.
CREATE UNIQUE INDEX IF NOT EXISTS assessment_sessions_active_scope_idx
    ON assessment_sessions (family_id, COALESCE(child_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE state = 'active';

CREATE INDEX IF NOT EXISTS assessment_sessions_family_started_idx
    ON assessment_sessions (family_id, started_at DESC);

-- The expiry worker scans only live sessions, so keep that scan narrow.
CREATE INDEX IF NOT EXISTS assessment_sessions_active_expiry_idx
    ON assessment_sessions (expires_at)
    WHERE state = 'active';

CREATE TABLE IF NOT EXISTS assessment_answers (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES assessment_sessions (id) ON DELETE CASCADE,
    -- Position is zero-based and continues the sequence. It must equal the
    -- session's answered_count at write time; UNIQUE makes a replayed position
    -- a database error rather than a duplicated count.
    position INT NOT NULL CHECK (position >= 0),
    item_type TEXT NOT NULL CHECK (item_type IN ('word', 'sentence')),
    item_id TEXT NOT NULL CHECK (length(item_id) BETWEEN 1 AND 128),
    -- The lesson text the child was asked to say. It is bounded so the column
    -- cannot become free-form storage.
    target_text TEXT NOT NULL CHECK (length(target_text) BETWEEN 1 AND 160),
    score INT NOT NULL CHECK (score BETWEEN 0 AND 100),
    is_correct BOOLEAN NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_id, position)
);

CREATE INDEX IF NOT EXISTS assessment_answers_session_idx
    ON assessment_answers (session_id, position);

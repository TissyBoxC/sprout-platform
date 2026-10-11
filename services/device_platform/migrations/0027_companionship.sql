-- Companionship: growth records and habit reminders.
--
-- Growth records are the guardian-visible timeline of a child's milestones
-- (first poem recited, a completed habit streak, an encouragement moment).
-- They are family-scoped and bounded: each record keeps a short title, an
-- optional bounded note, and the timestamp the milestone happened so the
-- console can render a chronological story without unbounded free text.
--
-- Habit reminders are family-scoped schedules the platform (and later the
-- device) use to nudge a child. A reminder owns its local time of day, a
-- bounded set of weekdays, an enable flag and an optional prompt. Rows are
-- retained across "off" toggles so history and audits survive, and a child is
-- referenced softly (no cascade) so a reminder outliving a profile deletion is
-- still cleanup-able by the family owner.

CREATE TABLE IF NOT EXISTS companionship_growth_records (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID,
    category TEXT NOT NULL,
    title TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT companionship_growth_records_category_valid
        CHECK (category IN ('milestone', 'encouragement', 'habit', 'emotion')),
    CONSTRAINT companionship_growth_records_title_length
        CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT companionship_growth_records_note_length
        CHECK (char_length(note) <= 500)
);

CREATE INDEX IF NOT EXISTS companionship_growth_records_family_idx
    ON companionship_growth_records(family_id, recorded_at DESC);

CREATE TABLE IF NOT EXISTS companionship_habit_reminders (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID,
    label TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    reminder_time TIME NOT NULL,
    weekdays SMALLINT[] NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT companionship_habit_reminders_label_length
        CHECK (char_length(label) BETWEEN 1 AND 40),
    CONSTRAINT companionship_habit_reminders_prompt_length
        CHECK (char_length(prompt) <= 200)
);

CREATE INDEX IF NOT EXISTS companionship_habit_reminders_family_idx
    ON companionship_habit_reminders(family_id, created_at DESC);

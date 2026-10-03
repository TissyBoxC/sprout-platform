ALTER TABLE child_profiles
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'guardian';

ALTER TABLE child_profiles
    ADD CONSTRAINT child_profiles_source_valid
    CHECK (source IN ('guardian', 'registration'));

CREATE INDEX IF NOT EXISTS child_profiles_family_source_idx
    ON child_profiles(family_id, source, created_at);

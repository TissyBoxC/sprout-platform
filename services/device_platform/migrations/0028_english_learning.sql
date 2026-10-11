-- English learning: courses, lessons, words, sentence patterns and progress.
--
-- The platform owns the catalog and the guardian-visible progress; the device
-- plays a lesson and reports a practice attempt. Everything is family-scoped
-- so a guardian only ever reads or writes rows owned by the authenticated
-- family, and each content row keeps a small, bounded payload because the
-- device renders one lesson at a time.
--
-- lesson_id is a stable, human-readable identifier (like content package ids)
-- so the device can cache a lesson by id without a server-round-trip mapping.

CREATE TABLE IF NOT EXISTS english_lessons (
    lesson_id TEXT PRIMARY KEY,
    age_tier TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    sequence INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'published',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT english_lessons_lesson_id_valid
        CHECK (lesson_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'),
    CONSTRAINT english_lessons_age_tier_valid
        CHECK (age_tier IN ('age_3_4', 'age_5_6', 'age_7_8')),
    CONSTRAINT english_lessons_title_length
        CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT english_lessons_summary_length
        CHECK (char_length(summary) <= 200),
    CONSTRAINT english_lessons_status_valid
        CHECK (status IN ('draft', 'published', 'withdrawn'))
);

CREATE INDEX IF NOT EXISTS english_lessons_age_idx
    ON english_lessons(age_tier, sequence, lesson_id);

-- A word is a single vocabulary item under a lesson: the English term, its
-- phonetic hint, the Chinese gloss, and an optional example sentence. The
-- position keeps the presentation order stable.
CREATE TABLE IF NOT EXISTS english_words (
    id UUID PRIMARY KEY,
    lesson_id TEXT NOT NULL REFERENCES english_lessons(lesson_id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    term TEXT NOT NULL,
    phonetic TEXT NOT NULL DEFAULT '',
    translation TEXT NOT NULL,
    example TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT english_words_term_length
        CHECK (char_length(term) BETWEEN 1 AND 40),
    CONSTRAINT english_words_phonetic_length
        CHECK (char_length(phonetic) <= 40),
    CONSTRAINT english_words_translation_length
        CHECK (char_length(translation) BETWEEN 1 AND 60),
    CONSTRAINT english_words_example_length
        CHECK (char_length(example) <= 160),
    CONSTRAINT english_words_position_unique
        UNIQUE (lesson_id, position)
);

CREATE INDEX IF NOT EXISTS english_words_lesson_idx
    ON english_words(lesson_id, position);

-- A sentence pattern is a reusable spoken structure under a lesson, rendered
-- with a prompt and a model answer the device reads aloud.
CREATE TABLE IF NOT EXISTS english_sentences (
    id UUID PRIMARY KEY,
    lesson_id TEXT NOT NULL REFERENCES english_lessons(lesson_id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    pattern TEXT NOT NULL,
    prompt TEXT NOT NULL,
    model_answer TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT english_sentences_pattern_length
        CHECK (char_length(pattern) BETWEEN 1 AND 120),
    CONSTRAINT english_sentences_prompt_length
        CHECK (char_length(prompt) BETWEEN 1 AND 120),
    CONSTRAINT english_sentences_model_answer_length
        CHECK (char_length(model_answer) BETWEEN 1 AND 160),
    CONSTRAINT english_sentences_position_unique
        UNIQUE (lesson_id, position)
);

CREATE INDEX IF NOT EXISTS english_sentences_lesson_idx
    ON english_sentences(lesson_id, position);

-- Practice attempts are the pronunciation-feedback record. Each row keeps a
-- bounded, structured score (0-100) plus the recognized text so the guardian
-- can see progress and the device can decide what to replay. Rows are
-- family-scoped and pruned per child by the writer so the table stays bounded.
CREATE TABLE IF NOT EXISTS english_practice_attempts (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID,
    lesson_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    item_type TEXT NOT NULL,
    target_text TEXT NOT NULL,
    recognized_text TEXT NOT NULL DEFAULT '',
    score INTEGER NOT NULL,
    passed BOOLEAN NOT NULL DEFAULT FALSE,
    practiced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT english_practice_attempts_item_type_valid
        CHECK (item_type IN ('word', 'sentence')),
    CONSTRAINT english_practice_attempts_score_range
        CHECK (score BETWEEN 0 AND 100),
    CONSTRAINT english_practice_attempts_target_length
        CHECK (char_length(target_text) BETWEEN 1 AND 160),
    CONSTRAINT english_practice_attempts_recognized_length
        CHECK (char_length(recognized_text) <= 200)
);

CREATE INDEX IF NOT EXISTS english_practice_attempts_family_idx
    ON english_practice_attempts(family_id, practiced_at DESC);

CREATE INDEX IF NOT EXISTS english_practice_attempts_child_idx
    ON english_practice_attempts(family_id, child_id, practiced_at DESC);

package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDecodeSentencesKeepsModelAnswer(t *testing.T) {
	data := []byte(`[
		{
			"id": "sentence_apple",
			"lesson_id": "lesson_fruit",
			"position": 1,
			"pattern": "I like ___.",
			"prompt": "说出你喜欢苹果",
			"ModelAnswer": "I like apples."
		}
	]`)

	var sentences []domain.Sentence
	if err := decodeSentences(data, &sentences); err != nil {
		t.Fatalf("decode sentences: %v", err)
	}
	if len(sentences) != 1 || sentences[0].ModelAnswer != "I like apples." {
		t.Fatalf("decoded sentences = %+v, want preserved ModelAnswer", sentences)
	}
}

func TestDecodeSentencesReturnsEmptySliceForNull(t *testing.T) {
	var sentences []domain.Sentence
	if err := decodeSentences([]byte("null"), &sentences); err != nil {
		t.Fatalf("decode null sentences: %v", err)
	}
	if sentences == nil || len(sentences) != 0 {
		t.Fatalf("null sentences = %#v, want non-nil empty slice", sentences)
	}
}

func TestInsertAttemptRejectsInvalidIDsBeforeDatabase(t *testing.T) {
	executor := &recordingAttemptExecutor{}
	attempt := validRepositoryAttempt()
	attempt.FamilyID = "not-a-uuid"

	err := insertAttempt(context.Background(), executor, attempt)
	if !errors.Is(err, domain.ErrInvalidAttempt) {
		t.Fatalf("error = %v, want ErrInvalidAttempt", err)
	}
	if executor.execCalls != 0 {
		t.Fatalf("database exec calls = %d, want 0", executor.execCalls)
	}
}

func TestInsertAttemptRejectsNilAttempt(t *testing.T) {
	executor := &recordingAttemptExecutor{}
	err := insertAttempt(context.Background(), executor, nil)
	if !errors.Is(err, domain.ErrInvalidAttempt) {
		t.Fatalf("error = %v, want ErrInvalidAttempt", err)
	}
}

func TestInsertAttemptRejectsMissingLessonWithStableError(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 0"),
		queryRows: []attemptRow{
			{values: []any{false}},
		},
	}
	err := insertAttempt(
		context.Background(),
		executor,
		validRepositoryAttempt(),
	)
	if !errors.Is(err, domain.ErrLessonNotFound) {
		t.Fatalf("error = %v, want ErrLessonNotFound", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 1 {
		t.Fatalf(
			"calls = exec:%d query:%d, want 1/1",
			executor.execCalls,
			executor.queryCalls,
		)
	}
}

func TestInsertAttemptDoesNotWriteOnMissingLesson(t *testing.T) {
	executor := &recordingAttemptExecutor{
		queryRows: []attemptRow{{values: []any{false}}},
	}
	err := insertAttempt(
		context.Background(),
		executor,
		validRepositoryAttempt(),
	)
	if !errors.Is(err, domain.ErrLessonNotFound) {
		t.Fatalf("error = %v, want ErrLessonNotFound", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 1 {
		t.Fatalf("calls = exec:%d query:%d, want 1/1", executor.execCalls, executor.queryCalls)
	}
}

func TestInsertAttemptRejectsMissingChildWithStableError(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 0"),
		queryRows: []attemptRow{
			{values: []any{true}},
			{err: pgx.ErrNoRows},
		},
	}
	attempt := validRepositoryAttempt()
	attempt.ChildID = "00000000-0000-0000-0000-000000000002"
	err := insertAttempt(context.Background(), executor, attempt)
	if !errors.Is(err, domain.ErrChildNotFound) {
		t.Fatalf("error = %v, want ErrChildNotFound", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 2 {
		t.Fatalf(
			"calls = exec:%d query:%d, want 1/2",
			executor.execCalls,
			executor.queryCalls,
		)
	}
}

func TestInsertAttemptRejectsCrossFamilyChild(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 0"),
		queryRows: []attemptRow{
			{values: []any{true}},
			{values: []any{"00000000-0000-0000-0000-000000000099"}},
		},
	}
	attempt := validRepositoryAttempt()
	attempt.ChildID = "00000000-0000-0000-0000-000000000002"
	err := insertAttempt(context.Background(), executor, attempt)
	if !errors.Is(err, domain.ErrChildFamilyMismatch) {
		t.Fatalf("error = %v, want ErrChildFamilyMismatch", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 2 {
		t.Fatalf(
			"calls = exec:%d query:%d, want 1/2",
			executor.execCalls,
			executor.queryCalls,
		)
	}
}

func TestInsertAttemptRejectsUnknownOrMismatchedPracticeItem(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 0"),
		queryRows: []attemptRow{
			{values: []any{true}},
		},
	}
	err := insertAttempt(
		context.Background(),
		executor,
		validRepositoryAttempt(),
	)
	if !errors.Is(err, domain.ErrPracticeItemNotFound) {
		t.Fatalf("error = %v, want ErrPracticeItemNotFound", err)
	}
}

func TestInsertAttemptAcceptsValidAttempt(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 1"),
		insertRows: []attemptRow{
			{values: []any{1}},
		},
	}
	err := insertAttempt(
		context.Background(),
		executor,
		validRepositoryAttempt(),
	)
	if err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	if executor.execCalls != 1 || executor.queryCalls != 0 {
		t.Fatalf("calls = exec:%d query:%d, want 1/0", executor.execCalls, executor.queryCalls)
	}
}

func TestInsertAttemptSQLScopesSelectedLesson(t *testing.T) {
	executor := &recordingAttemptExecutor{
		execTag: pgconn.NewCommandTag("INSERT 0 1"),
		insertRows: []attemptRow{
			{values: []any{1}},
		},
	}
	if err := insertAttempt(
		context.Background(),
		executor,
		validRepositoryAttempt(),
	); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	if len(executor.insertSQL) != 1 {
		t.Fatalf("insert queries = %d, want 1", len(executor.insertSQL))
	}
	if !strings.Contains(
		executor.insertSQL[0],
		"FROM parent_accounts AS parent\n\t\t\tJOIN selected_lesson AS lesson ON TRUE",
	) {
		t.Fatalf("insert SQL does not scope lesson in candidate query: %s", executor.insertSQL[0])
	}
}

func TestCreateAttemptRejectsCrossTenantAndMissingResources(t *testing.T) {
	dsn := os.Getenv("DEVICE_PLATFORM_DATABASE_DSN")
	if dsn == "" {
		t.Skip("DEVICE_PLATFORM_DATABASE_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer store.Close(context.Background())
	pool := store.Pool()
	repository := NewPostgresRepository(pool)

	fixture := seedEnglishAttemptFixture(t, ctx, pool)
	valid := validRepositoryAttempt()
	valid.FamilyID = fixture.ownerFamilyID
	valid.ChildID = fixture.ownerChildID
	valid.LessonID = fixture.lessonID
	valid.ItemID = fixture.wordID
	if err := repository.CreateAttempt(ctx, valid); err != nil {
		// A prior failed run can leave the deterministic attempt id behind.
		// Clean it and retry once so the test remains repeatable.
		if _, cleanupErr := pool.Exec(
			ctx,
			`DELETE FROM english_practice_attempts WHERE id = $1`,
			valid.ID,
		); cleanupErr != nil {
			t.Fatalf("clear prior attempt: %v", cleanupErr)
		}
		if retryErr := repository.CreateAttempt(ctx, valid); retryErr != nil {
			t.Fatalf("create valid attempt: %v", retryErr)
		}
	}

	cases := []struct {
		name   string
		mutate func(*domain.PracticeAttempt)
		want   error
	}{
		{
			name: "missing lesson",
			mutate: func(attempt *domain.PracticeAttempt) {
				attempt.LessonID = "lesson_missing"
			},
			want: domain.ErrLessonNotFound,
		},
		{
			name: "cross family child",
			mutate: func(attempt *domain.PracticeAttempt) {
				attempt.ChildID = fixture.otherChildID
			},
			want: domain.ErrChildFamilyMismatch,
		},
		{
			name: "missing child",
			mutate: func(attempt *domain.PracticeAttempt) {
				attempt.ChildID = "00000000-0000-0000-0000-000000000999"
			},
			want: domain.ErrChildNotFound,
		},
		{
			name: "missing item",
			mutate: func(attempt *domain.PracticeAttempt) {
				attempt.ItemID = "00000000-0000-0000-0000-000000000998"
			},
			want: domain.ErrPracticeItemNotFound,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			attempt := validRepositoryAttempt()
			attempt.ID = deterministicUUID(testCase.name)
			attempt.FamilyID = fixture.ownerFamilyID
			attempt.ChildID = fixture.ownerChildID
			attempt.LessonID = fixture.lessonID
			attempt.ItemID = fixture.wordID
			testCase.mutate(attempt)
			err := repository.CreateAttempt(ctx, attempt)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestListProgressAggregatesBeyondListAttemptLimit(t *testing.T) {
	dsn := os.Getenv("DEVICE_PLATFORM_DATABASE_DSN")
	if dsn == "" {
		t.Skip("DEVICE_PLATFORM_DATABASE_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer store.Close(context.Background())
	pool := store.Pool()
	fixture := seedEnglishAttemptFixture(t, ctx, pool)
	repository := NewPostgresRepository(pool)

	if _, err := pool.Exec(
		ctx,
		`DELETE FROM english_practice_attempts
		 WHERE family_id = $1 AND child_id = $2`,
		fixture.ownerFamilyID,
		fixture.ownerChildID,
	); err != nil {
		t.Fatalf("clear attempts: %v", err)
	}
	for index := 0; index < 505; index++ {
		attempt := validRepositoryAttempt()
		attempt.ID = deterministicUUID("progress-" + string(rune(index)))
		attempt.FamilyID = fixture.ownerFamilyID
		attempt.ChildID = fixture.ownerChildID
		attempt.LessonID = fixture.lessonID
		attempt.ItemID = fixture.wordID
		attempt.Score = index % 101
		attempt.Passed = attempt.Score >= domain.PassingScore
		attempt.PracticedAt = time.Date(2026, 10, 18, 8, 0, index, 0, time.UTC)
		if err := repository.CreateAttempt(ctx, attempt); err != nil {
			t.Fatalf("seed attempt %d: %v", index, err)
		}
	}

	lessons, items, err := repository.ListProgress(
		ctx,
		fixture.ownerFamilyID,
		domain.AttemptQuery{
			ChildID: fixture.ownerChildID,
			Limit:   1,
		},
	)
	if err != nil {
		t.Fatalf("list progress: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Attempts != 505 {
		t.Fatalf("lesson progress = %+v, want 505 attempts", lessons)
	}
	if len(items) != 1 || items[0].Attempts != 505 {
		t.Fatalf("item progress = %+v, want 505 attempts", items)
	}
}

type englishAttemptFixture struct {
	ownerFamilyID string
	ownerChildID  string
	otherChildID  string
	lessonID      string
	wordID        string
}

func seedEnglishAttemptFixture(
	t *testing.T,
	ctx context.Context,
	pool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
) englishAttemptFixture {
	t.Helper()
	fixture := englishAttemptFixture{
		ownerFamilyID: "00000000-0000-0000-0000-000000000401",
		ownerChildID:  "00000000-0000-0000-0000-000000000402",
		otherChildID:  "00000000-0000-0000-0000-000000000403",
		lessonID:      "lesson_integrity_test",
		wordID:        "00000000-0000-0000-0000-000000000404",
	}
	for index, familyID := range []string{
		fixture.ownerFamilyID,
		"00000000-0000-0000-0000-000000000405",
	} {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO parent_accounts (
			     id, email, phone, password_hash, display_name,
			     guardian_consent_version, guardian_consented_at
			 ) VALUES ($1, $2, $3, 'test-hash', 'english integrity test',
			           '1.0.0', NOW())
			 ON CONFLICT (id) DO UPDATE
			 SET updated_at = NOW()`,
			familyID,
			"english-integrity-"+familyID+"@example.test",
			"english-integrity-"+familyID,
		); err != nil {
			t.Fatalf("seed family %d: %v", index, err)
		}
	}
	for index, childID := range []string{fixture.ownerChildID, fixture.otherChildID} {
		familyID := fixture.ownerFamilyID
		if index == 1 {
			familyID = "00000000-0000-0000-0000-000000000405"
		}
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO child_profiles (
			     id, family_id, nickname, age_tier, allowed_categories,
			     source, created_at, updated_at
			 ) VALUES ($1, $2, 'test-child', 'age_3_4',
			           ARRAY['english'], 'manual', NOW(), NOW())
			 ON CONFLICT (id) DO UPDATE
			 SET family_id = EXCLUDED.family_id,
			     updated_at = NOW()`,
			childID,
			familyID,
		); err != nil {
			t.Fatalf("seed child %d: %v", index, err)
		}
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO english_lessons (
		     lesson_id, age_tier, title, status
		 ) VALUES ($1, 'age_3_4', 'integrity test', 'published')
		 ON CONFLICT (lesson_id) DO UPDATE
		 SET updated_at = NOW()`,
		fixture.lessonID,
	); err != nil {
		t.Fatalf("seed lesson: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO english_words (
		     id, lesson_id, position, term, translation
		 ) VALUES ($1, $2, 1, 'apple', '苹果')
		 ON CONFLICT (id) DO UPDATE
		 SET lesson_id = EXCLUDED.lesson_id`,
		fixture.wordID,
		fixture.lessonID,
	); err != nil {
		t.Fatalf("seed word: %v", err)
	}
	return fixture
}

func deterministicUUID(seed string) string {
	// The test only needs a stable UUID; deriving from a SHA-1 namespace keeps
	// repeated runs idempotent without adding a random dependency.
	namespace := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	return uuid.NewSHA1(namespace, []byte(seed)).String()
}

func validRepositoryAttempt() *domain.PracticeAttempt {
	now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	return &domain.PracticeAttempt{
		ID:             "00000000-0000-0000-0000-000000000001",
		FamilyID:       "00000000-0000-0000-0000-000000000010",
		ChildID:        "",
		LessonID:       "lesson_fruit",
		ItemID:         "00000000-0000-0000-0000-000000000020",
		ItemType:       domain.ItemTypeWord,
		TargetText:     "apple",
		RecognizedText: "apple",
		Score:          80,
		Passed:         true,
		PracticedAt:    now,
		CreatedAt:      now,
	}
}

type attemptRow struct {
	values []any
	err    error
}

func (row attemptRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range row.values {
		switch target := dest[index].(type) {
		case *bool:
			*target = value.(bool)
		case *string:
			*target = value.(string)
		case *int:
			*target = value.(int)
		default:
			panic("unsupported scan target")
		}
	}
	return nil
}

type recordingAttemptExecutor struct {
	execCalls  int
	queryCalls int
	execTag    pgconn.CommandTag
	queryRows  []attemptRow
	insertRows []attemptRow
	insertSQL  []string
}

type attemptRows struct {
	rows    []attemptRow
	index   int
	tag     pgconn.CommandTag
	scanErr error
}

func (rows *attemptRows) Close() {}

func (rows *attemptRows) Err() error { return rows.scanErr }

func (rows *attemptRows) CommandTag() pgconn.CommandTag { return rows.tag }

func (rows *attemptRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (rows *attemptRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *attemptRows) Scan(dest ...any) error {
	if rows.index == 0 || rows.index > len(rows.rows) {
		return errors.New("scan called outside a row")
	}
	return rows.rows[rows.index-1].Scan(dest...)
}

func (rows *attemptRows) Values() ([]any, error) {
	return nil, errors.New("values is not implemented")
}

func (rows *attemptRows) RawValues() [][]byte {
	return nil
}

func (rows *attemptRows) Conn() *pgx.Conn {
	return nil
}

func (executor *recordingAttemptExecutor) Query(
	_ context.Context,
	sql string,
	_ ...any,
) (pgx.Rows, error) {
	executor.execCalls++
	executor.insertSQL = append(executor.insertSQL, sql)
	return &attemptRows{
		rows: executor.insertRows,
		tag:  executor.execTag,
	}, nil
}

func (executor *recordingAttemptExecutor) QueryRow(
	_ context.Context,
	_ string,
	_ ...any,
) pgx.Row {
	executor.queryCalls++
	if len(executor.queryRows) > 0 {
		row := executor.queryRows[0]
		executor.queryRows = executor.queryRows[1:]
		return row
	}
	return attemptRow{}
}

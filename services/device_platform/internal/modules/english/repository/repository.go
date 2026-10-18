// Package repository persists the English learning catalog and progress.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for English learning data.
type Repository interface {
	ListLessons(ctx context.Context, ageTier string) ([]domain.Lesson, error)
	GetLesson(ctx context.Context, lessonID string) (*domain.Lesson, error)

	CreateAttempt(ctx context.Context, attempt *domain.PracticeAttempt) error
	ListAttempts(
		ctx context.Context,
		familyID string,
		query domain.AttemptQuery,
	) ([]domain.PracticeAttempt, error)
}

// ProgressReader returns the complete family-scoped rollup. It deliberately
// has no attempt limit: progress is an aggregate over every stored attempt,
// unlike the bounded browsing API.
type ProgressReader interface {
	ListProgress(
		ctx context.Context,
		familyID string,
		query domain.AttemptQuery,
	) ([]domain.LessonProgress, []domain.ProgressItem, error)
}

// CatalogReader is the optional batch catalogue capability. Core Repository
// users that predate the batch query keep working unchanged.
type CatalogReader interface {
	ListLessonCatalog(ctx context.Context, ageTier string, limit int) ([]domain.Lesson, error)
}

// CatalogMaxLessons bounds the complete device catalogue. The summary list
// and the full catalogue use the same cap so callers never receive a manifest
// larger than the device can cache.
const CatalogMaxLessons = 100

// PostgresRepository is the PostgreSQL-backed English learning repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an English learning repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ListLessons returns every published lesson for one age tier, ordered by the
// authored sequence so the device presents them in a stable teaching order.
func (r *PostgresRepository) ListLessons(
	ctx context.Context,
	ageTier string,
) ([]domain.Lesson, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			lesson_id,
			age_tier,
			title,
			summary,
			sequence,
			status,
			created_at,
			updated_at
		FROM english_lessons
		WHERE status = 'published'
			AND ($1 = '' OR age_tier = $1)
		ORDER BY age_tier, sequence, lesson_id
		LIMIT $2
	`, ageTier, CatalogMaxLessons)
	if err != nil {
		return nil, fmt.Errorf("list english lessons: %w", err)
	}
	defer rows.Close()

	lessons := make([]domain.Lesson, 0, 16)
	for rows.Next() {
		var lesson domain.Lesson
		if err := rows.Scan(
			&lesson.LessonID,
			&lesson.AgeTier,
			&lesson.Title,
			&lesson.Summary,
			&lesson.Sequence,
			&lesson.Status,
			&lesson.CreatedAt,
			&lesson.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan english lesson: %w", err)
		}
		lessons = append(lessons, lesson)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english lessons: %w", err)
	}
	return lessons, nil
}

// ListLessonCatalog returns at most limit published lessons together with
// their ordered words and sentences. It uses one query and batch-aggregates
// the child rows, so query count is constant regardless of lesson count.
func (r *PostgresRepository) ListLessonCatalog(
	ctx context.Context,
	ageTier string,
	limit int,
) ([]domain.Lesson, error) {
	if limit < 1 || limit > CatalogMaxLessons {
		limit = CatalogMaxLessons
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			lesson.lesson_id,
			lesson.age_tier,
			lesson.title,
			lesson.summary,
			lesson.sequence,
			lesson.status,
			lesson.created_at,
			lesson.updated_at,
			COALESCE(words.items, '[]'::jsonb),
			COALESCE(sentences.items, '[]'::jsonb)
		FROM (
			SELECT
				lesson_id,
				age_tier,
				title,
				summary,
				sequence,
				status,
				created_at,
				updated_at
			FROM english_lessons
			WHERE status = 'published'
				AND ($1 = '' OR age_tier = $1)
			ORDER BY age_tier, sequence, lesson_id
			LIMIT $2
		) AS lesson
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(
				jsonb_build_object(
					'id', word.id,
					'lesson_id', word.lesson_id,
					'position', word.position,
					'term', word.term,
					'phonetic', word.phonetic,
					'translation', word.translation,
					'example', word.example
				)
				ORDER BY word.position, word.id
			) AS items
			FROM english_words AS word
			WHERE word.lesson_id = lesson.lesson_id
		) AS words ON TRUE
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(
				jsonb_build_object(
					'id', sentence.id,
					'lesson_id', sentence.lesson_id,
					'position', sentence.position,
					'pattern', sentence.pattern,
					'prompt', sentence.prompt,
					'ModelAnswer', sentence.model_answer
				)
				ORDER BY sentence.position, sentence.id
			) AS items
			FROM english_sentences AS sentence
			WHERE sentence.lesson_id = lesson.lesson_id
		) AS sentences ON TRUE
		ORDER BY lesson.age_tier, lesson.sequence, lesson.lesson_id
	`, ageTier, limit)
	if err != nil {
		return nil, fmt.Errorf("list english lesson catalog: %w", err)
	}
	defer rows.Close()

	lessons := make([]domain.Lesson, 0, 16)
	for rows.Next() {
		var lesson domain.Lesson
		var wordsJSON []byte
		var sentencesJSON []byte
		if err := rows.Scan(
			&lesson.LessonID,
			&lesson.AgeTier,
			&lesson.Title,
			&lesson.Summary,
			&lesson.Sequence,
			&lesson.Status,
			&lesson.CreatedAt,
			&lesson.UpdatedAt,
			&wordsJSON,
			&sentencesJSON,
		); err != nil {
			return nil, fmt.Errorf("scan english lesson catalog: %w", err)
		}
		if err := decodeWords(wordsJSON, &lesson.Words); err != nil {
			return nil, err
		}
		if err := decodeSentences(sentencesJSON, &lesson.Sentence); err != nil {
			return nil, err
		}
		for index := range lesson.Words {
			lesson.Words[index].LessonID = lesson.LessonID
		}
		for index := range lesson.Sentence {
			lesson.Sentence[index].LessonID = lesson.LessonID
		}
		lessons = append(lessons, lesson)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english lesson catalog: %w", err)
	}
	return lessons, nil
}

func decodeWords(data []byte, target *[]domain.Word) error {
	if len(data) == 0 {
		*target = []domain.Word{}
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode english lesson words: %w", err)
	}
	if *target == nil {
		*target = []domain.Word{}
	}
	return nil
}

func decodeSentences(data []byte, target *[]domain.Sentence) error {
	if len(data) == 0 {
		*target = []domain.Sentence{}
		return nil
	}
	var rows []struct {
		ID          string `json:"id"`
		LessonID    string `json:"lesson_id"`
		Position    int    `json:"position"`
		Pattern     string `json:"pattern"`
		Prompt      string `json:"prompt"`
		ModelAnswer string `json:"ModelAnswer"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return fmt.Errorf("decode english lesson sentences: %w", err)
	}
	*target = make([]domain.Sentence, 0, len(rows))
	for _, row := range rows {
		*target = append(*target, domain.Sentence{
			ID:          row.ID,
			LessonID:    row.LessonID,
			Position:    row.Position,
			Pattern:     row.Pattern,
			Prompt:      row.Prompt,
			ModelAnswer: row.ModelAnswer,
		})
	}
	return nil
}

// GetLesson returns one lesson with its ordered words and sentences.
func (r *PostgresRepository) GetLesson(
	ctx context.Context,
	lessonID string,
) (*domain.Lesson, error) {
	var lesson domain.Lesson
	err := r.pool.QueryRow(ctx, `
		SELECT
			lesson_id,
			age_tier,
			title,
			summary,
			sequence,
			status,
			created_at,
			updated_at
		FROM english_lessons
		WHERE lesson_id = $1
	`, lessonID).Scan(
		&lesson.LessonID,
		&lesson.AgeTier,
		&lesson.Title,
		&lesson.Summary,
		&lesson.Sequence,
		&lesson.Status,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrLessonNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get english lesson: %w", err)
	}

	words, err := r.listWords(ctx, lessonID)
	if err != nil {
		return nil, err
	}
	sentences, err := r.listSentences(ctx, lessonID)
	if err != nil {
		return nil, err
	}
	lesson.Words = words
	lesson.Sentence = sentences
	return &lesson, nil
}

func (r *PostgresRepository) listWords(
	ctx context.Context,
	lessonID string,
) ([]domain.Word, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, lesson_id, position, term, phonetic, translation, example
		FROM english_words
		WHERE lesson_id = $1
		ORDER BY position
	`, lessonID)
	if err != nil {
		return nil, fmt.Errorf("list english words: %w", err)
	}
	defer rows.Close()

	words := make([]domain.Word, 0, 16)
	for rows.Next() {
		var word domain.Word
		if err := rows.Scan(
			&word.ID,
			&word.LessonID,
			&word.Position,
			&word.Term,
			&word.Phonetic,
			&word.Translation,
			&word.Example,
		); err != nil {
			return nil, fmt.Errorf("scan english word: %w", err)
		}
		words = append(words, word)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english words: %w", err)
	}
	return words, nil
}

func (r *PostgresRepository) listSentences(
	ctx context.Context,
	lessonID string,
) ([]domain.Sentence, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, lesson_id, position, pattern, prompt, model_answer
		FROM english_sentences
		WHERE lesson_id = $1
		ORDER BY position
	`, lessonID)
	if err != nil {
		return nil, fmt.Errorf("list english sentences: %w", err)
	}
	defer rows.Close()

	sentences := make([]domain.Sentence, 0, 16)
	for rows.Next() {
		var sentence domain.Sentence
		if err := rows.Scan(
			&sentence.ID,
			&sentence.LessonID,
			&sentence.Position,
			&sentence.Pattern,
			&sentence.Prompt,
			&sentence.ModelAnswer,
		); err != nil {
			return nil, fmt.Errorf("scan english sentence: %w", err)
		}
		sentences = append(sentences, sentence)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english sentences: %w", err)
	}
	return sentences, nil
}

// CreateAttempt inserts one practice attempt.
func (r *PostgresRepository) CreateAttempt(
	ctx context.Context,
	attempt *domain.PracticeAttempt,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin english practice attempt: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if err := insertAttempt(ctx, transaction, attempt); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit english practice attempt: %w", err)
	}
	return nil
}

type execQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func insertAttempt(
	ctx context.Context,
	executor execQuerier,
	attempt *domain.PracticeAttempt,
) error {
	if attempt == nil {
		return domain.ErrInvalidAttempt
	}
	if !isUUID(attempt.ID) || !isUUID(attempt.FamilyID) {
		return domain.ErrInvalidAttempt
	}
	childID := strings.TrimSpace(attempt.ChildID)
	if childID != "" && !isUUID(childID) {
		return domain.ErrInvalidAttempt
	}
	rows, err := executor.Query(ctx, `
		WITH lesson_item AS (
			SELECT word.id::text AS item_id
			FROM english_words AS word
			WHERE word.lesson_id = $4
			  AND $6 = 'word'
			  AND word.id::text = $5
			UNION ALL
			SELECT sentence.id::text AS item_id
			FROM english_sentences AS sentence
			WHERE sentence.lesson_id = $4
			  AND $6 = 'sentence'
			  AND sentence.id::text = $5
		),
		selected_lesson AS (
			SELECT lesson_id
			FROM english_lessons
			WHERE lesson_id = $4
		),
		candidate AS (
			SELECT
				parent.id AS family_id,
				child.id AS child_id,
				lesson.lesson_id,
				item.item_id
			FROM parent_accounts AS parent
			JOIN selected_lesson AS lesson ON TRUE
			JOIN lesson_item AS item ON TRUE
			LEFT JOIN child_profiles AS child
			  ON ($3 <> '' AND child.id::text = $3)
			 AND child.family_id = parent.id
			WHERE parent.id::text = $2
			  AND ($3 = '' OR child.id IS NOT NULL)
		)
		INSERT INTO english_practice_attempts (
			id,
			family_id,
			child_id,
			lesson_id,
			item_id,
			item_type,
			target_text,
			recognized_text,
			score,
			passed,
			practiced_at,
			created_at
		)
		SELECT
			$1, candidate.family_id, candidate.child_id,
			candidate.lesson_id, candidate.item_id,
			$6, $7, $8, $9, $10, $11, $12
		FROM candidate
		RETURNING 1
	`, attempt.ID,
		attempt.FamilyID,
		attempt.ChildID,
		attempt.LessonID,
		attempt.ItemID,
		attempt.ItemType,
		attempt.TargetText,
		attempt.RecognizedText,
		attempt.Score,
		attempt.Passed,
		attempt.PracticedAt,
		attempt.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert english practice attempt: %w", err)
	}
	defer rows.Close()
	inserted := false
	for rows.Next() {
		var marker int
		if err := rows.Scan(&marker); err != nil {
			return fmt.Errorf("scan inserted english practice attempt: %w", err)
		}
		inserted = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("finish english practice attempt insert: %w", err)
	}
	if !inserted {
		var resourceExists bool
		err := executor.QueryRow(
			ctx,
			`SELECT EXISTS (
			     SELECT 1
			     FROM english_lessons
			     WHERE lesson_id = $1
			 )`,
			attempt.LessonID,
		).Scan(&resourceExists)
		if err != nil {
			return fmt.Errorf("classify rejected english attempt: %w", err)
		}
		if !resourceExists {
			return domain.ErrLessonNotFound
		}
		if childID != "" {
			var childFamilyID string
			err := executor.QueryRow(
				ctx,
				`SELECT family_id::text
				 FROM child_profiles
				 WHERE id::text = $1`,
				childID,
			).Scan(&childFamilyID)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrChildNotFound
			}
			if err != nil {
				return fmt.Errorf("classify missing english child: %w", err)
			}
			if childFamilyID != attempt.FamilyID {
				return domain.ErrChildFamilyMismatch
			}
		}
		return domain.ErrPracticeItemNotFound
	}
	return nil
}

// ListAttempts returns one family's attempts, newest first, bounded by the
// query limit so a long-lived family cannot exhaust the response.
func (r *PostgresRepository) ListAttempts(
	ctx context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.PracticeAttempt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			family_id,
			COALESCE(child_id::text, ''),
			lesson_id,
			item_id,
			item_type,
			target_text,
			recognized_text,
			score,
			passed,
			practiced_at,
			created_at
		FROM english_practice_attempts
		WHERE family_id = $1
			AND ($2 = '' OR child_id::text = $2)
			AND ($3 = '' OR lesson_id = $3)
		ORDER BY practiced_at DESC, id DESC
		LIMIT $4
	`,
		familyID,
		query.ChildID,
		query.LessonID,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list english practice attempts: %w", err)
	}
	defer rows.Close()

	attempts := make([]domain.PracticeAttempt, 0, query.Limit)
	for rows.Next() {
		var attempt domain.PracticeAttempt
		if err := rows.Scan(
			&attempt.ID,
			&attempt.FamilyID,
			&attempt.ChildID,
			&attempt.LessonID,
			&attempt.ItemID,
			&attempt.ItemType,
			&attempt.TargetText,
			&attempt.RecognizedText,
			&attempt.Score,
			&attempt.Passed,
			&attempt.PracticedAt,
			&attempt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan english practice attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english practice attempts: %w", err)
	}
	return attempts, nil
}

// ListProgress folds every attempt owned by the family into lesson and item
// aggregates. The SQL returns one row per practiced item across the entire
// history; the caller never applies the bounded browsing LIMIT.
func (r *PostgresRepository) ListProgress(
	ctx context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.LessonProgress, []domain.ProgressItem, error) {
	transaction, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("begin english progress read: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	lessons, items, err := listProgress(ctx, transaction, familyID, query)
	if err != nil {
		return nil, nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit english progress read: %w", err)
	}
	return lessons, items, nil
}

type progressQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func listProgress(
	ctx context.Context,
	querier progressQuerier,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.LessonProgress, []domain.ProgressItem, error) {
	lessons, err := listProgressLessons(ctx, querier, familyID, query)
	if err != nil {
		return nil, nil, err
	}
	items, err := listProgressItems(ctx, querier, familyID, query)
	if err != nil {
		return nil, nil, err
	}
	return lessons, items, nil
}

func listProgressItems(
	ctx context.Context,
	querier progressQuerier,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.ProgressItem, error) {
	rows, err := querier.Query(ctx, `
		SELECT
			lesson_id,
			item_id,
			item_type,
			target_text,
			MAX(score) AS best_score,
			COUNT(*) AS attempts,
			BOOL_OR(passed) AS passed,
			MAX(practiced_at) AS last_at
		FROM english_practice_attempts
		WHERE family_id = $1
			AND ($2 = '' OR child_id::text = $2)
			AND ($3 = '' OR lesson_id = $3)
		GROUP BY lesson_id, item_id, item_type, target_text
		ORDER BY MAX(practiced_at) DESC, lesson_id ASC, item_id ASC
	`, familyID, query.ChildID, query.LessonID)
	if err != nil {
		return nil, fmt.Errorf("aggregate english practice progress: %w", err)
	}
	defer rows.Close()

	items := make([]domain.ProgressItem, 0)
	for rows.Next() {
		var item domain.ProgressItem
		if err := rows.Scan(
			&item.LessonID,
			&item.ItemID,
			&item.ItemType,
			&item.TargetText,
			&item.BestScore,
			&item.Attempts,
			&item.Passed,
			&item.LastAt,
		); err != nil {
			return nil, fmt.Errorf("scan english progress item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english progress items: %w", err)
	}
	return items, nil
}

func listProgressLessons(
	ctx context.Context,
	querier progressQuerier,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.LessonProgress, error) {
	rows, err := querier.Query(ctx, `
		SELECT
			attempt.lesson_id,
			COUNT(*) AS attempts,
			COUNT(DISTINCT attempt.item_id)
			    FILTER (WHERE attempt.passed) AS passed_items,
			COUNT(DISTINCT attempt.item_id) AS total_items,
			MAX(attempt.score) AS best_score,
			ROUND(AVG(attempt.score))::int AS average_score,
			MAX(attempt.practiced_at) AS last_practiced
		FROM english_practice_attempts AS attempt
		WHERE attempt.family_id = $1
			AND ($2 = '' OR attempt.child_id::text = $2)
			AND ($3 = '' OR attempt.lesson_id = $3)
		GROUP BY attempt.lesson_id
		ORDER BY MAX(attempt.practiced_at) DESC, attempt.lesson_id ASC
	`, familyID, query.ChildID, query.LessonID)
	if err != nil {
		return nil, fmt.Errorf("aggregate english lesson progress: %w", err)
	}
	defer rows.Close()

	lessons := make([]domain.LessonProgress, 0)
	for rows.Next() {
		var lesson domain.LessonProgress
		if err := rows.Scan(
			&lesson.LessonID,
			&lesson.Attempts,
			&lesson.PassedItems,
			&lesson.TotalItems,
			&lesson.BestScore,
			&lesson.AverageScore,
			&lesson.LastPracticed,
		); err != nil {
			return nil, fmt.Errorf("scan english lesson progress: %w", err)
		}
		lessons = append(lessons, lesson)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate english lesson progress: %w", err)
	}
	return lessons, nil
}

// nullableUUID returns NULL for an empty id so the soft child reference stays
// optional without storing an empty string in a UUID column.
func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

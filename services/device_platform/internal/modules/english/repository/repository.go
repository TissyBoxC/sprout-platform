// Package repository persists the English learning catalog and progress.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
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
	`, ageTier)
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
	_, err := r.pool.Exec(ctx, `
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		attempt.ID,
		attempt.FamilyID,
		nullableUUID(attempt.ChildID),
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

// nullableUUID returns NULL for an empty id so the soft child reference stays
// optional without storing an empty string in a UUID column.
func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

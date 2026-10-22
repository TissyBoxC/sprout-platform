package service

import (
	"context"
	"errors"
	"fmt"

	englishdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

// englishLessonReader is the slice of the English learning service an
// assessment needs. Keeping it an interface means this package depends on the
// course's public contract rather than its repository.
type englishLessonReader interface {
	// LessonCatalog returns the published lessons with their ordered items.
	LessonCatalog(ctx context.Context, ageTier string) ([]englishdomain.Lesson, error)
	GetLesson(ctx context.Context, lessonID string) (*englishdomain.Lesson, error)
}

// EnglishCatalog adapts the English learning catalog to the assessment's course
// seam.
//
// Only published lessons with at least one item are advertised: assessing a draft
// lesson would serve content that was never approved for children.
type EnglishCatalog struct {
	reader englishLessonReader
}

// NewEnglishCatalog builds the adapter over the English learning service.
func NewEnglishCatalog(reader englishLessonReader) *EnglishCatalog {
	return &EnglishCatalog{reader: reader}
}

// Lessons returns the published lessons that have assessable items.
func (catalog *EnglishCatalog) Lessons(ctx context.Context, limit int) ([]Lesson, error) {
	if catalog == nil || catalog.reader == nil {
		return nil, errors.New("assessment lesson catalog is not configured")
	}
	lessons, err := catalog.reader.LessonCatalog(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("read english lesson catalog: %w", err)
	}
	published := make([]Lesson, 0, len(lessons))
	for index := range lessons {
		if lessons[index].Status != englishdomain.LessonStatusPublished {
			continue
		}
		converted := convertLesson(&lessons[index])
		if len(converted.Items) == 0 {
			continue
		}
		published = append(published, converted)
		if limit > 0 && len(published) >= limit {
			break
		}
	}
	return published, nil
}

// Lesson returns one published lesson with its ordered items.
//
// A draft or withdrawn lesson is reported as not found: the caller cannot
// distinguish it from a lesson that does not exist, which is the intent because
// a withdrawn lesson must not be assessable.
func (catalog *EnglishCatalog) Lesson(ctx context.Context, lessonID string) (Lesson, error) {
	if catalog == nil || catalog.reader == nil {
		return Lesson{}, errors.New("assessment lesson catalog is not configured")
	}
	lesson, err := catalog.reader.GetLesson(ctx, lessonID)
	if err != nil {
		if errors.Is(err, englishdomain.ErrLessonNotFound) {
			return Lesson{}, ErrLessonNotFound
		}
		return Lesson{}, fmt.Errorf("read english lesson: %w", err)
	}
	if lesson.Status != englishdomain.LessonStatusPublished {
		return Lesson{}, ErrLessonNotFound
	}
	return convertLesson(lesson), nil
}

// convertLesson flattens a lesson into the assessable item order used by the
// difficulty curve.
//
// Words come before sentences, because a child reads a word and then a phrase;
// vocabulary mastery is the prerequisite for a spoken pattern. Phrases require
// the whole sentence pattern, so a sentence lesson keeps its prompts intact.
func convertLesson(lesson *englishdomain.Lesson) Lesson {
	converted := Lesson{LessonID: lesson.LessonID, AgeTier: lesson.AgeTier}
	// A word may be spoken in a phrase later; the assessment needs the text the
	// child is asked to say, which is the word term.
	converted.Items = make([]Item, 0, len(lesson.Words)+len(lesson.Sentence))
	for _, word := range lesson.Words {
		if word.Term == "" {
			continue
		}
		converted.Items = append(converted.Items, Item{
			ID:   word.ID,
			Type: englishdomain.ItemTypeWord,
			Text: word.Term,
		})
	}
	for _, sentence := range lesson.Sentence {
		if sentence.ModelAnswer == "" {
			continue
		}
		converted.Items = append(converted.Items, Item{
			ID:   sentence.ID,
			Type: englishdomain.ItemTypeSentence,
			Text: sentence.ModelAnswer,
		})
	}
	return converted
}

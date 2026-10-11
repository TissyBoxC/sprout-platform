package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

type stubRepository struct {
	commands   []domain.Command
	revision   int
	replaceErr error
	deleteErr  error
}

func (r *stubRepository) ListCommands(
	_ context.Context,
	_ string,
) ([]domain.Command, error) {
	return append([]domain.Command{}, r.commands...), nil
}

func (r *stubRepository) ReplaceCommands(
	_ context.Context,
	_ string,
	commands []domain.Command,
) (int, error) {
	if r.replaceErr != nil {
		return 0, r.replaceErr
	}
	r.commands = append([]domain.Command{}, commands...)
	r.revision += 1
	return r.revision, nil
}

func (r *stubRepository) DeleteCommand(
	_ context.Context,
	_ string,
	commandID string,
) (int, error) {
	if r.deleteErr != nil {
		return 0, r.deleteErr
	}
	r.revision += 1
	return r.revision, nil
}

func (r *stubRepository) Revision(_ context.Context, _ string) (int, error) {
	return r.revision, nil
}

func TestReplaceValidatesAndBumpsRevision(t *testing.T) {
	repository := &stubRepository{}
	service := New(Options{Repository: repository})

	set, err := service.Replace(context.Background(), "family-1", []domain.CommandInput{
		{
			CommandID: "cmd_play",
			Phrase:    " 听故事 ",
			Action:    domain.ActionPlayPackage,
			Argument:  "story_001",
			Enabled:   true,
		},
		{
			CommandID: "cmd_pause",
			Phrase:    "停一下",
			Action:    domain.ActionPause,
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if set.Revision != 1 {
		t.Fatalf("revision = %d, want 1", set.Revision)
	}
	if len(set.Commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(set.Commands))
	}
	if set.Commands[0].Phrase != "听故事" {
		t.Fatalf("phrase = %q, want trimmed", set.Commands[0].Phrase)
	}
}

func TestReplaceRejectsDuplicatePhrase(t *testing.T) {
	service := New(Options{Repository: &stubRepository{}})
	_, err := service.Replace(context.Background(), "family-1", []domain.CommandInput{
		{CommandID: "a", Phrase: "听故事", Action: domain.ActionStop},
		{CommandID: "b", Phrase: " 听 故事 ", Action: domain.ActionStop},
	})
	if !errors.Is(err, domain.ErrDuplicatePhrase) {
		t.Fatalf("err = %v, want ErrDuplicatePhrase", err)
	}
}

func TestReplaceRejectsArgumentMismatch(t *testing.T) {
	service := New(Options{Repository: &stubRepository{}})
	_, err := service.Replace(context.Background(), "family-1", []domain.CommandInput{
		{CommandID: "a", Phrase: "播放", Action: domain.ActionPlayPackage},
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestReplaceRejectsTooManyCommands(t *testing.T) {
	service := New(Options{Repository: &stubRepository{}})
	inputs := make([]domain.CommandInput, 0, domain.MaxCommands+1)
	for index := 0; index <= domain.MaxCommands; index++ {
		inputs = append(inputs, domain.CommandInput{
			CommandID: "cmd",
			Phrase:    "口令",
			Action:    domain.ActionStop,
		})
	}
	_, err := service.Replace(context.Background(), "family-1", inputs)
	if !errors.Is(err, domain.ErrInvalidCommand) {
		t.Fatalf("err = %v, want ErrInvalidCommand", err)
	}
}

func TestListReturnsCurrentSet(t *testing.T) {
	repository := &stubRepository{
		commands: []domain.Command{{CommandID: "a", Phrase: "停", Action: domain.ActionStop}},
		revision: 4,
	}
	service := New(Options{Repository: repository})
	set, err := service.List(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if set.Revision != 4 || len(set.Commands) != 1 {
		t.Fatalf("set = %+v", set)
	}
}

func TestDeletePropagatesNotFound(t *testing.T) {
	service := New(Options{Repository: &stubRepository{
		deleteErr: domain.ErrCommandNotFound,
	}})
	if _, err := service.Delete(
		context.Background(),
		"family-1",
		"missing",
	); !errors.Is(err, domain.ErrCommandNotFound) {
		t.Fatalf("err = %v, want ErrCommandNotFound", err)
	}
}

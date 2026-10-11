// Package service contains local command use cases.
//
// A guardian publishes the whole command set as one revision; the device reads
// the family's current set and resolves phrases locally. The service validates
// the batch and phrase uniqueness before it touches the store so a malformed
// revision never replaces a working set.
package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	ListCommands(ctx context.Context, familyID string) ([]domain.Command, error)
	ReplaceCommands(
		ctx context.Context,
		familyID string,
		commands []domain.Command,
	) (int, error)
	DeleteCommand(
		ctx context.Context,
		familyID string,
		commandID string,
	) (int, error)
	Revision(ctx context.Context, familyID string) (int, error)
}

// CommandSet is a family's commands plus the revision a device syncs against.
type CommandSet struct {
	Revision int
	Commands []domain.Command
}

// Service owns local command publishing and reads.
type Service struct {
	repository Repository
}

// Options contains local command service dependencies.
type Options struct {
	Repository Repository
}

// New creates the local command service.
func New(options Options) *Service {
	return &Service{repository: options.Repository}
}

// List returns a family's current command set.
func (s *Service) List(
	ctx context.Context,
	familyID string,
) (CommandSet, error) {
	if s.repository == nil {
		return CommandSet{}, nil
	}
	commands, err := s.repository.ListCommands(ctx, familyID)
	if err != nil {
		return CommandSet{}, err
	}
	revision, err := s.repository.Revision(ctx, familyID)
	if err != nil {
		return CommandSet{}, err
	}
	return CommandSet{Revision: revision, Commands: commands}, nil
}

// Replace validates and replaces the whole command set.
func (s *Service) Replace(
	ctx context.Context,
	familyID string,
	inputs []domain.CommandInput,
) (CommandSet, error) {
	if s.repository == nil {
		return CommandSet{}, nil
	}
	if len(inputs) > domain.MaxCommands {
		return CommandSet{}, domain.ErrInvalidCommand
	}
	commands := make([]domain.Command, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	seenIDs := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		normalized, err := domain.ValidateInput(input)
		if err != nil {
			return CommandSet{}, err
		}
		phraseKey := domain.NormalizePhrase(normalized.Phrase)
		if _, exists := seen[phraseKey]; exists {
			return CommandSet{}, domain.ErrDuplicatePhrase
		}
		if _, exists := seenIDs[normalized.CommandID]; exists {
			return CommandSet{}, domain.ErrInvalidCommand
		}
		seen[phraseKey] = struct{}{}
		seenIDs[normalized.CommandID] = struct{}{}
		commands = append(commands, domain.Command{
			CommandID: normalized.CommandID,
			Phrase:    normalized.Phrase,
			Action:    normalized.Action,
			Argument:  normalized.Argument,
			Enabled:   normalized.Enabled,
		})
	}
	revision, err := s.repository.ReplaceCommands(ctx, familyID, commands)
	if err != nil {
		return CommandSet{}, err
	}
	for index := range commands {
		commands[index].Revision = revision
	}
	return CommandSet{Revision: revision, Commands: commands}, nil
}

// Delete removes one command.
func (s *Service) Delete(
	ctx context.Context,
	familyID string,
	commandID string,
) (int, error) {
	if s.repository == nil {
		return 0, nil
	}
	return s.repository.DeleteCommand(ctx, familyID, commandID)
}

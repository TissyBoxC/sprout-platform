// Package domain contains guardian-published local command types shared by the
// platform modules.
//
// A local command maps a bounded trigger phrase to an action the device can
// perform without the network. Commands are family-scoped: a guardian only
// ever reads or writes the family's own set, and a device only ever receives
// the list for the family it is bound to.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// ErrCommandNotFound reports a missing local command.
	ErrCommandNotFound = errors.New("local command not found")
	// ErrInvalidCommand reports a malformed local command.
	ErrInvalidCommand = errors.New("invalid local command")
	// ErrInvalidPhrase reports a malformed trigger phrase.
	ErrInvalidPhrase = errors.New("invalid local command phrase")
	// ErrInvalidAction reports an unsupported local command action.
	ErrInvalidAction = errors.New("invalid local command action")
	// ErrInvalidArgument reports an argument that does not match the action.
	ErrInvalidArgument = errors.New("invalid local command argument")
	// ErrDuplicatePhrase reports two commands sharing a trigger phrase.
	ErrDuplicatePhrase = errors.New("duplicate local command phrase")
	// ErrRepositoryUnavailable reports a missing persistence dependency.
	ErrRepositoryUnavailable = errors.New("local command repository is unavailable")
)

// Approved local command actions.
const (
	ActionPlayPackage = "play_package"
	ActionPause       = "pause"
	ActionResume      = "resume"
	ActionStop        = "stop"
	ActionSetVolume   = "set_volume"
	ActionSleep       = "sleep"
)

// Actions lists the approved actions in a stable order.
var Actions = []string{
	ActionPlayPackage,
	ActionPause,
	ActionResume,
	ActionStop,
	ActionSetVolume,
	ActionSleep,
}

// Bounds keep a command list and each field small enough for the device cache.
const (
	MaxCommands      = 24
	MaxPhraseRunes   = 48
	MaxArgumentRunes = 80
)

var packageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)

// Command is one guardian-published local command macro.
type Command struct {
	CommandID string
	Phrase    string
	Action    string
	Argument  string
	Enabled   bool
	Revision  int
}

// CommandInput contains the editable fields accepted from a guardian.
type CommandInput struct {
	CommandID string
	Phrase    string
	Action    string
	Argument  string
	Enabled   bool
}

// IsAction reports whether value is an approved local command action.
func IsAction(value string) bool {
	for _, action := range Actions {
		if action == value {
			return true
		}
	}
	return false
}

// expectsArgument reports whether an action requires a target argument.
func expectsArgument(action string) bool {
	return action == ActionPlayPackage || action == ActionSetVolume
}

// ValidateInput normalizes and validates one command.
func ValidateInput(input CommandInput) (CommandInput, error) {
	normalized := CommandInput{
		CommandID: strings.TrimSpace(input.CommandID),
		Phrase:    strings.TrimSpace(input.Phrase),
		Action:    strings.TrimSpace(input.Action),
		Argument:  strings.TrimSpace(input.Argument),
		Enabled:   input.Enabled,
	}
	if normalized.CommandID == "" {
		return CommandInput{}, ErrInvalidCommand
	}
	if normalized.Phrase == "" ||
		utf8.RuneCountInString(normalized.Phrase) > MaxPhraseRunes {
		return CommandInput{}, ErrInvalidPhrase
	}
	if !IsAction(normalized.Action) {
		return CommandInput{}, ErrInvalidAction
	}
	if utf8.RuneCountInString(normalized.Argument) > MaxArgumentRunes {
		return CommandInput{}, ErrInvalidArgument
	}
	if expectsArgument(normalized.Action) {
		if normalized.Argument == "" {
			return CommandInput{}, ErrInvalidArgument
		}
		if normalized.Action == ActionPlayPackage &&
			!packageIDPattern.MatchString(normalized.Argument) {
			return CommandInput{}, ErrInvalidArgument
		}
	} else if normalized.Argument != "" {
		return CommandInput{}, ErrInvalidArgument
	}
	return normalized, nil
}

// NormalizePhrase folds a trigger phrase for duplicate detection: surrounding
// whitespace removed, internal whitespace collapsed away, lower case.
func NormalizePhrase(value string) string {
	var builder strings.Builder
	for _, current := range value {
		if current == ' ' || current == '\t' || current == '\n' ||
			current == '\r' {
			continue
		}
		builder.WriteRune(current)
	}
	return strings.ToLower(builder.String())
}

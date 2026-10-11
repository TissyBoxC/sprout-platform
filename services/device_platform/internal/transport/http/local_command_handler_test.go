package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	localcommanddomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
	localcommandservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/service"
)

// localCommandTestService is an in-memory double so the handler tests exercise
// the real service validation without a database.
type localCommandTestService struct {
	commands []localcommanddomain.Command
	revision int
}

func (s *localCommandTestService) List(
	_ context.Context,
	_ string,
) (localcommandservice.CommandSet, error) {
	return localcommandservice.CommandSet{
		Revision: s.revision,
		Commands: append([]localcommanddomain.Command{}, s.commands...),
	}, nil
}

func (s *localCommandTestService) Replace(
	_ context.Context,
	_ string,
	inputs []localcommanddomain.CommandInput,
) (localcommandservice.CommandSet, error) {
	commands := make([]localcommanddomain.Command, 0, len(inputs))
	seen := map[string]struct{}{}
	for _, input := range inputs {
		normalized, err := localcommanddomain.ValidateInput(input)
		if err != nil {
			return localcommandservice.CommandSet{}, err
		}
		key := localcommanddomain.NormalizePhrase(normalized.Phrase)
		if _, exists := seen[key]; exists {
			return localcommandservice.CommandSet{}, localcommanddomain.ErrDuplicatePhrase
		}
		seen[key] = struct{}{}
		commands = append(commands, localcommanddomain.Command{
			CommandID: normalized.CommandID,
			Phrase:    normalized.Phrase,
			Action:    normalized.Action,
			Argument:  normalized.Argument,
			Enabled:   normalized.Enabled,
		})
	}
	s.commands = commands
	s.revision += 1
	for index := range s.commands {
		s.commands[index].Revision = s.revision
	}
	return localcommandservice.CommandSet{
		Revision: s.revision,
		Commands: s.commands,
	}, nil
}

func (s *localCommandTestService) Delete(
	_ context.Context,
	_ string,
	commandID string,
) (int, error) {
	for index := range s.commands {
		if s.commands[index].CommandID == commandID {
			s.commands = append(
				s.commands[:index],
				s.commands[index+1:]...,
			)
			s.revision += 1
			return s.revision, nil
		}
	}
	return 0, localcommanddomain.ErrCommandNotFound
}

func newLocalCommandHandlerForTest() localCommandHandler {
	return localCommandHandler{service: &localCommandTestService{}}
}

func TestLocalCommandListRequiresAuthentication(t *testing.T) {
	handler := newLocalCommandHandlerForTest()
	request := englishRequest(
		http.MethodGet,
		"/api/v1/local-commands",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLocalCommandReplaceAndList(t *testing.T) {
	handler := newLocalCommandHandlerForTest()
	body := `{"commands":[{"command_id":"cmd_play","phrase":"听故事","action":"play_package","argument":"story_001","enabled":true},{"command_id":"cmd_pause","phrase":"停一下","action":"pause","enabled":true}]}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/local-commands",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.replace(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Revision int `json:"revision"`
			Commands []struct {
				CommandID string `json:"command_id"`
				Action    string `json:"action"`
			} `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Revision != 1 || len(envelope.Data.Commands) != 2 {
		t.Fatalf("unexpected set: %s", recorder.Body.String())
	}

	request = englishRequest(
		http.MethodGet,
		"/api/v1/local-commands",
		"",
		"family-1",
	)
	recorder = httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "cmd_play") {
		t.Fatalf("list missing command: %s", recorder.Body.String())
	}
}

func TestLocalCommandReplaceRejectsDuplicatePhrase(t *testing.T) {
	handler := newLocalCommandHandlerForTest()
	body := `{"commands":[{"command_id":"a","phrase":"听故事","action":"stop"},{"command_id":"b","phrase":"听 故事","action":"stop"}]}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/local-commands",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.replace(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestLocalCommandReplaceRejectsArgumentMismatch(t *testing.T) {
	handler := newLocalCommandHandlerForTest()
	body := `{"commands":[{"command_id":"a","phrase":"播放","action":"play_package"}]}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/local-commands",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.replace(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestLocalCommandDeleteMissingIsNotFound(t *testing.T) {
	handler := newLocalCommandHandlerForTest()
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/local-commands/nope",
		"",
		"family-1",
	)
	request.SetPathValue("command_id", "nope")
	recorder := httptest.NewRecorder()
	handler.delete(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestLocalCommandDeleteRemovesCommand(t *testing.T) {
	service := &localCommandTestService{}
	handler := localCommandHandler{service: service}
	if _, err := service.Replace(
		context.Background(),
		"family-1",
		[]localcommanddomain.CommandInput{
			{CommandID: "cmd_stop", Phrase: "停下来", Action: localcommanddomain.ActionStop},
		},
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/local-commands/cmd_stop",
		"",
		"family-1",
	)
	request.SetPathValue("command_id", "cmd_stop")
	recorder := httptest.NewRecorder()
	handler.delete(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if len(service.commands) != 0 {
		t.Fatalf("commands = %d, want 0", len(service.commands))
	}
}

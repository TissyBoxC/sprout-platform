package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/domain"
)

// stubBindingService answers a switch without a database.
type stubBindingService struct {
	binding domain.Binding
	err     error
}

func (service stubBindingService) Bind(
	_ context.Context,
	_ domain.Request,
) (domain.Binding, error) {
	return service.binding, service.err
}

func (service stubBindingService) Resolve(
	_ context.Context,
	_ string,
) (domain.Resolution, error) {
	return domain.Resolution{}, nil
}

func (service stubBindingService) Unbind(_ context.Context, _ string) error {
	return nil
}

func (service stubBindingService) History(
	_ context.Context,
	_ string,
) ([]domain.History, error) {
	return nil, nil
}

// recordingCommandSender captures the commands a handler queues.
type recordingCommandSender struct {
	deviceID    string
	commandType runtimeDomain.CommandType
	requestedBy string
	payload     map[string]any
	err         error
	calls       int
}

func (sender *recordingCommandSender) CreateCommand(
	_ context.Context,
	deviceID string,
	commandType runtimeDomain.CommandType,
	requestedBy string,
	payload map[string]any,
) (*runtimeDomain.Command, error) {
	sender.calls++
	sender.deviceID = deviceID
	sender.commandType = commandType
	sender.requestedBy = requestedBy
	sender.payload = payload
	if sender.err != nil {
		return nil, sender.err
	}
	return &runtimeDomain.Command{}, nil
}

func bindingActivateRequest(childID string) *http.Request {
	body, _ := json.Marshal(map[string]string{"child_id": childID})
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/families/family-1/devices/device-1/child",
		strings.NewReader(string(body)),
	)
	request.SetPathValue("family_id", "family-1")
	request.SetPathValue("device_id", "device-1")
	return request
}

// TestActivateTellsTheDeviceToReload covers the reason the command exists: the
// device would otherwise keep the previous child's level until its next
// background refresh, which can be fifteen minutes away.
func TestActivateTellsTheDeviceToReload(t *testing.T) {
	sender := &recordingCommandSender{}
	handler := guardianBindingHandler{
		service: stubBindingService{binding: domain.Binding{
			DeviceID:    "device-1",
			ChildID:     "child-2",
			FamilyID:    "family-1",
			ActivatedAt: time.Now().UTC(),
		}},
		commands: sender,
	}
	recorder := httptest.NewRecorder()
	handler.activate(recorder, bindingActivateRequest("child-2"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if sender.calls != 1 {
		t.Fatalf("commands queued = %d, want 1", sender.calls)
	}
	if sender.commandType != runtimeDomain.CommandRefreshConfiguration {
		t.Fatalf("command = %q, want %q",
			sender.commandType, runtimeDomain.CommandRefreshConfiguration)
	}
	if sender.deviceID != "device-1" {
		t.Fatalf("device = %q, want device-1", sender.deviceID)
	}
	if got := sender.payload["child_id"]; got != "child-2" {
		t.Fatalf("payload child = %v, want child-2", got)
	}
}

// TestActivateSucceedsEvenIfTheCommandFails keeps a queued-command failure from
// reporting the switch itself as failed: the binding is already stored.
func TestActivateSucceedsEvenIfTheCommandFails(t *testing.T) {
	sender := &recordingCommandSender{err: errors.New("queue unavailable")}
	handler := guardianBindingHandler{
		service: stubBindingService{binding: domain.Binding{
			DeviceID:    "device-1",
			ChildID:     "child-2",
			FamilyID:    "family-1",
			ActivatedAt: time.Now().UTC(),
		}},
		commands: sender,
	}
	recorder := httptest.NewRecorder()
	handler.activate(recorder, bindingActivateRequest("child-2"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

// TestActivateWithoutACommandSenderStillSwitches keeps the handler usable when
// no runtime service is wired.
func TestActivateWithoutACommandSenderStillSwitches(t *testing.T) {
	handler := guardianBindingHandler{
		service: stubBindingService{binding: domain.Binding{
			DeviceID:    "device-1",
			ChildID:     "child-2",
			FamilyID:    "family-1",
			ActivatedAt: time.Now().UTC(),
		}},
	}
	recorder := httptest.NewRecorder()
	handler.activate(recorder, bindingActivateRequest("child-2"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

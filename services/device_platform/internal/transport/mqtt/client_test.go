package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

// stubToken is a completed paho token with an optional error.
type stubToken struct {
	done chan struct{}
	err  error
}

func newStubToken(err error) *stubToken {
	done := make(chan struct{})
	close(done)
	return &stubToken{done: done, err: err}
}

func (token *stubToken) Wait() bool                     { return true }
func (token *stubToken) WaitTimeout(time.Duration) bool { return true }
func (token *stubToken) Done() <-chan struct{}          { return token.done }
func (token *stubToken) Error() error                   { return token.err }

// stubPahoClient records the most recent publish.
type stubPahoClient struct {
	connected   bool
	topic       string
	qos         byte
	retained    bool
	payload     interface{}
	publishErr  error
	publishCall int
}

func (client *stubPahoClient) Connect() paho.Token { return newStubToken(nil) }
func (client *stubPahoClient) Disconnect(uint)     {}
func (client *stubPahoClient) IsConnectionOpen() bool {
	return client.connected
}
func (client *stubPahoClient) Publish(
	topic string,
	qos byte,
	retained bool,
	payload interface{},
) paho.Token {
	client.publishCall++
	client.topic = topic
	client.qos = qos
	client.retained = retained
	client.payload = payload
	return newStubToken(client.publishErr)
}

func sampleCommand() *runtimedomain.Command {
	return &runtimedomain.Command{
		ID:        "command-1",
		DeviceID:  "device_test_001",
		Type:      runtimedomain.CommandDisplayMessage,
		Payload:   map[string]any{"message": "晚安"},
		RequestID: "request-1",
		CreatedAt: time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC),
	}
}

func TestPublishCommandUsesVersionedTopicAndEnvelope(t *testing.T) {
	t.Parallel()
	client := &Client{client: &stubPahoClient{connected: true}}

	if err := client.PublishCommand(
		context.Background(),
		"device_test_001",
		sampleCommand(),
	); err != nil {
		t.Fatalf("PublishCommand() error = %v", err)
	}

	stub, ok := client.client.(*stubPahoClient)
	if !ok {
		t.Fatal("unexpected client type")
	}
	if stub.topic != "sprout/v1/devices/device_test_001/commands" {
		t.Fatalf("unexpected topic: %q", stub.topic)
	}
	if stub.qos != 1 {
		t.Fatalf("expected QoS 1, got %d", stub.qos)
	}
	if stub.retained {
		t.Fatal("command publishes must not be retained")
	}

	encoded, ok := stub.payload.([]byte)
	if !ok {
		t.Fatalf("expected a []byte payload, got %T", stub.payload)
	}
	var event DeviceCommandEvent
	if err := json.Unmarshal(encoded, &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.SchemaVersion != "1.0.0" {
		t.Fatalf("unexpected schema version: %q", event.SchemaVersion)
	}
	if event.EventType != "device.command" {
		t.Fatalf("unexpected event type: %q", event.EventType)
	}
	if event.Producer != "device_platform" {
		t.Fatalf("unexpected producer: %q", event.Producer)
	}
	if event.Subject.Type != "device" || event.Subject.ID != "device_test_001" {
		t.Fatalf("unexpected subject: %#v", event.Subject)
	}
	if event.RequestID != "request-1" {
		t.Fatalf("unexpected request id: %q", event.RequestID)
	}
	if event.Payload.CommandID != "command-1" {
		t.Fatalf("unexpected command id: %q", event.Payload.CommandID)
	}
	if event.Payload.CommandType != string(runtimedomain.CommandDisplayMessage) {
		t.Fatalf("unexpected command type: %q", event.Payload.CommandType)
	}
	if event.Payload.Data["message"] != "晚安" {
		t.Fatalf("unexpected payload data: %#v", event.Payload.Data)
	}
	if event.OccurredAt != "2026-10-14T09:00:00Z" {
		t.Fatalf("unexpected occurred_at: %q", event.OccurredAt)
	}
}

func TestPublishCommandRejectsMissingClientOrCommand(t *testing.T) {
	t.Parallel()
	var nilClient *Client
	if err := nilClient.PublishCommand(
		context.Background(),
		"device_test_001",
		sampleCommand(),
	); err == nil {
		t.Fatal("expected nil client to be rejected")
	}

	client := &Client{client: &stubPahoClient{connected: true}}
	if err := client.PublishCommand(
		context.Background(),
		"",
		sampleCommand(),
	); err == nil {
		t.Fatal("expected empty device id to be rejected")
	}
	if err := client.PublishCommand(
		context.Background(),
		"device_test_001",
		nil,
	); err == nil {
		t.Fatal("expected nil command to be rejected")
	}
	command := sampleCommand()
	command.RequestID = ""
	if err := client.PublishCommand(
		context.Background(),
		"device_test_001",
		command,
	); err == nil {
		t.Fatal("expected missing request id to be rejected")
	}
}

func TestPublishCommandSurfacesBrokerError(t *testing.T) {
	t.Parallel()
	client := &Client{client: &stubPahoClient{
		connected:  true,
		publishErr: errors.New("topic rejected"),
	}}

	err := client.PublishCommand(
		context.Background(),
		"device_test_001",
		sampleCommand(),
	)
	if err == nil {
		t.Fatal("expected broker error to be surfaced")
	}
}

func TestConnectedReflectsBrokerState(t *testing.T) {
	t.Parallel()
	client := &Client{client: &stubPahoClient{connected: false}}
	if client.Connected() {
		t.Fatal("expected disconnected client to report false")
	}
	var nilClient *Client
	if nilClient.Connected() {
		t.Fatal("expected nil client to report false")
	}
}

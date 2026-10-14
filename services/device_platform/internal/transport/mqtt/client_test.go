package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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
	// subscription state
	subTopic   string
	subQoS     byte
	subHandler paho.MessageHandler
	subErr     error
	subscribed bool
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

func (client *stubPahoClient) Subscribe(
	topic string,
	qos byte,
	callback paho.MessageHandler,
) paho.Token {
	client.subscribed = true
	client.subTopic = topic
	client.subQoS = qos
	client.subHandler = callback
	return newStubToken(client.subErr)
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

// stubAckHandler records dispatched acknowledgements.
type stubAckHandler struct {
	mu       sync.Mutex
	calls    int
	deviceID string
	ack      DeviceCommandAck
	err      error
}

func (handler *stubAckHandler) HandleCommandAck(
	_ context.Context,
	deviceID string,
	ack DeviceCommandAck,
) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.calls++
	handler.deviceID = deviceID
	handler.ack = ack
	return handler.err
}

func (handler *stubAckHandler) snapshot() (int, string, DeviceCommandAck) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return handler.calls, handler.deviceID, handler.ack
}

// stubMessage implements paho.Message for the subscriber callback.
type stubMessage struct {
	topic   string
	payload []byte
}

func (message stubMessage) Duplicate() bool   { return false }
func (message stubMessage) Qos() byte         { return 1 }
func (message stubMessage) Retained() bool    { return false }
func (message stubMessage) Topic() string     { return message.topic }
func (message stubMessage) MessageID() uint16 { return 0 }
func (message stubMessage) Payload() []byte   { return message.payload }
func (message stubMessage) Ack()              {}

func TestDeviceIDFromAckTopic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		topic    string
		deviceID string
		ok       bool
	}{
		{"sprout/v1/devices/device_test_001/commands/ack", "device_test_001", true},
		{"sprout/v1/devices/device_test_001/commands", "", false},
		{"sprout/v1/devices//commands/ack", "", false},
		{"sprout/v1/devices/a/b/commands/ack", "", false},
		{"other/topic", "", false},
	}
	for _, test := range tests {
		deviceID, ok := DeviceIDFromAckTopic(test.topic)
		if ok != test.ok || deviceID != test.deviceID {
			t.Fatalf(
				"DeviceIDFromAckTopic(%q) = (%q, %v), want (%q, %v)",
				test.topic,
				deviceID,
				ok,
				test.deviceID,
				test.ok,
			)
		}
	}
}

func TestSubscribeCommandAcksDispatchesValidAck(t *testing.T) {
	t.Parallel()
	stub := &stubPahoClient{connected: true}
	client := &Client{client: stub}
	handler := &stubAckHandler{}

	if err := client.SubscribeCommandAcks(context.Background(), handler); err != nil {
		t.Fatalf("SubscribeCommandAcks() error = %v", err)
	}
	if !stub.subscribed {
		t.Fatal("expected the client to subscribe")
	}
	if stub.subTopic != "sprout/v1/devices/+/commands/ack" {
		t.Fatalf("unexpected subscription topic: %q", stub.subTopic)
	}
	if stub.subQoS != 1 {
		t.Fatalf("expected QoS 1, got %d", stub.subQoS)
	}

	payload, err := json.Marshal(DeviceCommandAck{
		SchemaVersion: "1.0.0",
		EventID:       "event-1",
		EventType:     "device.command_ack",
		Producer:      "firmware",
		Subject:       DeviceCommandSubject{Type: "device", ID: "device_test_001"},
		RequestID:     "request-1",
		Payload: DeviceCommandAckPayload{
			CommandID:  "command-1",
			Status:     "acknowledged",
			ResultCode: "ok",
			RequestID:  "request-1",
		},
	})
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands/ack",
		payload: payload,
	})
	waitFor(t, func() bool {
		calls, _, _ := handler.snapshot()
		return calls == 1
	})
	calls, deviceID, ack := handler.snapshot()
	if calls != 1 {
		t.Fatalf("expected one dispatch, got %d", calls)
	}
	if deviceID != "device_test_001" {
		t.Fatalf("unexpected device id: %q", deviceID)
	}
	if ack.Payload.CommandID != "command-1" {
		t.Fatalf("unexpected command id: %q", ack.Payload.CommandID)
	}
	if ack.Payload.Status != "acknowledged" {
		t.Fatalf("unexpected status: %q", ack.Payload.Status)
	}
}

func TestSubscribeCommandAcksDropsInvalidMessages(t *testing.T) {
	t.Parallel()
	stub := &stubPahoClient{connected: true}
	client := &Client{client: stub}
	handler := &stubAckHandler{}

	if err := client.SubscribeCommandAcks(context.Background(), handler); err != nil {
		t.Fatalf("SubscribeCommandAcks() error = %v", err)
	}

	validPayload := `{"schema_version":"1.0.0","event_id":"e","event_type":"device.command_ack","producer":"firmware","subject":{"type":"device","id":"device_test_001"},"request_id":"r","payload":{"command_id":"command-1","status":"acknowledged","request_id":"r"}}`

	// Wrong topic family.
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands",
		payload: []byte(validPayload),
	})
	// Malformed JSON.
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands/ack",
		payload: []byte("{"),
	})
	// Subject does not match the topic device id.
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands/ack",
		payload: []byte(`{"schema_version":"1.0.0","event_type":"device.command_ack","subject":{"type":"device","id":"device_other_002"},"payload":{"command_id":"command-1","status":"acknowledged"}}`),
	})
	// Wrong event type.
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands/ack",
		payload: []byte(`{"schema_version":"1.0.0","event_type":"device.online","subject":{"type":"device","id":"device_test_001"},"payload":{"command_id":"command-1","status":"acknowledged"}}`),
	})
	// Missing command id.
	stub.subHandler(nil, stubMessage{
		topic:   "sprout/v1/devices/device_test_001/commands/ack",
		payload: []byte(`{"schema_version":"1.0.0","event_type":"device.command_ack","subject":{"type":"device","id":"device_test_001"},"payload":{"status":"acknowledged"}}`),
	})

	calls, _, _ := handler.snapshot()
	if calls != 0 {
		t.Fatalf("expected invalid messages to be dropped, got %d dispatches", calls)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSubscribeCommandAcksRejectsMissingInputs(t *testing.T) {
	t.Parallel()
	var nilClient *Client
	if err := nilClient.SubscribeCommandAcks(
		context.Background(),
		&stubAckHandler{},
	); err == nil {
		t.Fatal("expected nil client to be rejected")
	}

	client := &Client{client: &stubPahoClient{connected: true}}
	if err := client.SubscribeCommandAcks(
		context.Background(),
		nil,
	); err == nil {
		t.Fatal("expected nil handler to be rejected")
	}
}

func TestSubscribeCommandAcksSurfacesSubscriptionError(t *testing.T) {
	t.Parallel()
	client := &Client{client: &stubPahoClient{
		connected: true,
		subErr:    errors.New("not authorized"),
	}}
	err := client.SubscribeCommandAcks(
		context.Background(),
		&stubAckHandler{},
	)
	if err == nil {
		t.Fatal("expected subscription error to be surfaced")
	}
}

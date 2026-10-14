// Package mqtt contains the MQTT device-control transport.
package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

// Client owns the MQTT connection used by device-control handlers.
type Client struct {
	client pahoClient
}

// pahoClient is the subset of paho.Client this adapter uses. Narrowing the
// surface lets tests exercise topic, QoS, and payload construction without a
// live broker.
type pahoClient interface {
	Connect() paho.Token
	Disconnect(quiesce uint)
	IsConnectionOpen() bool
	Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token
	Subscribe(
		topic string,
		qos byte,
		callback paho.MessageHandler,
	) paho.Token
}

// Open connects to the broker with mutual TLS and verifies the connection
// before returning. Certificate verification cannot be disabled.
func Open(_ context.Context, cfg config.MQTTConfig) (*Client, error) {
	if cfg.InsecureSkipVerify {
		return nil, fmt.Errorf("insecure MQTT TLS is prohibited")
	}
	if cfg.Broker == "" || cfg.CAFile == "" || cfg.ClientCertificateFile == "" || cfg.ClientKeyFile == "" {
		return nil, fmt.Errorf("MQTT broker and TLS certificate paths are required")
	}

	tlsConfig, err := loadTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	options := paho.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetTLSConfig(tlsConfig).
		SetAutoReconnect(true).
		// Command acknowledgements are QoS 1 device-to-platform events. A
		// persistent session lets the broker queue them while the platform is
		// reconnecting, and ResumeSubs restores the wildcard subscription.
		SetCleanSession(false).
		SetResumeSubs(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetConnectTimeout(5 * time.Second)

	client := paho.NewClient(options)
	token := client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		return nil, fmt.Errorf("connect MQTT broker: timeout")
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("connect MQTT broker: %w", err)
	}

	return &Client{client: client}, nil
}

func loadTLSConfig(cfg config.MQTTConfig) (*tls.Config, error) {
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read MQTT CA certificate: %w", err)
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse MQTT CA certificate")
	}

	clientCertificate, err := tls.LoadX509KeyPair(cfg.ClientCertificateFile, cfg.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load MQTT client certificate: %w", err)
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{clientCertificate},
	}, nil
}

// Close disconnects the MQTT client.
func (c *Client) Close() {
	if c == nil || c.client == nil {
		return
	}
	c.client.Disconnect(250)
}

// Connected reports whether the broker connection is currently established.
func (c *Client) Connected() bool {
	if c == nil || c.client == nil {
		return false
	}
	return c.client.IsConnectionOpen()
}

// commandTopic is the versioned platform-to-device command channel from
// packages/contracts/mqtt/README.md.
func commandTopic(deviceID string) string {
	return "sprout/v1/devices/" + deviceID + "/commands"
}

// PublishCommand publishes one command event to the device command topic.
//
// The payload follows the shared device_event envelope and carries only the
// authored command plus its routing identifiers. Publishing is QoS 1 and not
// retained: a durable command row plus the device's HTTP poll remain the
// recovery path, so a missed publish cannot silently drop a command.
func (c *Client) PublishCommand(
	ctx context.Context,
	deviceID string,
	command *runtimedomain.Command,
) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("publish device command: MQTT client is not connected")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return fmt.Errorf("publish device command: device id is required")
	}
	if command == nil {
		return fmt.Errorf("publish device command: command is required")
	}
	if strings.TrimSpace(command.RequestID) == "" {
		return fmt.Errorf("publish device command: request id is required")
	}

	event := DeviceCommandEvent{
		SchemaVersion: "1.0.0",
		EventID:       command.ID,
		EventType:     "device.command",
		OccurredAt:    command.CreatedAt.UTC().Format(time.RFC3339),
		Producer:      "device_platform",
		Subject: DeviceCommandSubject{
			Type: "device",
			ID:   deviceID,
		},
		RequestID: command.RequestID,
		Payload: DeviceCommandPayload{
			CommandID:   command.ID,
			CommandType: string(command.Type),
			RequestID:   command.RequestID,
			Data:        command.Payload,
		},
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode device command event: %w", err)
	}

	token := c.client.Publish(commandTopic(deviceID), 1, false, encoded)
	select {
	case <-ctx.Done():
		return fmt.Errorf("publish device command: %w", ctx.Err())
	case <-tokenDone(token):
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish device command: %w", err)
	}
	return nil
}

// tokenDone exposes a paho token's completion channel so callers can bound a
// publish with a context.
func tokenDone(token paho.Token) <-chan struct{} {
	return token.Done()
}

// DeviceCommandEvent is the platform-to-device command envelope defined by
// packages/contracts/events/device_event.schema.json.
type DeviceCommandEvent struct {
	SchemaVersion string               `json:"schema_version"`
	EventID       string               `json:"event_id"`
	EventType     string               `json:"event_type"`
	OccurredAt    string               `json:"occurred_at"`
	Producer      string               `json:"producer"`
	Subject       DeviceCommandSubject `json:"subject"`
	RequestID     string               `json:"request_id"`
	Payload       DeviceCommandPayload `json:"payload"`
}

// DeviceCommandSubject identifies the target device.
type DeviceCommandSubject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// DeviceCommandPayload carries the authored command plus routing identifiers.
type DeviceCommandPayload struct {
	CommandID   string         `json:"command_id"`
	CommandType string         `json:"command_type"`
	RequestID   string         `json:"request_id"`
	Data        map[string]any `json:"data,omitempty"`
}

// DeviceCommandAck is the device-to-platform command result envelope. It uses
// the same device_event framing as the command publish.
type DeviceCommandAck struct {
	SchemaVersion string                  `json:"schema_version"`
	EventID       string                  `json:"event_id"`
	EventType     string                  `json:"event_type"`
	OccurredAt    string                  `json:"occurred_at"`
	Producer      string                  `json:"producer"`
	Subject       DeviceCommandSubject    `json:"subject"`
	RequestID     string                  `json:"request_id"`
	Payload       DeviceCommandAckPayload `json:"payload"`
}

// DeviceCommandAckPayload carries the applied command result.
type DeviceCommandAckPayload struct {
	CommandID  string `json:"command_id"`
	Status     string `json:"status"`
	ResultCode string `json:"result_code,omitempty"`
	RequestID  string `json:"request_id"`
}

// CommandAckHandler consumes one validated device command acknowledgement.
// The device id comes from the authenticated topic, never from the payload.
type CommandAckHandler interface {
	HandleCommandAck(
		ctx context.Context,
		deviceID string,
		ack DeviceCommandAck,
	) error
}

// ackTopicPrefix is the versioned shared prefix for command acknowledgements.
const ackTopicPrefix = "sprout/v1/devices/"
const ackTopicSuffix = "/commands/ack"

// DeviceIDFromAckTopic extracts the device id from a command ack topic.
//
// It returns ok=false for any topic outside
// sprout/v1/devices/{device_id}/commands/ack so the subscriber never trusts a
// payload-supplied identity on an unexpected topic.
func DeviceIDFromAckTopic(topic string) (string, bool) {
	topic = strings.TrimSpace(topic)
	if !strings.HasPrefix(topic, ackTopicPrefix) ||
		!strings.HasSuffix(topic, ackTopicSuffix) {
		return "", false
	}
	deviceID := topic[len(ackTopicPrefix) : len(topic)-len(ackTopicSuffix)]
	if deviceID == "" || strings.Contains(deviceID, "/") {
		return "", false
	}
	return deviceID, true
}

// SubscribeCommandAcks subscribes to every device command acknowledgement
// topic with QoS 1 and dispatches validated acks to the handler.
//
// A malformed or unauthenticated-topic message is dropped without calling the
// handler. The broker performs mutual-TLS device authentication, and the
// topic's device id is the only identity the handler trusts.
func (c *Client) SubscribeCommandAcks(
	ctx context.Context,
	handler CommandAckHandler,
) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("subscribe command acks: MQTT client is not connected")
	}
	if handler == nil {
		return fmt.Errorf("subscribe command acks: handler is required")
	}
	token := c.client.Subscribe(
		"sprout/v1/devices/+/commands/ack",
		1,
		func(_ paho.Client, message paho.Message) {
			deviceID, ok := DeviceIDFromAckTopic(message.Topic())
			if !ok {
				return
			}
			var ack DeviceCommandAck
			if err := json.Unmarshal(message.Payload(), &ack); err != nil {
				return
			}
			if ack.SchemaVersion != "1.0.0" || ack.EventType != "device.command_ack" {
				return
			}
			if ack.Subject.Type != "device" || ack.Subject.ID != deviceID {
				return
			}
			if strings.TrimSpace(ack.Payload.CommandID) == "" {
				return
			}
			// The paho callback must not block its ordered dispatch goroutine,
			// so persistence runs separately. The handler is safe for
			// concurrent use; the durable command row plus the device HTTP
			// poll remain the recovery path if persistence fails here.
			go func() {
				_ = handler.HandleCommandAck(ctx, deviceID, ack)
			}()
		},
	)
	select {
	case <-ctx.Done():
		return fmt.Errorf("subscribe command acks: %w", ctx.Err())
	case <-tokenDone(token):
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe command acks: %w", err)
	}
	return nil
}

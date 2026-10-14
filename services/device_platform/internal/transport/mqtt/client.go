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
	SchemaVersion string                `json:"schema_version"`
	EventID       string                `json:"event_id"`
	EventType     string                `json:"event_type"`
	OccurredAt    string                `json:"occurred_at"`
	Producer      string                `json:"producer"`
	Subject       DeviceCommandSubject  `json:"subject"`
	RequestID     string                `json:"request_id"`
	Payload       DeviceCommandPayload  `json:"payload"`
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

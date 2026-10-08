// Package mqtt contains the MQTT device-control transport.
package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
)

// Client owns the MQTT connection used by device-control handlers.
type Client struct {
	client paho.Client
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

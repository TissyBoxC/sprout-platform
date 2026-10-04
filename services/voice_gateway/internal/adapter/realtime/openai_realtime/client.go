// Package openai_realtime contains the OpenAI Realtime WebSocket adapter.
package openai_realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/realtime"
	"github.com/gorilla/websocket"
)

const (
	defaultBaseURL      = "https://api.openai.com"
	defaultModel        = "gpt-4o-realtime-preview"
	defaultVoice        = "alloy"
	defaultTimeout      = 60 * time.Second
	maxIncomingMessage  = 1 << 20
	maxAudioDeltaBytes  = 8 << 20
	readLimitBufferSize = 32 << 10
)

// Config controls the OpenAI Realtime session.
//
// APIKey is sent only in the handshake header. Instructions may contain child
// safety policy and are supplied by the caller; they are never logged here.
type Config struct {
	BaseURL      string
	APIKey       string
	Model        string
	Voice        string
	Instructions string
	Dialer       *websocket.Dialer
	Timeout      time.Duration
}

// Client connects to the OpenAI Realtime API.
type Client struct {
	baseURL      string
	apiKey       string
	model        string
	voice        string
	instructions string
	dialer       *websocket.Dialer
	timeout      time.Duration
}

// New creates a Realtime connector. APIKey is required at Connect time.
func New(config Config) *Client {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = defaultModel
	}
	voice := strings.TrimSpace(config.Voice)
	if voice == "" {
		voice = defaultVoice
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	return &Client{
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(config.APIKey),
		model:        model,
		voice:        voice,
		instructions: config.Instructions,
		dialer:       dialer,
		timeout:      timeout,
	}
}

// Connect opens the WebSocket, sends session.update, and starts the receive
// loop. It returns only after the handshake and session update are accepted.
func (c *Client) Connect(ctx context.Context) (realtime.Session, error) {
	if c == nil {
		return nil, errors.New("openai realtime client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("openai realtime API key is not configured")
	}
	endpoint, err := realtimeURL(c.baseURL)
	if err != nil {
		return nil, err
	}
	dialContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	connection, response, err := c.dialer.DialContext(dialContext, endpoint, headers)
	if err != nil {
		if response != nil && response.Body != nil {
			defer response.Body.Close()
		}
		return nil, fmt.Errorf("openai realtime handshake failed: %w", err)
	}
	session := &session{
		connection: connection,
		out:        make(chan []byte, 16),
		done:       make(chan struct{}),
	}
	intro := sessionUpdate{
		Type: "session.update",
		Session: sessionConfig{
			Modalities:        []string{"audio", "text"},
			Instructions:      c.instructions,
			Voice:             c.voice,
			InputAudioFormat:  "pcm16",
			OutputAudioFormat: "pcm16",
		},
		Model: c.model,
	}
	if err := session.writeJSON(intro); err != nil {
		_ = connection.Close()
		return nil, err
	}
	go session.read()
	return session, nil
}

type sessionUpdate struct {
	Type    string        `json:"type"`
	Session sessionConfig `json:"session"`
	Model   string        `json:"model,omitempty"`
}

type sessionConfig struct {
	Modalities        []string `json:"modalities"`
	Instructions      string   `json:"instructions,omitempty"`
	Voice             string   `json:"voice,omitempty"`
	InputAudioFormat  string   `json:"input_audio_format"`
	OutputAudioFormat string   `json:"output_audio_format"`
}

type session struct {
	connection *websocket.Conn
	out        chan []byte
	done       chan struct{}
	once       sync.Once

	writeMu sync.Mutex
	mu      sync.Mutex
	err     error
}

func (s *session) Audio() <-chan []byte { return s.out }

func (s *session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// WriteAudio appends one PCM segment as base64 input audio. The Realtime API
// expects PCM16 at 16 kHz mono in this adapter profile.
func (s *session) WriteAudio(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	select {
	case <-s.done:
		return errors.New("openai realtime session is closed")
	default:
	}
	payload := map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(data),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode realtime audio append: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.connection.WriteMessage(websocket.TextMessage, encoded); err != nil {
		return fmt.Errorf("write realtime audio append: %w", err)
	}
	return nil
}

// Close closes the WebSocket and waits for the receiver. It is idempotent.
func (s *session) Close() error {
	var closeErr error
	s.once.Do(func() {
		s.markDone()
		closeErr = s.connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		if closeErr != nil {
			closeErr = s.connection.Close()
		}
	})
	<-s.done
	return closeErr
}

func (s *session) markDone() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *session) writeJSON(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode realtime control message: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.connection.WriteMessage(websocket.TextMessage, encoded); err != nil {
		return fmt.Errorf("write realtime control message: %w", err)
	}
	return nil
}

func (s *session) read() {
	defer close(s.out)
	defer s.markDone()
	s.connection.SetReadLimit(maxIncomingMessage)
	for {
		messageType, payload, err := s.connection.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			s.setError(fmt.Errorf("read openai realtime message: %w", err))
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal(payload, &event); err != nil {
			s.setError(fmt.Errorf("decode openai realtime message: %w", err))
			return
		}
		switch event.Type {
		case "response.output_audio.delta", "response.audio.delta":
			audioData := event.Delta
			if audioData == "" {
				audioData = event.Audio
			}
			audio, err := base64.StdEncoding.DecodeString(audioData)
			if err != nil {
				s.setError(fmt.Errorf("decode openai realtime audio: %w", err))
				return
			}
			if len(audio) == 0 {
				continue
			}
			if len(audio) > maxAudioDeltaBytes {
				s.setError(errors.New("openai realtime audio delta exceeds the safety limit"))
				return
			}
			select {
			case s.out <- audio:
			case <-s.done:
				return
			}
		case "error":
			s.setError(errors.New("openai realtime provider returned an error event"))
			return
		}
	}
}

func (s *session) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func realtimeURL(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse openai realtime base URL: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("openai realtime base URL has unsupported scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/realtime"
	return parsed.String(), nil
}

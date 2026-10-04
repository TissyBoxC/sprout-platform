package openai_realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnectConfiguresSessionAndStreamsAudio(t *testing.T) {
	received := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/realtime" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("missing authorization header")
		}
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer connection.Close()
		for index := 0; index < 2; index++ {
			_, payload, err := connection.ReadMessage()
			if err != nil {
				t.Fatalf("read message: %v", err)
			}
			var event map[string]any
			if err := json.Unmarshal(payload, &event); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			received <- event
		}
		_ = connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_audio.delta","delta":"`+
			base64.StdEncoding.EncodeToString([]byte("audio"))+`"}`))
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:      server.URL,
		APIKey:       "key",
		Instructions: "安全指令",
		Dialer:       websocket.DefaultDialer,
	})
	session, err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	if err := session.WriteAudio([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	first := <-received
	if first["type"] != "session.update" {
		t.Fatalf("unexpected first event %+v", first)
	}
	second := <-received
	if second["type"] != "input_audio_buffer.append" {
		t.Fatalf("unexpected second event %+v", second)
	}
	audio := <-session.Audio()
	if string(audio) != "audio" {
		t.Fatalf("unexpected audio %q", audio)
	}
}

func TestConnectRequiresKey(t *testing.T) {
	client := New(Config{BaseURL: "http://127.0.0.1"})
	if _, err := client.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("expected API key error, got %v", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer connection.Close()
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, APIKey: "key"})
	session, err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = session.Close()
		_ = session.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not return")
	}
}

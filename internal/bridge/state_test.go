package bridge

import (
	"path/filepath"
	"testing"
)

func TestWriteAndLoadDirectState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_bridge", directStateFileName)
	state := DirectState{
		SessionID:        "sess_bridge",
		WorkingDirectory: "/work",
		RuntimeState:     DirectRuntimeRunning,
		URL:              "http://127.0.0.1:1234",
		WebSocketURL:     "ws://127.0.0.1:1234/ws",
		TokenRequired:    true,
		Commands:         2,
	}
	if err := WriteDirectState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDirectState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_bridge" || loaded.GeneratedAt == "" || loaded.RuntimeState != DirectRuntimeRunning || loaded.WebSocketURL != "ws://127.0.0.1:1234/ws" || !loaded.TokenRequired {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestDirectWebSocketURL(t *testing.T) {
	if got := directWebSocketURL("http://127.0.0.1:1234"); got != "ws://127.0.0.1:1234/ws" {
		t.Fatalf("ws url = %q", got)
	}
	if got := directWebSocketURL("https://example.com/base"); got != "wss://example.com/base/ws" {
		t.Fatalf("wss url = %q", got)
	}
}

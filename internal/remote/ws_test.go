package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchWebSocketEventsUsesAuthAndDecodesFrames(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.String()
		conn := acceptRemoteTestWebSocket(t, w, r)
		defer conn.Close()
		writeRemoteTestFrame(t, conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-ws","team":"remote/team","event":"deploy","message":"Ship it."}`))
		writeRemoteTestFrame(t, conn, remoteWebSocketOpcodeClose, []byte{0x03, 0xe8})
	}))
	defer server.Close()

	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/events?token=secret",
		AuthToken:    "ws-token",
	})
	if result.Error != "" || result.FrameCount != 1 || len(result.Events) != 1 {
		t.Fatalf("websocket result = %#v", result)
	}
	if gotAuth != "Bearer ws-token" || gotPath != "/events?token=secret" {
		t.Fatalf("auth/path = %q %q", gotAuth, gotPath)
	}
	event := result.Events[0]
	if event.EventID != "evt-ws" || event.TeamID != "remote/team" || event.Event != "deploy" || event.Message != "Ship it." {
		t.Fatalf("event = %#v", event)
	}
}

func TestFetchWebSocketEventsReportsInvalidURLRedacted(t *testing.T) {
	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL: "https://user:pass@example.invalid/ws?token=secret",
	})
	if result.Error == "" || strings.Contains(result.Error, "token=secret") || strings.Contains(result.Error, "user:pass") {
		t.Fatalf("error = %q", result.Error)
	}
}

func acceptRemoteTestWebSocket(t *testing.T, w http.ResponseWriter, r *http.Request) netConn {
	t.Helper()
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		t.Fatalf("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Fatalf("response writer cannot hijack")
	}
	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", remoteWebSocketAccept(key))
	if _, err := bufrw.WriteString(response); err != nil {
		t.Fatal(err)
	}
	if err := bufrw.Flush(); err != nil {
		t.Fatal(err)
	}
	return conn
}

type netConn interface {
	Write([]byte) (int, error)
	Close() error
}

func writeRemoteTestFrame(t *testing.T, conn netConn, opcode byte, payload []byte) {
	t.Helper()
	header, _, err := remoteWebSocketFrameHeader(opcode, payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
}

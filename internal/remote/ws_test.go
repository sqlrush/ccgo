package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestFetchWebSocketEventsReconnectsAndReadsMultipleFrames(t *testing.T) {
	connections := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections++
		conn := acceptRemoteTestWebSocket(t, w, r)
		defer conn.Close()
		if connections == 1 {
			writeRemoteTestFrame(t, conn, remoteWebSocketOpcodeClose, []byte{0x03, 0xf3})
			return
		}
		writeRemoteTestFrame(t, conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-1","team":"remote/team","message":"one"}`))
		writeRemoteTestFrame(t, conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-2","team":"remote/team","message":"two"}`))
	}))
	defer server.Close()

	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL:          "ws" + strings.TrimPrefix(server.URL, "http") + "/events",
		MaxFrames:             2,
		ReconnectAttempts:     1,
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
	})
	if result.Error != "" || result.ConnectCount != 2 || result.ReconnectCount != 1 || result.FrameCount != 2 || len(result.Events) != 2 {
		t.Fatalf("websocket result = %#v connections=%d", result, connections)
	}
	if result.Events[0].EventID != "evt-1" || result.Events[1].EventID != "evt-2" || result.LastError == "" {
		t.Fatalf("events/last error = %#v last=%q", result.Events, result.LastError)
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

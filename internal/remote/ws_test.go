package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetchWebSocketEventsUsesAuthAndDecodesFrames(t *testing.T) {
	var gotAuth string
	var gotPath string
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.String()
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-ws","team":"remote/team","event":"deploy","message":"Ship it."}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeClose, []byte{0x03, 0xe8}); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/events?token=secret",
		AuthToken:    "ws-token",
		MaxFrames:    2,
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
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
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		connections++
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if connections == 1 {
			if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeClose, []byte{0x03, 0xf3}); err != nil {
				recordRemoteTestHandlerError(handlerErrors, err)
			}
			return
		}
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-1","team":"remote/team","message":"one"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-2","team":"remote/team","message":"two"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL:          "ws" + strings.TrimPrefix(server.URL, "http") + "/events",
		MaxFrames:             2,
		ReconnectAttempts:     1,
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
	if result.Error != "" || result.ConnectCount != 2 || result.ReconnectCount != 1 || result.FrameCount != 2 || len(result.Events) != 2 {
		t.Fatalf("websocket result = %#v connections=%d", result, connections)
	}
	if result.Events[0].EventID != "evt-1" || result.Events[1].EventID != "evt-2" || result.LastError == "" {
		t.Fatalf("events/last error = %#v last=%q", result.Events, result.LastError)
	}
}

func TestFetchWebSocketEventsHonorsUpgradeRetryAfter(t *testing.T) {
	connections := 0
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		connections++
		if connections == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-retry-after","team":"remote/team","message":"retry after"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	start := time.Now()
	result := FetchWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL:          "ws" + strings.TrimPrefix(server.URL, "http") + "/events",
		MaxFrames:             1,
		ReconnectAttempts:     1,
		ReconnectInitialDelay: time.Second,
		ReconnectMaxDelay:     time.Second,
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
	if result.Error != "" || result.ConnectCount != 1 || result.ReconnectCount != 1 || result.FrameCount != 1 || len(result.Events) != 1 || result.Events[0].EventID != "evt-retry-after" {
		t.Fatalf("websocket retry-after result = %#v connections=%d", result, connections)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("websocket reconnect ignored Retry-After header; elapsed=%s", elapsed)
	}
}

func TestStreamWebSocketEventsReconnectsAndCallsHandler(t *testing.T) {
	connections := 0
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		connections++
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if connections == 1 {
			if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeClose, []byte{0x03, 0xf3}); err != nil {
				recordRemoteTestHandlerError(handlerErrors, err)
			}
			return
		}
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-stream-1","team":"remote/team","message":"one"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-stream-2","team":"remote/team","message":"two"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	var delivered []PollEvent
	result := StreamWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL:          "ws" + strings.TrimPrefix(server.URL, "http") + "/stream",
		MaxFrames:             2,
		ReconnectAttempts:     1,
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
	}, func(events []PollEvent) error {
		delivered = append(delivered, events...)
		return nil
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
	if result.Error != "" || result.ConnectCount != 2 || result.ReconnectCount != 1 || result.FrameCount != 2 || len(result.Events) != 0 || len(delivered) != 2 {
		t.Fatalf("stream result=%#v delivered=%#v connections=%d", result, delivered, connections)
	}
	if delivered[0].EventID != "evt-stream-1" || delivered[1].EventID != "evt-stream-2" || result.LastError == "" {
		t.Fatalf("delivered=%#v last=%q", delivered, result.LastError)
	}
}

func TestStreamWebSocketEventsHonorsUpgradeRetryAfter(t *testing.T) {
	connections := 0
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		connections++
		if connections == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "slow down", http.StatusServiceUnavailable)
			return
		}
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-stream-retry-after","team":"remote/team","message":"retry after"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	var delivered []PollEvent
	start := time.Now()
	result := StreamWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL:          "ws" + strings.TrimPrefix(server.URL, "http") + "/stream",
		MaxFrames:             1,
		ReconnectAttempts:     1,
		ReconnectInitialDelay: time.Second,
		ReconnectMaxDelay:     time.Second,
	}, func(events []PollEvent) error {
		delivered = append(delivered, events...)
		return nil
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
	if result.Error != "" || result.ConnectCount != 1 || result.ReconnectCount != 1 || result.FrameCount != 1 || len(delivered) != 1 || delivered[0].EventID != "evt-stream-retry-after" {
		t.Fatalf("stream retry-after result=%#v delivered=%#v connections=%d", result, delivered, connections)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("stream reconnect ignored Retry-After header; elapsed=%s", elapsed)
	}
}

func TestStreamWebSocketEventsReturnsHandlerError(t *testing.T) {
	var handlers sync.WaitGroup
	handlerErrors := make(chan error, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		conn, err := acceptRemoteTestWebSocket(w, r)
		if err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
		defer conn.Close()
		if err := writeRemoteTestFrame(conn, remoteWebSocketOpcodeText, []byte(`{"id":"evt-stream","team":"remote/team","message":"fail"}`)); err != nil {
			recordRemoteTestHandlerError(handlerErrors, err)
			return
		}
	}))
	defer server.Close()

	result := StreamWebSocketEvents(context.Background(), WebSocketOptions{
		WebSocketURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/stream",
		MaxFrames:    1,
	}, func(events []PollEvent) error {
		return fmt.Errorf("handler failed for %s", events[0].EventID)
	})
	waitRemoteTestHandlers(t, &handlers, handlerErrors)
	if result.Error != "handler failed for evt-stream" || result.FrameCount != 1 {
		t.Fatalf("stream result = %#v", result)
	}
}

func acceptRemoteTestWebSocket(w http.ResponseWriter, r *http.Request) (netConn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("response writer cannot hijack")
	}
	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", remoteWebSocketAccept(key))
	if _, err := bufrw.WriteString(response); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := bufrw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

type netConn interface {
	Write([]byte) (int, error)
	Close() error
}

func writeRemoteTestFrame(conn netConn, opcode byte, payload []byte) error {
	header, _, err := remoteWebSocketFrameHeader(opcode, payload, false)
	if err != nil {
		return err
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func recordRemoteTestHandlerError(errors chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case errors <- err:
	default:
	}
}

func waitRemoteTestHandlers(t *testing.T, handlers *sync.WaitGroup, errors <-chan error) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		handlers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("websocket test handler did not finish")
	}
	for {
		select {
		case err := <-errors:
			if err != nil {
				t.Fatal(err)
			}
		default:
			return
		}
	}
}

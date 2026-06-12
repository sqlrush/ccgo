package mcp

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestWSTransportRoundTrip(t *testing.T) {
	requests := make(chan RPCRequest, 1)
	server := newTestWebSocketServer(t, func(r *http.Request, conn net.Conn, reader *bufio.Reader) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization = %q", got)
		}
		opcode, payload, err := readServerWebSocketFrame(reader)
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		if opcode != webSocketOpcodeText {
			t.Errorf("opcode = %d", opcode)
			return
		}
		var request RPCRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Errorf("request json: %v", err)
			return
		}
		requests <- request
		inbound := `{"jsonrpc":"2.0","id":"server-1","method":"elicitation/create","params":{"message":"Proceed?"}}`
		if err := writeServerWebSocketFrame(conn, webSocketOpcodeText, []byte(inbound)); err != nil {
			t.Errorf("write inbound request: %v", err)
			return
		}
		_, clientResponse, err := readServerWebSocketFrame(reader)
		if err != nil {
			t.Errorf("read inbound response: %v", err)
			return
		}
		if !strings.Contains(string(clientResponse), `"id":"server-1"`) || !strings.Contains(string(clientResponse), `"action":"decline"`) {
			t.Errorf("client response = %s", clientResponse)
			return
		}
		notification := `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"debug"}}`
		if err := writeServerWebSocketFrame(conn, webSocketOpcodeText, []byte(notification)); err != nil {
			t.Errorf("write notification: %v", err)
			return
		}
		response := `{"jsonrpc":"2.0","id":"` + request.ID + `","result":{"tools":[{"name":"ws-tool","readOnly":true}]}}`
		if err := writeServerWebSocketFrame(conn, webSocketOpcodeText, []byte(response)); err != nil {
			t.Errorf("write frame: %v", err)
		}
	})
	defer server.Close()

	transport := NewWSTransport(wsURL(server.URL, "/mcp"), map[string]string{"Authorization": "Bearer token"})
	transport.SetRequestHandler(func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		if request.Method != "elicitation/create" || !strings.Contains(string(request.Params), "Proceed?") {
			t.Fatalf("request = %#v", request)
		}
		return map[string]any{"action": "decline"}, nil
	})
	var notifications []RPCNotification
	transport.SetNotificationHandler(func(notification RPCNotification) {
		notifications = append(notifications, notification)
	})
	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("5", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "5" || !strings.Contains(string(response.Result), `"ws-tool"`) {
		t.Fatalf("response = %#v", response)
	}
	request := <-requests
	if request.Method != "tools/list" {
		t.Fatalf("request = %#v", request)
	}
	if len(notifications) != 1 || notifications[0].Method != "notifications/message" || !strings.Contains(string(notifications[0].Params), `"debug"`) {
		t.Fatalf("notifications = %#v", notifications)
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenServerClientSupportsWSTransport(t *testing.T) {
	server := newTestWebSocketServer(t, func(r *http.Request, conn net.Conn, reader *bufio.Reader) {
		_, payload, err := readServerWebSocketFrame(reader)
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		var request RPCRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Errorf("request json: %v", err)
			return
		}
		response := `{"jsonrpc":"2.0","id":"` + request.ID + `","result":{"tools":[{"name":"open-ws","readOnly":true}]}}`
		if err := writeServerWebSocketFrame(conn, webSocketOpcodeText, []byte(response)); err != nil {
			t.Errorf("write frame: %v", err)
		}
	})
	defer server.Close()

	handle, err := OpenServerClient(context.Background(), "remote", contracts.MCPServer{
		Type: TransportWS,
		URL:  wsURL(server.URL, "/mcp"),
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := handle.Client.ListTools(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "open-ws" || !tools[0].ReadOnly {
		t.Fatalf("tools = %#v", tools)
	}
	if handle.Close == nil {
		t.Fatal("expected ws close")
	}
}

func TestWSTransportRejectsBadAccept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", "wrong")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer server.Close()

	_, err := NewWSTransport(wsURL(server.URL, "/mcp"), nil).RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil))
	if err == nil || !strings.Contains(err.Error(), "accept mismatch") {
		t.Fatalf("expected accept mismatch, got %v", err)
	}
}

func TestWSTransportRoundTripHonorsContextCancellation(t *testing.T) {
	requestRead := make(chan struct{})
	server := newTestWebSocketServer(t, func(r *http.Request, _ net.Conn, reader *bufio.Reader) {
		_, _, err := readServerWebSocketFrame(reader)
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		close(requestRead)
		<-r.Context().Done()
	})
	defer server.Close()

	transport := NewWSTransport(wsURL(server.URL, "/mcp"), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(ctx, NewRPCRequest("cancel", "tools/list", nil))
		done <- err
	}()

	select {
	case <-requestRead:
	case <-time.After(time.Second):
		t.Fatal("websocket request was not sent")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("round trip did not return after context cancellation")
	}
}

func newTestWebSocketServer(t *testing.T, handle func(*http.Request, net.Conn, *bufio.Reader)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("missing hijacker")
		}
		conn, readWriter, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		if key == "" {
			t.Error("missing websocket key")
			_ = conn.Close()
			return
		}
		response := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + testWebSocketAccept(key) + "\r\n\r\n"
		if _, err := conn.Write([]byte(response)); err != nil {
			t.Error(err)
			_ = conn.Close()
			return
		}
		go func() {
			defer conn.Close()
			handle(r, conn, readWriter.Reader)
		}()
	}))
}

func readServerWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	opcode, _, payload, err := readSingleWebSocketFrame(reader, DefaultWebSocketFrameLimit)
	return opcode, payload, err
}

func writeServerWebSocketFrame(conn net.Conn, opcode byte, payload []byte) error {
	header, _, err := webSocketFrameHeader(opcode, payload, false)
	if err != nil {
		return err
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(payload)
	return err
}

func wsURL(raw string, path string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	parsed.Scheme = "ws"
	parsed.Path = path
	return parsed.String()
}

func testWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + webSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func TestWebSocketDialAddress(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"ws://example.com/mcp", "example.com:80"},
		{"wss://example.com/mcp", "example.com:443"},
		{"ws://example.com:8080/mcp", "example.com:8080"},
	} {
		parsed, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := webSocketDialAddress(parsed); got != tc.want {
			t.Fatalf("%s address = %q", tc.raw, got)
		}
	}
}

func TestWriteWebSocketHandshakeIncludesQuery(t *testing.T) {
	parsed, err := url.Parse("ws://example.com/mcp?x=1")
	if err != nil {
		t.Fatal(err)
	}
	var builder strings.Builder
	if err := writeWebSocketHandshake(&builder, parsed, "key", map[string]string{"X-Test": "yes"}, "2025-01-01"); err != nil {
		t.Fatal(err)
	}
	text := builder.String()
	for _, want := range []string{"GET /mcp?x=1 HTTP/1.1", "X-Test: yes", "mcp-protocol-version: 2025-01-01"} {
		if !strings.Contains(text, want) {
			t.Fatalf("handshake missing %q in:\n%s", want, text)
		}
	}
}

func Example_webSocketAccept() {
	fmt.Println(webSocketAccept("dGhlIHNhbXBsZSBub25jZQ=="))
	// Output: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
}

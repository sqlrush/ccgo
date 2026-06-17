package bridge

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"ccgo/internal/commands"
)

func TestDirectWebSocketResolveAndExecute(t *testing.T) {
	server, err := StartDirectServer(DirectServerOptions{
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	conn, reader := dialDirectWebSocket(t, server.URL()+"/ws", nil)
	defer conn.Close()

	writeClientWebSocketText(t, conn, `{"action":"resolve","command":"/question prod deploy"}`)
	var resolved DirectWebSocketResponse
	readServerWebSocketJSON(t, reader, &resolved)
	if resolved.Type != "resolve" || resolved.Resolve == nil || !resolved.Resolve.Allowed || resolved.Resolve.Name != "ask" || resolved.Resolve.Args != "prod deploy" {
		t.Fatalf("resolve websocket response = %#v", resolved)
	}

	writeClientWebSocketText(t, conn, `{"action":"execute","command":"compact focus on API","uuid":"user_bridge"}`)
	var executed DirectWebSocketResponse
	readServerWebSocketJSON(t, reader, &executed)
	if executed.Type != "execute" || executed.Execute == nil || !executed.Execute.Allowed || !executed.Execute.Handled || executed.Execute.Name != "compact" {
		t.Fatalf("execute websocket response = %#v", executed)
	}
	if executed.Execute.LocalResult == nil || executed.Execute.LocalResult.Type != commands.LocalCommandResultCompact || !executed.Execute.LocalResult.HasValue {
		t.Fatalf("execute local result = %#v", executed.Execute.LocalResult)
	}
}

func TestDirectWebSocketRemoteTrigger(t *testing.T) {
	var got DirectRemoteTriggerRequest
	server, err := StartDirectServer(DirectServerOptions{
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
			RemoteTrigger: func(_ context.Context, req DirectRemoteTriggerRequest) (DirectRemoteTriggerResponse, int) {
				got = req
				return DirectRemoteTriggerResponse{
					Accepted:  true,
					TeamID:    req.TeamID,
					Target:    req.Target,
					EventID:   req.EventID,
					SentCount: 1,
				}, http.StatusOK
			},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	conn, reader := dialDirectWebSocket(t, server.URL()+"/ws", nil)
	defer conn.Close()

	writeClientWebSocketText(t, conn, `{"action":"remote_trigger","remote_trigger":{"team_id":"ops/team","target":"coordinator","event_id":"evt-2","message":"Investigate deploy."}}`)
	var response DirectWebSocketResponse
	readServerWebSocketJSON(t, reader, &response)
	if response.Type != "remote_trigger" || response.RemoteTrigger == nil || !response.RemoteTrigger.Accepted || response.RemoteTrigger.SentCount != 1 || response.RemoteTrigger.EventID != "evt-2" {
		t.Fatalf("remote trigger websocket response = %#v", response)
	}
	if got.TeamID != "ops/team" || got.Target != "coordinator" || got.EventID != "evt-2" || got.Message != "Investigate deploy." {
		t.Fatalf("remote trigger websocket request = %#v", got)
	}
}

func TestDirectWebSocketUsesDirectTokenGuard(t *testing.T) {
	server, err := StartDirectServer(DirectServerOptions{
		Token: "secret",
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	resp, err := http.Get(server.URL() + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized websocket status = %d", resp.StatusCode)
	}
	conn, _ := dialDirectWebSocket(t, server.URL()+"/ws", map[string]string{"X-Bridge-Token": "secret"})
	conn.Close()
}

func dialDirectWebSocket(t *testing.T, raw string, headers map[string]string) (net.Conn, *bufio.Reader) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := parsed.RequestURI()
	if path == "" {
		path = "/"
	}
	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", path, parsed.Host, key)
	for name, value := range headers {
		_, _ = fmt.Fprintf(&b, "%s: %s\r\n", name, value)
	}
	_, _ = b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		t.Fatalf("websocket status = %q", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn, reader
}

func writeClientWebSocketText(t *testing.T, w io.Writer, payload string) {
	t.Helper()
	data := []byte(payload)
	mask := [4]byte{1, 2, 3, 4}
	header := []byte{0x81}
	switch {
	case len(data) < 126:
		header = append(header, 0x80|byte(len(data)))
	case len(data) <= 0xFFFF:
		header = append(header, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(header[2:], uint16(len(data)))
	default:
		t.Fatalf("test payload too large: %d", len(data))
	}
	frame := append(header, mask[:]...)
	for i, b := range data {
		frame = append(frame, b^mask[i%4])
	}
	if _, err := w.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func readServerWebSocketJSON(t *testing.T, reader *bufio.Reader, target any) {
	t.Helper()
	opcode, payload := readServerWebSocketText(t, reader)
	if opcode != 0x1 {
		t.Fatalf("opcode = %d, want text", opcode)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatal(err)
	}
}

func readServerWebSocketText(t *testing.T, reader *bufio.Reader) (byte, []byte) {
	t.Helper()
	first, err := reader.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	opcode := first & 0x0F
	length := int(second & 0x7F)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(reader, buf[:]); err != nil {
			t.Fatal(err)
		}
		length = int(binary.BigEndian.Uint16(buf[:]))
	case 127:
		t.Fatal("unexpected large websocket frame in test")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return opcode, payload
}

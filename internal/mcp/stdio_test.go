package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type signalingWriter struct {
	bytes.Buffer
	wrote chan struct{}
	once  sync.Once
}

func (w *signalingWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.once.Do(func() {
		close(w.wrote)
	})
	return n, err
}

func TestStdioTransportRoundTripWritesRequestAndReadsResponse(t *testing.T) {
	reader := strings.NewReader(`{"jsonrpc":"2.0","id":"1","result":{"ok":true}}` + "\n")
	var writer bytes.Buffer
	transport := NewStdioTransport(reader, &writer)

	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "ping", map[string]any{"x": "y"}))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "1" || !strings.Contains(string(response.Result), `"ok":true`) {
		t.Fatalf("response = %#v", response)
	}
	var request RPCRequest
	if err := json.Unmarshal(bytes.TrimSpace(writer.Bytes()), &request); err != nil {
		t.Fatal(err)
	}
	if request.JSONRPC != JSONRPCVersion || request.ID != "1" || request.Method != "ping" {
		t.Fatalf("request = %#v", request)
	}
	if !strings.HasSuffix(writer.String(), "\n") {
		t.Fatalf("request was not newline terminated: %q", writer.String())
	}
}

func TestStdioTransportSkipsNotificationsUntilMatchingResponse(t *testing.T) {
	reader := strings.NewReader(
		`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}` + "\n" +
			`{"jsonrpc":"2.0","id":"old","result":{"ignored":true}}` + "\n" +
			`{"jsonrpc":"2.0","id":"2","result":{"ok":true}}` + "\n",
	)
	var writer bytes.Buffer
	transport := NewStdioTransport(reader, &writer)

	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("2", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "2" || !strings.Contains(string(response.Result), `"ok":true`) {
		t.Fatalf("response = %#v", response)
	}
}

func TestStdioTransportRejectsInvalidStdout(t *testing.T) {
	transport := NewStdioTransport(strings.NewReader("server log on stdout\n"), &bytes.Buffer{})

	_, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil))
	if err == nil || !strings.Contains(err.Error(), "decode mcp stdio response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestStdioTransportRoundTripHonorsContextCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	requestWriter := &signalingWriter{wrote: make(chan struct{})}
	transport := NewStdioTransport(reader, requestWriter)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(ctx, NewRPCRequest("cancel", "tools/list", nil))
		done <- err
	}()

	select {
	case <-requestWriter.wrote:
	case <-time.After(time.Second):
		t.Fatal("stdio request was not written")
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
	if !strings.Contains(requestWriter.String(), `"id":"cancel"`) {
		t.Fatalf("request = %s", requestWriter.String())
	}
}

func TestStdioEnvMergesOverridesDeterministically(t *testing.T) {
	env := stdioEnv(map[string]string{"ZED": "last", "ABC": "first", " ": "ignored"})
	if len(env) < 2 {
		t.Fatalf("env = %#v", env)
	}
	tail := env[len(env)-2:]
	if tail[0] != "ABC=first" || tail[1] != "ZED=last" {
		t.Fatalf("custom env tail = %#v", tail)
	}
}

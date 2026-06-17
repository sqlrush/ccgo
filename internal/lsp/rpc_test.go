package lsp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadFramedMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFramedMessage(&buf, []byte(`{"jsonrpc":"2.0","method":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "Content-Length: 33\r\n\r\n") {
		t.Fatalf("frame = %q", buf.String())
	}
	payload, err := ReadFramedMessage(bufio.NewReader(&buf), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"jsonrpc":"2.0","method":"ping"}` {
		t.Fatalf("payload = %q", payload)
	}
}

func TestReadFramedMessageAcceptsExtraHeadersAndLF(t *testing.T) {
	input := "Content-Type: application/vscode-jsonrpc; charset=utf-8\nContent-Length: 2\n\n{}"
	payload, err := ReadFramedMessage(bufio.NewReader(strings.NewReader(input)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "{}" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestReadFramedMessageErrors(t *testing.T) {
	if _, err := ReadFramedMessage(bufio.NewReader(strings.NewReader("\r\n{}")), 1024); err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("missing content length err = %v", err)
	}
	if _, err := ReadFramedMessage(bufio.NewReader(strings.NewReader("Content-Length: nope\r\n\r\n")), 1024); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid content length err = %v", err)
	}
	if _, err := ReadFramedMessage(bufio.NewReader(strings.NewReader("Content-Length: 5\r\n\r\nhello")), 4); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("large frame err = %v", err)
	}
	if _, err := ReadFramedMessage(bufio.NewReader(strings.NewReader("")), 1024); !errors.Is(err, io.EOF) {
		t.Fatalf("empty stream err = %v", err)
	}
}

func TestProcessDiagnosticsStreamAppliesPublishDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticsFileName)
	stream := framedMessages(t,
		`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`,
		`{"jsonrpc":"2.0","method":"window/logMessage","params":{"message":"ignored"}}`,
		`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[{"severity":1,"source":"gopls","message":"broken"}]}}`,
		`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[]}}`,
	)
	result, err := ProcessDiagnosticsStream(context.Background(), strings.NewReader(stream), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Messages != 4 || result.DiagnosticsUpdates != 2 || len(result.LastSnapshot) != 0 {
		t.Fatalf("result = %#v", result)
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded diagnostics = %#v", loaded)
	}
}

func TestProcessDiagnosticsStreamPreservesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticsFileName)
	if err := WriteSnapshot(path, []Diagnostic{{FilePath: "/work/other.go", Severity: "warning", Message: "keep"}}); err != nil {
		t.Fatal(err)
	}
	stream := framedMessages(t,
		`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[{"severity":1,"message":"broken"}]}}`,
	)
	result, err := ProcessDiagnosticsStream(context.Background(), strings.NewReader(stream), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiagnosticsUpdates != 1 || len(result.LastSnapshot) != 2 {
		t.Fatalf("result = %#v", result)
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].FilePath != "/work/main.go" || loaded[1].FilePath != "/work/other.go" {
		t.Fatalf("loaded diagnostics = %#v", loaded)
	}
}

func TestProcessDiagnosticsStreamRejectsInvalidInputs(t *testing.T) {
	if _, err := ProcessDiagnosticsStream(context.Background(), nil, "path"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil reader err = %v", err)
	}
	if _, err := ProcessDiagnosticsStream(context.Background(), strings.NewReader(""), ""); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("empty path err = %v", err)
	}
}

func framedMessages(t *testing.T, payloads ...string) string {
	t.Helper()
	var buf bytes.Buffer
	for _, payload := range payloads {
		if err := WriteFramedMessage(&buf, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	return buf.String()
}

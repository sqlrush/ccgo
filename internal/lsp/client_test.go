package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteInitializeAndOpenFramesHandshake(t *testing.T) {
	var buf bytes.Buffer
	err := WriteInitializeAndOpen(context.Background(), &buf, ServerHandshakeOptions{
		InitializeID:  9,
		ProcessID:     1234,
		RootURI:       "file:///work",
		RootPath:      "/work",
		ClientName:    "ccgo-test",
		ClientVersion: "v1",
		Trace:         "off",
		Documents: []OpenDocument{{
			FilePath:   filepath.Join(t.TempDir(), "main.go"),
			LanguageID: "go",
			Version:    7,
			Text:       "package main\n",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeFramedMessages(t, buf.String(), 3)
	if messages[0]["method"] != "initialize" || numberValue(messages[0]["id"]) != 9 {
		t.Fatalf("initialize message = %#v", messages[0])
	}
	initializeParams := objectValue(messages[0]["params"])
	if numberValue(initializeParams["processId"]) != 1234 || initializeParams["rootUri"] != "file:///work" || initializeParams["rootPath"] != "/work" {
		t.Fatalf("initialize params = %#v", initializeParams)
	}
	clientInfo := objectValue(initializeParams["clientInfo"])
	if clientInfo["name"] != "ccgo-test" || clientInfo["version"] != "v1" {
		t.Fatalf("clientInfo = %#v", clientInfo)
	}
	if messages[1]["method"] != "initialized" {
		t.Fatalf("initialized message = %#v", messages[1])
	}
	if messages[2]["method"] != "textDocument/didOpen" {
		t.Fatalf("didOpen message = %#v", messages[2])
	}
	didOpenParams := objectValue(messages[2]["params"])
	textDocument := objectValue(didOpenParams["textDocument"])
	if textDocument["languageId"] != "go" || numberValue(textDocument["version"]) != 7 || textDocument["text"] != "package main\n" {
		t.Fatalf("textDocument = %#v", textDocument)
	}
	if uri, ok := textDocument["uri"].(string); !ok || !strings.HasPrefix(uri, "file://") || !strings.HasSuffix(uri, "/main.go") {
		t.Fatalf("uri = %#v", textDocument["uri"])
	}
}

func TestWriteDidOpenNotificationDefaultsDocumentFields(t *testing.T) {
	var buf bytes.Buffer
	path := filepath.Join(t.TempDir(), "with space.py")
	if err := WriteDidOpenNotification(context.Background(), &buf, OpenDocument{FilePath: path}); err != nil {
		t.Fatal(err)
	}
	message := decodeFramedMessages(t, buf.String(), 1)[0]
	textDocument := objectValue(objectValue(message["params"])["textDocument"])
	if textDocument["languageId"] != "python" || numberValue(textDocument["version"]) != 1 {
		t.Fatalf("textDocument = %#v", textDocument)
	}
	if uri := textDocument["uri"].(string); !strings.Contains(uri, "with%20space.py") {
		t.Fatalf("uri = %q", uri)
	}
}

func TestLSPClientWritersRejectInvalidInputs(t *testing.T) {
	if err := WriteInitializeAndOpen(context.Background(), nil, ServerHandshakeOptions{}); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil writer err = %v", err)
	}
	if err := WriteDidOpenNotification(context.Background(), &bytes.Buffer{}, OpenDocument{}); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("empty doc err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WriteInitializeRequest(ctx, &bytes.Buffer{}, ServerHandshakeOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context err = %v", err)
	}
}

func decodeFramedMessages(t *testing.T, stream string, count int) []map[string]any {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(stream))
	messages := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		payload, err := ReadFramedMessage(reader, defaultFrameLimit)
		if err != nil {
			t.Fatal(err)
		}
		var message map[string]any
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	return messages
}

func objectValue(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return nil
}

func numberValue(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}

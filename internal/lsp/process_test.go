package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartServerProcessConsumesDiagnosticsAndUpdatesStatus(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), diagnosticsFileName)
	statusPath := filepath.Join(t.TempDir(), managerStatusFileName)
	process, err := StartServerProcess(context.Background(), ServerProcessOptions{
		SessionID:         "sess_lsp_process",
		Definition:        helperServerDefinition("helper-diagnostics"),
		SnapshotPath:      snapshotPath,
		ManagerStatusPath: statusPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := <-process.Done()
	if result.RuntimeState != ServerRuntimeExited || result.Error != "" {
		t.Fatalf("process result = %#v", result)
	}
	if result.Diagnostics.Messages != 1 || result.Diagnostics.DiagnosticsUpdates != 1 {
		t.Fatalf("diagnostics result = %#v", result.Diagnostics)
	}
	diagnostics, err := LoadSnapshot(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].FilePath != "/work/main.go" || diagnostics[0].Message != "broken" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	status, err := LoadManagerStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	server := serverStatus(status.Servers, "helper-diagnostics")
	if server.RuntimeState != ServerRuntimeExited || server.ProcessID == 0 || server.StartedAt == "" || server.EndedAt == "" {
		t.Fatalf("server status = %#v", server)
	}
}

func TestStartServerProcessRecordsFailures(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), diagnosticsFileName)
	statusPath := filepath.Join(t.TempDir(), managerStatusFileName)
	process, err := StartServerProcess(context.Background(), ServerProcessOptions{
		SessionID:         "sess_lsp_process",
		Definition:        helperServerDefinition("helper-fail"),
		SnapshotPath:      snapshotPath,
		ManagerStatusPath: statusPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := <-process.Done()
	if result.RuntimeState != ServerRuntimeFailed || result.Error == "" {
		t.Fatalf("process result = %#v", result)
	}
	status, err := LoadManagerStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	server := serverStatus(status.Servers, "helper-fail")
	if server.RuntimeState != ServerRuntimeFailed || server.Reason == "" {
		t.Fatalf("server status = %#v", server)
	}
}

func TestServerProcessStopCancelsRunningProcess(t *testing.T) {
	snapshotPath := filepath.Join(t.TempDir(), diagnosticsFileName)
	process, err := StartServerProcess(context.Background(), ServerProcessOptions{
		Definition:   helperServerDefinition("helper-sleep"),
		SnapshotPath: snapshotPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := process.Stop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeState != ServerRuntimeFailed {
		t.Fatalf("stopped process result = %#v", result)
	}
}

func TestServerProcessInitializeAndOpenWritesHandshakeToStdin(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, diagnosticsFileName)
	capturePath := filepath.Join(dir, "handshake.json")
	process, err := StartServerProcess(context.Background(), ServerProcessOptions{
		Definition:   helperServerDefinition("helper-capture-handshake", capturePath),
		SnapshotPath: snapshotPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.InitializeAndOpen(context.Background(), ServerHandshakeOptions{
		InitializeID:  3,
		ProcessID:     99,
		RootURI:       "file:///work",
		ClientName:    "ccgo-test",
		ClientVersion: "test",
		Documents: []OpenDocument{{
			URI:        "file:///work/main.go",
			LanguageID: "go",
			Version:    5,
			Text:       "package main\n",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	result := <-process.Done()
	if result.RuntimeState != ServerRuntimeExited || result.Error != "" {
		t.Fatalf("process result = %#v", result)
	}
	messages := loadCapturedMessages(t, capturePath)
	if len(messages) != 3 {
		t.Fatalf("captured messages = %#v", messages)
	}
	if messages[0]["method"] != "initialize" || numberValue(messages[0]["id"]) != 3 {
		t.Fatalf("initialize message = %#v", messages[0])
	}
	params := objectValue(messages[0]["params"])
	if numberValue(params["processId"]) != 99 || params["rootUri"] != "file:///work" {
		t.Fatalf("initialize params = %#v", params)
	}
	if messages[1]["method"] != "initialized" {
		t.Fatalf("initialized message = %#v", messages[1])
	}
	textDocument := objectValue(objectValue(messages[2]["params"])["textDocument"])
	if messages[2]["method"] != "textDocument/didOpen" || textDocument["uri"] != "file:///work/main.go" || textDocument["languageId"] != "go" || numberValue(textDocument["version"]) != 5 {
		t.Fatalf("didOpen message = %#v", messages[2])
	}
}

func TestServerProcessCapturesInitializeResponseAndDiagnostics(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, diagnosticsFileName)
	process, err := StartServerProcess(context.Background(), ServerProcessOptions{
		Definition:   helperServerDefinition("helper-lsp-session"),
		SnapshotPath: snapshotPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.InitializeAndOpen(context.Background(), ServerHandshakeOptions{
		InitializeID: 11,
		RootURI:      "file:///work",
		Documents: []OpenDocument{{
			URI:        "file:///work/main.go",
			LanguageID: "go",
			Version:    1,
			Text:       "package main\n",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	result := <-process.Done()
	if result.RuntimeState != ServerRuntimeExited || result.Error != "" {
		t.Fatalf("process result = %#v", result)
	}
	if result.Diagnostics.InitializeResponses != 1 || numberValue(result.Diagnostics.ServerCapabilities["textDocumentSync"]) != 1 {
		t.Fatalf("initialize response = %#v", result.Diagnostics)
	}
	if result.Diagnostics.DiagnosticsUpdates != 1 {
		t.Fatalf("diagnostics result = %#v", result.Diagnostics)
	}
	diagnostics, err := LoadSnapshot(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "session broken" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestStartServerProcessRejectsInvalidOptions(t *testing.T) {
	if _, err := StartServerProcess(context.Background(), ServerProcessOptions{}); err == nil {
		t.Fatal("StartServerProcess accepted empty options")
	}
	if _, err := StartServerProcess(context.Background(), ServerProcessOptions{
		Definition: ServerDefinition{Name: "missing-command"},
	}); err == nil {
		t.Fatal("StartServerProcess accepted missing command")
	}
}

func TestNormalizeServerDefinitionPreservesProcessArgOrder(t *testing.T) {
	definition := normalizeServerDefinition(ServerDefinition{
		Name:    " helper ",
		Command: " cmd ",
		Args:    []string{" -test.run=TestLSPServerProcessHelper ", " -- ", " helper-diagnostics "},
	})
	got := strings.Join(definition.Args, "\n")
	want := strings.Join([]string{"-test.run=TestLSPServerProcessHelper", "--", "helper-diagnostics"}, "\n")
	if got != want {
		t.Fatalf("args = %#v, want %#v", definition.Args, strings.Split(want, "\n"))
	}
}

func helperServerDefinition(mode string, extraArgs ...string) ServerDefinition {
	args := []string{"-test.run=TestLSPServerProcessHelper", "--", mode}
	args = append(args, extraArgs...)
	return ServerDefinition{
		Name:    mode,
		Command: os.Args[0],
		Args:    args,
	}
}

func TestLSPServerProcessHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Getenv("GO_WANT_LSP_HELPER_PROCESS") != "1" {
		return
	}
	mode := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	switch mode {
	case "helper-diagnostics":
		_ = WriteFramedMessage(os.Stdout, []byte(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[{"severity":1,"message":"broken"}]}}`))
		os.Exit(0)
	case "helper-capture-handshake":
		if runCaptureHandshake() != nil {
			os.Exit(8)
		}
		os.Exit(0)
	case "helper-lsp-session":
		if runLSPSession() != nil {
			os.Exit(9)
		}
		os.Exit(0)
	case "helper-fail":
		_, _ = os.Stderr.WriteString("failed\n")
		os.Exit(7)
	case "helper-sleep":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func TestMain(m *testing.M) {
	if shouldRunLSPHelper() {
		os.Setenv("GO_WANT_LSP_HELPER_PROCESS", "1")
	}
	os.Exit(m.Run())
}

func shouldRunLSPHelper() bool {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && strings.HasPrefix(os.Args[i+1], "helper-") {
			return true
		}
	}
	return false
}

func runCaptureHandshake() error {
	capturePath := ""
	for i, arg := range os.Args {
		if arg == "--" && i+2 < len(os.Args) {
			capturePath = os.Args[i+2]
			break
		}
	}
	if strings.TrimSpace(capturePath) == "" {
		return os.ErrInvalid
	}
	reader := bufio.NewReader(os.Stdin)
	messages := make([]json.RawMessage, 0, 3)
	for len(messages) < 3 {
		payload, err := ReadFramedMessage(reader, defaultFrameLimit)
		if err != nil {
			return err
		}
		messages = append(messages, append(json.RawMessage(nil), payload...))
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	return os.WriteFile(capturePath, data, 0o644)
}

func runLSPSession() error {
	reader := bufio.NewReader(os.Stdin)
	if _, err := ReadFramedMessage(reader, defaultFrameLimit); err != nil {
		return err
	}
	if err := WriteFramedMessage(os.Stdout, []byte(`{"jsonrpc":"2.0","id":11,"result":{"capabilities":{"textDocumentSync":1,"diagnosticProvider":{"interFileDependencies":false}}}}`)); err != nil {
		return err
	}
	if _, err := ReadFramedMessage(reader, defaultFrameLimit); err != nil {
		return err
	}
	if _, err := ReadFramedMessage(reader, defaultFrameLimit); err != nil {
		return err
	}
	return WriteFramedMessage(os.Stdout, []byte(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[{"severity":1,"message":"session broken"}]}}`))
}

func loadCapturedMessages(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	return messages
}

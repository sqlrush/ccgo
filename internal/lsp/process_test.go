package lsp

import (
	"context"
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

func helperServerDefinition(mode string) ServerDefinition {
	return ServerDefinition{
		Name:    mode,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestLSPServerProcessHelper", "--", mode},
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

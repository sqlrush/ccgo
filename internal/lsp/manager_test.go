package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionManagerStatusPath(t *testing.T) {
	got := SessionManagerStatusPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", managerStatusFileName)
	if got != want {
		t.Fatalf("SessionManagerStatusPath() = %q, want %q", got, want)
	}
	if got := SessionManagerStatusPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionManagerStatusPath("session.jsonl", ""); got != "" {
		t.Fatalf("empty session id = %q, want empty", got)
	}
}

func TestBuildManagerStatusResolvesWorkspaceMatchesWithoutStartingServers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := BuildManagerStatus("sess_lsp", dir, []ServerDefinition{
		{Name: "gopls", Command: "gopls", FileExtensions: []string{"go"}, RootMarkers: []string{"go.mod"}},
		{Name: "typescript-language-server", Command: "typescript-language-server", Args: []string{"--stdio"}, FileExtensions: []string{".ts"}, RootMarkers: []string{"package.json"}},
		{Name: "rust-analyzer", Command: "rust-analyzer", FileExtensions: []string{".rs"}, RootMarkers: []string{"Cargo.toml"}},
	}, []string{"src/app.ts", "README.md"})
	if status.SessionID != "sess_lsp" || status.WorkingDirectory != dir || status.GeneratedAt == "" {
		t.Fatalf("manager metadata = %#v", status)
	}
	if len(status.Servers) != 3 {
		t.Fatalf("servers = %#v", status.Servers)
	}
	gopls := serverStatus(status.Servers, "gopls")
	if gopls.RuntimeState != ServerRuntimeNotStarted || len(gopls.MatchReasons) != 1 || gopls.MatchReasons[0] != "root:go.mod" {
		t.Fatalf("gopls status = %#v", gopls)
	}
	ts := serverStatus(status.Servers, "typescript-language-server")
	if ts.RuntimeState != ServerRuntimeNotStarted || len(ts.MatchReasons) != 1 || ts.MatchReasons[0] != "extension:.ts" {
		t.Fatalf("typescript status = %#v", ts)
	}
	rust := serverStatus(status.Servers, "rust-analyzer")
	if rust.RuntimeState != ServerRuntimeNoWorkspaceMatch || len(rust.MatchReasons) != 0 {
		t.Fatalf("rust status = %#v", rust)
	}
	counts := CountServerRuntimeStates(status.Servers)
	if counts[ServerRuntimeNotStarted] != 2 || counts[ServerRuntimeNoWorkspaceMatch] != 1 || CountMatchedServers(status.Servers) != 2 {
		t.Fatalf("counts=%#v matched=%d", counts, CountMatchedServers(status.Servers))
	}
}

func TestBuildManagerStatusMarksInvalidDefinitions(t *testing.T) {
	status := BuildManagerStatus("sess_lsp", "", []ServerDefinition{{Name: "broken"}}, nil)
	if len(status.Servers) != 1 || status.Servers[0].RuntimeState != ServerRuntimeInvalid {
		t.Fatalf("status = %#v", status)
	}
}

func TestBuildManagerStatusUsesDefaultDefinitions(t *testing.T) {
	status := BuildManagerStatus("sess_lsp", "", nil, []string{"main.go"})
	gopls := serverStatus(status.Servers, "gopls")
	if gopls.Name != "gopls" || gopls.RuntimeState != ServerRuntimeNotStarted {
		t.Fatalf("gopls status = %#v", gopls)
	}
}

func TestWriteAndLoadManagerStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_lsp", managerStatusFileName)
	input := ManagerStatus{
		SessionID: "sess_lsp",
		Servers: []ServerStatus{{
			Name:           "gopls",
			Command:        "gopls",
			FileExtensions: []string{"go", ".go"},
			RuntimeState:   ServerRuntimeNotStarted,
			MatchReasons:   []string{"root:go.mod"},
		}},
	}
	if err := WriteManagerStatus(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManagerStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_lsp" || loaded.GeneratedAt == "" || len(loaded.Servers) != 1 {
		t.Fatalf("loaded status = %#v", loaded)
	}
	if got := loaded.Servers[0].FileExtensions; len(got) != 1 || got[0] != ".go" {
		t.Fatalf("loaded extensions = %#v", got)
	}
}

func TestUpsertServerStatusIgnoresEmptyServer(t *testing.T) {
	status := ManagerStatus{
		Servers: []ServerStatus{{
			Name:         "gopls",
			RuntimeState: ServerRuntimeNotStarted,
		}},
	}
	got := UpsertServerStatus(status, ServerStatus{})
	if len(got.Servers) != 1 || got.Servers[0].Name != "gopls" {
		t.Fatalf("status = %#v", got)
	}
}

func TestManagerStatusIORejectsEmptyPath(t *testing.T) {
	if err := WriteManagerStatus("", ManagerStatus{}); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("WriteManagerStatus empty path err = %v", err)
	}
	if _, err := LoadManagerStatus(""); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("LoadManagerStatus empty path err = %v", err)
	}
}

func serverStatus(servers []ServerStatus, name string) ServerStatus {
	for _, server := range servers {
		if server.Name == name {
			return server
		}
	}
	return ServerStatus{}
}

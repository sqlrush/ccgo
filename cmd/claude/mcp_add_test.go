package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/config"
	"ccgo/internal/mcp"
)

func TestParseAddStdio(t *testing.T) {
	name, server, scope, err := parseAddArgs([]string{
		"fs", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp",
		"-e", "FOO=bar", "--scope", "user",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if name != "fs" || scope != mcp.ScopeUser {
		t.Fatalf("name/scope = %q/%q", name, scope)
	}
	if server.Command != "npx" {
		t.Fatalf("command = %q", server.Command)
	}
	wantArgs := []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}
	if strings.Join(server.Args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v want %v", server.Args, wantArgs)
	}
	if server.Env["FOO"] != "bar" {
		t.Fatalf("env = %v", server.Env)
	}
	if mcp.Transport(server) != mcp.TransportStdio {
		t.Fatalf("transport = %q", mcp.Transport(server))
	}
}

func TestParseAddHTTPInfersTransport(t *testing.T) {
	_, server, _, err := parseAddArgs([]string{
		"remote", "https://mcp.example.com/v1", "-H", "Authorization: Bearer tok",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if server.URL != "https://mcp.example.com/v1" {
		t.Fatalf("url = %q", server.URL)
	}
	if mcp.Transport(server) != mcp.TransportHTTP {
		t.Fatalf("transport = %q want http", mcp.Transport(server))
	}
	if server.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("headers = %v", server.Headers)
	}
}

func TestParseAddSSEExplicit(t *testing.T) {
	_, server, _, err := parseAddArgs([]string{
		"sserv", "https://mcp.example.com/sse", "-t", "sse",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if mcp.Transport(server) != mcp.TransportSSE {
		t.Fatalf("transport = %q want sse", mcp.Transport(server))
	}
}

func TestParseAddRejectsBadScope(t *testing.T) {
	if _, _, _, err := parseAddArgs([]string{"x", "cmd", "--scope", "bogus"}); err == nil {
		t.Fatal("expected error for bad scope")
	}
}

func TestParseAddRejectsMissingTarget(t *testing.T) {
	if _, _, _, err := parseAddArgs([]string{"onlyname"}); err == nil {
		t.Fatal("expected error: missing command/url")
	}
}

func TestMCPAddWritesAndIsImmutable(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add", "fs", "npx", "server-fs", "--scope", "user"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("add exit=%d stderr=%q", code, errb.String())
	}
	settings, err := config.LoadSettingsFile(env.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := settings.MCPServers["fs"]
	if !ok || got.Command != "npx" {
		t.Fatalf("server not persisted: %+v", settings.MCPServers)
	}
}

func TestMCPAddPreservesExistingDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.json")
	writeSettings(t, path, map[string]any{"keep": map[string]any{"command": "old"}})
	env := mcpCLIEnv{UserPath: path, ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"add", "new", "cmd2", "--scope", "user"}, &out, &errb, env); code != 0 {
		t.Fatalf("add exit=%d", code)
	}
	settings, _ := config.LoadSettingsFile(path)
	if _, ok := settings.MCPServers["keep"]; !ok {
		t.Fatal("existing server was dropped (non-immutable write)")
	}
	if _, ok := settings.MCPServers["new"]; !ok {
		t.Fatal("new server not added")
	}
}

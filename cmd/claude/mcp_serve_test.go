package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMCPServeListsTools(t *testing.T) {
	srv, err := newBuiltinMCPServer(t.TempDir())
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	// initialize then tools/list over an in-memory pipe.
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Run(ctx, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Expect a tools/list response with a non-empty tools array.
	if !strings.Contains(out.String(), `"tools"`) {
		t.Fatalf("no tools in output: %s", out.String())
	}
	// Sanity: each line must be valid JSON-RPC.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("non-JSON line %q: %v", line, err)
		}
	}
}

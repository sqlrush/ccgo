package doctor

import (
	"strings"
	"testing"
)

// --- F3-C04: doctor deep checks ---

func TestMultipleInstallationDetection(t *testing.T) {
	// SUBCMD-DOCTOR-08: DetectMultipleInstallations returns >1 entry when
	// multiple paths look like valid installs.
	paths := []string{
		"/usr/local/lib/node_modules/.bin/claude",
		"/usr/local/bin/claude",
	}
	installs := DetectMultipleInstallations(paths)
	if len(installs) < 2 {
		t.Fatalf("expected ≥2 entries for paths %v, got %v", paths, installs)
	}
	// Each entry should have a non-empty Type.
	for _, inst := range installs {
		if inst.Type == "" {
			t.Fatalf("installation entry has empty Type: %+v", inst)
		}
		if inst.Path == "" {
			t.Fatalf("installation entry has empty Path: %+v", inst)
		}
	}
}

func TestMultipleInstallationSinglePath(t *testing.T) {
	// Single path → one entry, no warning needed.
	paths := []string{"/usr/local/bin/claude"}
	installs := DetectMultipleInstallations(paths)
	if len(installs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(installs))
	}
}

func TestRunChecksMultipleInstallationsWarning(t *testing.T) {
	// SUBCMD-DOCTOR-08: Run should emit a WARN check when multiple installs detected.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		// Inject two paths that look like different install types.
		AdditionalBinaryPaths: []string{
			"/usr/local/lib/node_modules/.bin/claude",
			"/usr/local/bin/claude",
		},
	})
	var sawMulti bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "multiple") {
			sawMulti = true
			if c.Status != StatusWarn {
				t.Fatalf("multiple-install check should be WARN, got %q", c.Status)
			}
		}
	}
	if !sawMulti {
		t.Fatal("expected a multiple-installations check when >1 paths provided")
	}
}

func TestRunChecksNoMultipleInstallationsWhenSingle(t *testing.T) {
	// When only one installation path is present, no multiple-install warning.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		AdditionalBinaryPaths: []string{"/usr/local/bin/claude"},
	})
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "multiple") && c.Status == StatusWarn {
			t.Fatalf("should not warn about multiple installs with one path; check: %+v", c)
		}
	}
}

func TestRunChecksMCPParseError(t *testing.T) {
	// SUBCMD-DOCTOR-13: bad .mcp.json → WARN check.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		ReadSettingsFile: func(path string) ([]byte, error) {
			return []byte(`{"model":"ok"}`), nil
		},
		MCPConfigContent: []byte(`{invalid json`),
	})
	var sawMCP bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "mcp") {
			sawMCP = true
			if c.Status != StatusWarn && c.Status != StatusError {
				t.Fatalf("MCP parse error check should be WARN or ERR, got %q", c.Status)
			}
		}
	}
	if !sawMCP {
		t.Fatal("expected an MCP check when MCPConfigContent is provided")
	}
}

func TestRunChecksMCPParseOK(t *testing.T) {
	// Valid .mcp.json → OK check.
	report := Run(Input{
		Version:          "1.0.0",
		CWD:              t.TempDir(),
		MCPConfigContent: []byte(`{"mcpServers":{}}`),
	})
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "mcp") {
			if c.Status == StatusError {
				t.Fatalf("valid MCP config should not be error; detail: %q", c.Detail)
			}
		}
	}
}

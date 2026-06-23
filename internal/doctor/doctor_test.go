package doctor

import (
	"strings"
	"testing"
)

func TestRunChecksReturnsResults(t *testing.T) {
	report := Run(Input{Version: "0.1.0", CWD: t.TempDir()})
	if len(report.Checks) == 0 {
		t.Fatal("expected at least one check")
	}
	var sawVersion bool
	for _, c := range report.Checks {
		if c.Name == "" || (c.Status != StatusOK && c.Status != StatusWarn && c.Status != StatusError) {
			t.Fatalf("malformed check: %+v", c)
		}
		if strings.Contains(strings.ToLower(c.Name), "version") {
			sawVersion = true
		}
	}
	if !sawVersion {
		t.Fatal("expected a version check")
	}
}

func TestFormatReportDeterministic(t *testing.T) {
	report := Report{Checks: []Check{{Name: "Version", Status: StatusOK, Detail: "0.1.0"}}}
	out := Format(report)
	if !strings.Contains(out, "Version") || !strings.Contains(out, "0.1.0") {
		t.Fatalf("format missing content: %q", out)
	}
}

func TestRunChecksRipgrepPresent(t *testing.T) {
	// Inject a LookPath that always finds rg.
	report := Run(Input{
		Version:  "1.0.0",
		CWD:      t.TempDir(),
		LookPath: func(file string) (string, error) { return "/usr/bin/" + file, nil },
	})
	var sawRipgrep bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "ripgrep") {
			sawRipgrep = true
			if c.Status != StatusOK {
				t.Fatalf("ripgrep check should be OK when rg found, got %q", c.Status)
			}
		}
	}
	if !sawRipgrep {
		t.Fatal("expected a ripgrep check")
	}
}

func TestRunChecksRipgrepAbsent(t *testing.T) {
	// Inject a LookPath that never finds anything.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		LookPath: func(file string) (string, error) {
			return "", &lookPathError{file}
		},
	})
	var sawRipgrep bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "ripgrep") {
			sawRipgrep = true
			if c.Status != StatusWarn {
				t.Fatalf("ripgrep check should be WARN when rg absent, got %q", c.Status)
			}
		}
	}
	if !sawRipgrep {
		t.Fatal("expected a ripgrep check")
	}
}

func TestRunChecksSettingsParseError(t *testing.T) {
	// Inject a settings reader that returns a parse error.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		ReadSettingsFile: func(path string) ([]byte, error) {
			return []byte(`{invalid json`), nil
		},
	})
	var sawError bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "settings") && c.Status == StatusError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected a settings parse error check")
	}
}

func TestRunChecksSettingsParseOK(t *testing.T) {
	// Inject a settings reader that returns valid JSON.
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		ReadSettingsFile: func(path string) ([]byte, error) {
			return []byte(`{"model":"opus"}`), nil
		},
	})
	var sawSettings bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "settings") {
			sawSettings = true
			if c.Status == StatusError {
				t.Fatalf("settings check should not be error for valid JSON, got detail: %q", c.Detail)
			}
		}
	}
	if !sawSettings {
		t.Fatal("expected at least one settings check")
	}
}

func TestFormatShowsStatus(t *testing.T) {
	report := Report{Checks: []Check{
		{Name: "Version", Status: StatusOK, Detail: "1.2.3"},
		{Name: "Ripgrep", Status: StatusWarn, Detail: "rg not found"},
		{Name: "Settings", Status: StatusError, Detail: "invalid JSON"},
	}}
	out := Format(report)
	if !strings.Contains(out, "[OK]") {
		t.Fatalf("format should contain [OK]: %q", out)
	}
	if !strings.Contains(out, "[WARN]") {
		t.Fatalf("format should contain [WARN]: %q", out)
	}
	if !strings.Contains(out, "[ERR]") {
		t.Fatalf("format should contain [ERR]: %q", out)
	}
}

func TestRunChecksInstallTypePresent(t *testing.T) {
	// SUBCMD-DOCTOR-01: report must include an install-type check.
	report := Run(Input{
		Version:     "1.0.0",
		CWD:         t.TempDir(),
		ExecutableFn: func() (string, error) { return "/usr/local/bin/claude", nil },
	})
	var sawInstall bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "install") {
			sawInstall = true
			if c.Status != StatusOK && c.Status != StatusWarn {
				t.Fatalf("install-type check should be OK or WARN, got %q detail=%q", c.Status, c.Detail)
			}
			if c.Detail == "" {
				t.Fatal("install-type check must include a non-empty detail (install type or path)")
			}
		}
	}
	if !sawInstall {
		t.Fatal("expected an install-type check in the report")
	}
}

func TestRunChecksInstallTypeInFormat(t *testing.T) {
	// SUBCMD-DOCTOR-01: Format output must contain install type information.
	report := Run(Input{
		Version:     "1.0.0",
		CWD:         t.TempDir(),
		ExecutableFn: func() (string, error) { return "/usr/local/bin/claude", nil },
	})
	out := Format(report)
	if !strings.Contains(strings.ToLower(out), "install") {
		t.Fatalf("Format output should contain install info: %q", out)
	}
}

// lookPathError is a simple error type for faking missing binaries.
type lookPathError struct{ name string }

func (e *lookPathError) Error() string { return "exec: " + e.name + ": not found" }

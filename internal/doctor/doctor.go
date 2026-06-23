// Package doctor implements deterministic, local-only health checks for ccgo.
// It is shared by /doctor (slash command) and `claude doctor` (CLI subcommand).
// No network calls, no auth checks — filesystem + exec.LookPath only.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Status represents the outcome of a single diagnostic check.
type Status string

const (
	StatusOK    Status = "ok"
	StatusWarn  Status = "warn"
	StatusError Status = "error"
)

// Check holds the result of one diagnostic.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report holds all diagnostic checks for a single run.
type Report struct {
	Checks []Check
}

// HasErrors reports whether any check has StatusError.
func (r Report) HasErrors() bool {
	for _, c := range r.Checks {
		if c.Status == StatusError {
			return true
		}
	}
	return false
}

// InstallationType mirrors CC's 6 install categories (doctorDiagnostic.ts:514).
type InstallationType string

const (
	InstallTypeNpmGlobal      InstallationType = "npm-global"
	InstallTypeNpmLocal       InstallationType = "npm-local"
	InstallTypeNative         InstallationType = "native"
	InstallTypePackageManager InstallationType = "package-manager"
	InstallTypeDevelopment    InstallationType = "development"
	InstallTypeUnknown        InstallationType = "unknown"
)

// Installation represents one detected binary installation.
type Installation struct {
	Type InstallationType
	Path string
}

// DetectMultipleInstallations classifies each path in the given list and
// returns a slice of Installation entries. Useful for warning when multiple
// install types are present simultaneously (SUBCMD-DOCTOR-08).
func DetectMultipleInstallations(paths []string) []Installation {
	out := make([]Installation, 0, len(paths))
	for _, p := range paths {
		it, _ := DetectInstallType(func() (string, error) { return p, nil })
		out = append(out, Installation{Type: it, Path: p})
	}
	return out
}

// Input carries injected signals so tests are deterministic and network-free.
type Input struct {
	// Version is the binary version string (e.g. "0.0.0-dev").
	Version string

	// CWD is the working directory to check for .claude/ dir existence.
	CWD string

	// LookPath is exec.LookPath-compatible; defaults to exec.LookPath when nil.
	LookPath func(file string) (string, error)

	// ReadSettingsFile reads a settings JSON file; defaults to os.ReadFile when nil.
	ReadSettingsFile func(path string) ([]byte, error)

	// UserSettingsPath overrides the user settings path; defaults to ~/.claude/settings.json.
	UserSettingsPath string

	// ProjectSettingsPath overrides the project settings path; defaults to CWD/.claude/settings.json.
	ProjectSettingsPath string

	// ExecutableFn returns the path to the running binary; defaults to os.Executable when nil.
	// Injected in tests for deterministic install-type detection.
	ExecutableFn func() (string, error)

	// AdditionalBinaryPaths, when non-nil, is used instead of PATH-discovery to
	// detect multiple installations (SUBCMD-DOCTOR-08). Injected in tests.
	AdditionalBinaryPaths []string

	// MCPConfigContent, when non-nil, is the raw bytes of .mcp.json to parse
	// (SUBCMD-DOCTOR-13). When nil and CWD is set, the file is read from disk.
	MCPConfigContent []byte
}

// DetectInstallType classifies the running binary path into one of CC's 6
// install categories (doctorDiagnostic.ts:514). It uses the provided
// executableFn (or os.Executable when nil) to obtain the binary path.
func DetectInstallType(executableFn func() (string, error)) (InstallationType, string) {
	fn := executableFn
	if fn == nil {
		fn = os.Executable
	}
	exePath, err := fn()
	if err != nil {
		return InstallTypeUnknown, ""
	}
	exePath = strings.ToLower(exePath)

	switch {
	case strings.Contains(exePath, "node_modules/.bin") || strings.Contains(exePath, "node_modules\\.bin"):
		return InstallTypeNpmLocal, exePath
	case strings.Contains(exePath, "/npm/") || strings.Contains(exePath, "\\npm\\") ||
		strings.Contains(exePath, "npm-global") || strings.Contains(exePath, "\\appdata\\roaming\\npm"):
		return InstallTypeNpmGlobal, exePath
	case strings.Contains(exePath, "/homebrew/") || strings.Contains(exePath, "/linuxbrew/") ||
		strings.Contains(exePath, "/nix/store/") || strings.Contains(exePath, "/macports/"):
		return InstallTypePackageManager, exePath
	case strings.Contains(exePath, "/go/bin/") || strings.Contains(exePath, "\\go\\bin\\") ||
		strings.Contains(exePath, "/go-build") || strings.Contains(exePath, "\\go-build") ||
		strings.HasSuffix(exePath, ".test"):
		return InstallTypeDevelopment, exePath
	case strings.Contains(exePath, "/usr/local/bin/") || strings.Contains(exePath, "/usr/bin/") ||
		strings.Contains(exePath, "/opt/") || strings.Contains(exePath, "\\program files\\"):
		return InstallTypeNative, exePath
	default:
		return InstallTypeUnknown, exePath
	}
}

// Run performs all diagnostic checks and returns a Report.
func Run(in Input) Report {
	lookPath := in.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	readFile := in.ReadSettingsFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	userSettingsPath := in.UserSettingsPath
	if userSettingsPath == "" {
		home, _ := os.UserHomeDir()
		userSettingsPath = fmt.Sprintf("%s/.claude/settings.json", home)
	}
	projectSettingsPath := in.ProjectSettingsPath
	if projectSettingsPath == "" && in.CWD != "" {
		projectSettingsPath = fmt.Sprintf("%s/.claude/settings.json", in.CWD)
	}

	var checks []Check

	// Version check.
	version := strings.TrimSpace(in.Version)
	if version == "" {
		version = "unknown"
	}
	checks = append(checks, Check{
		Name:   "Version",
		Status: StatusOK,
		Detail: version,
	})

	// Ripgrep availability check.
	if _, err := lookPath("rg"); err != nil {
		checks = append(checks, Check{
			Name:   "Ripgrep (rg)",
			Status: StatusWarn,
			Detail: "rg not found in PATH — file search performance may be degraded",
		})
	} else {
		checks = append(checks, Check{
			Name:   "Ripgrep (rg)",
			Status: StatusOK,
			Detail: "rg found in PATH",
		})
	}

	// User settings parse check.
	checks = append(checks, settingsCheck("User settings", userSettingsPath, readFile))

	// Project settings parse check.
	if projectSettingsPath != "" {
		checks = append(checks, settingsCheck("Project settings", projectSettingsPath, readFile))
	}

	// .claude config directory presence check.
	if in.CWD != "" {
		claudeDir := fmt.Sprintf("%s/.claude", in.CWD)
		if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
			checks = append(checks, Check{
				Name:   "Config dir (.claude)",
				Status: StatusWarn,
				Detail: "no .claude/ directory in CWD — project settings and skills unavailable",
			})
		} else {
			checks = append(checks, Check{
				Name:   "Config dir (.claude)",
				Status: StatusOK,
				Detail: claudeDir,
			})
		}
	}

	// Install-type check (SUBCMD-DOCTOR-01): classify the running binary.
	installType, exePath := DetectInstallType(in.ExecutableFn)
	detail := string(installType)
	if exePath != "" {
		detail = fmt.Sprintf("%s (%s)", installType, exePath)
	}
	checks = append(checks, Check{
		Name:   "Install type",
		Status: StatusOK,
		Detail: detail,
	})

	// Multiple-installations check (SUBCMD-DOCTOR-08).
	// When AdditionalBinaryPaths is injected use those; otherwise skip the
	// expensive PATH scan so the check is network/exec-free by default.
	if len(in.AdditionalBinaryPaths) > 0 {
		installs := DetectMultipleInstallations(in.AdditionalBinaryPaths)
		if len(installs) > 1 {
			var parts []string
			for _, inst := range installs {
				parts = append(parts, fmt.Sprintf("%s at %s", inst.Type, inst.Path))
			}
			checks = append(checks, Check{
				Name:   "Multiple installations",
				Status: StatusWarn,
				Detail: "Multiple Claude installations detected: " + strings.Join(parts, "; "),
			})
		} else if len(installs) == 1 {
			checks = append(checks, Check{
				Name:   "Multiple installations",
				Status: StatusOK,
				Detail: fmt.Sprintf("Single installation: %s at %s", installs[0].Type, installs[0].Path),
			})
		}
	}

	// MCP config parse check (SUBCMD-DOCTOR-13).
	mcpBytes := in.MCPConfigContent
	if mcpBytes == nil && in.CWD != "" {
		mcpPath := fmt.Sprintf("%s/.mcp.json", in.CWD)
		data, err := readFile(mcpPath)
		if err == nil {
			mcpBytes = data
		}
		// Non-existent .mcp.json is fine — no check needed.
	}
	if mcpBytes != nil {
		var raw map[string]any
		if err := json.Unmarshal(mcpBytes, &raw); err != nil {
			checks = append(checks, Check{
				Name:   "MCP config (.mcp.json)",
				Status: StatusWarn,
				Detail: fmt.Sprintf("parse error in .mcp.json: %v", err),
			})
		} else {
			checks = append(checks, Check{
				Name:   "MCP config (.mcp.json)",
				Status: StatusOK,
				Detail: ".mcp.json is valid JSON",
			})
		}
	}

	return Report{Checks: checks}
}

// settingsCheck reads and parses a settings JSON file, returning a Check.
func settingsCheck(name, path string, readFile func(string) ([]byte, error)) Check {
	data, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{
				Name:   name,
				Status: StatusOK,
				Detail: "not found (will use defaults)",
			}
		}
		return Check{
			Name:   name,
			Status: StatusWarn,
			Detail: fmt.Sprintf("cannot read %s: %v", path, err),
		}
	}
	if len(data) == 0 {
		return Check{
			Name:   name,
			Status: StatusOK,
			Detail: "empty (will use defaults)",
		}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Check{
			Name:   name,
			Status: StatusError,
			Detail: fmt.Sprintf("parse error in %s: %v", path, err),
		}
	}
	return Check{
		Name:   name,
		Status: StatusOK,
		Detail: path,
	}
}

// Format renders a Report as an aligned human-readable string.
// Each line: [OK]/[WARN]/[ERR] Name — Detail
func Format(r Report) string {
	if len(r.Checks) == 0 {
		return "Doctor: no checks ran."
	}
	lines := make([]string, 0, len(r.Checks)+2)
	lines = append(lines, "ccgo doctor")
	lines = append(lines, strings.Repeat("-", 40))
	for _, c := range r.Checks {
		tag := statusTag(c.Status)
		line := fmt.Sprintf("%s %s", tag, c.Name)
		if c.Detail != "" {
			line += " — " + c.Detail
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func statusTag(s Status) string {
	switch s {
	case StatusOK:
		return "[OK]  "
	case StatusWarn:
		return "[WARN]"
	case StatusError:
		return "[ERR] "
	default:
		return "[?]   "
	}
}

// Package doctor implements deterministic, local-only health checks for ccgo.
// It is shared by /doctor (slash command) and `claude doctor` (CLI subcommand).
// Core checks are network-free; an opt-in network check is available via
// Input.NetworkCheckEndpoint (SUBCMD-DOCTOR-10).
package doctor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
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

	// ConfigInstallMethod, when non-empty, is the installMethod value from the
	// global config file (CC: getGlobalConfig().installMethod). When it disagrees
	// with the detected install type, a WARN check is emitted (SUBCMD-DOCTOR-09).
	// Known values: "local", "native", "global", "unknown".
	ConfigInstallMethod string

	// LinuxGlobPatterns holds the set of glob patterns found in sandbox permission
	// rules. On Linux these patterns are silently ignored, so they warrant a WARN
	// (SUBCMD-DOCTOR-11). The check runs when ForceLinuxGlobCheck is true or when
	// runtime.GOOS=="linux" and len(LinuxGlobPatterns)>0.
	LinuxGlobPatterns []string

	// ForceLinuxGlobCheck, when true, runs the LinuxGlobPatterns check regardless
	// of the runtime OS. Used in tests to verify the check on non-Linux CI.
	ForceLinuxGlobCheck bool

	// StaleLockFiles, when non-empty, lists stale PID lock files found under
	// ~/.local/state/claude/locks/ (SUBCMD-DOCTOR-12). A WARN check is emitted.
	// In production this is detected by FindStaleLockFiles; tests inject directly.
	StaleLockFiles []string

	// SandboxEnabled, when true, indicates the user has sandbox.enabled=true.
	// When true, sandbox diagnostic checks are emitted (SBX-38/SBX-39).
	SandboxEnabled bool

	// SandboxUnavailableReason, when non-empty, is the human-readable reason
	// why the sandbox is unavailable despite being enabled (SBX-38/SBX-39).
	// Populated by sandbox.UnavailableReason + sandbox.DepCheck diagnostics.
	SandboxUnavailableReason string

	// SandboxDepWarnings holds degraded-but-functional dependency warnings
	// (e.g. missing ripgrep) from sandbox.DepCheck. Emitted as WARN checks.
	SandboxDepWarnings []string

	// NetworkCheckEndpoint, when non-empty, enables an opt-in network
	// connectivity check (SUBCMD-DOCTOR-10). A HEAD request is made to the
	// given URL; the check is skipped when this field is empty (default) to
	// preserve network-free behaviour.
	// CC ref: src/screens/Doctor.tsx:131 distTagsPromise + getDoctorDiagnostic.
	NetworkCheckEndpoint string

	// NetworkCheckFn, when non-nil, is used instead of a real HTTP HEAD request.
	// Injected in tests so no real network calls are needed.
	// Signature: func(url string) error — nil return means reachable.
	NetworkCheckFn func(url string) error
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

	// Config/reality mismatch check (SUBCMD-DOCTOR-09).
	// CC stores installMethod in global config; ccgo exposes it via Input.ConfigInstallMethod.
	// Only check when the field is explicitly set — empty means "not recorded".
	if in.ConfigInstallMethod != "" && in.ConfigInstallMethod != "unknown" {
		checks = append(checks, configMismatchCheck(installType, in.ConfigInstallMethod))
	}

	// Linux sandbox glob-pattern warning (SUBCMD-DOCTOR-11).
	// On Linux, glob patterns in sandbox Edit/Read permission rules are silently
	// ignored by the kernel's landlock interface, so we surface a WARN.
	if len(in.LinuxGlobPatterns) > 0 && (runtime.GOOS == "linux" || in.ForceLinuxGlobCheck) {
		checks = append(checks, globPatternCheck(in.LinuxGlobPatterns))
	}

	// Stale PID lock files check (SUBCMD-DOCTOR-12).
	// ccgo does not maintain a PID lock system itself, but it can detect leftover
	// lock files from previous sessions and report them.
	if len(in.StaleLockFiles) > 0 {
		checks = append(checks, staleLockCheck(in.StaleLockFiles))
	}

	// Sandbox diagnostic check (SBX-38/SBX-39).
	// Only surface when sandbox is explicitly enabled (no noise for users who don't use it).
	if in.SandboxEnabled {
		checks = append(checks, sandboxCheck(in.SandboxUnavailableReason, in.SandboxDepWarnings))
	}

	// Opt-in network connectivity check (SUBCMD-DOCTOR-10).
	// Only runs when NetworkCheckEndpoint is explicitly set (default off).
	// CC ref: src/screens/Doctor.tsx:131 distTagsPromise / getDoctorDiagnostic().
	if endpoint := strings.TrimSpace(in.NetworkCheckEndpoint); endpoint != "" {
		checks = append(checks, networkConnectivityCheck(endpoint, in.NetworkCheckFn))
	}

	return Report{Checks: checks}
}

// DefaultNetworkCheckFn performs a HEAD request with a 5-second timeout.
// Used as the production network probe when NetworkCheckFn is nil.
func DefaultNetworkCheckFn(url string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Head(url) //nolint:noctx — doctor check, not user data path
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// networkConnectivityCheck probes the given endpoint and returns a Check.
// probeFn is called instead of a real HTTP request when non-nil (enables tests).
func networkConnectivityCheck(endpoint string, probeFn func(string) error) Check {
	fn := probeFn
	if fn == nil {
		fn = DefaultNetworkCheckFn
	}
	if err := fn(endpoint); err != nil {
		return Check{
			Name:   "Network connectivity",
			Status: StatusWarn,
			Detail: fmt.Sprintf("cannot reach %s: %v", endpoint, err),
		}
	}
	return Check{
		Name:   "Network connectivity",
		Status: StatusOK,
		Detail: fmt.Sprintf("%s reachable", endpoint),
	}
}

// configMismatchCheck returns a Check for SUBCMD-DOCTOR-09.
// It compares the runtime-detected install type with the value recorded in the
// global config file. A mismatch warrants a fix pointer.
func configMismatchCheck(detected InstallationType, configured string) Check {
	// Map CC's config values to our install type strings.
	// CC values: "local"→npm-local, "native"→native, "global"→npm-global, "unknown"→unknown.
	mismatch := false
	switch configured {
	case "local":
		mismatch = detected != InstallTypeNpmLocal
	case "native":
		mismatch = detected != InstallTypeNative
	case "global":
		mismatch = detected != InstallTypeNpmGlobal
	}
	if !mismatch {
		return Check{
			Name:   "Config/install mismatch",
			Status: StatusOK,
			Detail: fmt.Sprintf("detected install type %q matches config installMethod %q", detected, configured),
		}
	}
	return Check{
		Name:   "Config/install mismatch",
		Status: StatusWarn,
		Detail: fmt.Sprintf(
			"detected install type %q but config installMethod is %q — run claude install to update configuration",
			detected, configured,
		),
	}
}

// globPatternCheck returns a WARN Check for Linux sandbox glob patterns (SUBCMD-DOCTOR-11).
func globPatternCheck(patterns []string) Check {
	display := patterns
	if len(display) > 3 {
		display = patterns[:3]
	}
	remaining := len(patterns) - len(display)
	patternList := strings.Join(display, ", ")
	if remaining > 0 {
		patternList = fmt.Sprintf("%s (%d more)", patternList, remaining)
	}
	return Check{
		Name:   "Sandbox glob patterns",
		Status: StatusWarn,
		Detail: fmt.Sprintf(
			"Glob patterns in sandbox permission rules are not fully supported on Linux. Found %d pattern(s): %s. On Linux, glob patterns in Edit/Read rules will be ignored.",
			len(patterns), patternList,
		),
	}
}

// staleLockCheck returns a WARN Check for stale PID lock files (SUBCMD-DOCTOR-12).
func staleLockCheck(files []string) Check {
	return Check{
		Name:   "Stale lock files",
		Status: StatusWarn,
		Detail: fmt.Sprintf(
			"Found %d stale PID lock file(s) in ~/.local/state/claude/locks/ — these can be safely removed.",
			len(files),
		),
	}
}

// sandboxCheck returns a Check for sandbox availability (SBX-38/SBX-39).
// unavailableReason is non-empty when the sandbox is configured but cannot run.
// depWarnings lists degraded (non-fatal) dependency messages.
func sandboxCheck(unavailableReason string, depWarnings []string) Check {
	if unavailableReason != "" {
		return Check{
			Name:   "Sandbox",
			Status: StatusWarn,
			Detail: unavailableReason,
		}
	}
	if len(depWarnings) > 0 {
		return Check{
			Name:   "Sandbox",
			Status: StatusWarn,
			Detail: strings.Join(depWarnings, "; "),
		}
	}
	return Check{
		Name:   "Sandbox",
		Status: StatusOK,
		Detail: "sandbox enabled and available",
	}
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

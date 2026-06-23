package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// installEntry describes one detected installation of the binary.
type installEntry struct {
	Type string
	Path string
}

// updateOptions carries injected state for runUpdateCLIv2, enabling deterministic
// unit tests without touching the real filesystem or network.
type updateOptions struct {
	// Ver is the current binary version string.
	Ver string
	// Channel is "latest" or "stable" (from autoUpdatesChannel setting).
	Channel string
	// InstallType is the detected install type (mirrors doctor.InstallationType strings).
	// Injected in tests; production code derives this from the binary path.
	InstallType string
	// PackageManager is "homebrew", "winget", "apk", or "" for others.
	// Only meaningful when InstallType == "package-manager".
	PackageManager string
	// MultipleInstallations lists all detected binary paths when >1 exist.
	MultipleInstallations []installEntry
}

// detectInstallMethod returns a best-effort description of how the claude
// binary was installed. It checks common installation paths without any
// network access.
func detectInstallMethod() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	exe = filepath.ToSlash(exe)

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			gopath = filepath.Join(home, "go")
		}
	}

	switch {
	case containsAny(exe, "/usr/local/bin", "/usr/bin", "/opt/homebrew"):
		return "system package manager (homebrew / apt / yum)"
	case gopath != "" && containsAny(exe, filepath.ToSlash(gopath)+"/bin"):
		return "go install"
	case containsAny(exe, "/tmp/", filepath.ToSlash(os.TempDir())):
		return "temporary build"
	default:
		return "manual / local build"
	}
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && len(sub) > 1 {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// runUpdateCLI is the legacy entry point kept for backward compat with existing
// tests. It derives updateOptions from the binary path and delegates.
func runUpdateCLI(args []string, ver string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "check for updates (stub; no network access)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	channel := resolveAutoUpdatesChannel()
	installType := resolveInstallType()

	if *check {
		fmt.Fprintf(stdout, "claude version %s\n", ver)
		fmt.Fprintf(stdout, "Channel: %s\n", channel)
		fmt.Fprintln(stdout, "Note: --check is a stub; no network check is performed.")
		fmt.Fprintln(stdout, "Use your package manager to check for and install updates.")
		return 0
	}

	opts := updateOptions{
		Ver:         ver,
		Channel:     channel,
		InstallType: installType,
	}
	return runUpdateCLIv2(args[:0], opts, stdout, stderr)
}

// runUpdateCLIv2 is the primary implementation: it uses the injected opts so
// tests can exercise all branches without touching the filesystem or network.
// Returns 0 on success, 1 on error.
func runUpdateCLIv2(args []string, opts updateOptions, stdout, stderr io.Writer) int {
	// Resolve defaults.
	channel := opts.Channel
	if channel == "" {
		channel = "latest"
	}
	installType := opts.InstallType
	if installType == "" {
		installType = resolveInstallType()
	}

	fmt.Fprintf(stdout, "claude version %s\n", opts.Ver)
	fmt.Fprintf(stdout, "Channel: %s\n", channel)

	// Multiple-installations warning (SUBCMD-UPDATE-05).
	if len(opts.MultipleInstallations) > 1 {
		fmt.Fprintln(stdout, "\nWarning: Multiple installations found")
		for _, inst := range opts.MultipleInstallations {
			fmt.Fprintf(stdout, "  - %s at %s\n", inst.Type, inst.Path)
		}
	}

	// Development build guard (SUBCMD-UPDATE-06).
	if installType == "development" {
		fmt.Fprintln(stdout, "\nWarning: Cannot update development build.")
		fmt.Fprintln(stdout, "Build from source to update: go install ccgo@latest")
		return 1
	}

	// Package-manager installs: emit precise commands (SUBCMD-UPDATE-04).
	if installType == "package-manager" {
		fmt.Fprintln(stdout)
		switch strings.ToLower(opts.PackageManager) {
		case "homebrew":
			fmt.Fprintln(stdout, "Claude is managed by Homebrew.")
			fmt.Fprintln(stdout, "To update, run:")
			fmt.Fprintln(stdout, "  brew upgrade claude-code")
		case "winget":
			fmt.Fprintln(stdout, "Claude is managed by winget.")
			fmt.Fprintln(stdout, "To update, run:")
			fmt.Fprintln(stdout, "  winget upgrade Anthropic.ClaudeCode")
		case "apk":
			fmt.Fprintln(stdout, "Claude is managed by apk.")
			fmt.Fprintln(stdout, "To update, run:")
			fmt.Fprintln(stdout, "  apk upgrade claude-code")
		default:
			fmt.Fprintln(stdout, "Claude is managed by a package manager.")
			fmt.Fprintln(stdout, "Please use your package manager to update.")
		}
		return 0
	}

	// Generic fallback for native/npm/go/manual installs.
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "To update claude, use your package manager:")
	fmt.Fprintln(stdout, "  • Homebrew:  brew upgrade claude-code")
	fmt.Fprintln(stdout, "  • Go source: go install ccgo@latest")
	fmt.Fprintln(stdout, "  • Other:     refer to your distribution's instructions")
	return 0
}

// resolveAutoUpdatesChannel reads autoUpdatesChannel from the user settings
// file (SUBCMD-UPDATE-02). Returns "latest" if unset or on read error.
func resolveAutoUpdatesChannel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "latest"
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return "latest"
	}
	// Simple JSON key extraction to avoid importing the config package
	// (to keep this file self-contained).
	if ch := extractJSONStringField(data, "autoUpdatesChannel"); ch != "" {
		return ch
	}
	return "latest"
}

// resolveInstallType maps the binary path to a CC-style install-type string.
func resolveInstallType() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	exe = strings.ToLower(filepath.ToSlash(exe))

	switch {
	case strings.Contains(exe, "node_modules/.bin") || strings.Contains(exe, "node_modules\\.bin"):
		return "npm-local"
	case strings.Contains(exe, "/npm/") || strings.Contains(exe, "\\npm\\") ||
		strings.Contains(exe, "npm-global") || strings.Contains(exe, "\\appdata\\roaming\\npm"):
		return "npm-global"
	case strings.Contains(exe, "/homebrew/") || strings.Contains(exe, "/linuxbrew/") ||
		strings.Contains(exe, "/nix/store/") || strings.Contains(exe, "/macports/"):
		return "package-manager"
	case strings.Contains(exe, "/go/bin/") || strings.Contains(exe, "\\go\\bin\\") ||
		strings.Contains(exe, "/go-build") || strings.Contains(exe, "\\go-build") ||
		strings.HasSuffix(exe, ".test"):
		return "development"
	case strings.Contains(exe, "/usr/local/bin/") || strings.Contains(exe, "/usr/bin/") ||
		strings.Contains(exe, "/opt/") || strings.Contains(exe, "\\program files\\"):
		return "native"
	default:
		return "unknown"
	}
}

// extractJSONStringField performs a minimal, allocation-light extraction of a
// top-level string field from a JSON byte slice. It avoids importing
// encoding/json to keep the function dependency-free.
func extractJSONStringField(data []byte, field string) string {
	// Build target: `"field":"  or `"field": "
	needle := `"` + field + `"`
	s := string(data)
	idx := strings.Index(s, needle)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(needle):]
	// Skip whitespace and colon.
	rest = strings.TrimLeft(rest, " \t\n\r:")
	rest = strings.TrimLeft(rest, " \t\n\r")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

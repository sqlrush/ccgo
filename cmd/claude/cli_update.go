package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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

// runUpdateCLI implements "claude update".
// It is deliberately network-free: it reports the current version and
// instructs the user to update via their package manager.
// The injected ver parameter enables deterministic testing.
// Returns 0 on success.
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

	method := detectInstallMethod()

	if *check {
		// Stub: no network access. Report current version only.
		fmt.Fprintf(stdout, "claude version %s\n", ver)
		fmt.Fprintf(stdout, "Install method: %s\n", method)
		fmt.Fprintln(stdout, "Note: --check is a stub; no network check is performed.")
		fmt.Fprintln(stdout, "Use your package manager to check for and install updates.")
		return 0
	}

	fmt.Fprintf(stdout, "claude version %s\n", ver)
	fmt.Fprintf(stdout, "Install method: %s\n", method)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Self-update is not configured.")
	fmt.Fprintln(stdout, "To update claude, use your package manager:")
	fmt.Fprintln(stdout, "  • Homebrew:  brew upgrade ccgo")
	fmt.Fprintln(stdout, "  • Go source: go install ccgo@latest")
	fmt.Fprintln(stdout, "  • Other:     refer to your distribution's instructions")
	return 0
}

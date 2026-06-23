package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// errNotFound is a sentinel error for DepCheck injection in tests.
var errNotFound = errors.New("not found")

// DepCheckResult holds the outcome of a sandbox dependency scan. Mirrors CC's
// SandboxDependencyCheck type (sandbox-adapter.ts:451).
type DepCheckResult struct {
	// Available is true when all required executables are present and the
	// sandbox can be used on this platform.
	Available bool
	// Errors lists missing required dependencies; each entry is a human-readable
	// reason why the sandbox cannot function.
	Errors []string
	// Warnings lists degraded-but-functional situations (e.g. ripgrep absent).
	Warnings []string
}

// depCheckInput allows tests to inject filesystem/exec lookups without real I/O.
type depCheckInput struct {
	// lookupFile returns true if the given filesystem path exists.
	// When nil defaults to checking os.Stat.
	lookupFile func(path string) bool
	// lookPath resolves a command name using PATH.
	// When nil defaults to exec.LookPath.
	lookPath func(cmd string) (string, error)
}

// DepCheck probes for sandbox runtime dependencies and returns a DepCheckResult.
//
// Required (errors if absent):
//   - Darwin: /usr/bin/sandbox-exec
//   - Linux: bubblewrap (bwrap) — presence checked via PATH
//
// Degraded warnings (warnings if absent):
//   - ripgrep (rg) — used for fast file search; sandbox still works without it
//
// On other platforms the sandbox is unavailable by build-tag; DepCheck still
// runs and surfaces an appropriate message.
//
// SBX-38: mirrors CC's checkDependencies (sandbox-adapter.ts:451).
func DepCheck(in depCheckInput) DepCheckResult {
	lookupFile := in.lookupFile
	if lookupFile == nil {
		lookupFile = func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
	}
	lookPath := in.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	var result DepCheckResult

	switch runtime.GOOS {
	case "darwin":
		checkDarwin(&result, lookupFile)
	case "linux":
		checkLinux(&result, lookPath)
	default:
		result.Errors = append(result.Errors,
			"sandbox not available on "+runtime.GOOS+" (requires macOS or Linux)")
	}

	// Ripgrep is optional (degraded if absent).
	if _, err := lookPath("rg"); err != nil {
		result.Warnings = append(result.Warnings,
			"rg (ripgrep) not found in PATH — file-search performance may be degraded")
	}

	result.Available = len(result.Errors) == 0
	return result
}

// checkDarwin checks macOS-specific dependencies.
func checkDarwin(r *DepCheckResult, lookupFile func(string) bool) {
	const sandboxExec = "/usr/bin/sandbox-exec"
	if !lookupFile(sandboxExec) {
		r.Errors = append(r.Errors,
			"sandbox-exec not found at "+sandboxExec+" — macOS seatbelt unavailable")
	}
}

// checkLinux checks Linux-specific dependencies (bubblewrap).
// NOTE: landlock+seccomp are kernel built-ins and are not checked here;
// their availability is gated at build-time and detected at applyLandlock call time.
func checkLinux(r *DepCheckResult, lookPath func(string) (string, error)) {
	if _, err := lookPath("bwrap"); err != nil {
		// bwrap is only needed if the ccgo linux sandbox backend uses it; the
		// current backend uses landlock+seccomp re-exec and does not require bwrap.
		// Surface as a warning (informational) rather than an error.
		r.Warnings = append(r.Warnings,
			"bwrap (bubblewrap) not found in PATH — optional Linux sandbox companion absent")
	}
}

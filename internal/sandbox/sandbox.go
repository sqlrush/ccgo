package sandbox

import (
	"errors"
	"runtime"
)

// ErrUnsupported is returned by Wrap when the sandbox is required but the
// current platform has no enforcement backend.
var ErrUnsupported = errors.New("sandbox not supported on this platform")

// Supported reports whether OS-level enforcement is available here.
func Supported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

// Wrap returns the executable + args needed to run (name, args...) confined by
// p. Per-OS implementations live in enforce_<os>.go behind build tags.
// SECURITY: Wrap confines unconditionally; the caller (bash tool) decides
// whether to call it via Policy.ShouldSandbox.
func Wrap(name string, args []string, p Policy) (string, []string, error) {
	return wrap(name, args, p)
}

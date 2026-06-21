//go:build linux

package sandbox

// wrap on Linux returns ErrUnsupported until Task 3 adds seccomp/bwrap enforcement.
// SECURITY: returning the command unwrapped here would silently disable the sandbox,
// so we error and let the caller decide (fail closed when FailIfUnavailable).
func wrap(name string, args []string, p Policy) (string, []string, error) {
	return "", nil, ErrUnsupported
}

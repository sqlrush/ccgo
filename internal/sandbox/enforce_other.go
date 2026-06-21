//go:build !darwin && !linux

package sandbox

// wrap on unsupported platforms refuses to confine. SECURITY: returning the
// command unwrapped here would silently disable the sandbox, so we error and
// let the caller decide (fail closed when FailIfUnavailable).
func wrap(name string, args []string, p Policy) (string, []string, error) {
	return "", nil, ErrUnsupported
}

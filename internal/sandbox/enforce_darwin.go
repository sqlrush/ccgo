//go:build darwin

package sandbox

import (
	"os"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

// wrap confines (name, args...) under a generated seatbelt profile, exec'd via
// /usr/bin/sandbox-exec -p <profile> -- <name> <args...>.
func wrap(name string, args []string, p Policy) (string, []string, error) {
	cwd, _ := os.Getwd()
	profile := buildSeatbeltProfile(p, cwd)
	wrapped := make([]string, 0, len(args)+4)
	wrapped = append(wrapped, "-p", profile, "--", name)
	wrapped = append(wrapped, args...)
	return sandboxExecPath, wrapped, nil
}

package repl

import (
	"bytes"
	"os/exec"
)

// execOutput runs name with args and returns trimmed stdout. Any stderr is
// discarded. Returns an error if the process exits non-zero.
func execOutput(name string, args []string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// execSilent runs name with args, discarding stdout and stderr.
// Returns an error if the process exits non-zero.
func execSilent(name string, args []string) error {
	return exec.Command(name, args...).Run()
}

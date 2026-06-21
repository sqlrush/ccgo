package orchestration

import "fmt"

// Isolation is the subagent isolation strategy. Only "worktree" is supported;
// "remote" is intentionally out of scope (cloud stack, roadmap §1).
type Isolation string

const (
	IsolationNone     Isolation = ""
	IsolationWorktree Isolation = "worktree"
)

// ValidateIsolation parses an isolation value, rejecting "remote" and unknowns.
func ValidateIsolation(s string) (Isolation, error) {
	switch s {
	case "":
		return IsolationNone, nil
	case "worktree":
		return IsolationWorktree, nil
	case "remote":
		return "", fmt.Errorf("isolation %q is not supported (cloud/remote is out of scope)", s)
	default:
		return "", fmt.Errorf("unknown isolation %q (supported: worktree)", s)
	}
}

// ValidateModelAlias parses a Task model override (CC enum sonnet/opus/haiku).
// An empty string is valid and means "use the session default".
func ValidateModelAlias(s string) (string, error) {
	switch s {
	case "", "sonnet", "opus", "haiku":
		return s, nil
	default:
		return "", fmt.Errorf("unknown model %q (supported: sonnet, opus, haiku)", s)
	}
}

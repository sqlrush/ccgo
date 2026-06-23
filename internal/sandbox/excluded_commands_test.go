package sandbox

import "testing"

// TestShouldSandboxCommandExcludedSimple verifies SBX-05:
// a simple command matching an excludedCommands entry skips sandbox.
func TestShouldSandboxCommandExcludedSimple(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"git"},
	}
	if p.ShouldSandboxCommand("git status", false) {
		t.Fatal("SBX-05: 'git status' matched excludedCommands 'git' but ShouldSandboxCommand returned true")
	}
}

// TestShouldSandboxCommandExcludedCompound verifies SBX-06:
// compound commands (&&) are split; any segment matching excludedCommands skips the sandbox.
func TestShouldSandboxCommandExcludedCompound(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"git"},
	}
	// "echo hi && git status" — 'git status' segment matches "git"
	if p.ShouldSandboxCommand("echo hi && git status", false) {
		t.Fatal("SBX-06: compound command with 'git status' segment must skip sandbox")
	}
	// pipes and semicolons also split
	if p.ShouldSandboxCommand("echo hi | git log", false) {
		t.Fatal("SBX-06: pipe compound with 'git log' segment must skip sandbox")
	}
	if p.ShouldSandboxCommand("echo hi; git status", false) {
		t.Fatal("SBX-06: semicolon compound with 'git status' must skip sandbox")
	}
}

// TestShouldSandboxCommandExcludedStripPrefixes verifies SBX-07:
// env-var prefixes and safe wrappers (timeout, env) are stripped before matching.
func TestShouldSandboxCommandExcludedStripPrefixes(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"make"},
	}
	// "FOO=bar timeout 30 make all" → strip env var → strip timeout → "make all" → prefix "make"
	if p.ShouldSandboxCommand("FOO=bar timeout 30 make all", false) {
		t.Fatal("SBX-07: env-prefix+wrapper-stripped command must match 'make'")
	}
}

// TestShouldSandboxCommandNoExclusionSandboxes verifies that without exclusions,
// an enabled policy sandboxes normally.
func TestShouldSandboxCommandNoExclusionSandboxes(t *testing.T) {
	p := Policy{Enabled: true}
	if !p.ShouldSandboxCommand("git status", false) {
		t.Fatal("with no excludedCommands, enabled policy should sandbox")
	}
}

// TestShouldSandboxCommandWildcardPattern verifies wildcard matching via * suffix.
func TestShouldSandboxCommandWildcardPattern(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"docker *"},
	}
	if p.ShouldSandboxCommand("docker ps", false) {
		t.Fatal("wildcard pattern 'docker *' should match 'docker ps'")
	}
	// But plain 'docker' shouldn't match 'docker *' (requires at least one arg)
	// Actually CC's wildcard pattern treats 'docker *' as starts-with 'docker ' OR exact
	// Let's just verify docker ps matches
}

// TestShouldSandboxCommandPrefixPattern verifies prefix pattern (trailing :*).
func TestShouldSandboxCommandPrefixPattern(t *testing.T) {
	p := Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"bazel:*"},
	}
	if p.ShouldSandboxCommand("bazel build //...", false) {
		t.Fatal("prefix pattern 'bazel:*' should match 'bazel build //...'")
	}
}

// TestShouldSandboxCommandDisabledIgnoresExclusions verifies that when sandbox
// is disabled, ShouldSandboxCommand returns false regardless of exclusions.
func TestShouldSandboxCommandDisabledIgnoresExclusions(t *testing.T) {
	p := Policy{
		Enabled:          false,
		ExcludedCommands: []string{"git"},
	}
	if p.ShouldSandboxCommand("echo unsafe", false) {
		t.Fatal("disabled sandbox always returns false")
	}
}

// TestShouldSandboxCommandEmptyCommand verifies empty command never sandboxes.
func TestShouldSandboxCommandEmptyCommand(t *testing.T) {
	p := Policy{Enabled: true}
	if p.ShouldSandboxCommand("", false) {
		t.Fatal("empty command should not sandbox")
	}
}

// TestSplitCompoundCommand tests the internal compound splitter.
func TestSplitCompoundCommand(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"git status", []string{"git status"}},
		{"echo hi && git log", []string{"echo hi ", " git log"}},
		{"a || b", []string{"a ", " b"}},
		{"a | b", []string{"a ", " b"}},
		{"a; b; c", []string{"a", " b", " c"}},
		{"a && b || c; d | e", []string{"a ", " b ", " c", " d ", " e"}},
	}
	for _, tc := range cases {
		got := splitCompoundCommand(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("splitCompoundCommand(%q): got %v want %v", tc.input, got, tc.want)
			continue
		}
		for i, g := range got {
			if g != tc.want[i] {
				t.Errorf("splitCompoundCommand(%q)[%d]: got %q want %q", tc.input, i, g, tc.want[i])
			}
		}
	}
}

// TestStripLeadingEnvVars tests env-var prefix stripping.
func TestStripLeadingEnvVars(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"make all", "make all"},
		{"FOO=bar make all", "make all"},
		{"A=1 B=2 make", "make"},
		{"timeout 30 make all", "timeout 30 make all"}, // env vars only stripped, not wrappers here
	}
	for _, tc := range cases {
		got := stripLeadingEnvVars(tc.input)
		if got != tc.want {
			t.Errorf("stripLeadingEnvVars(%q) = %q want %q", tc.input, got, tc.want)
		}
	}
}

// TestStripSafeWrappers tests safe-wrapper stripping (timeout, env, etc).
func TestStripSafeWrappers(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"make all", "make all"},
		{"timeout 30 make all", "make all"},
		{"env FOO=bar make all", "FOO=bar make all"},
		{"time make all", "make all"},
		{"nice make all", "make all"},
	}
	for _, tc := range cases {
		got := stripSafeWrapper(tc.input)
		if got != tc.want {
			t.Errorf("stripSafeWrapper(%q) = %q want %q", tc.input, got, tc.want)
		}
	}
}

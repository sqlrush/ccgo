package sandbox

// Policy is the OS-agnostic sandbox configuration for a single command.
// It is immutable: builders return new copies; ShouldSandbox is pure.
//
// Default (zero-value Policy): Enabled=false, AllowUnsandboxed=false.
// When Enabled is false, ShouldSandbox always returns false — sandbox is OFF.
// This matches CC behaviour: sandboxing must be opted-in via settings; the
// zero-value does NOT sandbox silently (there is no platform available by
// default). To enforce confinement, callers must set Enabled=true.
type Policy struct {
	Enabled           bool
	FailIfUnavailable bool
	AllowUnsandboxed  bool
	AllowWrite        []string
	DenyWrite         []string
	DenyRead          []string
	AllowRead         []string
	AllowNetwork      bool
}

// ShouldSandbox decides whether this command must be confined.
//
// Decision rule (CC parity — mirrors shouldUseSandbox.ts:130-153):
//
//	sandbox UNLESS (!Enabled) OR (dangerouslyDisableSandbox && AllowUnsandboxed)
//
// SECURITY — anti-footgun: the dangerouslyDisableSandbox flag bypasses
// confinement ONLY when the policy explicitly permits unsandboxed commands
// (AllowUnsandboxed=true). A stray flag alone does NOT silently disable the
// sandbox. This preserves the CC invariant: operator policy controls whether
// the flag is effective.
func (p Policy) ShouldSandbox(dangerouslyDisableSandbox bool) bool {
	if !p.Enabled {
		return false
	}
	if dangerouslyDisableSandbox && p.AllowUnsandboxed {
		return false
	}
	return true
}

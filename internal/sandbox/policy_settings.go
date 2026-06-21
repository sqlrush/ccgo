package sandbox

import "ccgo/internal/contracts"

// PolicyFromSettings builds an immutable Policy from merged settings.
//
// Defaults match CC (sandbox-adapter.ts:474-485):
//   - Enabled: false (must be explicitly enabled)
//   - AllowUnsandboxed: true  (allowUnsandboxedCommands ?? true)
//   - FailIfUnavailable: false (failIfUnavailable ?? false)
//   - AllowNetwork: false
//
// Unknown or wrong-typed values are silently ignored; the function never
// returns an error so that a bad settings value cannot prevent startup.
func PolicyFromSettings(s contracts.Settings) Policy {
	// CC default: allowUnsandboxedCommands ?? true
	p := Policy{AllowUnsandboxed: true}

	box := s.Sandbox
	if box == nil {
		return p
	}

	if v, ok := boolAt(box, "enabled"); ok {
		p.Enabled = v
	}
	if v, ok := boolAt(box, "failIfUnavailable"); ok {
		p.FailIfUnavailable = v
	}
	if v, ok := boolAt(box, "allowUnsandboxedCommands"); ok {
		p.AllowUnsandboxed = v
	}
	if v, ok := boolAt(box, "allowNetworkAccess"); ok {
		p.AllowNetwork = v
	}
	if fs, ok := box["filesystem"].(map[string]any); ok {
		p.AllowWrite = stringsAt(fs, "allowWrite")
		p.DenyWrite = stringsAt(fs, "denyWrite")
		p.DenyRead = stringsAt(fs, "denyRead")
		p.AllowRead = stringsAt(fs, "allowRead")
	}
	return p
}

// boolAt extracts a bool from a map[string]any, returning (false, false) if
// the key is absent or the value is not a bool.
func boolAt(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

// stringsAt extracts a []string from a []any in a map[string]any.
// Empty strings are dropped. Returns nil (not []) when nothing is found.
func stringsAt(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

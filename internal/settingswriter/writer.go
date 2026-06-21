// Package settingswriter persists permission-rule updates to the appropriate
// settings.json file (user or project). It is the shared persistence sink
// for "Allow always" dialogs and /permissions commands.
package settingswriter

import (
	"fmt"
	"sort"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
)

// Writer resolves and writes permission rules to the correct settings.json.
// Fields are public so callers can construct the value directly; use New for
// the standard constructor.
type Writer struct {
	UserPath    string
	ProjectPath string
}

// New returns a Writer that will persist user rules to userPath and project
// rules to projectPath.
func New(userPath, projectPath string) Writer {
	return Writer{UserPath: userPath, ProjectPath: projectPath}
}

// Apply persists a permission-rule update. Only addRules-style updates that
// carry at least one rule are handled; empty rule lists are rejected.
//
// The function follows an immutable read-modify-write pattern: it reads the
// destination document, builds a new merged map, and writes it back — the
// caller's update struct is never mutated.
//
// Rules are deduplicated (idempotent) and written in sorted order so that
// repeated calls and test assertions remain stable.
func (w Writer) Apply(update contracts.PermissionUpdate) error {
	if len(update.Rules) == 0 {
		return fmt.Errorf("settingswriter: update has no rules")
	}

	key, err := behaviorKey(update.Behavior)
	if err != nil {
		return err
	}

	path := w.destinationPath(update.Destination)

	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("settingswriter: read %s: %w", path, err)
	}

	// Build a new permissions map (do not mutate the one from doc).
	existing := asStringSet(asMap(doc["permissions"])[key])
	for _, value := range update.Rules {
		rule := permissions.PermissionRuleValueToString(value)
		existing[rule] = struct{}{}
	}

	// Build a new top-level doc to avoid mutating the doc map in-place with
	// a reference we don't own.
	newPerms := copyMap(asMap(doc["permissions"]))
	newPerms[key] = sortedKeys(existing)

	newDoc := copyMap(doc)
	newDoc["permissions"] = newPerms

	return config.WriteSettingsDocument(path, newDoc)
}

// destinationPath maps a PermissionUpdate.Destination to a filesystem path.
// Both projectSettings and localSettings go to the project file; everything
// else (including userSettings and the default) goes to the user file.
func (w Writer) destinationPath(destination string) string {
	switch destination {
	case string(contracts.PermissionSourceProjectSettings),
		string(contracts.PermissionSourceLocalSettings):
		return w.ProjectPath
	default:
		return w.UserPath
	}
}

// behaviorKey maps a PermissionBehavior to the JSON key used in
// permissions.{allow,deny,ask}.
func behaviorKey(behavior contracts.PermissionBehavior) (string, error) {
	switch behavior {
	case contracts.PermissionAllow:
		return "allow", nil
	case contracts.PermissionDeny:
		return "deny", nil
	case contracts.PermissionAsk:
		return "ask", nil
	default:
		return "", fmt.Errorf("settingswriter: unsupported behavior %q", behavior)
	}
}

// asMap safely casts v to map[string]any, returning a fresh empty map if the
// cast fails. It does NOT return the original map to prevent callers from
// accidentally mutating shared state.
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// copyMap returns a shallow copy of m. This is sufficient because values we
// store ([]any slices of strings) are replaced entirely rather than mutated.
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// asStringSet converts a []any (JSON array) to a set of strings, ignoring
// non-string elements.
func asStringSet(v any) map[string]struct{} {
	out := map[string]struct{}{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				out[s] = struct{}{}
			}
		}
	}
	return out
}

// sortedKeys converts a string set to a sorted []any slice for deterministic
// JSON output.
func sortedKeys(set map[string]struct{}) []any {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

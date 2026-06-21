// Package config — permission-rule persistence helpers.
//
// AddPermissionRule and RemovePermissionRule follow the same immutable
// read-copy-write pattern as internal/settingswriter: they read the full
// settings document, build a new permissions map (never mutating the original),
// and write back. They share ReadSettingsDocument/WriteSettingsDocument so
// there is exactly one writer path — no divergent parallel writer.
package config

import (
	"fmt"
	"sort"
)

// validBehaviors is the set of JSON keys under settings.permissions that may
// hold rule arrays. It matches PermissionsSetting.Allow/Deny/Ask.
var validBehaviors = map[string]bool{
	"allow": true,
	"deny":  true,
	"ask":   true,
}

// AddPermissionRule appends rule to the permissions.<behavior> array in the
// settings document at path, creating the file if it does not exist.
//
// Rules are deduplicated (idempotent) and written in sorted order for
// deterministic output. All other permissions keys and top-level keys are
// preserved unchanged (no-clobber).
func AddPermissionRule(path, behavior, rule string) error {
	if !validBehaviors[behavior] {
		return fmt.Errorf("config: invalid permission behavior %q: must be allow, deny, or ask", behavior)
	}
	if rule == "" {
		return fmt.Errorf("config: permission rule must not be empty")
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	origPerms := asPermMap(doc["permissions"])
	existingSet := rulesAsSet(origPerms[behavior])
	existingSet[rule] = struct{}{} // idempotent add

	newPerms := copyAny(origPerms)
	newPerms[behavior] = sortedSlice(existingSet)

	newDoc := copyAny(doc)
	newDoc["permissions"] = newPerms

	return WriteSettingsDocument(path, newDoc)
}

// RemovePermissionRule removes rule from the permissions.<behavior> array in
// the settings document at path. If the rule is not present, the call is a
// no-op and returns nil. All other keys are preserved unchanged (no-clobber).
func RemovePermissionRule(path, behavior, rule string) error {
	if !validBehaviors[behavior] {
		return fmt.Errorf("config: invalid permission behavior %q: must be allow, deny, or ask", behavior)
	}
	if rule == "" {
		return fmt.Errorf("config: permission rule must not be empty")
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}

	origPerms := asPermMap(doc["permissions"])
	existingSet := rulesAsSet(origPerms[behavior])

	if _, found := existingSet[rule]; !found {
		// Nothing to remove — no-op.
		return nil
	}
	delete(existingSet, rule)

	newPerms := copyAny(origPerms)
	newPerms[behavior] = sortedSlice(existingSet)

	newDoc := copyAny(doc)
	newDoc["permissions"] = newPerms

	return WriteSettingsDocument(path, newDoc)
}

// asPermMap safely casts v to map[string]any, returning a fresh empty map when
// v is nil or not a map. Callers copy before writing (see copyAny).
func asPermMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// rulesAsSet converts a []any JSON array to a set of strings, ignoring
// non-string values.
func rulesAsSet(v any) map[string]struct{} {
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

// sortedSlice converts a string set to a sorted []any for deterministic JSON.
func sortedSlice(set map[string]struct{}) []any {
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

// copyAny returns a shallow copy of a map[string]any. Values that are
// themselves maps are NOT deep-copied; they are referenced rather than
// mutated, so the shallow copy is safe for our write-replace pattern.
func copyAny(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

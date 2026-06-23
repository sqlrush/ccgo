package anthropic

import (
	"encoding/json"
	"os"
	"strconv"
	"time"
)

// APIMaxMediaPerRequest is the maximum number of media items (images + documents)
// allowed in a single API request. Matches CC: constants/apiLimits.ts:94.
const APIMaxMediaPerRequest = 100

// getExtraBody returns extra JSON fields to merge into the API request body,
// sourced from the CLAUDE_CODE_EXTRA_BODY environment variable.
// The env var must be a JSON object; non-objects and invalid JSON are silently ignored.
// CC ref: services/api/claude.ts:272-330 (getExtraBodyParams).
func getExtraBody() map[string]any {
	raw := os.Getenv("CLAUDE_CODE_EXTRA_BODY")
	if raw == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// Invalid JSON — silently ignore per CC behaviour.
		return nil
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		// Non-object (array, string, number, bool) — silently ignore per CC behaviour.
		return nil
	}
	return obj
}

// NonstreamingFallbackTimeout returns the timeout for non-streaming (fallback)
// API requests. Remote sessions default to 120s; local sessions to 300s.
// The API_TIMEOUT_MS env var overrides both.
// CC ref: services/api/claude.ts:807-811 (getNonstreamingFallbackTimeoutMs).
func NonstreamingFallbackTimeout() time.Duration {
	if override := os.Getenv("API_TIMEOUT_MS"); override != "" {
		if ms, err := strconv.Atoi(override); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_REMOTE")) {
		return 120 * time.Second
	}
	return 300 * time.Second
}

// mergeExtraBody merges extra key-value pairs into an existing JSON object.
// Extra fields are shallow-merged into the top-level object; existing keys
// from the payload take precedence (extra body can only add new keys).
// CC ref: services/api/claude.ts:562 (...extraBodyParams spread).
func mergeExtraBody(payload []byte, extra map[string]any) ([]byte, error) {
	var base map[string]any
	if err := json.Unmarshal(payload, &base); err != nil {
		return payload, err
	}
	for k, v := range extra {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return json.Marshal(base)
}

// isEnvTruthy mirrors CC's isEnvTruthy: "1", "true", "yes", "on" (case-insensitive).
func isEnvTruthy(value string) bool {
	switch value {
	case "1", "true", "yes", "on", "TRUE", "YES", "ON", "True", "Yes", "On":
		return true
	}
	return false
}

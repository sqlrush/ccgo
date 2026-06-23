package conversation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

// deviceIDFile is the file under ClaudeHomeDir that persists the stable
// per-machine device identifier. CC ref: getOrCreateUserID (utils/config.ts:1757).
const deviceIDFile = "device_id"

var (
	deviceIDOnce  sync.Once
	cachedDeviceID string
)

// getOrCreateDeviceID returns a stable per-machine hex ID, creating and
// persisting one on first call. Concurrent-safe via sync.Once.
// CC ref: getOrCreateUserID (utils/config.ts:1757).
func getOrCreateDeviceID() string {
	deviceIDOnce.Do(func() {
		cachedDeviceID = loadOrGenerateDeviceID()
	})
	return cachedDeviceID
}

func loadOrGenerateDeviceID() string {
	path := filepath.Join(platform.ClaudeHomeDir(), deviceIDFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	// Generate a new 32-byte (64-hex-char) random ID matching CC's randomBytes(32).
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use a zero-filled placeholder rather than panicking.
		b = make([]byte, 32)
	}
	id := hex.EncodeToString(b)
	// Best-effort persist; failures are non-fatal.
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}

// resetDeviceIDCache clears the once cache; only for use in tests.
func resetDeviceIDCache() {
	deviceIDOnce = sync.Once{}
	cachedDeviceID = ""
}

// buildAPIMetadata constructs the request metadata map containing user_id as a
// JSON-encoded string with device_id and session_id, matching CC's getAPIMetadata.
// CC ref: getAPIMetadata (services/api/claude.ts:503-528).
func buildAPIMetadata(sessionID contracts.ID) map[string]any {
	inner := map[string]any{
		"device_id":    getOrCreateDeviceID(),
		"session_id":   string(sessionID),
		"account_uuid": "",
	}
	encoded, err := json.Marshal(inner)
	if err != nil {
		// Should never happen; use empty object string as fallback.
		encoded = []byte("{}")
	}
	return map[string]any{
		"user_id": string(encoded),
	}
}

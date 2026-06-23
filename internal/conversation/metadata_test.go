package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// TestBuildRequestIncludesMetadataUserID verifies that buildRequest populates
// request.Metadata["user_id"] as a JSON-encoded string containing session_id
// and device_id. CC ref: getAPIMetadata (claude.ts:503-528).
func TestBuildRequestIncludesMetadataUserID(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools:     tool.NewExecutor(reg),
		Model:     "claude-sonnet-4-6",
		SessionID: contracts.ID("test-session-123"),
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if req.Metadata == nil {
		t.Fatal("Metadata should not be nil")
	}
	rawUserID, ok := req.Metadata["user_id"]
	if !ok {
		t.Fatal("Metadata should contain user_id key")
	}
	userIDStr, ok := rawUserID.(string)
	if !ok {
		t.Fatalf("Metadata.user_id should be a string, got %T", rawUserID)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(userIDStr), &parsed); err != nil {
		t.Fatalf("Metadata.user_id is not valid JSON: %v (value: %q)", err, userIDStr)
	}
	if parsed["session_id"] != "test-session-123" {
		t.Fatalf("Metadata.user_id.session_id = %q want test-session-123", parsed["session_id"])
	}
	deviceID, _ := parsed["device_id"].(string)
	if deviceID == "" {
		t.Fatal("Metadata.user_id.device_id should be non-empty")
	}
}

// TestGetOrCreateDeviceIDPersists verifies that repeated calls return the
// same ID and that it is stored in the config dir.
func TestGetOrCreateDeviceIDPersists(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	id1 := getOrCreateDeviceID()
	id2 := getOrCreateDeviceID()
	if id1 == "" {
		t.Fatal("device ID should be non-empty")
	}
	if id1 != id2 {
		t.Fatalf("device ID changed between calls: %q vs %q", id1, id2)
	}
}

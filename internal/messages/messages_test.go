package messages

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestNormalizeForAPI(t *testing.T) {
	in := []contracts.Message{
		SystemText("notice", "ignored"),
		UserText("hello"),
		AssistantText("world", "sonnet", nil),
	}
	out := NormalizeForAPI(in)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d", len(out))
	}
	if out[0].Role != "user" || out[1].Role != "assistant" {
		t.Fatalf("roles = %#v", out)
	}
}

func TestNormalizeForAPIExpandsDeferredToolsDeltaAttachment(t *testing.T) {
	in := []contracts.Message{
		{
			Type: contracts.MessageAttachment,
			Raw: map[string]any{"attachment": map[string]any{
				"type":         "deferred_tools_delta",
				"addedLines":   []any{"Read", "mcp__github__search"},
				"removedNames": []any{"mcp__old__search"},
			}},
		},
		UserText("hello"),
	}
	out := NormalizeForAPI(in)
	if len(out) != 2 || out[0].Role != "user" || out[1].Role != "user" {
		t.Fatalf("out = %#v", out)
	}
	text := out[0].Content[0].Text
	if !containsAll(text,
		"The following deferred tools are now available via ToolSearch:",
		"Read",
		"mcp__github__search",
		"The following deferred tools are no longer available",
		"mcp__old__search",
	) {
		t.Fatalf("attachment text = %q", text)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestLinkParentChain(t *testing.T) {
	linked := LinkParentChain([]contracts.Message{UserText("a"), AssistantText("b", "", nil)})
	if len(linked) != 2 {
		t.Fatalf("len(linked) = %d", len(linked))
	}
	if linked[0].ParentUUID != nil {
		t.Fatal("first message should not have parent")
	}
	if linked[1].ParentUUID == nil || *linked[1].ParentUUID != linked[0].UUID {
		t.Fatalf("second parent = %#v, first uuid = %s", linked[1].ParentUUID, linked[0].UUID)
	}
}

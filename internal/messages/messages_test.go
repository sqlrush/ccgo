package messages

import (
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

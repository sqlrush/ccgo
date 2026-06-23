package repl

import (
	"testing"

	asktools "ccgo/internal/tools/ask"
	"ccgo/internal/tool"
)

// TestMergeQuestionAskerInjectsKey verifies that mergeQuestionAsker returns a
// new metadata map containing the loopQuestionAsker under MetadataQuestionAskerKey
// and does not mutate the input map.
func TestMergeQuestionAskerInjectsKey(t *testing.T) {
	ch := make(chan questionRequest, 1)

	// nil base map
	m := mergeQuestionAsker(nil, ch)
	if m == nil {
		t.Fatal("mergeQuestionAsker(nil, ch) returned nil")
	}
	asker, ok := m[asktools.MetadataQuestionAskerKey].(tool.QuestionAsker)
	if !ok || asker == nil {
		t.Fatalf("MetadataQuestionAskerKey not set to a QuestionAsker; got %T", m[asktools.MetadataQuestionAskerKey])
	}

	// pre-existing map is not mutated
	base := map[string]any{"existing": "value"}
	m2 := mergeQuestionAsker(base, ch)
	if _, preserved := m2["existing"]; !preserved {
		t.Fatal("mergeQuestionAsker should preserve existing keys")
	}
	if _, mutated := base[asktools.MetadataQuestionAskerKey]; mutated {
		t.Fatal("mergeQuestionAsker must not mutate the input map")
	}
}

// TestMergeQuestionAskerProducesLoopQuestionAsker verifies the concrete type
// returned is a loopQuestionAsker wired to the given channel.
func TestMergeQuestionAskerProducesLoopQuestionAsker(t *testing.T) {
	ch := make(chan questionRequest, 1)
	m := mergeQuestionAsker(nil, ch)
	qa := m[asktools.MetadataQuestionAskerKey]
	lqa, ok := qa.(loopQuestionAsker)
	if !ok {
		t.Fatalf("expected loopQuestionAsker, got %T", qa)
	}
	if lqa.questionCh != ch {
		t.Fatal("loopQuestionAsker should be wired to the provided channel")
	}
}

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestApplyToolResultBudgetPersistsLargestFreshResult(t *testing.T) {
	storeDir := t.TempDir()
	messages := budgetMessages("12345678", "abcdefgh")
	state := NewContentReplacementState()

	updated, records, err := ApplyToolResultBudget(messages, state, ToolResultBudgetOptions{
		LimitChars: 9,
		StoreDir:   storeDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "tool-result" || records[0].ToolUseID != "toolu_1" {
		t.Fatalf("records = %#v", records)
	}
	content := updated[len(updated)-1].Content
	if got, _ := content[0].Content.(string); !strings.HasPrefix(got, PersistedOutputTag) {
		t.Fatalf("first result was not replaced: %#v", got)
	}
	if got, _ := content[1].Content.(string); got != "abcdefgh" {
		t.Fatalf("second result = %#v", got)
	}
	data, err := os.ReadFile(filepath.Join(storeDir, "toolu_1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "12345678" {
		t.Fatalf("persisted content = %q", string(data))
	}
}

func TestApplyToolResultBudgetReappliesReconstructedReplacement(t *testing.T) {
	messages := budgetMessages("12345678", "abcdefgh")
	replacement := PersistedOutputTag + "\nold preview\n" + persistedOutputClosingTag
	state := ReconstructContentReplacementState(messages, []ContentReplacementRecord{{
		Kind:        "tool-result",
		ToolUseID:   "toolu_2",
		Replacement: replacement,
	}})

	updated, records, err := ApplyToolResultBudget(messages, state, ToolResultBudgetOptions{LimitChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v", records)
	}
	if got, _ := updated[len(updated)-1].Content[1].Content.(string); got != replacement {
		t.Fatalf("reapplied = %#v", got)
	}
	if got, _ := updated[len(updated)-1].Content[0].Content.(string); got != "12345678" {
		t.Fatalf("frozen result changed = %#v", got)
	}
}

func budgetMessages(first string, second string) []contracts.Message {
	return []contracts.Message{
		{
			ID:   "msg_parallel",
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentToolUse, ID: "toolu_1", Name: "Big"},
				{Type: contracts.ContentToolUse, ID: "toolu_2", Name: "Big"},
			},
		},
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentToolResult, ToolUseID: "toolu_1", Content: first},
				{Type: contracts.ContentToolResult, ToolUseID: "toolu_2", Content: second},
			},
		},
	}
}

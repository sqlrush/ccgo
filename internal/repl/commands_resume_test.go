package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestResumeHandlerLoadsByID(t *testing.T) {
	listed := []resumeEntry{
		{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "first"},
		{ID: "sess-b", Path: "/x/sess-b.jsonl", Title: "second"},
	}
	loaded := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			if id != "sess-b" {
				t.Fatalf("loaded wrong session %q", id)
			}
			return loaded, nil
		},
	)
	out, err := h(context.Background(), CommandContext{Args: "sess-b"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled || !out.ReplaceHistory || len(out.NewHistory) != 1 {
		t.Fatalf("unexpected outcome %+v", out)
	}
}

func TestResumeHandlerNoArgListsSessions(t *testing.T) {
	listed := []resumeEntry{{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "first"}}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(string, contracts.ID) ([]contracts.Message, error) { t.Fatal("should not load"); return nil, nil },
	)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if out.ReplaceHistory {
		t.Fatal("no-arg resume must not replace history; it lists")
	}
	if out.Status == "" {
		t.Fatal("expected a listing in Status")
	}
}

func TestResumeHandlerLoadsByIndex(t *testing.T) {
	listed := []resumeEntry{
		{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "alpha"},
		{ID: "sess-b", Path: "/x/sess-b.jsonl", Title: "beta"},
	}
	loaded := []contracts.Message{
		{Type: contracts.MessageAssistant, Content: []contracts.ContentBlock{contracts.NewTextBlock("reply")}},
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("msg2")}},
	}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			if id != "sess-b" {
				t.Fatalf("index 2 should resolve to sess-b, got %q", id)
			}
			return loaded, nil
		},
	)
	out, err := h(context.Background(), CommandContext{Args: "2"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled || !out.ReplaceHistory || len(out.NewHistory) != 2 {
		t.Fatalf("unexpected outcome %+v", out)
	}
	if !strings.Contains(out.Status, "sess-b") {
		t.Fatalf("status should mention the session id; got %q", out.Status)
	}
}

func TestResumeHandlerUnresolvableArgIsHandledWithError(t *testing.T) {
	listed := []resumeEntry{{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "alpha"}}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(string, contracts.ID) ([]contracts.Message, error) { t.Fatal("should not load"); return nil, nil },
	)
	out, err := h(context.Background(), CommandContext{Args: "nonexistent-xyz"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	// Must be Handled (don't fall through to model) with an error status.
	if !out.Handled {
		t.Fatal("unresolvable arg must still be Handled=true")
	}
	if out.ReplaceHistory {
		t.Fatal("unresolvable arg must not replace history")
	}
	if out.Status == "" {
		t.Fatal("expected an error status for unresolvable arg")
	}
}

func TestResumeHandlerLoadsBySearchTerm(t *testing.T) {
	listed := []resumeEntry{
		{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "debugging go channels"},
		{ID: "sess-b", Path: "/x/sess-b.jsonl", Title: "writing tests"},
	}
	loaded := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("go")},
		},
	}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			if id != "sess-a" {
				t.Fatalf("search 'debugging' should resolve to sess-a, got %q", id)
			}
			return loaded, nil
		},
	)
	out, err := h(context.Background(), CommandContext{Args: "debugging"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled || !out.ReplaceHistory || len(out.NewHistory) != 1 {
		t.Fatalf("unexpected outcome %+v", out)
	}
}

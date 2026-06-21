package asktools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeQuestionAsker struct {
	got    []tool.Question
	answer []tool.QuestionAnswer
}

func (f *fakeQuestionAsker) AskQuestions(_ context.Context, qs []tool.Question) ([]tool.QuestionAnswer, error) {
	f.got = qs
	return f.answer, nil
}

func validAskInput() json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which theme do you want?",
			"options": []any{
				map[string]any{"label": "Dark", "description": "Dark UI"},
				map[string]any{"label": "Light", "description": "Light UI"},
			},
		}},
	})
	return raw
}

func TestAskUserQuestionValidatesSchema(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	// Empty questions array → error.
	bad, _ := json.Marshal(map[string]any{"questions": []any{}})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for empty questions")
	}
	// Valid input passes.
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, validAskInput()); err != nil {
		t.Fatalf("valid input failed validation: %v", err)
	}
}

func TestAskUserQuestionCallsAsker(t *testing.T) {
	asker := &fakeQuestionAsker{answer: []tool.QuestionAnswer{{Header: "Theme", Selected: []string{"Dark"}}}}
	toolImpl := NewAskUserQuestionTool()
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataQuestionAskerKey: asker},
	}
	res, err := toolImpl.Call(ctx, validAskInput(), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if len(asker.got) != 1 || asker.got[0].Header != "Theme" {
		t.Fatalf("asker did not receive question: %+v", asker.got)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Dark") {
		t.Fatalf("result missing answer: %q", content)
	}
}

func TestAskUserQuestionHeadlessDeny(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	res, err := toolImpl.Call(tool.Context{Context: context.Background()}, validAskInput(), tool.NopProgressSink())
	if err == nil && !res.IsError {
		t.Fatal("expected error when no QuestionAsker is configured")
	}
}

func TestAskUserQuestionValidation_TooManyQuestions(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	questions := make([]any, 5)
	for i := range questions {
		questions[i] = map[string]any{
			"header":   "H" + strings.Repeat("x", i),
			"question": "Question text?",
			"options": []any{
				map[string]any{"label": "A", "description": "desc"},
				map[string]any{"label": "B", "description": "desc"},
			},
		}
	}
	bad, _ := json.Marshal(map[string]any{"questions": questions})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for >4 questions")
	}
}

func TestAskUserQuestionValidation_TooFewOptions(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which?",
			"options": []any{
				map[string]any{"label": "A", "description": "only one"},
			},
		}},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for <2 options")
	}
}

func TestAskUserQuestionValidation_TooManyOptions(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	options := make([]any, 5)
	for i := range options {
		options[i] = map[string]any{"label": string(rune('A' + i)), "description": "desc"}
	}
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which?",
			"options":  options,
		}},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for >4 options")
	}
}

func TestAskUserQuestionValidation_HeaderTooLong(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "TooLongHeader", // 13 chars
			"question": "Which?",
			"options": []any{
				map[string]any{"label": "A", "description": "desc"},
				map[string]any{"label": "B", "description": "desc"},
			},
		}},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for header >12 chars")
	}
}

func TestAskUserQuestionValidation_QuestionNotEndingWithQuestionMark(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which theme do you want",
			"options": []any{
				map[string]any{"label": "A", "description": "desc"},
				map[string]any{"label": "B", "description": "desc"},
			},
		}},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error when question does not end with '?'")
	}
}

func TestAskUserQuestionValidation_DuplicateQuestionText(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{
			map[string]any{
				"header":   "H1",
				"question": "Same question?",
				"options": []any{
					map[string]any{"label": "A", "description": "desc"},
					map[string]any{"label": "B", "description": "desc"},
				},
			},
			map[string]any{
				"header":   "H2",
				"question": "Same question?",
				"options": []any{
					map[string]any{"label": "A", "description": "desc"},
					map[string]any{"label": "B", "description": "desc"},
				},
			},
		},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for duplicate question texts")
	}
}

func TestAskUserQuestionValidation_DuplicateOptionLabel(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	bad, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which?",
			"options": []any{
				map[string]any{"label": "Same", "description": "desc1"},
				map[string]any{"label": "Same", "description": "desc2"},
			},
		}},
	})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for duplicate option labels")
	}
}

func TestAskUserQuestionResultFormat(t *testing.T) {
	asker := &fakeQuestionAsker{
		answer: []tool.QuestionAnswer{
			{Header: "Theme", Selected: []string{"Dark"}},
			{Header: "Lang", Selected: []string{"Go", "Rust"}},
		},
	}
	toolImpl := NewAskUserQuestionTool()
	input, _ := json.Marshal(map[string]any{
		"questions": []any{
			map[string]any{
				"header":   "Theme",
				"question": "Which theme?",
				"options": []any{
					map[string]any{"label": "Dark", "description": "Dark UI"},
					map[string]any{"label": "Light", "description": "Light UI"},
				},
			},
			map[string]any{
				"header":      "Lang",
				"question":    "Which languages?",
				"multiSelect": true,
				"options": []any{
					map[string]any{"label": "Go", "description": "The Go language"},
					map[string]any{"label": "Rust", "description": "The Rust language"},
				},
			},
		},
	})
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataQuestionAskerKey: asker},
	}
	res, err := toolImpl.Call(ctx, input, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.HasPrefix(content, "User has answered your questions:") {
		t.Errorf("wrong prefix: %q", content)
	}
	if !strings.Contains(content, "Theme: Dark") {
		t.Errorf("missing Theme answer: %q", content)
	}
	if !strings.Contains(content, "Lang: Go, Rust") {
		t.Errorf("missing Lang answer: %q", content)
	}
}

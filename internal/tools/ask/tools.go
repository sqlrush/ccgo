package asktools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// MetadataQuestionAskerKey is the key under which a tool.QuestionAsker is
// stored in tool.Context.Metadata. The TUI injects its chip dialog here;
// headless callers omit it and the tool errors cleanly without hanging.
const MetadataQuestionAskerKey = "ccgo.tools.ask.asker"

const (
	maxQuestions   = 4
	minOptions     = 2
	maxOptions     = 4
	maxHeaderChars = 12
)

type askInput struct {
	Questions []askQuestion `json:"questions"`
}

type askQuestion struct {
	Header      string      `json:"header"`
	Question    string      `json:"question"`
	Options     []askOption `json:"options"`
	MultiSelect bool        `json:"multiSelect,omitempty"`
}

type askOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// NewAskUserQuestionTool constructs the AskUserQuestion tool. It reads a
// QuestionAsker from ctx.Metadata; if absent it returns a headless-safe
// error instead of hanging.
func NewAskUserQuestionTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "AskUserQuestion",
			Description:         "Ask the user one to four multiple-choice questions and collect their answers.",
			RequiresInteraction: true,
			ReadOnly:            true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"questions"},
				"properties": map[string]any{
					"questions": map[string]any{
						"type":        "array",
						"description": "1-4 questions to present to the user.",
						"minItems":    1,
						"maxItems":    maxQuestions,
						"items": map[string]any{
							"type":     "object",
							"required": []any{"header", "question", "options"},
							"properties": map[string]any{
								"header": map[string]any{
									"type":        "string",
									"description": "Short chip label, max 12 characters.",
									"maxLength":   maxHeaderChars,
								},
								"question": map[string]any{
									"type":        "string",
									"description": "The question text, must end with '?'.",
								},
								"multiSelect": map[string]any{
									"type":    "boolean",
									"default": false,
								},
								"options": map[string]any{
									"type":     "array",
									"minItems": minOptions,
									"maxItems": maxOptions,
									"items": map[string]any{
										"type":     "object",
										"required": []any{"label", "description"},
										"properties": map[string]any{
											"label": map[string]any{
												"type":        "string",
												"description": "1-5 word option label.",
											},
											"description": map[string]any{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Asks the user 1-4 multiple-choice questions and waits for their selections. " +
				"Each question has a short header (≤12 chars), a question text ending with '?', " +
				"and 2-4 options with a label and description. " +
				"An 'Other' free-text option is always added automatically by the UI. " +
				"Returns the user's selections formatted as text.", nil
		},
		ValidateFunc: validateAsk,
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			// Always allow: the interaction itself is the user's consent.
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "AskUserQuestion is inherently interactive; user answers constitute consent",
			}, nil
		},
		CallFunc:        callAsk,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func validateAsk(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeAsk(raw)
	if err != nil {
		return err
	}
	if len(input.Questions) == 0 {
		return fmt.Errorf("questions is required and must have 1-%d entries", maxQuestions)
	}
	if len(input.Questions) > maxQuestions {
		return fmt.Errorf("at most %d questions allowed, got %d", maxQuestions, len(input.Questions))
	}
	seenQuestionText := make(map[string]struct{}, len(input.Questions))
	for i, q := range input.Questions {
		if strings.TrimSpace(q.Header) == "" {
			return fmt.Errorf("questions[%d].header is required", i)
		}
		if len([]rune(q.Header)) > maxHeaderChars {
			return fmt.Errorf("questions[%d].header must be at most %d characters, got %d",
				i, maxHeaderChars, len([]rune(q.Header)))
		}
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("questions[%d].question is required", i)
		}
		if !strings.HasSuffix(strings.TrimSpace(q.Question), "?") {
			return fmt.Errorf("questions[%d].question must end with '?': %q", i, q.Question)
		}
		if _, dup := seenQuestionText[q.Question]; dup {
			return fmt.Errorf("questions[%d].question text is a duplicate: %q", i, q.Question)
		}
		seenQuestionText[q.Question] = struct{}{}
		if len(q.Options) < minOptions || len(q.Options) > maxOptions {
			return fmt.Errorf("questions[%d].options must have %d-%d entries, got %d",
				i, minOptions, maxOptions, len(q.Options))
		}
		seenLabel := make(map[string]struct{}, len(q.Options))
		for j, o := range q.Options {
			if strings.TrimSpace(o.Label) == "" {
				return fmt.Errorf("questions[%d].options[%d].label is required", i, j)
			}
			if _, dup := seenLabel[o.Label]; dup {
				return fmt.Errorf("questions[%d].options[%d].label is a duplicate within the question: %q",
					i, j, o.Label)
			}
			seenLabel[o.Label] = struct{}{}
		}
	}
	return nil
}

func callAsk(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeAsk(raw)
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("AskUserQuestion: decode input: %w", err)
	}
	asker := questionAskerFromMetadata(ctx.Metadata)
	if asker == nil {
		return contracts.ToolResult{
			IsError: true,
			Content: "AskUserQuestion is unavailable: no interactive question handler is configured " +
				"(headless mode). Cannot display a question dialog without a connected terminal.",
		}, fmt.Errorf("AskUserQuestion: no QuestionAsker configured (headless mode)")
	}
	answers, err := asker.AskQuestions(ctx.Context, toToolQuestions(input.Questions))
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("AskUserQuestion: question dialog: %w", err)
	}
	return contracts.ToolResult{
		Content:           formatAnswers(answers),
		StructuredContent: map[string]any{"type": "ask_user_question", "answers": structuredAnswers(answers)},
	}, nil
}

func toToolQuestions(qs []askQuestion) []tool.Question {
	out := make([]tool.Question, len(qs))
	for i, q := range qs {
		opts := make([]tool.QuestionOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = tool.QuestionOption{Label: o.Label, Description: o.Description}
		}
		out[i] = tool.Question{
			Header:      q.Header,
			Question:    q.Question,
			Options:     opts,
			MultiSelect: q.MultiSelect,
		}
	}
	return out
}

func formatAnswers(answers []tool.QuestionAnswer) string {
	parts := make([]string, len(answers))
	for i, a := range answers {
		parts[i] = fmt.Sprintf("%s: %s", a.Header, strings.Join(a.Selected, ", "))
	}
	return "User has answered your questions: " + strings.Join(parts, "; ") +
		". You can now continue with the user's answers in mind."
}

func structuredAnswers(answers []tool.QuestionAnswer) []map[string]any {
	out := make([]map[string]any, len(answers))
	for i, a := range answers {
		out[i] = map[string]any{"header": a.Header, "selected": a.Selected}
	}
	return out
}

func questionAskerFromMetadata(metadata map[string]any) tool.QuestionAsker {
	if metadata == nil {
		return nil
	}
	asker, _ := metadata[MetadataQuestionAskerKey].(tool.QuestionAsker)
	return asker
}

func decodeAsk(raw json.RawMessage) (askInput, error) {
	var input askInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return askInput{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return input, nil
}

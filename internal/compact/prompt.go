package compact

import (
	"strings"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

const NoToolsPreamble = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn -- you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.
`

const BaseSummaryPrompt = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Your summary should include:

1. Primary Request and Intent
2. Key Technical Concepts
3. Files and Code Sections
4. Errors and fixes
5. Problem Solving
6. All user messages
7. Pending Tasks
8. Current Work
9. Optional Next Step
`

type PromptMode string

const (
	PromptFull    PromptMode = "full"
	PromptPartial PromptMode = "partial"
)

func SummaryPrompt(mode PromptMode, extraInstructions string) string {
	var builder strings.Builder
	builder.WriteString(NoToolsPreamble)
	builder.WriteString("\n")
	builder.WriteString(BaseSummaryPrompt)
	if mode == PromptPartial {
		builder.WriteString("\nFocus on the recent messages only; earlier retained context will remain available.\n")
	}
	if strings.TrimSpace(extraInstructions) != "" {
		builder.WriteString("\nAdditional compact instructions:\n")
		builder.WriteString(strings.TrimSpace(extraInstructions))
		builder.WriteString("\n")
	}
	return builder.String()
}

func SummaryRequestMessage(mode PromptMode, extraInstructions string) contracts.Message {
	return msgs.UserText(SummaryPrompt(mode, extraInstructions))
}

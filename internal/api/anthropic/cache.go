package anthropic

import "ccgo/internal/contracts"

type CacheBreakpointOptions struct {
	SkipCacheWrite bool
	CacheControl   contracts.CacheControl
	NewCacheEdits  []contracts.CacheEdit
}

func AddCacheBreakpoints(messages []contracts.APIMessage, enablePromptCaching bool, options CacheBreakpointOptions) []contracts.APIMessage {
	out := copyAPIMessages(messages)
	if len(out) == 0 {
		return out
	}
	if options.CacheControl.Type == "" {
		options.CacheControl.Type = "ephemeral"
	}
	if enablePromptCaching {
		markerIndex := len(out) - 1
		if options.SkipCacheWrite {
			markerIndex = len(out) - 2
		}
		if markerIndex >= 0 {
			ensureContentBlock(&out[markerIndex])
			last := len(out[markerIndex].Content) - 1
			out[markerIndex].Content[last].CacheControl = &contracts.CacheControl{
				Type:  options.CacheControl.Type,
				Scope: options.CacheControl.Scope,
				TTL:   options.CacheControl.TTL,
			}
		}
	}
	if len(options.NewCacheEdits) > 0 {
		insertCacheEditsIntoLastUserMessage(out, dedupeCacheEdits(options.NewCacheEdits, nil))
	}
	if enablePromptCaching {
		addCacheReferencesBeforeLastCacheControl(out)
	}
	return out
}

func copyAPIMessages(messages []contracts.APIMessage) []contracts.APIMessage {
	out := make([]contracts.APIMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if msg.Content != nil {
			out[i].Content = append([]contracts.ContentBlock(nil), msg.Content...)
		}
	}
	return out
}

func insertCacheEditsIntoLastUserMessage(messages []contracts.APIMessage, edits []contracts.CacheEdit) {
	if len(edits) == 0 {
		return
	}
	block := contracts.ContentBlock{Type: contracts.ContentCacheEdits, Edits: edits}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		ensureContentBlock(&messages[i])
		insertAt := len(messages[i].Content)
		for j, content := range messages[i].Content {
			if content.Type == contracts.ContentToolResult {
				insertAt = j + 1
			}
		}
		messages[i].Content = append(messages[i].Content, contracts.ContentBlock{})
		copy(messages[i].Content[insertAt+1:], messages[i].Content[insertAt:])
		messages[i].Content[insertAt] = block
		return
	}
}

func addCacheReferencesBeforeLastCacheControl(messages []contracts.APIMessage) {
	lastCacheControlMessage := -1
	for i, msg := range messages {
		for _, block := range msg.Content {
			if block.CacheControl != nil {
				lastCacheControlMessage = i
			}
		}
	}
	if lastCacheControlMessage <= 0 {
		return
	}
	for i := 0; i < lastCacheControlMessage; i++ {
		if messages[i].Role != "user" {
			continue
		}
		for j, block := range messages[i].Content {
			if block.Type == contracts.ContentToolResult && block.ToolUseID != "" {
				messages[i].Content[j].CacheReference = block.ToolUseID
			}
		}
	}
}

func dedupeCacheEdits(edits []contracts.CacheEdit, seen map[string]struct{}) []contracts.CacheEdit {
	if seen == nil {
		seen = map[string]struct{}{}
	}
	out := make([]contracts.CacheEdit, 0, len(edits))
	for _, edit := range edits {
		if edit.CacheReference == "" {
			continue
		}
		if _, ok := seen[edit.CacheReference]; ok {
			continue
		}
		seen[edit.CacheReference] = struct{}{}
		if edit.Type == "" {
			edit.Type = "delete"
		}
		out = append(out, edit)
	}
	return out
}

func ensureContentBlock(message *contracts.APIMessage) {
	if len(message.Content) == 0 {
		message.Content = []contracts.ContentBlock{contracts.NewTextBlock("")}
	}
}

package conversation

import (
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	"ccgo/internal/tool"
)

func (r Runner) BuildRequest(history []contracts.Message, model string) (anthropic.Request, error) {
	return r.buildRequest(history, model, relevantMemoryRequestContext{})
}

type relevantMemoryRequestContext struct {
	Prefetch *memory.RelevantMemoryPrefetchResult
	SkipSync bool
}

func (r Runner) buildRequest(history []contracts.Message, model string, relevantMemory relevantMemoryRequestContext) (anthropic.Request, error) {
	history, err := r.applySessionMemoryRecall(history)
	if err != nil {
		return anthropic.Request{}, err
	}
	if relevantMemory.Prefetch != nil {
		history = appendRelevantMemoryPrefetch(history, *relevantMemory.Prefetch)
	} else if !relevantMemory.SkipSync {
		history, err = r.applyRelevantMemoryAttachments(history)
		if err != nil {
			return anthropic.Request{}, err
		}
	}
	history = memory.ExpandRelevantMemoryAttachments(history, time.Time{})
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  msgs.NormalizeForAPI(history),
	}
	if system := r.systemPromptWithOutputStyle(); system != "" {
		request.System = system
	}
	if r.Tools.Registry != nil {
		definitions, err := r.Tools.Registry.Definitions(toolPromptContext(r))
		if err != nil {
			return anthropic.Request{}, err
		}
		if len(definitions) > 0 {
			request.Tools = anthropic.ToolsFromContracts(definitions)
		}
	}
	return request, nil
}

func appendRelevantMemoryPrefetch(history []contracts.Message, result memory.RelevantMemoryPrefetchResult) []contracts.Message {
	if len(result.Memories) == 0 {
		return history
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, memory.RelevantMemoriesAttachmentMessage(result.Memories))
	return out
}

func (r Runner) applyRelevantMemoryAttachments(history []contracts.Message) ([]contracts.Message, error) {
	if r.RelevantMemoryDir == "" {
		return history, nil
	}
	plan, ok := memory.RelevantMemoryPrefetchPlanForMessages(history, 0)
	if !ok {
		return history, nil
	}
	selected, err := memory.FindRelevantMemorySelections(
		r.RelevantMemoryDir,
		plan.Input,
		memory.CollectRecentSuccessfulTools(history),
		plan.Surfaced.Paths,
		r.relevantMemoryLimit(),
	)
	if err != nil {
		return nil, err
	}
	memories := memory.ReadMemoriesForSurfacing(selected, memory.RelevantMemorySurfaceOptions{})
	if len(memories) == 0 {
		return history, nil
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, memory.RelevantMemoriesAttachmentMessage(memories))
	return out, nil
}

func (r Runner) applySessionMemoryRecall(history []contracts.Message) ([]contracts.Message, error) {
	if !r.EnableSessionMemoryRecall {
		return history, nil
	}
	root := r.SessionMemoryRecallRoot
	if root == "" {
		root = r.SessionMemoryRoot
	}
	if root == "" {
		root = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	if root == "" {
		return history, nil
	}
	query := lastUserText(history)
	if query == "" {
		return history, nil
	}
	matches, err := memory.RecallSessionSummaries(root, query, memory.RecallOptions{
		Limit:            r.sessionMemoryRecallLimit(),
		ExcludeSessionID: r.SessionID,
	})
	if err != nil {
		return nil, err
	}
	message := memory.RecallContextMessage(matches)
	if message.Type == "" {
		return history, nil
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, message)
	out = append(out, history...)
	return out, nil
}

func (r Runner) sessionMemoryRecallLimit() int {
	if r.SessionMemoryRecallLimit > 0 {
		return r.SessionMemoryRecallLimit
	}
	return 3
}

func (r Runner) relevantMemoryLimit() int {
	if r.RelevantMemoryLimit > 0 {
		return r.RelevantMemoryLimit
	}
	return memory.MaxRelevantMemoryAttachments
}

func lastUserText(history []contracts.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Type != contracts.MessageUser {
			continue
		}
		if history[i].Subtype == memory.RecallContextSubtype {
			continue
		}
		if text := msgs.TextContent(history[i]); text != "" {
			return text
		}
	}
	return ""
}

func toolPromptContext(r Runner) tool.PromptContext {
	return tool.PromptContext{
		Model:            r.model(),
		WorkingDirectory: r.WorkingDirectory,
	}
}

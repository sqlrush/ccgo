package conversation

import (
	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/tool"
)

func (r Runner) BuildRequest(history []contracts.Message, model string) (anthropic.Request, error) {
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  msgs.NormalizeForAPI(history),
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

func toolPromptContext(r Runner) tool.PromptContext {
	return tool.PromptContext{
		Model:            r.model(),
		WorkingDirectory: r.WorkingDirectory,
	}
}

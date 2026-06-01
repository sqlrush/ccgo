package messages

import (
	"strings"

	"ccgo/internal/contracts"
)

func UserText(text string) contracts.Message {
	return contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Content: []contracts.ContentBlock{contracts.NewTextBlock(text)},
	}
}

func AssistantText(text string, model string, usage *contracts.Usage) contracts.Message {
	return contracts.Message{
		Type:    contracts.MessageAssistant,
		UUID:    contracts.NewID(),
		Model:   model,
		Usage:   usage,
		Content: []contracts.ContentBlock{contracts.NewTextBlock(text)},
	}
}

func SystemText(subtype string, text string) contracts.Message {
	return contracts.Message{
		Type:    contracts.MessageSystem,
		UUID:    contracts.NewID(),
		Subtype: subtype,
		Content: []contracts.ContentBlock{contracts.NewTextBlock(text)},
	}
}

func ToolResult(toolUseID contracts.ID, content any, isError bool) contracts.Message {
	return contracts.Message{
		Type: contracts.MessageUser,
		UUID: contracts.NewID(),
		Content: []contracts.ContentBlock{{
			Type:      contracts.ContentToolResult,
			ToolUseID: string(toolUseID),
			Content:   content,
			IsError:   isError,
		}},
	}
}

func TextContent(message contracts.Message) string {
	var parts []string
	for _, block := range message.Content {
		if block.Type == contracts.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

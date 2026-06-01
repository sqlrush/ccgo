package compact

import (
	"encoding/json"
	"fmt"

	"ccgo/internal/contracts"
)

func EstimateTokens(messages []contracts.Message) int {
	chars := 0
	for _, message := range messages {
		chars += len(message.ID) + len(message.Model) + len(message.Subtype)
		for _, block := range message.Content {
			switch block.Type {
			case contracts.ContentText, contracts.ContentThinking:
				chars += len(block.Text)
			case contracts.ContentToolUse:
				chars += len(block.Name) + len(block.Input)
			case contracts.ContentToolResult:
				chars += len(block.ToolUseID) + len(fmt.Sprint(block.Content))
			default:
				data, _ := json.Marshal(block)
				chars += len(data)
			}
		}
	}
	if chars == 0 {
		return 0
	}
	tokens := chars / 4
	if chars%4 != 0 {
		tokens++
	}
	return tokens
}

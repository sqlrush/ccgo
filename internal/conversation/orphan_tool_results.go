package conversation

import (
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

// synthesizeOrphanedToolResults returns one synthetic is_error tool_result user
// message for every tool_use block in assistant that has no matching tool_result
// in produced. This prevents a 400 (orphaned tool_use) on the next request when a
// turn bails mid-tool-execution. Mirrors CC's yieldMissingToolResultBlocks.
func synthesizeOrphanedToolResults(sessionID contracts.ID, assistant contracts.Message, produced []contracts.Message, reason string) []contracts.Message {
	resolved := map[string]bool{}
	for _, m := range produced {
		for _, block := range m.Content {
			if block.Type == contracts.ContentToolResult && block.ToolUseID != "" {
				resolved[block.ToolUseID] = true
			}
		}
	}
	if reason == "" {
		reason = "Tool execution was interrupted."
	}
	var out []contracts.Message
	for _, block := range assistant.Content {
		if block.Type != contracts.ContentToolUse || block.ID == "" {
			continue
		}
		if resolved[block.ID] {
			continue
		}
		msg := msgs.ToolResult(contracts.ID(block.ID), reason, true)
		if sessionID != "" {
			msg.SessionID = sessionID
		}
		out = append(out, msg)
	}
	return out
}

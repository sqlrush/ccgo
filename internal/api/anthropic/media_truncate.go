package anthropic

import "ccgo/internal/contracts"

// StripExcessMediaItems removes the oldest media blocks (images + documents)
// from messages when the total count exceeds limit. The most recent media is
// preserved; oldest entries are stripped first (left-to-right, outermost first,
// then nested inside tool_result content).
//
// This mirrors CC: services/api/claude.ts:956-1015 (stripExcessMediaItems).
// The public constant APIMaxMediaPerRequest (100) sets the production limit.
func StripExcessMediaItems(messages []contracts.APIMessage, limit int) []contracts.APIMessage {
	// Count total media across all messages.
	total := countMedia(messages)
	toRemove := total - limit
	if toRemove <= 0 {
		return messages
	}

	out := make([]contracts.APIMessage, len(messages))
	for i, msg := range messages {
		if toRemove <= 0 {
			out[i] = msg
			continue
		}
		stripped, removed := stripMediaFromContent(msg.Content, &toRemove)
		if removed {
			out[i] = contracts.APIMessage{Role: msg.Role, Content: stripped}
		} else {
			out[i] = msg
		}
	}
	return out
}

// countMedia counts media blocks (images and PDFs) across all messages,
// including those nested inside tool_result content.
func countMedia(messages []contracts.APIMessage) int {
	count := 0
	for _, msg := range messages {
		count += countMediaInContent(msg.Content)
	}
	return count
}

func countMediaInContent(content []contracts.ContentBlock) int {
	count := 0
	for _, block := range content {
		if isMediaBlock(block) {
			count++
		}
		if block.Type == contracts.ContentToolResult {
			if nested, ok := block.Content.([]contracts.ContentBlock); ok {
				for _, nb := range nested {
					if isMediaBlock(nb) {
						count++
					}
				}
			}
		}
	}
	return count
}

func isMediaBlock(block contracts.ContentBlock) bool {
	return block.Type == contracts.ContentImage
}

// stripMediaFromContent strips the oldest media blocks from content, updating
// *toRemove. Returns the new content slice and whether any change was made.
func stripMediaFromContent(content []contracts.ContentBlock, toRemove *int) ([]contracts.ContentBlock, bool) {
	if *toRemove <= 0 {
		return content, false
	}

	// First pass: strip nested media inside tool_result blocks.
	out := make([]contracts.ContentBlock, len(content))
	changed := false
	for i, block := range content {
		out[i] = block
		if *toRemove <= 0 {
			continue
		}
		if block.Type != contracts.ContentToolResult {
			continue
		}
		nested, ok := block.Content.([]contracts.ContentBlock)
		if !ok {
			continue
		}
		strippedNested, removedNested := stripMediaSlice(nested, toRemove)
		if removedNested {
			nb := block
			nb.Content = strippedNested
			out[i] = nb
			changed = true
		}
	}

	// Second pass: strip top-level media blocks.
	if *toRemove > 0 {
		filtered := make([]contracts.ContentBlock, 0, len(out))
		for _, block := range out {
			if *toRemove > 0 && isMediaBlock(block) {
				*toRemove--
				changed = true
				continue
			}
			filtered = append(filtered, block)
		}
		return filtered, changed
	}

	return out, changed
}

// stripMediaSlice strips the oldest media blocks from a slice.
func stripMediaSlice(blocks []contracts.ContentBlock, toRemove *int) ([]contracts.ContentBlock, bool) {
	if *toRemove <= 0 {
		return blocks, false
	}
	out := make([]contracts.ContentBlock, 0, len(blocks))
	changed := false
	for _, block := range blocks {
		if *toRemove > 0 && isMediaBlock(block) {
			*toRemove--
			changed = true
			continue
		}
		out = append(out, block)
	}
	return out, changed
}

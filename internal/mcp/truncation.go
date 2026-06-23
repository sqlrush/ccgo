package mcp

import (
	"strings"
	"unicode/utf8"
)

// MaxMCPDescriptionLength is the limit CC applies to MCP tool descriptions
// and server instructions before sending them to the model.
// CC ref: src/services/mcp/client.ts:218 (MAX_MCP_DESCRIPTION_LENGTH = 2048).
const MaxMCPDescriptionLength = 2048

// TruncateMCPText truncates text that exceeds MaxMCPDescriptionLength,
// appending "… [truncated]" to signal the cut.  Short or empty text is
// returned unchanged (new allocation-free path).
//
// CC ref: src/services/mcp/client.ts:1164-1166 (instructions) and
//
//	src/services/mcp/client.ts:1792 (tool description in prompt()).
func TruncateMCPText(text string) string {
	if len(text) <= MaxMCPDescriptionLength {
		return text
	}
	// Trim at a clean rune boundary: walk backwards from the cut point until
	// the remaining prefix is valid UTF-8 (no truncated multi-byte sequence).
	cut := text[:MaxMCPDescriptionLength]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, " \t") + "… [truncated]"
}

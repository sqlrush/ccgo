package repl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/messages"
)

// exportHandler returns a CommandHandler for /export that writes the
// conversation history to a plain-text file inside cwd.
//
// Usage: /export [filename]
//   - With arg: writes <cwd>/<filename>.txt (strips any existing .txt suffix
//     then appends it, so "/export foo" and "/export foo.txt" both write foo.txt).
//   - Without arg: writes <cwd>/claude-export-<unix>.txt.
//
// Filename validation: rejects anything with ".." path components and absolute
// paths — only bare filenames relative to cwd are accepted.
func exportHandler(cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		name := strings.TrimSpace(cc.Args)

		// Derive the target filename.
		var filename string
		if name == "" {
			filename = fmt.Sprintf("claude-export-%d.txt", time.Now().Unix())
		} else {
			// Reject absolute paths and path-traversal components.
			if filepath.IsAbs(name) || strings.Contains(name, "..") {
				return CommandOutcome{
					Handled: true,
					Status:  fmt.Sprintf("Invalid filename %q: must be a plain filename with no path separators or '..' components.", name),
				}, nil
			}
			// Strip any directory component — we only allow bare names.
			base := filepath.Base(name)
			if base == "." || base == "" {
				return CommandOutcome{
					Handled: true,
					Status:  fmt.Sprintf("Invalid filename %q.", name),
				}, nil
			}
			// Normalise: strip existing .txt suffix then re-add it.
			stem := strings.TrimSuffix(base, ".txt")
			filename = stem + ".txt"
		}

		dest := filepath.Join(cwd, filename)

		// Render the conversation.
		body := renderTranscript(cc.History)

		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
			return CommandOutcome{}, fmt.Errorf("export: write %s: %w", dest, err)
		}

		count := len(cc.History)
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Exported %d message(s) to %s", count, dest),
		}, nil
	}
}

// renderTranscript converts a slice of messages into a plain-text transcript
// using the format:
//
//	User: <text>
//
//	Assistant: <text>
//
// Only user and assistant messages with non-empty text content are included.
// Other message types (meta, attachment, tool results) are skipped.
func renderTranscript(history []contracts.Message) string {
	var sb strings.Builder
	first := true
	for _, msg := range history {
		text := messages.TextContent(msg)
		if text == "" {
			continue
		}
		var label string
		switch msg.Type {
		case contracts.MessageUser:
			label = "User"
		case contracts.MessageAssistant:
			label = "Assistant"
		default:
			continue
		}
		if !first {
			sb.WriteString("\n\n")
		}
		sb.WriteString(label)
		sb.WriteString(": ")
		sb.WriteString(text)
		first = false
	}
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	return sb.String()
}

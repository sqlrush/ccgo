package compact

import (
	"fmt"
	"sort"

	"ccgo/internal/contracts"
	"ccgo/internal/messages"
)

const (
	defaultPostCompactMaxFiles    = 5
	defaultPostCompactTokenBudget = 50000
	defaultPostCompactPerFile     = 5000
	defaultPostCompactTokensPerChar = 0.25 // ~4 chars per token
)

// ReadFileEntry is a recent-read file snapshot supplied by the caller.
// ccgo has no readFileState yet; this struct is the injection point.
type ReadFileEntry struct {
	Path      string
	Content   string
	Timestamp int64
}

// PostCompactOptions configures post-compact file re-attachment behaviour.
// Zero values fall back to CC defaults (MaxFiles=5, TokenBudget=50000,
// MaxTokensPerFile=5000, ApproxTokensPerChar=0.25).
type PostCompactOptions struct {
	MaxFiles            int
	PreservedPaths      map[string]bool
	ApproxTokensPerChar float64
	TokenBudget         int
	MaxTokensPerFile    int
}

// BuildPostCompactAttachments re-attaches the most recently read files after a
// compaction, matching CC's compact.ts createPostCompactFileAttachments
// (lines 1415-1464):
//
//   - Sort entries by Timestamp desc (newest first).
//   - Skip paths present in PreservedPaths (already in the summary).
//   - Cap at MaxFiles (default 5).
//   - Skip any individual file whose token estimate exceeds MaxTokensPerFile.
//   - Stop accumulating when the running total would exceed TokenBudget.
//
// The function is pure and deterministic; no I/O is performed.
func BuildPostCompactAttachments(files []ReadFileEntry, opts PostCompactOptions) []contracts.Message {
	// Apply defaults.
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultPostCompactMaxFiles
	}
	if opts.TokenBudget <= 0 {
		opts.TokenBudget = defaultPostCompactTokenBudget
	}
	if opts.MaxTokensPerFile <= 0 {
		opts.MaxTokensPerFile = defaultPostCompactPerFile
	}
	if opts.ApproxTokensPerChar <= 0 {
		opts.ApproxTokensPerChar = defaultPostCompactTokensPerChar
	}

	if len(files) == 0 {
		return nil
	}

	// Immutable copy sorted newest-first (do not mutate caller's slice).
	sorted := make([]ReadFileEntry, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp > sorted[j].Timestamp
	})

	msgs := make([]contracts.Message, 0, opts.MaxFiles)
	usedTokens := 0

	for _, f := range sorted {
		if len(msgs) >= opts.MaxFiles {
			break
		}
		if opts.PreservedPaths[f.Path] {
			continue
		}
		tokens := estimateFileTokens(f.Content, opts.ApproxTokensPerChar)
		if tokens > opts.MaxTokensPerFile {
			continue
		}
		if usedTokens+tokens > opts.TokenBudget {
			// Stop: remaining files (older) are also likely too large.
			break
		}
		usedTokens += tokens
		body := fmt.Sprintf("Re-reading %s after compaction:\n%s", f.Path, f.Content)
		msgs = append(msgs, messages.UserText(body))
	}

	return msgs
}

// estimateFileTokens approximates the token count for a raw file content string.
func estimateFileTokens(content string, tokensPerChar float64) int {
	n := int(float64(len(content)) * tokensPerChar)
	if len(content) > 0 && n == 0 {
		n = 1
	}
	return n
}

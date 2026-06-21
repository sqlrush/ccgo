package webtools

import "context"

// MetadataSecondaryModelClientKey injects the small/fast model client WebFetch
// uses to summarize rendered content against the user's prompt. Absent → no
// summarization (raw rendered text is returned, preserving existing behavior).
const MetadataSecondaryModelClientKey = "ccgo.tools.web.secondary_model"

// SecondaryModelClient runs a single non-streaming completion. Mirrors CC's
// queryHaiku (src/tools/WebFetchTool/utils.ts:503).
type SecondaryModelClient interface {
	Summarize(ctx context.Context, req SummarizeRequest) (string, error)
}

// SummarizeRequest carries everything the secondary model needs to produce
// a focused answer from a fetched page.
type SummarizeRequest struct {
	Model        string
	SystemPrompt string
	Content      string
	Prompt       string
}

// secondaryModelClient extracts the injected client from metadata.
// Returns nil when no client was injected.
func secondaryModelClient(metadata map[string]any) SecondaryModelClient {
	if metadata == nil {
		return nil
	}
	client, _ := metadata[MetadataSecondaryModelClientKey].(SecondaryModelClient)
	return client
}

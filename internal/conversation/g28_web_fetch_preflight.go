package conversation

// injectWebFetchSkipPreflight injects the skipWebFetchPreflight value from
// settings into the tool metadata map.
//
// CFG-38: settings.skipWebFetchPreflight → MetadataWebFetchSkipPreflightKey
// The WebFetch tool reads this key via webFetchSkipPreflight(ctx.Metadata) and
// bypasses the domain block-list pre-flight HEAD check when true.
// CC ref: utils/settings/types.ts:255+; web_fetch.go:webFetchSkipPreflight.
func injectWebFetchSkipPreflight(metadata map[string]any, skip *bool) {
	if skip == nil || !*skip {
		return
	}
	// Use the same key the web fetch tool reads from metadata.
	// Defined in internal/tools/web/web_fetch.go:MetadataWebFetchSkipPreflightKey.
	metadata["ccgo.tools.web.fetch.skip_preflight"] = true
}

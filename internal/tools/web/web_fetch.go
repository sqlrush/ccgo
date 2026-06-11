package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	MetadataWebFetchSkipPreflightKey = "ccgo.tools.web.fetch.skip_preflight"

	defaultWebFetchTimeoutMillis = 30_000
	maxWebFetchTimeoutMillis     = 120_000
	defaultWebFetchMaxBytes      = 200_000
	maxWebFetchMaxBytes          = 1_000_000
)

type webFetchInput struct {
	URL         string `json:"url"`
	Prompt      string `json:"prompt,omitempty"`
	Timeout     *int   `json:"timeout,omitempty"`
	MaxBytes    *int   `json:"max_bytes,omitempty"`
	MaxBytesAlt *int   `json:"maxBytes,omitempty"`
}

func NewWebFetchTool() tool.Tool {
	var self tool.Tool
	self = tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "WebFetch",
			Description:        "Fetch content from a URL.",
			SearchHint:         "fetch web page",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"url"},
				"properties": map[string]any{
					"url":       map[string]any{"type": "string"},
					"prompt":    map[string]any{"type": "string"},
					"timeout":   map[string]any{"type": "integer"},
					"max_bytes": map[string]any{"type": "integer"},
					"maxBytes":  map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Fetches a web URL and returns text content. Provide url and optionally prompt, timeout in milliseconds, and max_bytes. HTML responses are rendered to readable text, and prompts produce a focused excerpt when matching text is found. Browser rendering and model summarization are not implemented yet.", nil
		},
		ValidateFunc: validateWebFetch,
		PermissionFunc: func(ctx tool.Context, raw json.RawMessage) (contracts.PermissionDecision, error) {
			return checkWebFetchPermissions(self, ctx, raw)
		},
		CallFunc:        callWebFetch,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
	return self
}

func validateWebFetch(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeWebFetch(raw)
	if err != nil {
		return err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("url must include a hostname")
	}
	if input.Timeout != nil {
		if *input.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		if *input.Timeout > maxWebFetchTimeoutMillis {
			return fmt.Errorf("timeout must be at most %d milliseconds", maxWebFetchTimeoutMillis)
		}
	}
	maxBytes := webFetchMaxBytes(input)
	if maxBytes <= 0 {
		return fmt.Errorf("max_bytes must be positive")
	}
	if maxBytes > maxWebFetchMaxBytes {
		return fmt.Errorf("max_bytes must be at most %d", maxWebFetchMaxBytes)
	}
	return nil
}

func checkWebFetchPermissions(self tool.Tool, ctx tool.Context, raw json.RawMessage) (contracts.PermissionDecision, error) {
	if ctx.Permissions == nil {
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "no permission engine configured"}, nil
	}
	input, err := decodeWebFetch(raw)
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	domainInput, err := json.Marshal(map[string]any{
		"url":    "domain:" + strings.ToLower(parsed.Hostname()),
		"prompt": input.Prompt,
	})
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	return ctx.Permissions.DecideTool(self, domainInput, ctx)
}

func callWebFetch(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWebFetch(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	timeout := webFetchTimeout(input)
	maxBytes := webFetchMaxBytes(input)
	result, err := fetchURL(ctx.Context, parsed.String(), timeout, maxBytes, webFetchSkipPreflight(ctx.Metadata))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	result = prepareWebFetchResult(result, input.Prompt)
	content := formatWebFetchContent(input, result)
	return contracts.ToolResult{
		Content: content,
		IsError: result.StatusCode < 200 || result.StatusCode >= 300 || result.Binary,
		StructuredContent: map[string]any{
			"type":           "web_fetch",
			"url":            parsed.String(),
			"domain":         strings.ToLower(parsed.Hostname()),
			"prompt":         input.Prompt,
			"status_code":    result.StatusCode,
			"content_type":   result.ContentType,
			"body":           result.Body,
			"rendered":       result.Rendered,
			"rendered_body":  result.RenderedBody,
			"prompt_terms":   result.PromptTerms,
			"prompt_phrases": result.PromptPhrases,
			"prompt_excerpt": result.PromptExcerpt,
			"bytes":          result.Bytes,
			"truncated":      result.Truncated,
			"binary":         result.Binary,
			"duration_ms":    result.DurationMS,
			"preflight":      structuredWebFetchPreflight(result.Preflight),
		},
	}, nil
}

type fetchResult struct {
	StatusCode    int
	ContentType   string
	Body          string
	RenderedBody  string
	Rendered      bool
	PromptTerms   []string
	PromptPhrases []string
	PromptExcerpt string
	Bytes         int
	Truncated     bool
	Binary        bool
	DurationMS    int64
	Preflight     webFetchPreflight
}

type webFetchPreflight struct {
	Attempted     bool
	Skipped       bool
	StatusCode    int
	ContentType   string
	ContentLength int64
	SkippedGET    bool
	Error         string
}

func fetchURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes int, skipPreflight bool) (fetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	preflight := webFetchPreflight{Skipped: skipPreflight}
	if !skipPreflight {
		preflight = runWebFetchPreflight(fetchCtx, rawURL)
		if preflight.SkippedGET {
			return fetchResult{
				StatusCode:  preflight.StatusCode,
				ContentType: preflight.ContentType,
				Binary:      true,
				DurationMS:  time.Since(start).Milliseconds(),
				Preflight:   preflight,
			}, nil
		}
	}
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchResult{}, err
	}
	req.Header.Set("User-Agent", "ccgo-webfetch/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fetchResult{}, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, int64(maxBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fetchResult{}, err
	}
	truncated := len(data) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	binary := isBinaryWebContent(contentType, data)
	body := ""
	if !binary {
		body = string(data)
	}
	return fetchResult{
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Body:        body,
		Bytes:       len(data),
		Truncated:   truncated,
		Binary:      binary,
		DurationMS:  time.Since(start).Milliseconds(),
		Preflight:   preflight,
	}, nil
}

func runWebFetchPreflight(ctx context.Context, rawURL string) webFetchPreflight {
	preflight := webFetchPreflight{Attempted: true, ContentLength: -1}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		preflight.Error = err.Error()
		return preflight
	}
	req.Header.Set("User-Agent", "ccgo-webfetch/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		preflight.Error = err.Error()
		return preflight
	}
	defer resp.Body.Close()
	preflight.StatusCode = resp.StatusCode
	preflight.ContentType = resp.Header.Get("Content-Type")
	preflight.ContentLength = resp.ContentLength
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		return preflight
	}
	preflight.SkippedGET = isBinaryWebContentType(preflight.ContentType)
	return preflight
}

func prepareWebFetchResult(result fetchResult, prompt string) fetchResult {
	if result.Binary || result.Body == "" {
		return result
	}
	rendered, ok := renderWebFetchBody(result.ContentType, result.Body)
	if strings.TrimSpace(rendered) == "" {
		rendered = result.Body
	}
	result.RenderedBody = rendered
	result.Rendered = ok
	result.PromptTerms = webFetchPromptTerms(prompt)
	result.PromptPhrases = webFetchPromptPhrases(prompt)
	if prompt != "" {
		result.PromptExcerpt = promptFocusedWebFetchExcerpt(rendered, result.PromptTerms, result.PromptPhrases)
	}
	return result
}

func formatWebFetchContent(input webFetchInput, result fetchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fetched %s with status %d", input.URL, result.StatusCode)
	if result.ContentType != "" {
		fmt.Fprintf(&b, " (%s)", result.ContentType)
	}
	if result.Truncated {
		fmt.Fprintf(&b, "; truncated to %d bytes", result.Bytes)
	}
	b.WriteString(".")
	if input.Prompt != "" {
		b.WriteString("\nPrompt: ")
		b.WriteString(input.Prompt)
	}
	if result.Binary {
		if result.Preflight.SkippedGET {
			b.WriteString("\nPreflight identified binary content; GET was skipped.")
		}
		b.WriteString("\nResponse body is binary and was not included.")
		return b.String()
	}
	if result.Body == "" {
		b.WriteString("\nResponse body is empty.")
		return b.String()
	}
	body := result.RenderedBody
	if body == "" {
		body = result.Body
	}
	if input.Prompt != "" && result.PromptExcerpt != "" && result.PromptExcerpt != body {
		b.WriteString("\n\nRelevant excerpt:\n")
		b.WriteString(result.PromptExcerpt)
		b.WriteString("\n\nRendered body:\n")
		b.WriteString(body)
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\n\n")
	b.WriteString(body)
	return strings.TrimRight(b.String(), "\n")
}

func renderWebFetchBody(contentType string, body string) (string, bool) {
	if !isHTMLWebFetchContent(contentType, body) {
		return body, false
	}
	stripped := removeHTMLWebFetchBlocks(body, "script", "style", "noscript", "template", "svg", "canvas")
	rendered := stripHTMLWebFetchTags(stripped)
	rendered = html.UnescapeString(rendered)
	return normalizeWebFetchText(rendered), true
}

func isHTMLWebFetchContent(contentType string, body string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		mediaType = strings.ToLower(mediaType)
		return mediaType == "text/html" || mediaType == "application/xhtml+xml"
	}
	prefix := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<html")
}

func removeHTMLWebFetchBlocks(body string, names ...string) string {
	out := body
	for _, name := range names {
		for {
			lower := strings.ToLower(out)
			start := findHTMLWebFetchBlockStart(lower, name)
			if start < 0 {
				break
			}
			openEndRel := strings.IndexByte(out[start:], '>')
			if openEndRel < 0 {
				out = out[:start]
				break
			}
			contentStart := start + openEndRel + 1
			closeStartRel := strings.Index(strings.ToLower(out[contentStart:]), "</"+name)
			if closeStartRel < 0 {
				out = out[:start]
				break
			}
			closeStart := contentStart + closeStartRel
			closeEndRel := strings.IndexByte(out[closeStart:], '>')
			if closeEndRel < 0 {
				out = out[:start]
				break
			}
			closeEnd := closeStart + closeEndRel + 1
			out = out[:start] + "\n" + out[closeEnd:]
		}
	}
	return out
}

func findHTMLWebFetchBlockStart(lower string, name string) int {
	search := "<" + name
	offset := 0
	for {
		idx := strings.Index(lower[offset:], search)
		if idx < 0 {
			return -1
		}
		pos := offset + idx
		after := pos + len(search)
		if after >= len(lower) {
			return pos
		}
		next := lower[after]
		if next == '>' || next == '/' || unicode.IsSpace(rune(next)) {
			return pos
		}
		offset = after
	}
}

func stripHTMLWebFetchTags(body string) string {
	var b strings.Builder
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], "<!--") {
			if end := strings.Index(body[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			break
		}
		if body[i] != '<' {
			b.WriteByte(body[i])
			i++
			continue
		}
		end := strings.IndexByte(body[i:], '>')
		if end < 0 {
			b.WriteByte(body[i])
			i++
			continue
		}
		tag := htmlWebFetchTagName(body[i+1 : i+end])
		if tag == "br" || isBlockHTMLWebFetchTag(tag) {
			b.WriteByte('\n')
		}
		i += end + 1
	}
	return b.String()
}

func htmlWebFetchTagName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimLeft(raw, "/!?")
	if raw == "" {
		return ""
	}
	for idx, r := range raw {
		if unicode.IsSpace(r) || r == '/' || r == '>' {
			return strings.ToLower(raw[:idx])
		}
	}
	return strings.ToLower(raw)
}

func isBlockHTMLWebFetchTag(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "body", "caption", "dd", "details", "dialog", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	default:
		return false
	}
}

func normalizeWebFetchText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	lastBlank := true
	for _, line := range lines {
		normalized := strings.Join(strings.Fields(line), " ")
		if normalized == "" {
			if !lastBlank {
				out = append(out, "")
				lastBlank = true
			}
			continue
		}
		out = append(out, normalized)
		lastBlank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func webFetchPromptTerms(prompt string) []string {
	words := webFetchPromptWords(prompt)
	terms := make([]string, 0, len(words))
	seen := map[string]bool{}
	for _, word := range words {
		if seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	return terms
}

func webFetchPromptWords(prompt string) []string {
	words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.TrimSpace(word)
		if utf8.RuneCountInString(word) < 3 || isWebFetchStopWord(word) {
			continue
		}
		out = append(out, word)
	}
	return out
}

func webFetchPromptPhrases(prompt string) []string {
	words := webFetchPromptWords(prompt)
	if len(words) < 2 {
		return nil
	}
	seen := map[string]bool{}
	var phrases []string
	maxLen := 4
	if len(words) < maxLen {
		maxLen = len(words)
	}
	for n := maxLen; n >= 2; n-- {
		for i := 0; i+n <= len(words); i++ {
			phrase := strings.Join(words[i:i+n], " ")
			if seen[phrase] {
				continue
			}
			seen[phrase] = true
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

func isWebFetchStopWord(word string) bool {
	switch word {
	case "the", "and", "for", "with", "from", "this", "that", "you", "your", "are", "was", "were", "what", "when", "where", "which", "who", "why", "how", "summarize", "summary", "about", "please":
		return true
	default:
		return false
	}
}

type scoredWebFetchPassage struct {
	Index int
	Text  string
	Score int
}

func promptFocusedWebFetchExcerpt(text string, terms []string, phrases []string) string {
	if (len(terms) == 0 && len(phrases) == 0) || strings.TrimSpace(text) == "" {
		return ""
	}
	passages := splitWebFetchPassages(text)
	scored := make([]scoredWebFetchPassage, 0, len(passages))
	for idx, passage := range passages {
		score := scoreWebFetchPassage(passage, terms, phrases)
		if score > 0 {
			scored = append(scored, scoredWebFetchPassage{Index: idx, Text: passage, Score: score})
		}
	}
	if len(scored) == 0 {
		return ""
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Index < scored[j].Index
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > 3 {
		scored = scored[:3]
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Index < scored[j].Index
	})
	parts := make([]string, 0, len(scored))
	for _, passage := range scored {
		parts = append(parts, passage.Text)
	}
	return truncateWebFetchRunes(strings.Join(parts, "\n\n"), 1600)
}

func splitWebFetchPassages(text string) []string {
	raw := strings.Split(text, "\n")
	passages := make([]string, 0, len(raw))
	for _, passage := range raw {
		passage = strings.TrimSpace(passage)
		if passage == "" {
			continue
		}
		if utf8.RuneCountInString(passage) <= 500 {
			passages = append(passages, passage)
			continue
		}
		passages = append(passages, splitLongWebFetchPassage(passage)...)
	}
	return passages
}

func splitLongWebFetchPassage(passage string) []string {
	sentences := strings.FieldsFunc(passage, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '。' || r == '！' || r == '？'
	})
	out := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence != "" {
			out = append(out, sentence)
		}
	}
	if len(out) == 0 {
		return []string{passage}
	}
	return out
}

func scoreWebFetchPassage(passage string, terms []string, phrases []string) int {
	lower := normalizedWebFetchSearchText(passage)
	termCounts := webFetchSearchWordCounts(lower)
	score := 0
	for _, phrase := range phrases {
		count := countWebFetchPhraseOccurrences(lower, phrase)
		if count > 0 {
			score += count * (6 + 2*len(strings.Fields(phrase)))
		}
	}
	for _, term := range terms {
		count := termCounts[term]
		if count > 0 {
			score += 2 + count
		}
	}
	return score
}

func normalizedWebFetchSearchText(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, " ")
}

func webFetchSearchWordCounts(normalized string) map[string]int {
	counts := map[string]int{}
	for _, word := range strings.Fields(normalized) {
		counts[word]++
	}
	return counts
}

func countWebFetchPhraseOccurrences(normalized string, phrase string) int {
	phrase = strings.TrimSpace(strings.ToLower(phrase))
	if normalized == "" || phrase == "" {
		return 0
	}
	haystack := " " + normalized + " "
	needle := " " + phrase + " "
	count := 0
	for {
		idx := strings.Index(haystack, needle)
		if idx < 0 {
			return count
		}
		count++
		haystack = haystack[idx+len(needle)-1:]
	}
}

func truncateWebFetchRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n[truncated]"
}

func decodeWebFetch(raw json.RawMessage) (webFetchInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return webFetchInput{}, err
	}
	for key := range obj {
		switch key {
		case "url", "prompt", "timeout", "max_bytes", "maxBytes":
		default:
			return webFetchInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input webFetchInput
	data, err := json.Marshal(obj)
	if err != nil {
		return webFetchInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return webFetchInput{}, err
	}
	return input, nil
}

func parseFetchURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	return parsed, nil
}

func webFetchTimeout(input webFetchInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultWebFetchTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func webFetchMaxBytes(input webFetchInput) int {
	if input.MaxBytes != nil {
		return *input.MaxBytes
	}
	if input.MaxBytesAlt != nil {
		return *input.MaxBytesAlt
	}
	return defaultWebFetchMaxBytes
}

func webFetchSkipPreflight(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if metadataBool(metadata[MetadataWebFetchSkipPreflightKey]) {
		return true
	}
	return metadataBool(metadata["skipWebFetchPreflight"])
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func structuredWebFetchPreflight(preflight webFetchPreflight) map[string]any {
	return map[string]any{
		"attempted":      preflight.Attempted,
		"skipped":        preflight.Skipped,
		"status_code":    preflight.StatusCode,
		"content_type":   preflight.ContentType,
		"content_length": preflight.ContentLength,
		"skipped_get":    preflight.SkippedGET,
		"error":          preflight.Error,
	}
}

func isBinaryWebContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return false
	}
	return !isTextualWebMediaType(strings.ToLower(mediaType))
}

func isBinaryWebContent(contentType string, data []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		mediaType = strings.ToLower(mediaType)
		return !isTextualWebMediaType(mediaType)
	}
	if !utf8.Valid(data) {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func isTextualWebMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") ||
		strings.Contains(mediaType, "json") ||
		strings.Contains(mediaType, "xml") ||
		strings.Contains(mediaType, "javascript") ||
		strings.Contains(mediaType, "html")
}

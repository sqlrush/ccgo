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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
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

var allowedWebFetchInputKeys = map[string]struct{}{
	"url":       {},
	"prompt":    {},
	"timeout":   {},
	"max_bytes": {},
	"maxBytes":  {},
}

var webFetchSemanticNumberKeys = map[string]struct{}{
	"timeout":   {},
	"max_bytes": {},
	"maxBytes":  {},
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
				"required": []any{"url", "prompt"},
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
			return "Fetches a web URL and returns text content. Provide url, prompt, and optionally timeout in milliseconds and max_bytes. HTML responses are rendered to readable text, and prompts produce a focused excerpt when matching text is found. Browser rendering and model summarization are not implemented yet.", nil
		},
		NormalizeFunc: normalizeWebFetchRawInput,
		ValidateFunc:  validateWebFetch,
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
		IsError: !result.RedirectDetected && (result.StatusCode < 200 || result.StatusCode >= 300 || result.Binary),
		StructuredContent: map[string]any{
			"type":              "web_fetch",
			"url":               parsed.String(),
			"final_url":         result.FinalURL,
			"domain":            strings.ToLower(parsed.Hostname()),
			"prompt":            input.Prompt,
			"status_code":       result.StatusCode,
			"content_type":      result.ContentType,
			"charset":           result.Charset,
			"body":              result.Body,
			"rendered":          result.Rendered,
			"rendered_body":     result.RenderedBody,
			"prompt_terms":      result.PromptTerms,
			"prompt_phrases":    result.PromptPhrases,
			"prompt_excerpt":    result.PromptExcerpt,
			"bytes":             result.Bytes,
			"truncated":         result.Truncated,
			"binary":            result.Binary,
			"duration_ms":       result.DurationMS,
			"redirect_detected": result.RedirectDetected,
			"redirect_url":      result.RedirectURL,
			"preflight":         structuredWebFetchPreflight(result.Preflight),
		},
	}, nil
}

type fetchResult struct {
	StatusCode    int
	FinalURL      string
	ContentType   string
	Charset       string
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

	RedirectDetected bool
	RedirectURL      string
}

type webFetchPreflight struct {
	Attempted          bool
	Skipped            bool
	StatusCode         int
	ContentType        string
	ContentDisposition string
	ContentLength      int64
	SkippedGET         bool
	Error              string
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
				FinalURL:    rawURL,
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
	resp, err := webFetchHTTPClient().Do(req)
	if err != nil {
		return fetchResult{}, err
	}
	defer resp.Body.Close()
	if redirectURL, ok := webFetchCrossHostRedirectURL(rawURL, resp); ok {
		body := webFetchRedirectMessage(rawURL, redirectURL, resp.Status)
		return fetchResult{
			StatusCode:       resp.StatusCode,
			FinalURL:         rawURL,
			ContentType:      "text/plain; charset=utf-8",
			Charset:          "utf-8",
			Body:             body,
			Bytes:            len([]byte(body)),
			Binary:           false,
			DurationMS:       time.Since(start).Milliseconds(),
			Preflight:        preflight,
			RedirectDetected: true,
			RedirectURL:      redirectURL,
		}, nil
	}
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
	charset := ""
	if !binary {
		body, charset = decodeWebFetchText(contentType, data)
	}
	return fetchResult{
		StatusCode:  resp.StatusCode,
		FinalURL:    resp.Request.URL.String(),
		ContentType: contentType,
		Charset:     charset,
		Body:        body,
		Bytes:       len(data),
		Truncated:   truncated,
		Binary:      binary,
		DurationMS:  time.Since(start).Milliseconds(),
		Preflight:   preflight,
	}, nil
}

func webFetchHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if len(via) == 0 {
				return nil
			}
			if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func webFetchCrossHostRedirectURL(rawURL string, resp *http.Response) (string, bool) {
	if resp == nil || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", false
	}
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" || resp.Request == nil || resp.Request.URL == nil {
		return "", false
	}
	redirectRef, err := url.Parse(location)
	if err != nil {
		return "", false
	}
	redirectURL := resp.Request.URL.ResolveReference(redirectRef)
	originalURL, err := url.Parse(rawURL)
	if err != nil || originalURL.Host == "" || redirectURL.Host == "" {
		return "", false
	}
	if strings.EqualFold(redirectURL.Host, originalURL.Host) {
		return "", false
	}
	return redirectURL.String(), true
}

func webFetchRedirectMessage(originalURL string, redirectURL string, status string) string {
	if strings.TrimSpace(status) == "" {
		status = "redirect"
	}
	return fmt.Sprintf("REDIRECT DETECTED: The URL redirects to a different host. You should make a new WebFetch request to the redirect URL to fetch the content.\n\nOriginal URL: %s\nRedirect URL: %s\nStatus: %s", originalURL, redirectURL, status)
}

func runWebFetchPreflight(ctx context.Context, rawURL string) webFetchPreflight {
	preflight := webFetchPreflight{Attempted: true, ContentLength: -1}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		preflight.Error = err.Error()
		return preflight
	}
	req.Header.Set("User-Agent", "ccgo-webfetch/0.1")
	resp, err := webFetchHTTPClient().Do(req)
	if err != nil {
		preflight.Error = err.Error()
		return preflight
	}
	defer resp.Body.Close()
	preflight.StatusCode = resp.StatusCode
	preflight.ContentType = resp.Header.Get("Content-Type")
	preflight.ContentDisposition = resp.Header.Get("Content-Disposition")
	preflight.ContentLength = resp.ContentLength
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		return preflight
	}
	preflight.SkippedGET = isBinaryWebContentType(preflight.ContentType) || isBinaryWebAttachment(preflight.ContentDisposition)
	return preflight
}

func prepareWebFetchResult(result fetchResult, prompt string) fetchResult {
	if result.RedirectDetected || result.Binary || result.Body == "" {
		return result
	}
	rendered, ok := renderWebFetchBody(result.ContentType, result.Body, result.FinalURL)
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
	if result.RedirectDetected {
		return result.Body
	}
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

func renderWebFetchBody(contentType string, body string, baseURL string) (string, bool) {
	if !isHTMLWebFetchContent(contentType, body) {
		return body, false
	}
	stripped := removeHTMLWebFetchBlocks(body, "script", "style", "noscript", "template", "svg", "canvas")
	resolvedBaseURL := webFetchHTMLBaseURL(stripped, baseURL)
	rendered := stripHTMLWebFetchTags(stripped, resolvedBaseURL)
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

func stripHTMLWebFetchTags(body string, baseURL string) string {
	var b strings.Builder
	var anchors []htmlWebFetchAnchor
	var buttons []htmlWebFetchLabeledControl
	var textareas []htmlWebFetchLabeledControl
	var selects []htmlWebFetchSelectControl
	pictureDepth := 0
	pictureSource := ""
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], "<!--") {
			if end := strings.Index(body[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			break
		}
		if body[i] != '<' {
			if len(selects) > 0 {
				idx := len(selects) - 1
				if selects[idx].InOption {
					selects[idx].OptionText += string(body[i])
				}
				i++
				continue
			}
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
		rawTag := body[i+1 : i+end]
		tag, closing := htmlWebFetchTagInfo(rawTag)
		if tag == "select" {
			if closing {
				if len(selects) > 0 {
					idx := len(selects) - 1
					selects[idx] = finishHTMLWebFetchSelectOption(selects[idx])
					appendHTMLWebFetchSelectText(&b, selects[idx])
					selects = selects[:idx]
				}
			} else {
				label := firstNonEmptyWebFetchAttr(rawTag, "aria-label", "title")
				selects = append(selects, htmlWebFetchSelectControl{Label: label, Multiple: htmlWebFetchHasAttr(rawTag, "multiple")})
			}
			i += end + 1
			continue
		}
		if len(selects) > 0 {
			idx := len(selects) - 1
			if tag == "option" {
				selects[idx] = finishHTMLWebFetchSelectOption(selects[idx])
				if !closing {
					selects[idx].InOption = true
					selects[idx].OptionSelected = htmlWebFetchHasAttr(rawTag, "selected")
					selects[idx].OptionText = ""
					selects[idx].OptionLabel = firstNonEmptyWebFetchAttr(rawTag, "label")
				}
			}
			i += end + 1
			continue
		}
		if tag == "picture" {
			if closing {
				if pictureDepth > 0 {
					pictureDepth--
				}
				if pictureDepth == 0 {
					pictureSource = ""
				}
			} else {
				pictureDepth++
				if pictureDepth == 1 {
					pictureSource = ""
				}
			}
		}
		if tag == "a" {
			if closing {
				anchors, _ = appendHTMLWebFetchAnchorHref(&b, anchors)
			} else {
				href := strings.TrimSpace(htmlWebFetchAttr(rawTag, "href"))
				anchors = append(anchors, htmlWebFetchAnchor{Href: resolveWebFetchHTMLURL(href, baseURL), Start: b.Len()})
			}
		}
		if tag == "button" {
			if closing {
				buttons, _ = appendHTMLWebFetchControlLabel(&b, buttons)
			} else {
				label := firstNonEmptyWebFetchAttr(rawTag, "aria-label", "title", "value")
				buttons = append(buttons, htmlWebFetchLabeledControl{Label: label, Start: b.Len()})
			}
		}
		if tag == "textarea" {
			if closing {
				textareas, _ = appendHTMLWebFetchControlLabel(&b, textareas)
			} else {
				label := firstNonEmptyWebFetchAttr(rawTag, "placeholder", "aria-label", "title")
				textareas = append(textareas, htmlWebFetchLabeledControl{Label: label, Start: b.Len()})
			}
		}
		if tag == "source" && !closing && pictureDepth > 0 && pictureSource == "" {
			if candidate := resolveFirstWebFetchMediaURL(baseURL, webFetchSrcsetURLs(htmlWebFetchAttr(rawTag, "srcset"))...); candidate != "" {
				pictureSource = candidate
			}
		}
		if tag == "img" && !closing {
			appendHTMLWebFetchImageText(&b, rawTag, baseURL, pictureSource)
		}
		if tag == "input" && !closing {
			appendHTMLWebFetchInputText(&b, rawTag)
		}
		if tag == "br" || isBlockHTMLWebFetchTag(tag) {
			b.WriteByte('\n')
		}
		i += end + 1
	}
	return b.String()
}

func webFetchHTMLBaseURL(body string, fallback string) string {
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], "<!--") {
			if end := strings.Index(body[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			break
		}
		if body[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(body[i:], '>')
		if end < 0 {
			break
		}
		rawTag := body[i+1 : i+end]
		tag, closing := htmlWebFetchTagInfo(rawTag)
		if tag == "base" && !closing {
			href := strings.TrimSpace(htmlWebFetchAttr(rawTag, "href"))
			if resolved := resolveWebFetchBaseURL(href, fallback); resolved != "" {
				return resolved
			}
		}
		i += end + 1
	}
	return fallback
}

func resolveWebFetchBaseURL(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || hasUnsafeWebFetchHTMLURLScheme(raw) {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		fallbackURL, err := url.Parse(strings.TrimSpace(fallback))
		if err != nil || fallbackURL.Scheme == "" || fallbackURL.Host == "" {
			return ""
		}
		parsed = fallbackURL.ResolveReference(parsed)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

type htmlWebFetchAnchor struct {
	Href  string
	Start int
}

type htmlWebFetchLabeledControl struct {
	Label string
	Start int
}

type htmlWebFetchSelectControl struct {
	Label           string
	FirstOption     string
	SelectedOption  string
	SelectedOptions []string
	Multiple        bool
	InOption        bool
	OptionSelected  bool
	OptionText      string
	OptionLabel     string
}

func appendHTMLWebFetchAnchorHref(b *strings.Builder, anchors []htmlWebFetchAnchor) ([]htmlWebFetchAnchor, bool) {
	if len(anchors) == 0 {
		return anchors, false
	}
	anchor := anchors[len(anchors)-1]
	anchors = anchors[:len(anchors)-1]
	href := strings.TrimSpace(anchor.Href)
	if href == "" || strings.HasPrefix(href, "#") || hasUnsafeWebFetchHTMLURLScheme(href) {
		return anchors, false
	}
	text := ""
	current := b.String()
	if anchor.Start >= 0 && anchor.Start <= len(current) {
		text = strings.Join(strings.Fields(current[anchor.Start:]), " ")
	}
	if text != "" && (text == href || strings.Contains(text, href)) {
		return anchors, false
	}
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	b.WriteByte('(')
	b.WriteString(href)
	b.WriteByte(')')
	return anchors, true
}

func appendHTMLWebFetchControlLabel(b *strings.Builder, controls []htmlWebFetchLabeledControl) ([]htmlWebFetchLabeledControl, bool) {
	if len(controls) == 0 {
		return controls, false
	}
	control := controls[len(controls)-1]
	controls = controls[:len(controls)-1]
	label := strings.TrimSpace(control.Label)
	if label == "" {
		return controls, false
	}
	current := b.String()
	if control.Start >= 0 && control.Start <= len(current) {
		if text := strings.Join(strings.Fields(current[control.Start:]), " "); text != "" {
			return controls, false
		}
	}
	b.WriteString("\nInput: ")
	b.WriteString(label)
	b.WriteByte('\n')
	return controls, true
}

func finishHTMLWebFetchSelectOption(control htmlWebFetchSelectControl) htmlWebFetchSelectControl {
	if !control.InOption {
		return control
	}
	text := strings.TrimSpace(control.OptionLabel)
	if text == "" {
		text = strings.Join(strings.Fields(control.OptionText), " ")
	}
	if text != "" {
		if control.FirstOption == "" {
			control.FirstOption = text
		}
		if control.OptionSelected {
			if control.Multiple {
				control.SelectedOptions = append(control.SelectedOptions, text)
			} else {
				control.SelectedOption = text
			}
		}
	}
	control.InOption = false
	control.OptionSelected = false
	control.OptionText = ""
	control.OptionLabel = ""
	return control
}

func appendHTMLWebFetchSelectText(b *strings.Builder, control htmlWebFetchSelectControl) {
	text := ""
	if control.Multiple && len(control.SelectedOptions) > 0 {
		text = strings.Join(control.SelectedOptions, ", ")
	}
	if text == "" {
		text = strings.TrimSpace(control.SelectedOption)
	}
	if text == "" {
		text = strings.TrimSpace(control.FirstOption)
	}
	if text == "" {
		text = strings.TrimSpace(control.Label)
	}
	if text == "" {
		return
	}
	label := strings.TrimSpace(control.Label)
	if label != "" && label != text {
		text = label + ": " + text
	}
	b.WriteString("\nInput: ")
	b.WriteString(text)
	b.WriteByte('\n')
}

func appendHTMLWebFetchImageText(b *strings.Builder, rawTag string, baseURL string, sourceOverride string) {
	label := firstNonEmptyWebFetchAttr(rawTag, "alt", "title", "aria-label")
	srcValues := []string{sourceOverride}
	srcValues = append(srcValues, webFetchImageSources(rawTag)...)
	src := resolveFirstWebFetchMediaURL(baseURL, srcValues...)
	if label == "" {
		return
	}
	b.WriteString("\nImage: ")
	b.WriteString(label)
	if src != "" {
		b.WriteString(" (")
		b.WriteString(src)
		b.WriteByte(')')
	}
	b.WriteByte('\n')
}

func appendHTMLWebFetchInputText(b *strings.Builder, rawTag string) {
	inputType := strings.ToLower(strings.TrimSpace(htmlWebFetchAttr(rawTag, "type")))
	if inputType == "" {
		inputType = "text"
	}
	switch inputType {
	case "hidden", "password", "file":
		return
	}
	label := ""
	switch inputType {
	case "button", "submit", "reset":
		label = firstNonEmptyWebFetchAttr(rawTag, "value", "aria-label", "title")
	case "image":
		label = firstNonEmptyWebFetchAttr(rawTag, "alt", "aria-label", "title", "value")
	case "checkbox", "radio":
		label = firstNonEmptyWebFetchAttr(rawTag, "aria-label", "title")
	default:
		label = firstNonEmptyWebFetchAttr(rawTag, "value", "placeholder", "aria-label", "title")
	}
	if label == "" {
		return
	}
	b.WriteString("\nInput: ")
	b.WriteString(label)
	b.WriteByte('\n')
}

func webFetchImageSources(rawTag string) []string {
	var sources []string
	for _, name := range []string{"srcset", "data-srcset", "data-lazy-srcset"} {
		sources = append(sources, webFetchSrcsetURLs(htmlWebFetchAttr(rawTag, name))...)
	}
	for _, name := range []string{"src", "data-src", "data-original", "data-original-src", "data-lazy-src", "data-url", "data-image", "data-image-src"} {
		if src := strings.TrimSpace(htmlWebFetchAttr(rawTag, name)); src != "" {
			sources = append(sources, src)
		}
	}
	return sources
}

func webFetchSrcsetURLs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var urls []string
	skipDataContinuation := false
	for _, candidate := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.TrimSpace(candidate))
		if len(fields) == 0 {
			continue
		}
		src := strings.TrimSpace(fields[0])
		lower := strings.ToLower(src)
		if strings.HasPrefix(lower, "data:") {
			skipDataContinuation = true
			continue
		}
		if skipDataContinuation {
			if !looksLikeWebFetchURLCandidate(src) {
				continue
			}
			skipDataContinuation = false
		}
		urls = append(urls, src)
	}
	return urls
}

func resolveFirstWebFetchMediaURL(baseURL string, rawValues ...string) string {
	for _, raw := range rawValues {
		resolved := resolveWebFetchHTMLURL(raw, baseURL)
		if resolved == "" {
			continue
		}
		parsed, err := url.Parse(resolved)
		if err != nil || !parsed.IsAbs() {
			continue
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return parsed.String()
		}
	}
	return ""
}

func looksLikeWebFetchURLCandidate(raw string) bool {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(raw, "//") ||
		strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "./") ||
		strings.HasPrefix(raw, "../") ||
		strings.Contains(raw, ".")
}

func firstNonEmptyWebFetchAttr(rawTag string, names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(htmlWebFetchAttr(rawTag, name))
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveWebFetchHTMLURL(raw string, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return raw
	}
	if hasUnsafeWebFetchHTMLURLScheme(raw) {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return raw
	}
	return base.ResolveReference(parsed).String()
}

func hasUnsafeWebFetchHTMLURLScheme(raw string) bool {
	idx := strings.IndexByte(raw, ':')
	if idx <= 0 {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(raw[:idx]))
	switch scheme {
	case "javascript", "data", "blob", "vbscript":
		return true
	default:
		return false
	}
}

func htmlWebFetchAttr(rawTag string, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, match := range htmlWebFetchAttrRe.FindAllStringSubmatch(rawTag, -1) {
		if len(match) < 5 || strings.ToLower(match[1]) != name {
			continue
		}
		for _, value := range match[2:] {
			if value != "" {
				return html.UnescapeString(value)
			}
		}
	}
	return ""
}

func htmlWebFetchHasAttr(rawTag string, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	raw := strings.TrimSpace(rawTag)
	raw = strings.TrimLeft(raw, "/!?")
	i := 0
	for i < len(raw) && !unicode.IsSpace(rune(raw[i])) && raw[i] != '/' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && (unicode.IsSpace(rune(raw[i])) || raw[i] == '/') {
			i++
		}
		start := i
		for i < len(raw) && isHTMLWebFetchAttrNameByte(raw[i]) {
			i++
		}
		if start == i {
			i++
			continue
		}
		attr := strings.ToLower(raw[start:i])
		for i < len(raw) && unicode.IsSpace(rune(raw[i])) {
			i++
		}
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && unicode.IsSpace(rune(raw[i])) {
				i++
			}
			if i < len(raw) && (raw[i] == '"' || raw[i] == '\'') {
				quote := raw[i]
				i++
				for i < len(raw) && raw[i] != quote {
					i++
				}
				if i < len(raw) {
					i++
				}
			} else {
				for i < len(raw) && !unicode.IsSpace(rune(raw[i])) && raw[i] != '/' {
					i++
				}
			}
		}
		if attr == name {
			return true
		}
	}
	return false
}

func isHTMLWebFetchAttrNameByte(value byte) bool {
	return value == '_' ||
		value == ':' ||
		value == '.' ||
		value == '-' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}

func htmlWebFetchTagInfo(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	closing := strings.HasPrefix(raw, "/")
	raw = strings.TrimLeft(raw, "/!?")
	if raw == "" {
		return "", closing
	}
	for idx, r := range raw {
		if unicode.IsSpace(r) || r == '/' || r == '>' {
			return strings.ToLower(raw[:idx]), closing
		}
	}
	return strings.ToLower(raw), closing
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

var (
	htmlWebFetchAttrRe    = regexp.MustCompile("(?is)\\b([a-z_:][a-z0-9_:.:-]*)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")
	htmlWebFetchMetaTagRe = regexp.MustCompile(`(?is)<meta\b([^>]*)>`)
)

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

func decodeWebFetchText(contentType string, data []byte) (string, string) {
	declared := webFetchContentCharset(contentType)
	if text, detected, ok := decodeWebFetchTextBOM(data); ok {
		return text, detected
	}
	if declared == "" {
		declared = webFetchMetaCharset(contentType, data)
	}
	charset := normalizeWebFetchCharset(declared)
	switch charset {
	case "", "utf-8", "us-ascii":
		return string(data), charset
	case "utf-16":
		return decodeWebFetchUTF16(data, false), charset
	case "utf-16le":
		return decodeWebFetchUTF16(data, true), charset
	case "utf-16be":
		return decodeWebFetchUTF16(data, false), charset
	case "iso-8859-1", "latin-1":
		return decodeWebFetchSingleByte(data, nil), charset
	case "windows-1252":
		return decodeWebFetchSingleByte(data, webFetchWindows1252Rune), charset
	default:
		return string(data), charset
	}
}

func decodeWebFetchTextBOM(data []byte) (string, string, bool) {
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		return string(data[3:]), "utf-8", true
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		return decodeWebFetchUTF16(data[2:], true), "utf-16le", true
	}
	if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
		return decodeWebFetchUTF16(data[2:], false), "utf-16be", true
	}
	return "", "", false
}

func webFetchContentCharset(contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return params["charset"]
}

func webFetchMetaCharset(contentType string, data []byte) string {
	if !isHTMLWebFetchContent(contentType, string(data[:min(len(data), 4096)])) {
		return ""
	}
	prefix := string(data)
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	for _, match := range htmlWebFetchMetaTagRe.FindAllStringSubmatch(prefix, -1) {
		if len(match) < 2 {
			continue
		}
		rawTag := match[1]
		if charset := htmlWebFetchAttr(rawTag, "charset"); charset != "" {
			return charset
		}
		content := htmlWebFetchAttr(rawTag, "content")
		if content == "" {
			continue
		}
		if _, params, err := mime.ParseMediaType(content); err == nil && params["charset"] != "" {
			return params["charset"]
		}
	}
	return ""
}

func normalizeWebFetchCharset(charset string) string {
	charset = strings.Trim(strings.ToLower(strings.TrimSpace(charset)), "\"'")
	charset = strings.ReplaceAll(charset, "_", "-")
	switch charset {
	case "utf8":
		return "utf-8"
	case "ascii", "usascii":
		return "us-ascii"
	case "utf16":
		return "utf-16"
	case "utf16le":
		return "utf-16le"
	case "utf16be":
		return "utf-16be"
	case "latin1", "latin-1", "iso8859-1":
		return "iso-8859-1"
	case "cp1252", "windows1252":
		return "windows-1252"
	default:
		return charset
	}
}

func decodeWebFetchUTF16(data []byte, littleEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if littleEndian {
			units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
		} else {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		}
	}
	return string(utf16.Decode(units))
}

type webFetchByteDecoder func(byte) rune

func decodeWebFetchSingleByte(data []byte, decode webFetchByteDecoder) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, value := range data {
		if decode != nil {
			b.WriteRune(decode(value))
			continue
		}
		b.WriteRune(rune(value))
	}
	return b.String()
}

func webFetchWindows1252Rune(value byte) rune {
	if value < 0x80 || value > 0x9f {
		return rune(value)
	}
	switch value {
	case 0x80:
		return '\u20ac'
	case 0x82:
		return '\u201a'
	case 0x83:
		return '\u0192'
	case 0x84:
		return '\u201e'
	case 0x85:
		return '\u2026'
	case 0x86:
		return '\u2020'
	case 0x87:
		return '\u2021'
	case 0x88:
		return '\u02c6'
	case 0x89:
		return '\u2030'
	case 0x8a:
		return '\u0160'
	case 0x8b:
		return '\u2039'
	case 0x8c:
		return '\u0152'
	case 0x8e:
		return '\u017d'
	case 0x91:
		return '\u2018'
	case 0x92:
		return '\u2019'
	case 0x93:
		return '\u201c'
	case 0x94:
		return '\u201d'
	case 0x95:
		return '\u2022'
	case 0x96:
		return '\u2013'
	case 0x97:
		return '\u2014'
	case 0x98:
		return '\u02dc'
	case 0x99:
		return '\u2122'
	case 0x9a:
		return '\u0161'
	case 0x9b:
		return '\u203a'
	case 0x9c:
		return '\u0153'
	case 0x9e:
		return '\u017e'
	case 0x9f:
		return '\u0178'
	default:
		return rune(value)
	}
}

func decodeWebFetch(raw json.RawMessage) (webFetchInput, error) {
	obj, err := decodeWebStrictObject(raw, allowedWebFetchInputKeys)
	if err != nil {
		return webFetchInput{}, err
	}
	coerceWebSemanticJSONNumbers(obj, webFetchSemanticNumberKeys)
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

func normalizeWebFetchRawInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeWebStrictObject(raw, allowedWebFetchInputKeys)
	if err != nil {
		return nil, err
	}
	coerceWebSemanticJSONNumbers(obj, webFetchSemanticNumberKeys)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
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
		"attempted":           preflight.Attempted,
		"skipped":             preflight.Skipped,
		"status_code":         preflight.StatusCode,
		"content_type":        preflight.ContentType,
		"content_disposition": preflight.ContentDisposition,
		"content_length":      preflight.ContentLength,
		"skipped_get":         preflight.SkippedGET,
		"error":               preflight.Error,
	}
}

func isBinaryWebContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return false
	}
	return !isTextualWebMediaType(strings.ToLower(mediaType))
}

func isBinaryWebAttachment(contentDisposition string) bool {
	disposition, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil || strings.ToLower(disposition) != "attachment" {
		return false
	}
	filename := strings.TrimSpace(params["filename"])
	if filename == "" {
		filename = strings.TrimSpace(params["filename*"])
	}
	return isBinaryWebFilename(filename)
}

func isBinaryWebFilename(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico",
		".zip", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar", ".tar",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".mp3", ".mp4", ".mov", ".avi", ".webm", ".wav",
		".dmg", ".exe", ".dll", ".so", ".dylib", ".wasm":
		return true
	default:
		return false
	}
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

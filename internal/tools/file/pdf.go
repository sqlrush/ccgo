package filetools

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
)

var (
	pdfIndirectObjectPattern = regexp.MustCompile(`(?s)(\d+)\s+(\d+)\s+obj\b(.*?)endobj`)
	pdfIndirectRefPattern    = regexp.MustCompile(`(\d+)\s+(\d+)\s+R`)
	pdfCatalogTypePattern    = regexp.MustCompile(`/Type\s*/Catalog(?:[^A-Za-z]|$)`)
	pdfContentsArrayPattern  = regexp.MustCompile(`(?s)/Contents\s*\[(.*?)\]`)
	pdfContentsRefPattern    = regexp.MustCompile(`/Contents\s+(\d+)\s+(\d+)\s+R`)
	pdfKidsArrayPattern      = regexp.MustCompile(`(?s)/Kids\s*\[(.*?)\]`)
	pdfPageTypePattern       = regexp.MustCompile(`/Type\s*/Page(?:[^A-Za-z]|$)`)
	pdfPagesRootRefPattern   = regexp.MustCompile(`/Pages\s+(\d+)\s+(\d+)\s+R`)
)

type pdfObject struct {
	Number     int
	Generation int
	Body       []byte
}

func readPDFResult(displayPath string, path string, pages string) (contracts.ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	pageTexts := extractPDFPageTexts(data)
	pageCount := estimatePDFPageCount(data)
	if len(pageTexts) > pageCount {
		pageCount = len(pageTexts)
	}
	selectedPages, err := parsePDFPageSelection(pages, pageCount)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if len(selectedPages) == 0 && pageCount > 0 {
		selectedPages = make([]int, 0, pageCount)
		for page := 1; page <= pageCount; page++ {
			selectedPages = append(selectedPages, page)
		}
	}
	body, selectedText := formatPDFPages(selectedPages, pageTexts)
	if body == "" {
		body = "<no extractable text>"
	}
	return contracts.ToolResult{
		Content: fmt.Sprintf("PDF: %s\nPages: %d\n\n%s", displayPath, pageCount, body),
		StructuredContent: map[string]any{
			"type":           "pdf",
			"filePath":       displayPath,
			"pageCount":      pageCount,
			"selected_pages": selectedPages,
			"text":           selectedText,
			"extractable":    strings.TrimSpace(selectedText) != "",
		},
	}, nil
}

func estimatePDFPageCount(data []byte) int {
	if objects := parsePDFObjects(data); len(objects) > 0 {
		count := 0
		for _, object := range objects {
			if isPDFPageObject(object.Body) {
				count++
			}
		}
		if count > 0 {
			return count
		}
	}
	return len(pdfPageTypePattern.FindAll(data, -1))
}

func parsePDFPageSelection(raw string, pageCount int) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return nil, nil
	}
	var pages []int
	seen := map[int]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid PDF page range %q", raw)
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := parsePDFPageNumber(bounds[0])
			if err != nil {
				return nil, err
			}
			end, err := parsePDFPageNumber(bounds[1])
			if err != nil {
				return nil, err
			}
			if start > end {
				return nil, fmt.Errorf("invalid PDF page range %q: start is greater than end", part)
			}
			for page := start; page <= end; page++ {
				if err := appendPDFPage(&pages, seen, page, pageCount); err != nil {
					return nil, err
				}
			}
			continue
		}
		page, err := parsePDFPageNumber(part)
		if err != nil {
			return nil, err
		}
		if err := appendPDFPage(&pages, seen, page, pageCount); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func parsePDFPageNumber(raw string) (int, error) {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page <= 0 {
		return 0, fmt.Errorf("PDF page numbers must be positive integers")
	}
	return page, nil
}

func appendPDFPage(pages *[]int, seen map[int]struct{}, page int, pageCount int) error {
	if pageCount > 0 && page > pageCount {
		return fmt.Errorf("page %d exceeds PDF page count %d", page, pageCount)
	}
	if _, ok := seen[page]; ok {
		return nil
	}
	seen[page] = struct{}{}
	*pages = append(*pages, page)
	return nil
}

func formatPDFPages(selectedPages []int, pageTexts []string) (string, string) {
	if len(selectedPages) == 0 {
		if len(pageTexts) == 0 {
			return "", ""
		}
		selectedPages = make([]int, 0, len(pageTexts))
		for page := range pageTexts {
			selectedPages = append(selectedPages, page+1)
		}
	}
	sections := make([]string, 0, len(selectedPages))
	texts := make([]string, 0, len(selectedPages))
	for _, page := range selectedPages {
		text := ""
		if index := page - 1; index >= 0 && index < len(pageTexts) {
			text = pageTexts[index]
		}
		if strings.TrimSpace(text) == "" {
			sections = append(sections, fmt.Sprintf("Page %d:\n<no extractable text>", page))
			continue
		}
		sections = append(sections, fmt.Sprintf("Page %d:\n%s", page, text))
		texts = append(texts, text)
	}
	return strings.Join(sections, "\n\n"), strings.Join(texts, "\n\n")
}

func extractPDFPageTexts(data []byte) []string {
	if texts := extractPDFPageTextsFromObjects(data); len(texts) > 0 {
		return texts
	}
	streams := extractPDFStreams(data)
	texts := make([]string, 0, len(streams))
	for _, stream := range streams {
		if text := extractPDFText(stream); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		if text := extractPDFText(data); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func extractPDFPageTextsFromObjects(data []byte) []string {
	objects := parsePDFObjects(data)
	if len(objects) == 0 {
		return nil
	}
	byRef := make(map[string]pdfObject, len(objects))
	for _, object := range objects {
		byRef[pdfObjectRef(object.Number, object.Generation)] = object
	}
	pages := orderedPDFPageObjects(objects, byRef)
	var pageTexts []string
	for _, page := range pages {
		pageTexts = append(pageTexts, extractPDFPageText(page.Body, byRef))
	}
	return pageTexts
}

func parsePDFObjects(data []byte) []pdfObject {
	matches := pdfIndirectObjectPattern.FindAllSubmatch(data, -1)
	objects := make([]pdfObject, 0, len(matches))
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		number, err := strconv.Atoi(string(match[1]))
		if err != nil {
			continue
		}
		generation, err := strconv.Atoi(string(match[2]))
		if err != nil {
			continue
		}
		objects = append(objects, pdfObject{
			Number:     number,
			Generation: generation,
			Body:       bytes.TrimSpace(match[3]),
		})
	}
	return objects
}

func pdfObjectRef(number int, generation int) string {
	return fmt.Sprintf("%d %d", number, generation)
}

func isPDFPageObject(body []byte) bool {
	return pdfPageTypePattern.Match(body)
}

func orderedPDFPageObjects(objects []pdfObject, byRef map[string]pdfObject) []pdfObject {
	if root, ok := pdfCatalogPagesRef(objects); ok {
		refs := collectPDFPageRefs(root, byRef, map[string]struct{}{})
		pages := make([]pdfObject, 0, len(refs))
		for _, ref := range refs {
			if object, ok := byRef[ref]; ok {
				pages = append(pages, object)
			}
		}
		if len(pages) > 0 {
			return pages
		}
	}
	pages := make([]pdfObject, 0)
	for _, object := range objects {
		if isPDFPageObject(object.Body) {
			pages = append(pages, object)
		}
	}
	return pages
}

func pdfCatalogPagesRef(objects []pdfObject) (string, bool) {
	for _, object := range objects {
		if !pdfCatalogTypePattern.Match(object.Body) {
			continue
		}
		match := pdfPagesRootRefPattern.FindSubmatch(object.Body)
		if len(match) == 3 {
			return string(match[1]) + " " + string(match[2]), true
		}
	}
	return "", false
}

func collectPDFPageRefs(ref string, objects map[string]pdfObject, seen map[string]struct{}) []string {
	if _, ok := seen[ref]; ok {
		return nil
	}
	seen[ref] = struct{}{}
	object, ok := objects[ref]
	if !ok {
		return nil
	}
	if isPDFPageObject(object.Body) {
		return []string{ref}
	}
	var refs []string
	for _, childRef := range pdfObjectKidsRefs(object.Body) {
		refs = append(refs, collectPDFPageRefs(childRef, objects, seen)...)
	}
	return refs
}

func extractPDFPageText(pageBody []byte, objects map[string]pdfObject) string {
	refs := pdfPageContentRefs(pageBody)
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		object, ok := objects[ref]
		if !ok {
			continue
		}
		if stream, ok := extractPDFObjectStream(object.Body); ok {
			if text := extractPDFText(stream); text != "" {
				parts = append(parts, text)
			}
			continue
		}
		if text := extractPDFText(object.Body); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	if stream, ok := extractPDFObjectStream(pageBody); ok {
		return extractPDFText(stream)
	}
	return ""
}

func pdfPageContentRefs(body []byte) []string {
	return pdfObjectRefsForPattern(body, pdfContentsArrayPattern, pdfContentsRefPattern)
}

func pdfObjectKidsRefs(body []byte) []string {
	return pdfObjectRefsForPattern(body, pdfKidsArrayPattern, nil)
}

func pdfObjectRefsForPattern(body []byte, arrayPattern *regexp.Regexp, singlePattern *regexp.Regexp) []string {
	var refs []string
	for _, match := range arrayPattern.FindAllSubmatch(body, -1) {
		for _, refMatch := range pdfIndirectRefPattern.FindAllSubmatch(match[1], -1) {
			refs = append(refs, string(refMatch[1])+" "+string(refMatch[2]))
		}
	}
	if singlePattern != nil {
		for _, match := range singlePattern.FindAllSubmatch(body, -1) {
			refs = append(refs, string(match[1])+" "+string(match[2]))
		}
	}
	return refs
}

func extractPDFStreams(data []byte) [][]byte {
	var streams [][]byte
	for offset := 0; offset < len(data); {
		streamStartRel := bytes.Index(data[offset:], []byte("stream"))
		if streamStartRel < 0 {
			break
		}
		streamStart := offset + streamStartRel
		contentStart := streamStart + len("stream")
		if contentStart < len(data) && data[contentStart] == '\r' {
			contentStart++
			if contentStart < len(data) && data[contentStart] == '\n' {
				contentStart++
			}
		} else if contentStart < len(data) && data[contentStart] == '\n' {
			contentStart++
		}
		streamEndRel := bytes.Index(data[contentStart:], []byte("endstream"))
		if streamEndRel < 0 {
			break
		}
		streamEnd := contentStart + streamEndRel
		raw := bytes.TrimRight(data[contentStart:streamEnd], "\r\n")
		dict := pdfStreamDictionary(data[offset:streamStart])
		streams = append(streams, decodePDFStream(raw, dict))
		offset = streamEnd + len("endstream")
	}
	return streams
}

func extractPDFObjectStream(body []byte) ([]byte, bool) {
	streamStart := bytes.Index(body, []byte("stream"))
	if streamStart < 0 {
		return nil, false
	}
	contentStart := streamStart + len("stream")
	if contentStart < len(body) && body[contentStart] == '\r' {
		contentStart++
		if contentStart < len(body) && body[contentStart] == '\n' {
			contentStart++
		}
	} else if contentStart < len(body) && body[contentStart] == '\n' {
		contentStart++
	}
	streamEndRel := bytes.Index(body[contentStart:], []byte("endstream"))
	if streamEndRel < 0 {
		return nil, false
	}
	streamEnd := contentStart + streamEndRel
	raw := bytes.TrimRight(body[contentStart:streamEnd], "\r\n")
	dictionary := pdfStreamDictionary(body[:streamStart])
	return decodePDFStream(raw, dictionary), true
}

func pdfStreamDictionary(prefix []byte) []byte {
	start := bytes.LastIndex(prefix, []byte("<<"))
	if start < 0 {
		return nil
	}
	return prefix[start:]
}

func decodePDFStream(raw []byte, dictionary []byte) []byte {
	if !bytes.Contains(dictionary, []byte("/FlateDecode")) {
		return raw
	}
	if reader, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
		defer reader.Close()
		if decoded, readErr := io.ReadAll(reader); readErr == nil {
			return decoded
		}
	}
	reader := flate.NewReader(bytes.NewReader(raw))
	defer reader.Close()
	if decoded, err := io.ReadAll(reader); err == nil {
		return decoded
	}
	return raw
}

func extractPDFText(data []byte) string {
	parts := make([]string, 0)
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '(':
			text, next, ok := readPDFLiteralString(data, i)
			if ok {
				if normalized := normalizePDFText(text); normalized != "" {
					parts = append(parts, normalized)
				}
				i = next - 1
			}
		case '<':
			if i+1 < len(data) && data[i+1] == '<' {
				continue
			}
			text, next, ok := readPDFHexString(data, i)
			if ok {
				if normalized := normalizePDFText(text); normalized != "" {
					parts = append(parts, normalized)
				}
				i = next - 1
			}
		}
	}
	return strings.Join(parts, " ")
}

func readPDFLiteralString(data []byte, start int) (string, int, bool) {
	depth := 1
	var out []byte
	for i := start + 1; i < len(data); i++ {
		c := data[i]
		if c == '\\' {
			decoded, next := decodePDFEscapedByte(data, i+1)
			if decoded >= 0 {
				out = append(out, byte(decoded))
			}
			i = next - 1
			continue
		}
		if c == '(' {
			depth++
			out = append(out, c)
			continue
		}
		if c == ')' {
			depth--
			if depth == 0 {
				return string(out), i + 1, true
			}
			out = append(out, c)
			continue
		}
		out = append(out, c)
	}
	return "", start + 1, false
}

func decodePDFEscapedByte(data []byte, start int) (int, int) {
	if start >= len(data) {
		return -1, start
	}
	c := data[start]
	switch c {
	case 'n':
		return '\n', start + 1
	case 'r':
		return '\r', start + 1
	case 't':
		return '\t', start + 1
	case 'b':
		return '\b', start + 1
	case 'f':
		return '\f', start + 1
	case '(', ')', '\\':
		return int(c), start + 1
	case '\r':
		if start+1 < len(data) && data[start+1] == '\n' {
			return -1, start + 2
		}
		return -1, start + 1
	case '\n':
		return -1, start + 1
	}
	if c < '0' || c > '7' {
		return int(c), start + 1
	}
	end := start
	for end < len(data) && end < start+3 && data[end] >= '0' && data[end] <= '7' {
		end++
	}
	value, err := strconv.ParseInt(string(data[start:end]), 8, 16)
	if err != nil {
		return int(c), start + 1
	}
	return int(value), end
}

func readPDFHexString(data []byte, start int) (string, int, bool) {
	end := bytes.IndexByte(data[start+1:], '>')
	if end < 0 {
		return "", start + 1, false
	}
	end += start + 1
	raw := strings.Join(strings.Fields(string(data[start+1:end])), "")
	if raw == "" {
		return "", end + 1, true
	}
	if len(raw)%2 == 1 {
		raw += "0"
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return "", end + 1, false
	}
	return string(decoded), end + 1, true
}

func normalizePDFText(text string) string {
	text = strings.ReplaceAll(text, "\x00", "")
	return strings.Join(strings.Fields(text), " ")
}

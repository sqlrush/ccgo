package lsp

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const defaultInitializeID = 1

type ServerHandshakeOptions struct {
	InitializeID  int
	ProcessID     int
	RootURI       string
	RootPath      string
	ClientName    string
	ClientVersion string
	Trace         string
	Documents     []OpenDocument
}

type OpenDocument struct {
	URI        string
	FilePath   string
	LanguageID string
	Version    int
	Text       string
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type jsonRPCMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func (p *ServerProcess) InitializeAndOpen(ctx context.Context, opts ServerHandshakeOptions) error {
	if p == nil || p.stdin == nil {
		return os.ErrInvalid
	}
	return WriteInitializeAndOpen(ctx, p.stdin, opts)
}

func WriteInitializeAndOpen(ctx context.Context, w io.Writer, opts ServerHandshakeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil {
		return os.ErrInvalid
	}
	if err := WriteInitializeRequest(ctx, w, opts); err != nil {
		return err
	}
	if err := WriteInitializedNotification(ctx, w); err != nil {
		return err
	}
	for _, doc := range opts.Documents {
		if err := WriteDidOpenNotification(ctx, w, doc); err != nil {
			return err
		}
	}
	return nil
}

func WriteInitializeRequest(ctx context.Context, w io.Writer, opts ServerHandshakeOptions) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if w == nil {
		return os.ErrInvalid
	}
	id := opts.InitializeID
	if id == 0 {
		id = defaultInitializeID
	}
	processID := opts.ProcessID
	if processID == 0 {
		processID = os.Getpid()
	}
	clientName := strings.TrimSpace(opts.ClientName)
	if clientName == "" {
		clientName = "ccgo"
	}
	params := map[string]any{
		"processId": processID,
		"clientInfo": map[string]any{
			"name":    clientName,
			"version": strings.TrimSpace(opts.ClientVersion),
		},
		"capabilities": defaultClientCapabilities(),
	}
	if rootURI := strings.TrimSpace(opts.RootURI); rootURI != "" {
		params["rootUri"] = rootURI
	}
	if rootPath := strings.TrimSpace(opts.RootPath); rootPath != "" {
		params["rootPath"] = rootPath
	}
	if trace := strings.TrimSpace(opts.Trace); trace != "" {
		params["trace"] = trace
	}
	return writeFramedJSON(w, jsonRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params:  params,
	})
}

func WriteInitializedNotification(ctx context.Context, w io.Writer) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if w == nil {
		return os.ErrInvalid
	}
	return writeFramedJSON(w, jsonRPCMessage{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  map[string]any{},
	})
}

func WriteDidOpenNotification(ctx context.Context, w io.Writer, doc OpenDocument) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if w == nil {
		return os.ErrInvalid
	}
	item, err := normalizeOpenDocument(doc)
	if err != nil {
		return err
	}
	return writeFramedJSON(w, jsonRPCMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/didOpen",
		Params: map[string]any{
			"textDocument": item,
		},
	})
}

func FileURIFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(cleaned)}
	return u.String()
}

func defaultClientCapabilities() map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"publishDiagnostics": map[string]any{
				"relatedInformation": true,
			},
		},
		"workspace": map[string]any{
			"workspaceFolders": true,
		},
	}
}

func normalizeOpenDocument(doc OpenDocument) (textDocumentItem, error) {
	uri := strings.TrimSpace(doc.URI)
	if uri == "" {
		uri = FileURIFromPath(doc.FilePath)
	}
	if uri == "" {
		return textDocumentItem{}, os.ErrInvalid
	}
	languageID := strings.TrimSpace(doc.LanguageID)
	if languageID == "" {
		languageID = languageIDFromPath(doc.FilePath)
	}
	if languageID == "" {
		languageID = "plaintext"
	}
	version := doc.Version
	if version == 0 {
		version = 1
	}
	return textDocumentItem{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Text:       doc.Text,
	}, nil
}

func languageIDFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return "cpp"
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".json":
		return "json"
	case ".lua":
		return "lua"
	case ".md", ".markdown":
		return "markdown"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".ts", ".tsx":
		return "typescript"
	default:
		return ""
	}
}

func writeFramedJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return WriteFramedMessage(w, data)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

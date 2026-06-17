package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const diagnosticsFileName = "lsp-diagnostics.json"

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	FilePath string `json:"file_path"`
	Range    Range  `json:"range"`
	Severity string `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string                    `json:"uri"`
	Diagnostics []PublishDiagnosticRecord `json:"diagnostics"`
}

type PublishDiagnosticRecord struct {
	Range    Range  `json:"range"`
	Severity any    `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type Filter struct {
	FilePath string
	Severity string
	Limit    int
}

func SessionDiagnosticsPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), diagnosticsFileName)
}

func WriteSnapshot(path string, diagnostics []Diagnostic) error {
	if path == "" {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(NormalizeDiagnostics(diagnostics), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadSnapshot(path string) ([]Diagnostic, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var diagnostics []Diagnostic
	if err := json.Unmarshal(data, &diagnostics); err != nil {
		return nil, err
	}
	return NormalizeDiagnostics(diagnostics), nil
}

func DiagnosticsFromPublishDiagnostics(data []byte) ([]Diagnostic, error) {
	var params PublishDiagnosticsParams
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.URI) == "" {
		var wrapper struct {
			Params PublishDiagnosticsParams `json:"params"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, err
		}
		params = wrapper.Params
	}
	if strings.TrimSpace(params.URI) == "" {
		return nil, fmt.Errorf("publishDiagnostics uri is required")
	}
	filePath := URIToPath(params.URI)
	out := make([]Diagnostic, 0, len(params.Diagnostics))
	for _, diagnostic := range params.Diagnostics {
		out = append(out, Diagnostic{
			FilePath: filePath,
			Range:    diagnostic.Range,
			Severity: diagnosticSeverity(diagnostic.Severity),
			Code:     diagnosticCode(diagnostic.Code),
			Source:   diagnostic.Source,
			Message:  diagnostic.Message,
		})
	}
	return NormalizeDiagnostics(out), nil
}

func ApplyDiagnosticsUpdate(existing []Diagnostic, update []Diagnostic) []Diagnostic {
	normalizedUpdate := NormalizeDiagnostics(update)
	if len(normalizedUpdate) == 0 {
		return NormalizeDiagnostics(existing)
	}
	updatedFiles := map[string]struct{}{}
	for _, diagnostic := range normalizedUpdate {
		updatedFiles[diagnostic.FilePath] = struct{}{}
	}
	out := make([]Diagnostic, 0, len(existing)+len(normalizedUpdate))
	for _, diagnostic := range NormalizeDiagnostics(existing) {
		if _, ok := updatedFiles[diagnostic.FilePath]; ok {
			continue
		}
		out = append(out, diagnostic)
	}
	out = append(out, normalizedUpdate...)
	return NormalizeDiagnostics(out)
}

func URIToPath(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return raw
	}
	path := parsed.Path
	if parsed.Host != "" && parsed.Host != "localhost" {
		path = "//" + parsed.Host + path
	}
	return path
}

func NormalizeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		diagnostic.FilePath = normalizePath(diagnostic.FilePath)
		diagnostic.Severity = NormalizeSeverity(diagnostic.Severity)
		diagnostic.Source = strings.TrimSpace(diagnostic.Source)
		diagnostic.Code = strings.TrimSpace(diagnostic.Code)
		diagnostic.Message = strings.TrimSpace(diagnostic.Message)
		if diagnostic.FilePath == "" || diagnostic.Message == "" {
			continue
		}
		out = append(out, diagnostic)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		if out[i].Range.Start.Character != out[j].Range.Start.Character {
			return out[i].Range.Start.Character < out[j].Range.Start.Character
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func FilterDiagnostics(diagnostics []Diagnostic, filter Filter) ([]Diagnostic, bool) {
	filePath := normalizePath(filter.FilePath)
	severity := NormalizeSeverity(filter.Severity)
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range NormalizeDiagnostics(diagnostics) {
		if filePath != "" && diagnostic.FilePath != filePath {
			continue
		}
		if severity != "" && diagnostic.Severity != severity {
			continue
		}
		out = append(out, diagnostic)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = len(out)
	}
	if limit < len(out) {
		return append([]Diagnostic(nil), out[:limit]...), true
	}
	return out, false
}

func diagnosticSeverity(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return NormalizeSeverity(v)
	case float64:
		if v == float64(int(v)) {
			return NormalizeSeverity(strconv.Itoa(int(v)))
		}
		return NormalizeSeverity(strconv.FormatFloat(v, 'f', -1, 64))
	case int:
		return NormalizeSeverity(strconv.Itoa(v))
	default:
		return NormalizeSeverity(fmt.Sprint(v))
	}
}

func diagnosticCode(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int(v)) {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func NormalizeSeverity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none":
		return ""
	case "1", "error", "err":
		return "error"
	case "2", "warning", "warn":
		return "warning"
	case "3", "information", "info":
		return "info"
	case "4", "hint":
		return "hint"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func IsKnownSeverity(raw string) bool {
	switch NormalizeSeverity(raw) {
	case "", "error", "warning", "info", "hint":
		return true
	default:
		return false
	}
}

func normalizePath(raw string) string {
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw)))
	if path == "." {
		return ""
	}
	return strings.TrimPrefix(path, "./")
}

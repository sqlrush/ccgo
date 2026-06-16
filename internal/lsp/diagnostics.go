package lsp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

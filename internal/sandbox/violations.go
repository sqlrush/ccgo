package sandbox

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Violation records a single sandbox denial event (SBX-54).
// The fields mirror the information emitted in macOS seatbelt
// <sandbox_violations> XML output.
type Violation struct {
	// Rule is the seatbelt/landlock rule that was denied (e.g. "file-write*").
	Rule string
	// Path is the filesystem path involved, if any.
	Path string
	// Operation is the operation that was blocked (e.g. "write", "connect").
	Operation string
	// At is when the violation was recorded.
	At time.Time
}

// String returns a human-readable single-line description of the violation.
func (v Violation) String() string {
	if v.Path != "" {
		return fmt.Sprintf("sandbox denied %s on %q (rule: %s)", v.Operation, v.Path, v.Rule)
	}
	return fmt.Sprintf("sandbox denied %s (rule: %s)", v.Operation, v.Rule)
}

// ViolationStore records sandbox denial events per command (SBX-54).
// It is concurrency-safe for use across goroutines (-race clean).
type ViolationStore struct {
	mu         sync.Mutex
	violations []Violation
}

// Record appends a violation event to the store.
func (s *ViolationStore) Record(v Violation) {
	if v.At.IsZero() {
		v.At = time.Now()
	}
	s.mu.Lock()
	s.violations = append(s.violations, v)
	s.mu.Unlock()
}

// All returns a snapshot of all recorded violations (oldest first).
// The returned slice is a copy; callers may mutate it freely.
func (s *ViolationStore) All() []Violation {
	s.mu.Lock()
	out := make([]Violation, len(s.violations))
	copy(out, s.violations)
	s.mu.Unlock()
	return out
}

// Reset clears all recorded violations.
func (s *ViolationStore) Reset() {
	s.mu.Lock()
	s.violations = nil
	s.mu.Unlock()
}

// IgnoreViolationsConfig specifies which violations to suppress from reporting.
// Keyed by rule name; values are path-prefix patterns to ignore.
// An empty slice for a rule means "ignore all violations of that rule".
//
// Mirrors CC's ignoreViolations setting (sandbox-adapter.ts:375).
type IgnoreViolationsConfig map[string][]string

// ShouldIgnore reports whether v should be suppressed according to cfg.
// If cfg is nil, nothing is suppressed.
func (cfg IgnoreViolationsConfig) ShouldIgnore(v Violation) bool {
	if cfg == nil {
		return false
	}
	patterns, ok := cfg[v.Rule]
	if !ok {
		return false
	}
	if len(patterns) == 0 {
		return true // ignore all violations of this rule
	}
	for _, pat := range patterns {
		if strings.HasPrefix(v.Path, pat) || v.Path == pat {
			return true
		}
	}
	return false
}

// sandboxViolationTag is the XML element emitted by macOS seatbelt in stderr.
// Format: <sandbox_violations>…denial lines…</sandbox_violations>
var sandboxViolationTagRE = regexp.MustCompile(`(?s)<sandbox_violations>.*?</sandbox_violations>`)

// RemoveSandboxViolationTags strips all <sandbox_violations>…</sandbox_violations>
// blocks from text for clean UI display. Mirrors CC's removeSandboxViolationTags
// (sandbox-ui-utils.ts:10). SBX-56.
func RemoveSandboxViolationTags(text string) string {
	return sandboxViolationTagRE.ReplaceAllString(text, "")
}

// AnnotateStderrWithSandboxFailures parses macOS seatbelt <sandbox_violations>
// XML from stderr, records the violations in store (if non-nil), and returns
// a decorated version of stderr with a human-readable summary appended.
// Suppressed violations (cfg) are not annotated.
// Mirrors CC's annotateStderrWithSandboxFailures (sandbox-adapter.ts:918). SBX-55.
func AnnotateStderrWithSandboxFailures(
	stderr string,
	store *ViolationStore,
	cfg IgnoreViolationsConfig,
) string {
	matches := sandboxViolationTagRE.FindAllString(stderr, -1)
	if len(matches) == 0 {
		return stderr
	}

	var active []Violation
	for _, block := range matches {
		vv := parseViolationBlock(block)
		for _, v := range vv {
			if !cfg.ShouldIgnore(v) {
				active = append(active, v)
				if store != nil {
					store.Record(v)
				}
			}
		}
	}

	annotated := RemoveSandboxViolationTags(stderr)
	if len(active) == 0 {
		return annotated
	}

	var sb strings.Builder
	sb.WriteString(annotated)
	if !strings.HasSuffix(strings.TrimRight(annotated, "\n"), "") {
		sb.WriteByte('\n')
	}
	sb.WriteString("\n[sandbox] blocked operations:\n")
	for _, v := range active {
		sb.WriteString("  • ")
		sb.WriteString(v.String())
		sb.WriteByte('\n')
	}
	return sb.String()
}

// parseViolationBlock extracts Violation structs from one <sandbox_violations> block.
// macOS seatbelt emits lines like:
//
//	Sandbox: <executable>(<pid>) deny(1) file-write-data /path/to/file
//
// We do best-effort parsing; unrecognised lines produce an Operation-only violation.
func parseViolationBlock(block string) []Violation {
	inner := block
	if i := strings.Index(inner, ">"); i >= 0 {
		inner = inner[i+1:]
	}
	if i := strings.LastIndex(inner, "<"); i >= 0 {
		inner = inner[:i]
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil
	}

	var out []Violation
	for _, line := range strings.Split(inner, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v := parseSeatbeltLine(line)
		out = append(out, v)
	}
	return out
}

// seatbeltDenyRE matches macOS seatbelt denial lines:
//
//	Sandbox: execname(pid) deny(n) rule-name /optional/path
var seatbeltDenyRE = regexp.MustCompile(`(?i)sandbox:\s*\S+\s+deny\(\d+\)\s+(\S+)(?:\s+(.+))?`)

func parseSeatbeltLine(line string) Violation {
	m := seatbeltDenyRE.FindStringSubmatch(line)
	if m == nil {
		return Violation{Rule: "unknown", Operation: strings.TrimSpace(line)}
	}
	rule := m[1]
	path := strings.TrimSpace(m[2])
	// Derive a friendly operation name from the rule (file-write-data → write).
	op := ruleToOperation(rule)
	return Violation{Rule: rule, Operation: op, Path: path}
}

func ruleToOperation(rule string) string {
	switch {
	case strings.HasPrefix(rule, "file-write"):
		return "write"
	case strings.HasPrefix(rule, "file-read"):
		return "read"
	case strings.HasPrefix(rule, "network"):
		return "network"
	case strings.HasPrefix(rule, "process"):
		return "exec"
	default:
		return rule
	}
}

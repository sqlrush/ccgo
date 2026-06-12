package permissions

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type FileOperationType string

const (
	FileOperationRead   FileOperationType = "read"
	FileOperationWrite  FileOperationType = "write"
	FileOperationCreate FileOperationType = "create"
)

type PathCheckResult struct {
	Allowed     bool
	Reason      string
	BlockedPath string
	Source      contracts.PermissionRuleSource
}

type InternalPathContext struct {
	ProjectDir        string
	ProjectTempDir    string
	SessionMemoryDir  string
	ToolResultsDir    string
	ScratchpadDir     string
	ScratchpadEnabled bool
	PlansDir          string
	PlanSlug          string
	JobDir            string
	JobsRoot          string
	AgentMemoryDir    string
	AutoMemoryDir     string
	TasksDir          string
	TeamsDir          string
	LaunchConfigPath  string
}

func ExpandTilde(path string) string {
	return platform.ExpandPath(path)
}

func PathsForPermissionCheck(path string) []string {
	path = platform.ExpandPath(path)
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	out := []string{path}
	seen := map[string]struct{}{normalizePathForComparison(path): {}}
	add := func(candidate string) {
		if candidate == "" {
			return
		}
		if !filepath.IsAbs(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				candidate = abs
			}
		}
		key := normalizePathForComparison(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	if isUNCPath(path) {
		return out
	}

	current := path
	visited := map[string]struct{}{}
	for depth := 0; depth < 40; depth++ {
		key := normalizePathForComparison(current)
		if _, ok := visited[key]; ok {
			break
		}
		visited[key] = struct{}{}
		info, err := os.Lstat(current)
		if err != nil {
			if current == path {
				add(resolveDeepestExistingAncestor(path))
			}
			break
		}
		if info.Mode()&os.ModeSymlink == 0 {
			break
		}
		target, err := os.Readlink(current)
		if err != nil {
			break
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		target = filepath.Clean(target)
		add(target)
		current = target
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		add(resolved)
	}
	return out
}

func PathInWorkingPath(path string, workingPath string) bool {
	path = normalizePathForComparison(path)
	workingPath = normalizePathForComparison(workingPath)
	if path == workingPath {
		return true
	}
	if !strings.HasSuffix(workingPath, string(filepath.Separator)) {
		workingPath += string(filepath.Separator)
	}
	return strings.HasPrefix(path, workingPath)
}

func IsPathAllowed(path string, cwd string, additional map[string]contracts.PermissionRuleSource) PathCheckResult {
	if cwd == "" {
		cwd = "."
	}
	resolved := expandPathForCwd(path, cwd)
	pathsToCheck := PathsForPermissionCheck(resolved)
	allowed := map[string]contracts.PermissionRuleSource{
		expandPathForCwd(cwd, ""): contracts.PermissionSourceSession,
	}
	for dir, source := range additional {
		allowed[expandPathForCwd(dir, cwd)] = source
	}
	if source, ok := pathsInAllowedWorkingPaths(pathsToCheck, allowed); ok {
		reason := "path is inside working directory"
		if source != contracts.PermissionSourceSession {
			reason = "path is inside additional working directory"
		}
		return PathCheckResult{Allowed: true, Reason: reason, Source: source}
	}
	return PathCheckResult{Allowed: false, Reason: "path is outside allowed working directories", BlockedPath: resolved}
}

func ValidatePath(path string, cwd string, operation FileOperationType, additional map[string]contracts.PermissionRuleSource) PathCheckResult {
	return ValidatePathWithInternalContext(path, cwd, operation, additional, InternalPathContext{})
}

func ValidatePathWithInternalContext(path string, cwd string, operation FileOperationType, additional map[string]contracts.PermissionRuleSource, internal InternalPathContext) PathCheckResult {
	if cwd == "" {
		cwd = "."
	}
	if isUNCPath(platform.ExpandPath(path)) {
		return PathCheckResult{Allowed: false, Reason: "path appears to be a UNC/network path", BlockedPath: path}
	}
	resolved := expandPathForCwd(path, cwd)
	pathsToCheck := PathsForPermissionCheck(resolved)
	for _, item := range pathsToCheck {
		if isUNCPath(item) {
			return PathCheckResult{Allowed: false, Reason: "path appears to be a UNC/network path", BlockedPath: item}
		}
	}
	if operation != FileOperationRead {
		if internalResult := CheckEditableInternalPath(resolved, internal); internalResult.Allowed {
			return internalResult
		}
		if IsDangerousRemovalPath(resolved) {
			return PathCheckResult{Allowed: false, Reason: "path targets a dangerous filesystem root", BlockedPath: resolved}
		}
		if safety := CheckPathSafetyForAutoEdit(resolved, pathsToCheck); !safety.Safe {
			return PathCheckResult{Allowed: false, Reason: safety.Message, BlockedPath: resolved}
		}
	} else {
		if internalResult := CheckReadableInternalPath(resolved, internal); internalResult.Allowed {
			return internalResult
		}
	}
	return IsPathAllowed(resolved, cwd, additional)
}

func CheckEditableInternalPath(path string, internal InternalPathContext) PathCheckResult {
	normalized := filepath.Clean(platform.ExpandPath(path))
	if isSessionPlanFile(normalized, internal) {
		return PathCheckResult{Allowed: true, Reason: "plan files for current session are allowed for writing", Source: contracts.PermissionSourceSession}
	}
	if internal.ScratchpadEnabled && pathInOptionalDir(normalized, internal.ScratchpadDir) {
		return PathCheckResult{Allowed: true, Reason: "scratchpad files for current session are allowed for writing", Source: contracts.PermissionSourceSession}
	}
	if jobDirAllows(normalized, internal) {
		return PathCheckResult{Allowed: true, Reason: "job directory files for current job are allowed for writing", Source: contracts.PermissionSourceSession}
	}
	if pathInOptionalDir(normalized, internal.AgentMemoryDir) {
		return PathCheckResult{Allowed: true, Reason: "agent memory files are allowed for writing", Source: contracts.PermissionSourceSession}
	}
	if pathInOptionalDir(normalized, internal.AutoMemoryDir) {
		return PathCheckResult{Allowed: true, Reason: "auto memory files are allowed for writing", Source: contracts.PermissionSourceSession}
	}
	if internal.LaunchConfigPath != "" && normalizePathForComparison(normalized) == normalizePathForComparison(platform.ExpandPath(internal.LaunchConfigPath)) {
		return PathCheckResult{Allowed: true, Reason: "preview launch config is allowed for writing", Source: contracts.PermissionSourceSession}
	}
	return PathCheckResult{Allowed: false}
}

func CheckReadableInternalPath(path string, internal InternalPathContext) PathCheckResult {
	normalized := filepath.Clean(platform.ExpandPath(path))
	switch {
	case pathInOptionalDir(normalized, internal.SessionMemoryDir):
		return PathCheckResult{Allowed: true, Reason: "session memory files are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.ProjectDir):
		return PathCheckResult{Allowed: true, Reason: "project directory files are allowed for reading", Source: contracts.PermissionSourceSession}
	case isSessionPlanFile(normalized, internal):
		return PathCheckResult{Allowed: true, Reason: "plan files for current session are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.ToolResultsDir):
		return PathCheckResult{Allowed: true, Reason: "tool result files are allowed for reading", Source: contracts.PermissionSourceSession}
	case internal.ScratchpadEnabled && pathInOptionalDir(normalized, internal.ScratchpadDir):
		return PathCheckResult{Allowed: true, Reason: "scratchpad files for current session are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.ProjectTempDir):
		return PathCheckResult{Allowed: true, Reason: "project temp directory files are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.AgentMemoryDir):
		return PathCheckResult{Allowed: true, Reason: "agent memory files are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.AutoMemoryDir):
		return PathCheckResult{Allowed: true, Reason: "auto memory files are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.TasksDir):
		return PathCheckResult{Allowed: true, Reason: "task files are allowed for reading", Source: contracts.PermissionSourceSession}
	case pathInOptionalDir(normalized, internal.TeamsDir):
		return PathCheckResult{Allowed: true, Reason: "team files are allowed for reading", Source: contracts.PermissionSourceSession}
	default:
		return PathCheckResult{Allowed: false}
	}
}

type PathSafetyResult struct {
	Safe                 bool
	Message              string
	ClassifierApprovable bool
}

func CheckPathSafetyForAutoEdit(path string, pathsToCheck []string) PathSafetyResult {
	if len(pathsToCheck) == 0 {
		pathsToCheck = PathsForPermissionCheck(path)
	}
	for _, item := range pathsToCheck {
		if HasSuspiciousWindowsPathPattern(item) {
			return PathSafetyResult{Safe: false, Message: "path contains a suspicious Windows path pattern", ClassifierApprovable: false}
		}
	}
	for _, item := range pathsToCheck {
		if isClaudeConfigFilePath(item) {
			return PathSafetyResult{Safe: false, Message: "path targets Claude configuration files", ClassifierApprovable: true}
		}
	}
	for _, item := range pathsToCheck {
		if IsDangerousFilePathToAutoEdit(item) {
			return PathSafetyResult{Safe: false, Message: "path targets a sensitive file or directory", ClassifierApprovable: true}
		}
	}
	return PathSafetyResult{Safe: true}
}

func HasSuspiciousWindowsPathPattern(path string) bool {
	if runtime.GOOS == "windows" && len(path) > 2 {
		if idx := strings.Index(path[2:], ":"); idx >= 0 {
			return true
		}
	}
	if regexp.MustCompile(`~\d`).MatchString(path) {
		return true
	}
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, "//?/") || strings.HasPrefix(path, "//./") {
		return true
	}
	if regexp.MustCompile(`[.\s]+$`).MatchString(path) {
		return true
	}
	if regexp.MustCompile(`(?i)\.(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`).MatchString(path) {
		return true
	}
	return regexp.MustCompile(`(^|/|\\)\.{3,}(/|\\|$)`).MatchString(path)
}

func IsDangerousFilePathToAutoEdit(path string) bool {
	if isUNCPath(path) {
		return true
	}
	normalized := normalizeCaseForComparison(filepath.Clean(platform.ExpandPath(path)))
	segments := splitPathSegments(normalized)
	for i, segment := range segments {
		switch segment {
		case ".git", ".vscode", ".idea":
			return true
		case ".claude":
			if i+1 < len(segments) && segments[i+1] == "worktrees" {
				continue
			}
			return true
		}
	}
	if len(segments) == 0 {
		return false
	}
	switch segments[len(segments)-1] {
	case ".gitconfig", ".gitmodules", ".bashrc", ".bash_profile", ".zshrc", ".zprofile", ".profile", ".ripgreprc", ".mcp.json", ".claude.json":
		return true
	default:
		return false
	}
}

func isClaudeConfigFilePath(path string) bool {
	normalized := normalizeCaseForComparison(filepath.Clean(platform.ExpandPath(path)))
	sep := string(filepath.Separator)
	return strings.HasSuffix(normalized, sep+".claude"+sep+"settings.json") ||
		strings.HasSuffix(normalized, sep+".claude"+sep+"settings.local.json") ||
		strings.Contains(normalized, sep+".claude"+sep+"commands"+sep) ||
		strings.Contains(normalized, sep+".claude"+sep+"agents"+sep) ||
		strings.Contains(normalized, sep+".claude"+sep+"skills"+sep)
}

func pathsInAllowedWorkingPaths(paths []string, allowed map[string]contracts.PermissionRuleSource) (contracts.PermissionRuleSource, bool) {
	var matchedSource contracts.PermissionRuleSource
	for _, path := range paths {
		pathMatched := false
		for dir, source := range allowed {
			for _, workingPath := range PathsForPermissionCheck(dir) {
				if PathInWorkingPath(path, workingPath) {
					if matchedSource == "" {
						matchedSource = source
					}
					pathMatched = true
					break
				}
			}
			if pathMatched {
				break
			}
		}
		if !pathMatched {
			return "", false
		}
	}
	if matchedSource == "" {
		matchedSource = contracts.PermissionSourceSession
	}
	return matchedSource, true
}

func isSessionPlanFile(path string, internal InternalPathContext) bool {
	if internal.PlansDir == "" || internal.PlanSlug == "" {
		return false
	}
	prefix := filepath.Join(platform.ExpandPath(internal.PlansDir), internal.PlanSlug)
	clean := filepath.Clean(platform.ExpandPath(path))
	return strings.HasPrefix(normalizePathForComparison(clean), normalizePathForComparison(prefix)) && strings.HasSuffix(strings.ToLower(clean), ".md")
}

func jobDirAllows(path string, internal InternalPathContext) bool {
	if internal.JobDir == "" || internal.JobsRoot == "" {
		return false
	}
	jobForms := PathsForPermissionCheck(internal.JobDir)
	rootForms := PathsForPermissionCheck(internal.JobsRoot)
	for _, jobForm := range jobForms {
		underRoot := false
		for _, rootForm := range rootForms {
			if PathInWorkingPath(jobForm, rootForm) {
				underRoot = true
				break
			}
		}
		if !underRoot {
			return false
		}
	}
	for _, targetForm := range PathsForPermissionCheck(path) {
		insideJob := false
		for _, jobForm := range jobForms {
			if PathInWorkingPath(targetForm, jobForm) {
				insideJob = true
				break
			}
		}
		if !insideJob {
			return false
		}
	}
	return true
}

func pathInOptionalDir(path string, dir string) bool {
	if dir == "" {
		return false
	}
	return PathInWorkingPath(path, platform.ExpandPath(dir))
}

func expandPathForCwd(path string, cwd string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = platform.ExpandPath(path)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if cwd == "" {
		cwd = "."
	}
	return filepath.Clean(filepath.Join(platform.ExpandPath(cwd), path))
}

func resolveDeepestExistingAncestor(path string) string {
	if isUNCPath(path) {
		return ""
	}
	clean := filepath.Clean(path)
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			parts := append([]string{resolved}, reverseStrings(suffix)...)
			return filepath.Join(parts...)
		}
		parent := filepath.Dir(clean)
		if parent == clean {
			return ""
		}
		suffix = append(suffix, filepath.Base(clean))
		clean = parent
	}
}

func reverseStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}

func splitPathSegments(path string) []string {
	normalized := strings.ReplaceAll(path, "\\", string(filepath.Separator))
	raw := strings.Split(normalized, string(filepath.Separator))
	out := raw[:0]
	for _, item := range raw {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func normalizeCaseForComparison(path string) string {
	return strings.ToLower(path)
}

func IsDangerousRemovalPath(path string) bool {
	resolved := normalizePathForComparison(platform.ExpandPath(path))
	home := normalizePathForComparison(platform.ExpandPath("~"))
	dangerous := []string{
		normalizePathForComparison(filepath.VolumeName(resolved) + string(filepath.Separator)),
		normalizePathForComparison("/"),
		home,
		normalizePathForComparison(filepath.Join(home, ".ssh")),
		normalizePathForComparison(filepath.Join(home, ".config")),
		normalizePathForComparison(platform.ClaudeHomeDir()),
	}
	for _, item := range dangerous {
		if item != "" && resolved == item {
			return true
		}
	}
	return false
}

func normalizePathForComparison(path string) string {
	clean := filepath.Clean(path)
	return strings.ToLower(clean)
}

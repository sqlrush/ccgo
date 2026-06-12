package permissions

import (
	"fmt"
	"sort"

	"ccgo/internal/contracts"
)

type Engine struct {
	mode    contracts.PermissionMode
	context contracts.PermissionContext
	rules   []Rule
}

type SettingsSource struct {
	Source      contracts.PermissionRuleSource
	Permissions *contracts.PermissionsSetting
	Sandbox     map[string]any
}

func NewEngine(context contracts.PermissionContext, rules ...Rule) Engine {
	mode := context.Mode
	if mode == "" {
		mode = contracts.PermissionDefault
	}
	context.Mode = mode
	return Engine{mode: mode, context: context, rules: append([]Rule(nil), rules...)}
}

func NewEngineFromSettings(source contracts.PermissionRuleSource, settings *contracts.PermissionsSetting) (Engine, error) {
	context := ContextFromSettings(source, settings)
	rules, err := RulesFromSettings(source, settings)
	if err != nil {
		return Engine{}, err
	}
	return NewEngine(context, rules...), nil
}

func NewEngineFromSettingsSources(managedRulesOnly bool, sources ...SettingsSource) (Engine, error) {
	context := contracts.PermissionContext{
		Mode:                         contracts.PermissionDefault,
		AdditionalWorkingDirectories: map[string]contracts.PermissionRuleSource{},
		AlwaysAllowRules:             map[contracts.PermissionRuleSource][]string{},
		AlwaysDenyRules:              map[contracts.PermissionRuleSource][]string{},
		AlwaysAskRules:               map[contracts.PermissionRuleSource][]string{},
		BypassAvailable:              true,
		AutoAvailable:                true,
	}
	var rules []Rule
	for _, source := range sources {
		if source.Permissions == nil && source.Sandbox == nil {
			continue
		}
		sourceContext := ContextFromSettings(source.Source, source.Permissions)
		if allowUnsandboxed, ok := sandboxAllowUnsandboxedCommands(source.Sandbox); ok {
			context.AllowUnsandboxedCommands = &allowUnsandboxed
		}
		if filesystem, ok := sandboxFilesystemPolicy(source.Sandbox); ok {
			context.SandboxFilesystem = mergeSandboxFilesystemPolicy(context.SandboxFilesystem, filesystem)
		}
		if source.Permissions != nil && source.Permissions.DefaultMode != "" {
			context.Mode = sourceContext.Mode
		}
		if !sourceContext.BypassAvailable {
			context.BypassAvailable = false
		}
		if !sourceContext.AutoAvailable {
			context.AutoAvailable = false
		}
		for dir, dirSource := range sourceContext.AdditionalWorkingDirectories {
			context.AdditionalWorkingDirectories[dir] = dirSource
		}
		if source.Permissions == nil {
			continue
		}
		if managedRulesOnly && source.Source != contracts.PermissionSourcePolicySettings {
			continue
		}
		sourceRules, err := RulesFromSettings(source.Source, source.Permissions)
		if err != nil {
			return Engine{}, err
		}
		rules = append(rules, sourceRules...)
		if len(source.Permissions.Allow) > 0 {
			context.AlwaysAllowRules[source.Source] = append([]string(nil), source.Permissions.Allow...)
		}
		if len(source.Permissions.Deny) > 0 {
			context.AlwaysDenyRules[source.Source] = append([]string(nil), source.Permissions.Deny...)
		}
		if len(source.Permissions.Ask) > 0 {
			context.AlwaysAskRules[source.Source] = append([]string(nil), source.Permissions.Ask...)
		}
	}
	return NewEngine(context, rules...), nil
}

func ContextFromSettings(source contracts.PermissionRuleSource, settings *contracts.PermissionsSetting) contracts.PermissionContext {
	context := contracts.PermissionContext{
		Mode:                         contracts.PermissionDefault,
		AdditionalWorkingDirectories: map[string]contracts.PermissionRuleSource{},
		AlwaysAllowRules:             map[contracts.PermissionRuleSource][]string{},
		AlwaysDenyRules:              map[contracts.PermissionRuleSource][]string{},
		AlwaysAskRules:               map[contracts.PermissionRuleSource][]string{},
		BypassAvailable:              true,
		AutoAvailable:                true,
	}
	if settings == nil {
		return context
	}
	if settings.DefaultMode != "" {
		context.Mode = settings.DefaultMode
	}
	if truthy(settings.DisableBypassMode) {
		context.BypassAvailable = false
	}
	if truthy(settings.DisableAutoMode) {
		context.AutoAvailable = false
	}
	for _, dir := range settings.AdditionalDirectories {
		context.AdditionalWorkingDirectories[dir] = source
	}
	if len(settings.Allow) > 0 {
		context.AlwaysAllowRules[source] = append([]string(nil), settings.Allow...)
	}
	if len(settings.Deny) > 0 {
		context.AlwaysDenyRules[source] = append([]string(nil), settings.Deny...)
	}
	if len(settings.Ask) > 0 {
		context.AlwaysAskRules[source] = append([]string(nil), settings.Ask...)
	}
	return context
}

func (e Engine) Mode() contracts.PermissionMode {
	return e.mode
}

func (e Engine) Context() contracts.PermissionContext {
	return e.context
}

func (e Engine) Rules() []Rule {
	out := append([]Rule(nil), e.rules...)
	return out
}

func (e *Engine) AddRule(rule Rule) {
	e.rules = append(e.rules, rule)
}

func (e Engine) Decide(req Request) contracts.PermissionDecision {
	req.ToolName = normalizeToolName(req.ToolName)
	if req.ToolName == "" {
		return decision(contracts.PermissionDeny, req, "missing tool name", "")
	}

	matched := e.matchingRules(req)
	if rule, ok := firstByBehavior(matched, contracts.PermissionDeny); ok {
		return decision(contracts.PermissionDeny, req, fmt.Sprintf("denied by %s rule %q", rule.Source, rule.String()), "")
	}
	if req.DangerouslyDisableSandbox {
		return e.sandboxOverrideDecision(req)
	}
	if pathDecision, ok := e.pathDecision(req); ok {
		return pathDecision
	}
	if rule, ok := firstByBehavior(matched, contracts.PermissionAllow); ok {
		return decision(contracts.PermissionAllow, req, fmt.Sprintf("allowed by %s rule %q", rule.Source, rule.String()), "")
	}
	if rule, ok := firstByBehavior(matched, contracts.PermissionAsk); ok {
		return decision(contracts.PermissionAsk, req, fmt.Sprintf("ask required by %s rule %q", rule.Source, rule.String()), "")
	}

	switch e.mode {
	case contracts.PermissionBypassPermissions:
		if e.context.BypassAvailable {
			return decision(contracts.PermissionAllow, req, "bypassPermissions mode", "")
		}
		return decision(contracts.PermissionAsk, req, "bypassPermissions mode is disabled", "")
	case contracts.PermissionDontAsk:
		if req.ReadOnly {
			return decision(contracts.PermissionAllow, req, "read-only tool in dontAsk mode", "")
		}
		return decision(contracts.PermissionDeny, req, "dontAsk mode denies unmatched write or destructive tools", "")
	case contracts.PermissionAcceptEdits:
		if req.ReadOnly || req.WritesFiles {
			return decision(contracts.PermissionAllow, req, "acceptEdits mode", "")
		}
		return decision(contracts.PermissionAsk, req, "acceptEdits mode requires confirmation for non-file action", "")
	case contracts.PermissionPlan:
		if req.ReadOnly {
			return decision(contracts.PermissionAllow, req, "read-only tool in plan mode", "")
		}
		return decision(contracts.PermissionAsk, req, "plan mode requires confirmation for mutating tools", "")
	case contracts.PermissionAuto:
		if e.context.AutoAvailable && req.ReadOnly && !req.Destructive {
			return decision(contracts.PermissionAllow, req, "auto mode allowed read-only tool", "")
		}
		if req.Destructive {
			return decision(contracts.PermissionAsk, req, "auto mode requires confirmation for destructive tool", "")
		}
		return decision(contracts.PermissionAsk, req, "auto mode requires confirmation", "")
	default:
		if req.ReadOnly {
			return decision(contracts.PermissionAllow, req, "default mode allowed read-only tool", "")
		}
		return decision(contracts.PermissionAsk, req, "default mode requires confirmation", "")
	}
}

func (e Engine) sandboxOverrideDecision(req Request) contracts.PermissionDecision {
	if e.context.AllowUnsandboxedCommands != nil && !*e.context.AllowUnsandboxedCommands {
		return decision(contracts.PermissionDeny, req, "sandbox.allowUnsandboxedCommands disables sandbox override", "")
	}
	if e.mode == contracts.PermissionBypassPermissions && e.context.BypassAvailable {
		return decision(contracts.PermissionAllow, req, "sandbox override allowed in bypassPermissions mode", "")
	}
	if e.mode == contracts.PermissionDontAsk {
		return decision(contracts.PermissionDeny, req, "sandbox override requires confirmation", "")
	}
	return decision(contracts.PermissionAsk, req, "sandbox override requires confirmation", "")
}

func sandboxAllowUnsandboxedCommands(sandbox map[string]any) (bool, bool) {
	if sandbox == nil {
		return false, false
	}
	value, ok := sandbox["allowUnsandboxedCommands"]
	if !ok {
		return false, false
	}
	allow, ok := value.(bool)
	return allow, ok
}

func sandboxFilesystemPolicy(sandbox map[string]any) (contracts.SandboxFilesystemPolicy, bool) {
	if sandbox == nil {
		return contracts.SandboxFilesystemPolicy{}, false
	}
	raw, ok := sandbox["filesystem"]
	if !ok {
		return contracts.SandboxFilesystemPolicy{}, false
	}
	filesystem, ok := raw.(map[string]any)
	if !ok {
		return contracts.SandboxFilesystemPolicy{}, false
	}
	policy := contracts.SandboxFilesystemPolicy{
		AllowWrite: sandboxStringSlice(filesystem["allowWrite"]),
		DenyWrite:  sandboxStringSlice(filesystem["denyWrite"]),
		DenyRead:   sandboxStringSlice(filesystem["denyRead"]),
		AllowRead:  sandboxStringSlice(filesystem["allowRead"]),
	}
	if len(policy.AllowWrite) == 0 && len(policy.DenyWrite) == 0 && len(policy.DenyRead) == 0 && len(policy.AllowRead) == 0 {
		return contracts.SandboxFilesystemPolicy{}, false
	}
	return policy, true
}

func sandboxStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func mergeSandboxFilesystemPolicy(current *contracts.SandboxFilesystemPolicy, next contracts.SandboxFilesystemPolicy) *contracts.SandboxFilesystemPolicy {
	if current == nil {
		cp := next
		cp.AllowWrite = append([]string(nil), next.AllowWrite...)
		cp.DenyWrite = append([]string(nil), next.DenyWrite...)
		cp.DenyRead = append([]string(nil), next.DenyRead...)
		cp.AllowRead = append([]string(nil), next.AllowRead...)
		return &cp
	}
	out := *current
	out.AllowWrite = mergeUniqueStrings(out.AllowWrite, next.AllowWrite)
	out.DenyWrite = mergeUniqueStrings(out.DenyWrite, next.DenyWrite)
	out.DenyRead = mergeUniqueStrings(out.DenyRead, next.DenyRead)
	out.AllowRead = mergeUniqueStrings(out.AllowRead, next.AllowRead)
	return &out
}

func mergeUniqueStrings(a, b []string) []string {
	out := append([]string(nil), a...)
	seen := map[string]struct{}{}
	for _, item := range out {
		seen[item] = struct{}{}
	}
	for _, item := range b {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (e Engine) pathDecision(req Request) (contracts.PermissionDecision, bool) {
	if req.Path == "" {
		return contracts.PermissionDecision{}, false
	}
	cwd := req.WorkingDirectory
	if cwd == "" {
		cwd = "."
	}
	resolved := expandPathForCwd(req.Path, cwd)
	operation := FileOperationWrite
	if req.ReadOnly && !req.WritesFiles && !req.Destructive {
		operation = FileOperationRead
	}
	pathsToCheck := PathsForPermissionCheck(resolved)
	if sandboxDecision, ok := e.sandboxFilesystemPathDecision(req, resolved, pathsToCheck, operation); ok {
		return sandboxDecision, true
	}
	if operation == FileOperationRead {
		if internal := CheckReadableInternalPath(resolved, req.InternalPaths); internal.Allowed {
			return decision(contracts.PermissionAllow, req, internal.Reason, ""), true
		}
		return contracts.PermissionDecision{}, false
	}
	if internal := CheckEditableInternalPath(resolved, req.InternalPaths); internal.Allowed {
		return decision(contracts.PermissionAllow, req, internal.Reason, ""), true
	}
	if IsDangerousRemovalPath(resolved) {
		got := decision(contracts.PermissionAsk, req, "path targets a dangerous filesystem root", "")
		got.BlockedPath = resolved
		return got, true
	}
	if safety := CheckPathSafetyForAutoEdit(resolved, pathsToCheck); !safety.Safe {
		got := decision(contracts.PermissionAsk, req, safety.Message, "")
		got.BlockedPath = resolved
		return got, true
	}
	if sandboxAllowsWritePath(e.context.SandboxFilesystem, pathsToCheck, cwd) {
		return decision(contracts.PermissionAllow, req, "sandbox.filesystem.allowWrite allows path", ""), true
	}
	return contracts.PermissionDecision{}, false
}

func (e Engine) sandboxFilesystemPathDecision(req Request, resolved string, pathsToCheck []string, operation FileOperationType) (contracts.PermissionDecision, bool) {
	policy := e.context.SandboxFilesystem
	if policy == nil {
		return contracts.PermissionDecision{}, false
	}
	cwd := req.WorkingDirectory
	if cwd == "" {
		cwd = "."
	}
	if operation == FileOperationRead {
		if sandboxPathMatches(pathsToCheck, cwd, policy.DenyRead) && !sandboxPathMatches(pathsToCheck, cwd, policy.AllowRead) {
			got := decision(contracts.PermissionDeny, req, "sandbox.filesystem.denyRead blocks path", "")
			got.BlockedPath = resolved
			return got, true
		}
		return contracts.PermissionDecision{}, false
	}
	if sandboxPathMatches(pathsToCheck, cwd, policy.DenyWrite) {
		got := decision(contracts.PermissionDeny, req, "sandbox.filesystem.denyWrite blocks path", "")
		got.BlockedPath = resolved
		return got, true
	}
	return contracts.PermissionDecision{}, false
}

func sandboxAllowsWritePath(policy *contracts.SandboxFilesystemPolicy, pathsToCheck []string, cwd string) bool {
	return policy != nil && sandboxPathMatches(pathsToCheck, cwd, policy.AllowWrite)
}

func sandboxPathMatches(pathsToCheck []string, cwd string, configured []string) bool {
	if len(configured) == 0 {
		return false
	}
	for _, configuredPath := range configured {
		if configuredPath == "" {
			continue
		}
		expanded := expandPathForCwd(configuredPath, cwd)
		for _, candidate := range pathsToCheck {
			if PathInWorkingPath(candidate, expanded) {
				return true
			}
		}
	}
	return false
}

func (e Engine) ApplyUpdate(update contracts.PermissionUpdate) (Engine, error) {
	next := e
	switch update.Type {
	case "", "rules", "addRules":
		for _, value := range update.Rules {
			raw := PermissionRuleValueToString(value)
			source := contracts.PermissionSourceSession
			if update.Destination != "" {
				source = contracts.PermissionRuleSource(update.Destination)
			}
			rule, err := ParseRule(source, update.Behavior, raw)
			if err != nil {
				return Engine{}, err
			}
			next.rules = append(next.rules, rule)
		}
	case "replaceRules":
		source := contracts.PermissionSourceSession
		if update.Destination != "" {
			source = contracts.PermissionRuleSource(update.Destination)
		}
		filtered := next.rules[:0]
		for _, rule := range next.rules {
			if rule.Source == source && rule.Behavior == update.Behavior {
				continue
			}
			filtered = append(filtered, rule)
		}
		next.rules = filtered
		for _, value := range update.Rules {
			rule, err := ParseRule(source, update.Behavior, PermissionRuleValueToString(value))
			if err != nil {
				return Engine{}, err
			}
			next.rules = append(next.rules, rule)
		}
	case "removeRules":
		source := contracts.PermissionSourceSession
		if update.Destination != "" {
			source = contracts.PermissionRuleSource(update.Destination)
		}
		remove := map[string]struct{}{}
		for _, value := range update.Rules {
			remove[PermissionRuleValueToString(value)] = struct{}{}
		}
		filtered := next.rules[:0]
		for _, rule := range next.rules {
			if rule.Source == source && rule.Behavior == update.Behavior {
				if _, ok := remove[rule.String()]; ok {
					continue
				}
			}
			filtered = append(filtered, rule)
		}
		next.rules = filtered
	case "mode", "setMode":
		if update.Mode == "" {
			return Engine{}, fmt.Errorf("mode update missing mode")
		}
		next.mode = update.Mode
		next.context.Mode = update.Mode
	case "directories", "addDirectories":
		if next.context.AdditionalWorkingDirectories == nil {
			next.context.AdditionalWorkingDirectories = map[string]contracts.PermissionRuleSource{}
		}
		source := contracts.PermissionSourceSession
		if update.Destination != "" {
			source = contracts.PermissionRuleSource(update.Destination)
		}
		for _, dir := range update.Directories {
			next.context.AdditionalWorkingDirectories[dir] = source
		}
	case "removeDirectories":
		for _, dir := range update.Directories {
			delete(next.context.AdditionalWorkingDirectories, dir)
		}
	default:
		return Engine{}, fmt.Errorf("unsupported permission update type %q", update.Type)
	}
	return next, nil
}

func (e Engine) matchingRules(req Request) []Rule {
	var matches []Rule
	for _, rule := range e.rules {
		if rule.Matches(req) {
			matches = append(matches, rule)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return behaviorRank(matches[i].Behavior) < behaviorRank(matches[j].Behavior)
	})
	return matches
}

func firstByBehavior(rules []Rule, behavior contracts.PermissionBehavior) (Rule, bool) {
	for _, rule := range rules {
		if rule.Behavior == behavior {
			return rule, true
		}
	}
	return Rule{}, false
}

func behaviorRank(behavior contracts.PermissionBehavior) int {
	switch behavior {
	case contracts.PermissionDeny:
		return 0
	case contracts.PermissionAllow:
		return 1
	case contracts.PermissionAsk:
		return 2
	default:
		return 3
	}
}

func decision(behavior contracts.PermissionBehavior, req Request, reason string, message string) contracts.PermissionDecision {
	if message == "" {
		message = reason
	}
	return contracts.PermissionDecision{
		Behavior:       behavior,
		Message:        message,
		DecisionReason: map[string]any{"type": "other", "reason": reason},
		ToolUseID:      req.ToolUseID,
	}
}

func normalizeToolName(name string) string {
	return name
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "disable" || v == "disabled"
	default:
		return false
	}
}

package commands

import (
	"path/filepath"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	pluginpkg "ccgo/internal/plugins"
	"ccgo/internal/skills"
)

const loadedFromCommandsDeprecated = "commands_DEPRECATED"

type Sources struct {
	BundledSkillPrompts       []PromptTemplate
	BundledSkills             []contracts.Command
	BuiltinPluginSkillPrompts []PromptTemplate
	BuiltinPluginSkills       []contracts.Command
	ProjectSkillPrompts       []PromptTemplate
	ProjectSkills             []contracts.Command
	WorkflowCommands          []contracts.Command
	PluginCommands            []contracts.Command
	PluginSkillPrompts        []PromptTemplate
	PluginSkills              []contracts.Command
	DynamicSkillPrompts       []PromptTemplate
	DynamicSkills             []contracts.Command
	Builtins                  []contracts.Command
}

type Options struct {
	CWD                  string
	Sources              Sources
	DisableProjectSkills bool
	DisableBuiltins      bool
	Settings             contracts.Settings
	PolicySettings       contracts.Settings
}

type Registry struct {
	commands        []contracts.Command
	promptTemplates map[string]PromptTemplate
}

func Load(opts Options) Registry {
	sources := opts.Sources
	policy := effectivePolicySettings(opts.Settings, opts.PolicySettings)
	skillsLocked := config.IsRestrictedToPluginOnly(policy, config.CustomizationSurfaceSkills)
	if skillsLocked {
		sources.ProjectSkillPrompts = nil
		sources.ProjectSkills = nil
		sources.DynamicSkillPrompts = nil
		sources.DynamicSkills = nil
	} else if !opts.DisableProjectSkills && opts.CWD != "" && len(sources.ProjectSkills) == 0 && len(sources.ProjectSkillPrompts) == 0 {
		sources.ProjectSkillPrompts = loadProjectSkillPrompts(opts.CWD)
	}
	if opts.CWD != "" && len(sources.PluginCommands) == 0 && len(sources.PluginSkillPrompts) == 0 && len(sources.PluginSkills) == 0 {
		sources.PluginSkillPrompts, sources.PluginCommands = loadProjectPluginCommands(opts.CWD, opts.Settings)
	}
	if !opts.DisableBuiltins && sources.Builtins == nil {
		sources.Builtins = BuiltinCommands()
	}
	if config.IsRestrictedToPluginOnly(policy, config.CustomizationSurfaceAgents) {
		sources = sanitizeAgentRestrictedSources(sources)
	}
	return FromSources(sources)
}

func effectivePolicySettings(settings contracts.Settings, policySettings contracts.Settings) contracts.Settings {
	if policySettings.StrictPluginOnlyCustomization != nil {
		return policySettings
	}
	return settings
}

func sanitizeAgentRestrictedSources(sources Sources) Sources {
	sources.BundledSkillPrompts = sanitizeAgentRestrictedPromptTemplates(sources.BundledSkillPrompts)
	sources.BundledSkills = sanitizeAgentRestrictedCommands(sources.BundledSkills)
	sources.BuiltinPluginSkillPrompts = sanitizeAgentRestrictedPromptTemplates(sources.BuiltinPluginSkillPrompts)
	sources.BuiltinPluginSkills = sanitizeAgentRestrictedCommands(sources.BuiltinPluginSkills)
	sources.ProjectSkillPrompts = sanitizeAgentRestrictedPromptTemplates(sources.ProjectSkillPrompts)
	sources.ProjectSkills = sanitizeAgentRestrictedCommands(sources.ProjectSkills)
	sources.WorkflowCommands = sanitizeAgentRestrictedCommands(sources.WorkflowCommands)
	sources.PluginCommands = sanitizeAgentRestrictedCommands(sources.PluginCommands)
	sources.PluginSkillPrompts = sanitizeAgentRestrictedPromptTemplates(sources.PluginSkillPrompts)
	sources.PluginSkills = sanitizeAgentRestrictedCommands(sources.PluginSkills)
	sources.DynamicSkillPrompts = sanitizeAgentRestrictedPromptTemplates(sources.DynamicSkillPrompts)
	sources.DynamicSkills = sanitizeAgentRestrictedCommands(sources.DynamicSkills)
	sources.Builtins = sanitizeAgentRestrictedCommands(sources.Builtins)
	return sources
}

func sanitizeAgentRestrictedPromptTemplates(prompts []PromptTemplate) []PromptTemplate {
	if len(prompts) == 0 {
		return nil
	}
	out := make([]PromptTemplate, len(prompts))
	for i, prompt := range prompts {
		prompt = clonePromptTemplate(prompt)
		prompt.Command = sanitizeAgentRestrictedCommand(prompt.Command)
		out[i] = prompt
	}
	return out
}

func sanitizeAgentRestrictedCommands(commands []contracts.Command) []contracts.Command {
	if len(commands) == 0 {
		return nil
	}
	out := make([]contracts.Command, len(commands))
	for i, cmd := range commands {
		out[i] = sanitizeAgentRestrictedCommand(cloneCommand(cmd))
	}
	return out
}

func sanitizeAgentRestrictedCommand(cmd contracts.Command) contracts.Command {
	if config.IsAdminTrustedCustomizationSource(string(cmd.Source)) || config.IsAdminTrustedCustomizationSource(cmd.LoadedFrom) {
		return cmd
	}
	cmd.Context = ""
	cmd.Agent = ""
	cmd.Effort = ""
	return cmd
}

func FromSources(sources Sources) Registry {
	promptTemplates := map[string]PromptTemplate{}
	var base []contracts.Command
	base = appendPromptCommands(base, promptTemplates, sources.BundledSkillPrompts)
	base = append(base, cloneCommands(sources.BundledSkills)...)
	base = appendPromptCommands(base, promptTemplates, sources.BuiltinPluginSkillPrompts)
	base = append(base, cloneCommands(sources.BuiltinPluginSkills)...)
	base = appendPromptCommands(base, promptTemplates, sources.ProjectSkillPrompts)
	base = append(base, cloneCommands(sources.ProjectSkills)...)
	base = append(base, cloneCommands(sources.WorkflowCommands)...)
	base = append(base, cloneCommands(sources.PluginCommands)...)
	base = appendPromptCommands(base, promptTemplates, sources.PluginSkillPrompts)
	base = append(base, cloneCommands(sources.PluginSkills)...)

	seen := commandNameSet(base)
	for _, prompt := range sources.DynamicSkillPrompts {
		if commandKnown(seen, prompt.Command) {
			continue
		}
		markCommand(seen, prompt.Command)
		base = append(base, cloneCommand(prompt.Command))
		registerPromptTemplate(promptTemplates, prompt)
	}
	for _, cmd := range sources.DynamicSkills {
		if commandKnown(seen, cmd) {
			continue
		}
		markCommand(seen, cmd)
		base = append(base, cloneCommand(cmd))
	}

	base = append(base, cloneCommands(sources.Builtins)...)
	return Registry{commands: base, promptTemplates: promptTemplates}
}

func (r Registry) All() []contracts.Command {
	return cloneCommands(r.commands)
}

func (r Registry) Visible() []contracts.Command {
	var out []contracts.Command
	for _, cmd := range r.commands {
		if cmd.Hidden {
			continue
		}
		out = append(out, cloneCommand(cmd))
	}
	return out
}

func (r Registry) Find(name string) (contracts.Command, bool) {
	return FindCommand(name, r.commands)
}

func (r Registry) Has(name string) bool {
	_, ok := r.Find(name)
	return ok
}

func (r Registry) SkillToolCommands() []contracts.Command {
	return SkillToolCommands(r.commands)
}

func (r Registry) SlashCommandToolSkills() []contracts.Command {
	return SlashCommandToolSkills(r.commands)
}

func (r Registry) PromptTemplate(name string) (PromptTemplate, bool) {
	template, ok := r.promptTemplates[strings.TrimSpace(name)]
	if !ok {
		if command, found := r.Find(name); found {
			template, ok = r.promptTemplates[command.Name]
		}
	}
	if !ok {
		return PromptTemplate{}, false
	}
	return clonePromptTemplate(template), true
}

func FindCommand(name string, commands []contracts.Command) (contracts.Command, bool) {
	name = strings.TrimSpace(name)
	for _, cmd := range commands {
		if cmd.Name == name || UserFacingName(cmd) == name {
			return cloneCommand(cmd), true
		}
		for _, alias := range cmd.Aliases {
			if alias == name {
				return cloneCommand(cmd), true
			}
		}
	}
	return contracts.Command{}, false
}

func UserFacingName(cmd contracts.Command) string {
	if cmd.DisplayName != "" {
		return cmd.DisplayName
	}
	return cmd.Name
}

func SkillToolCommands(commands []contracts.Command) []contracts.Command {
	var out []contracts.Command
	for _, cmd := range commands {
		if cmd.Type != contracts.CommandPrompt || cmd.DisableModelInvocation || cmd.Source == contracts.CommandSourceBuiltin {
			continue
		}
		if isAlwaysSkillLoadedFrom(cmd.LoadedFrom) || cmd.HasUserSpecifiedDetails || cmd.WhenToUse != "" {
			out = append(out, cloneCommand(cmd))
		}
	}
	return out
}

func SlashCommandToolSkills(commands []contracts.Command) []contracts.Command {
	var out []contracts.Command
	for _, cmd := range commands {
		if cmd.Type != contracts.CommandPrompt || cmd.Source == contracts.CommandSourceBuiltin {
			continue
		}
		if !cmd.HasUserSpecifiedDetails && cmd.WhenToUse == "" {
			continue
		}
		if cmd.LoadedFrom == "skills" || cmd.LoadedFrom == "plugin" || cmd.LoadedFrom == "bundled" || cmd.DisableModelInvocation {
			out = append(out, cloneCommand(cmd))
		}
	}
	return out
}

func MCPSkillCommands(commands []contracts.Command) []contracts.Command {
	var out []contracts.Command
	for _, cmd := range commands {
		if cmd.Type == contracts.CommandPrompt && cmd.LoadedFrom == "mcp" && !cmd.DisableModelInvocation {
			out = append(out, cloneCommand(cmd))
		}
	}
	return out
}

func IsBridgeSafeCommand(cmd contracts.Command) bool {
	switch cmd.Type {
	case contracts.CommandPrompt:
		return true
	case contracts.CommandLocalJSX:
		return false
	case contracts.CommandLocal:
		switch cmd.Name {
		case "compact", "clear", "cost", "summary", "release-notes", "files":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func BuiltinCommands() []contracts.Command {
	return cloneCommands([]contracts.Command{
		{Type: contracts.CommandLocalJSX, Name: "help", Description: "Show help and available commands", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "config", Aliases: []string{"settings"}, Description: "Open config panel", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "mcp", Description: "Manage MCP servers", ArgumentHint: "[enable|disable [server-name]]", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "plugin", Aliases: []string{"plugins", "marketplace"}, Description: "Manage plugins", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "skills", Description: "List available skills", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "memory", Description: "Edit memory files", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocal, Name: "native", Description: "Run explicit native integration commands", ArgumentHint: "clipboard|chrome|voice|computer", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocalJSX, Name: "resume", Aliases: []string{"continue"}, Description: "Resume a previous conversation", ArgumentHint: "[conversation id or search term]", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocal, Name: "clear", Aliases: []string{"reset", "new"}, Description: "Clear conversation history and free up context", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "compact", Description: "Compact the current conversation", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "cost", Description: "Show the total cost and duration of the current session", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "summary", Description: "Show a local conversation summary", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "release-notes", Description: "Show bundled release notes status", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "files", Description: "Show local workspace files", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "issue", Description: "Prepare a local issue report context", ArgumentHint: "[description]", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocalJSX, Name: "status", Description: "Show Claude Code status including version, model, account, API connectivity, and tool statuses", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "model", Description: "Set the AI model for Claude Code", ArgumentHint: "[model]", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "output-style", Description: "Deprecated: use /config to change output style", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "login", Description: "Sign in with your Claude account (OAuth)", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "logout", Description: "Sign out and remove stored credentials", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "theme", Description: "Set the color theme", ArgumentHint: "<name>", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "effort", Description: "Set the effort level for responses", ArgumentHint: "<low|medium|high|max|auto>", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "vim", Description: "Toggle vim keybinding mode", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "permissions", Aliases: []string{"allowed-tools"}, Description: "List and edit allow/deny/ask permission rules", ArgumentHint: "[list | allow <rule> | deny <rule> | ask <rule> | remove <allow|deny|ask> <rule>]", Source: contracts.CommandSourceBuiltin},
	})
}

func loadProjectSkillPrompts(cwd string) []PromptTemplate {
	skillDirs := skills.ProjectSkillDirs(cwd)
	skillDirs = append(skillDirs, skills.UserSkillDirs()...)
	loaded := skills.LoadSkillDirs(skillDirs, contracts.CommandSourceSkills)
	loaded = append(loaded, skills.LoadLegacyCommandSkills(cwd)...)
	loaded = append(loaded, skills.LoadUserLegacyCommandSkills()...)
	out := make([]PromptTemplate, 0, len(loaded))
	for _, skill := range loaded {
		out = append(out, PromptTemplate{
			Command: skill.Command,
			Content: skill.Content,
		})
	}
	return out
}

func loadProjectPluginCommands(cwd string, settings contracts.Settings) ([]PromptTemplate, []contracts.Command) {
	loaded := pluginpkg.LoadPluginDirsWithSettings(pluginpkg.InstalledPluginDirs(cwd), settings)
	var prompts []PromptTemplate
	var commands []contracts.Command
	for _, plugin := range loaded {
		userConfig := pluginUserConfig(settings, plugin)
		for _, prompt := range plugin.PromptTemplates {
			command := prompt.Command
			command.UserConfig = cloneAnyMap(userConfig)
			prompts = append(prompts, PromptTemplate{
				Command: command,
				Content: prompt.Content,
			})
		}
		for _, command := range plugin.Commands {
			command.UserConfig = cloneAnyMap(userConfig)
			commands = append(commands, command)
		}
	}
	return prompts, commands
}

func pluginUserConfig(settings contracts.Settings, plugin pluginpkg.LoadedPlugin) map[string]any {
	out := map[string]any{}
	for _, key := range pluginConfigKeys(plugin) {
		if config, ok := settings.PluginConfigs[key]; ok {
			mergeAnyInto(out, config.Options)
		}
		if value, ok := settings.Plugins[key]; ok {
			mergeLegacyPluginConfig(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginConfigKeys(plugin pluginpkg.LoadedPlugin) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, key := range []string{
		strings.TrimSpace(plugin.Name),
		strings.TrimSpace(filepath.Base(plugin.Root)),
		strings.TrimSpace(plugin.Root),
	} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func mergeLegacyPluginConfig(dst map[string]any, value any) {
	switch typed := value.(type) {
	case map[string]any:
		if options, ok := typed["options"].(map[string]any); ok {
			mergeAnyInto(dst, options)
			return
		}
		mergeAnyInto(dst, typed)
	case map[string]string:
		for key, value := range typed {
			dst[key] = value
		}
	}
}

func mergeAnyInto(dst map[string]any, src map[string]any) {
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dst[key] = cloneAny(value)
	}
}

func isAlwaysSkillLoadedFrom(loadedFrom string) bool {
	switch loadedFrom {
	case "bundled", "skills", loadedFromCommandsDeprecated:
		return true
	default:
		return false
	}
}

func commandNameSet(commands []contracts.Command) map[string]struct{} {
	seen := map[string]struct{}{}
	for _, cmd := range commands {
		markCommand(seen, cmd)
	}
	return seen
}

func appendPromptCommands(base []contracts.Command, templates map[string]PromptTemplate, prompts []PromptTemplate) []contracts.Command {
	for _, prompt := range prompts {
		base = append(base, cloneCommand(prompt.Command))
		registerPromptTemplate(templates, prompt)
	}
	return base
}

func registerPromptTemplate(templates map[string]PromptTemplate, prompt PromptTemplate) {
	if prompt.Command.Name == "" {
		return
	}
	prompt = clonePromptTemplate(prompt)
	for _, key := range commandKeys(prompt.Command) {
		if key == "" {
			continue
		}
		if _, exists := templates[key]; exists {
			continue
		}
		templates[key] = prompt
	}
}

func commandKnown(seen map[string]struct{}, cmd contracts.Command) bool {
	for _, key := range commandKeys(cmd) {
		if _, ok := seen[key]; ok {
			return true
		}
	}
	return false
}

func markCommand(seen map[string]struct{}, cmd contracts.Command) {
	for _, key := range commandKeys(cmd) {
		seen[key] = struct{}{}
	}
}

func commandKeys(cmd contracts.Command) []string {
	var keys []string
	if cmd.Name != "" {
		keys = append(keys, cmd.Name)
	}
	if name := UserFacingName(cmd); name != "" && name != cmd.Name {
		keys = append(keys, name)
	}
	keys = append(keys, cmd.Aliases...)
	return keys
}

func cloneCommands(commands []contracts.Command) []contracts.Command {
	if len(commands) == 0 {
		return nil
	}
	out := make([]contracts.Command, len(commands))
	for i, cmd := range commands {
		out[i] = cloneCommand(cmd)
	}
	return out
}

func clonePromptTemplate(prompt PromptTemplate) PromptTemplate {
	return PromptTemplate{
		Command: cloneCommand(prompt.Command),
		Content: prompt.Content,
	}
}

func cloneCommand(cmd contracts.Command) contracts.Command {
	cmd.Aliases = append([]string(nil), cmd.Aliases...)
	cmd.ArgumentNames = append([]string(nil), cmd.ArgumentNames...)
	cmd.AllowedTools = append([]string(nil), cmd.AllowedTools...)
	cmd.Paths = append([]string(nil), cmd.Paths...)
	cmd.Availability = append([]string(nil), cmd.Availability...)
	cmd.UserConfig = cloneAnyMap(cmd.UserConfig)
	return cmd
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

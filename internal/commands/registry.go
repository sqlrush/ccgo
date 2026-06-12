package commands

import (
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/skills"
)

const loadedFromCommandsDeprecated = "commands_DEPRECATED"

type Sources struct {
	BundledSkills       []contracts.Command
	BuiltinPluginSkills []contracts.Command
	ProjectSkills       []contracts.Command
	WorkflowCommands    []contracts.Command
	PluginCommands      []contracts.Command
	PluginSkills        []contracts.Command
	DynamicSkills       []contracts.Command
	Builtins            []contracts.Command
}

type Options struct {
	CWD                  string
	Sources              Sources
	DisableProjectSkills bool
	DisableBuiltins      bool
}

type Registry struct {
	commands []contracts.Command
}

func Load(opts Options) Registry {
	sources := opts.Sources
	if !opts.DisableProjectSkills && opts.CWD != "" && len(sources.ProjectSkills) == 0 {
		sources.ProjectSkills = skills.ProjectSkillCommands(opts.CWD)
	}
	if !opts.DisableBuiltins && sources.Builtins == nil {
		sources.Builtins = BuiltinCommands()
	}
	return FromSources(sources)
}

func FromSources(sources Sources) Registry {
	var base []contracts.Command
	base = append(base, cloneCommands(sources.BundledSkills)...)
	base = append(base, cloneCommands(sources.BuiltinPluginSkills)...)
	base = append(base, cloneCommands(sources.ProjectSkills)...)
	base = append(base, cloneCommands(sources.WorkflowCommands)...)
	base = append(base, cloneCommands(sources.PluginCommands)...)
	base = append(base, cloneCommands(sources.PluginSkills)...)

	seen := commandNameSet(base)
	for _, cmd := range sources.DynamicSkills {
		if commandKnown(seen, cmd) {
			continue
		}
		markCommand(seen, cmd)
		base = append(base, cloneCommand(cmd))
	}

	base = append(base, cloneCommands(sources.Builtins)...)
	return Registry{commands: base}
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
		{Type: contracts.CommandLocalJSX, Name: "config", Description: "Open configuration", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "mcp", Description: "Manage MCP servers and resources", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "plugin", Description: "Manage plugins", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "skills", Description: "Browse available skills", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "memory", Description: "Edit memory files", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocalJSX, Name: "resume", Description: "Resume a previous session", Source: contracts.CommandSourceBuiltin},
		{Type: contracts.CommandLocal, Name: "clear", Description: "Clear the current conversation", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "compact", Description: "Compact the current conversation", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "cost", Description: "Show session cost", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocal, Name: "status", Description: "Show current status", Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Type: contracts.CommandLocalJSX, Name: "model", Description: "Select or show the active model", Source: contracts.CommandSourceBuiltin},
	})
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

func cloneCommand(cmd contracts.Command) contracts.Command {
	cmd.Aliases = append([]string(nil), cmd.Aliases...)
	cmd.ArgumentNames = append([]string(nil), cmd.ArgumentNames...)
	cmd.AllowedTools = append([]string(nil), cmd.AllowedTools...)
	cmd.Paths = append([]string(nil), cmd.Paths...)
	cmd.Availability = append([]string(nil), cmd.Availability...)
	return cmd
}

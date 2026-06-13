package commands

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
)

const (
	CommandPermissionsSubtype = "command_permissions"

	commandNameTag         = "command-name"
	commandMessageTag      = "command-message"
	commandArgsTag         = "command-args"
	localCommandStderrTag  = "local-command-stderr"
	modelOnlySkillTemplate = "This skill can only be invoked by Claude, not directly by users. Ask Claude to use the %q skill for you."
)

type SlashCommand struct {
	CommandName string
	Args        string
	IsMCP       bool
}

type SlashOptions struct {
	SessionID contracts.ID
	UUID      contracts.ID
}

type SlashResult struct {
	Command      contracts.Command
	Messages     []contracts.Message
	ShouldQuery  bool
	Model        string
	AllowedTools []string
	ResultText   string
	LocalResult  *LocalCommandResult
	Unknown      bool
	Unsupported  bool
}

type LocalCommandResultType string

const (
	LocalCommandResultText    LocalCommandResultType = "text"
	LocalCommandResultSkip    LocalCommandResultType = "skip"
	LocalCommandResultClear   LocalCommandResultType = "clear"
	LocalCommandResultCompact LocalCommandResultType = "compact"
	LocalCommandResultCost    LocalCommandResultType = "cost"
	LocalCommandResultStatus  LocalCommandResultType = "status"
	LocalCommandResultModel   LocalCommandResultType = "model"
	LocalCommandResultMCP     LocalCommandResultType = "mcp"
	LocalCommandResultResume  LocalCommandResultType = "resume"
	LocalCommandResultConfig  LocalCommandResultType = "config"
	LocalCommandResultPlugin  LocalCommandResultType = "plugin"
	LocalCommandResultMemory  LocalCommandResultType = "memory"
)

type LocalCommandResult struct {
	Type  LocalCommandResultType
	Value string
}

type CommandPermissions struct {
	AllowedTools []string
	Model        string
}

func ParseSlashCommand(input string) (SlashCommand, bool) {
	trimmed := strings.TrimSpace(input)
	if !IsSlashInput(input) {
		return SlashCommand{}, false
	}
	withoutSlash := strings.TrimPrefix(trimmed, "/")
	words := strings.Split(withoutSlash, " ")
	if len(words) == 0 || words[0] == "" {
		return SlashCommand{}, false
	}
	commandName := words[0]
	argsStart := 1
	isMCP := false
	if len(words) > 1 && words[1] == "(MCP)" {
		commandName += " (MCP)"
		argsStart = 2
		isMCP = true
	}
	return SlashCommand{
		CommandName: commandName,
		Args:        strings.Join(words[argsStart:], " "),
		IsMCP:       isMCP,
	}, true
}

func IsSlashInput(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}

func ExecuteSlashCommand(registry Registry, input string, opts SlashOptions) (SlashResult, bool, error) {
	parsed, ok := ParseSlashCommand(input)
	if !ok {
		if IsSlashInput(input) {
			text := "Commands are in the form `/command [args]`"
			return SlashResult{
				Messages:    []contracts.Message{slashUserText(text, opts.SessionID, opts.UUID, false)},
				ShouldQuery: false,
				ResultText:  text,
			}, true, nil
		}
		return SlashResult{}, false, nil
	}
	cmd, found := registry.Find(parsed.CommandName)
	if !found {
		if !LooksLikeCommand(parsed.CommandName) {
			return SlashResult{}, false, nil
		}
		text := "Unknown skill: " + parsed.CommandName
		return SlashResult{
			Messages:    []contracts.Message{slashUserText(text, opts.SessionID, opts.UUID, false)},
			ShouldQuery: false,
			ResultText:  text,
			Unknown:     true,
		}, true, nil
	}
	if cmd.Type == contracts.CommandPrompt && cmd.Hidden {
		text := fmt.Sprintf(modelOnlySkillTemplate, parsed.CommandName)
		return SlashResult{
			Command:     cmd,
			Messages:    []contracts.Message{slashUserText(text, opts.SessionID, opts.UUID, false)},
			ShouldQuery: false,
			ResultText:  text,
		}, true, nil
	}
	switch cmd.Type {
	case contracts.CommandPrompt:
		expanded, err := registry.ExpandPrompt(parsed.CommandName, parsed.Args, opts.SessionID)
		if err != nil {
			return SlashResult{}, true, err
		}
		allowedTools := ParseToolList(cmd.AllowedTools)
		commandMessage := slashUserText(FormatCommandInputTags(cmd.Name, parsed.Args), opts.SessionID, opts.UUID, false)
		messages := []contracts.Message{commandMessage, expanded.Message}
		if attachment := CommandPermissionsAttachment(allowedTools, cmd.Model, opts.SessionID); attachment.Type != "" {
			messages = append(messages, attachment)
		}
		return SlashResult{
			Command:      cmd,
			Messages:     messages,
			ShouldQuery:  true,
			Model:        cmd.Model,
			AllowedTools: allowedTools,
		}, true, nil
	case contracts.CommandLocal, contracts.CommandLocalJSX:
		args := parsed.Args
		if cmd.Sensitive && strings.TrimSpace(args) != "" {
			args = "***"
		}
		if local, ok := ExecuteBuiltinLocalCommand(registry, cmd, parsed.Args); ok {
			messages := []contracts.Message{}
			if local.Type != LocalCommandResultSkip {
				messages = append(messages, slashUserText(FormatCommandInputTags(cmd.Name, args), opts.SessionID, opts.UUID, false))
				if local.Type == LocalCommandResultText && strings.TrimSpace(local.Value) != "" {
					messages = append(messages, slashUserText(local.Value, opts.SessionID, "", false))
				}
			}
			return SlashResult{
				Command:     cmd,
				Messages:    messages,
				ShouldQuery: false,
				ResultText:  local.Value,
				LocalResult: &local,
			}, true, nil
		}
		errText := fmt.Sprintf("Slash command /%s is not implemented in the Go runtime yet.", cmd.Name)
		return SlashResult{
			Command: cmd,
			Messages: []contracts.Message{
				slashUserText(FormatCommandInputTags(cmd.Name, args), opts.SessionID, opts.UUID, false),
				slashUserText(wrapTag(localCommandStderrTag, errText), opts.SessionID, "", false),
			},
			ShouldQuery: false,
			ResultText:  errText,
			Unsupported: true,
		}, true, nil
	default:
		return SlashResult{}, true, fmt.Errorf("unsupported slash command type %q", cmd.Type)
	}
}

func ExecuteBuiltinLocalCommand(registry Registry, cmd contracts.Command, args string) (LocalCommandResult, bool) {
	if cmd.Source != contracts.CommandSourceBuiltin {
		return LocalCommandResult{}, false
	}
	switch cmd.Name {
	case "help":
		return LocalCommandResult{Type: LocalCommandResultText, Value: formatHelpText(registry)}, true
	case "config":
		return LocalCommandResult{Type: LocalCommandResultConfig, Value: strings.TrimSpace(args)}, true
	case "mcp":
		return LocalCommandResult{Type: LocalCommandResultMCP, Value: strings.TrimSpace(args)}, true
	case "plugin":
		return LocalCommandResult{Type: LocalCommandResultPlugin, Value: strings.TrimSpace(args)}, true
	case "clear":
		return LocalCommandResult{Type: LocalCommandResultClear}, true
	case "compact":
		return LocalCommandResult{Type: LocalCommandResultCompact, Value: strings.TrimSpace(args)}, true
	case "cost":
		return LocalCommandResult{Type: LocalCommandResultCost}, true
	case "status":
		return LocalCommandResult{Type: LocalCommandResultStatus}, true
	case "model":
		return LocalCommandResult{Type: LocalCommandResultModel, Value: strings.TrimSpace(args)}, true
	case "resume":
		return LocalCommandResult{Type: LocalCommandResultResume, Value: strings.TrimSpace(args)}, true
	case "skills":
		return LocalCommandResult{Type: LocalCommandResultText, Value: formatSkillsText(registry)}, true
	case "memory":
		return LocalCommandResult{Type: LocalCommandResultMemory, Value: strings.TrimSpace(args)}, true
	default:
		return LocalCommandResult{}, false
	}
}

func formatHelpText(registry Registry) string {
	commands := registry.Visible()
	if len(commands) == 0 {
		return "No commands available."
	}
	var lines []string
	lines = append(lines, "Available commands:")
	for _, cmd := range commands {
		name := "/" + UserFacingName(cmd)
		description := strings.TrimSpace(cmd.Description)
		if description == "" {
			lines = append(lines, name)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s - %s", name, description))
	}
	return strings.Join(lines, "\n")
}

func formatSkillsText(registry Registry) string {
	var lines []string
	for _, cmd := range registry.Visible() {
		if cmd.Type != contracts.CommandPrompt || cmd.Source == contracts.CommandSourceBuiltin {
			continue
		}
		name := "/" + UserFacingName(cmd)
		description := strings.TrimSpace(firstNonEmptyString(cmd.Description, cmd.WhenToUse))
		if description == "" {
			lines = append(lines, name)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s - %s", name, description))
	}
	if len(lines) == 0 {
		return "No skills available."
	}
	return "Available skills:\n" + strings.Join(lines, "\n")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func CommandPermissionsAttachment(allowedTools []string, model string, sessionID contracts.ID) contracts.Message {
	allowedTools = ParseToolList(allowedTools)
	model = strings.TrimSpace(model)
	if len(allowedTools) == 0 && model == "" {
		return contracts.Message{}
	}
	payload := map[string]any{
		"type":         CommandPermissionsSubtype,
		"allowedTools": append([]string(nil), allowedTools...),
	}
	if model != "" {
		payload["model"] = model
	}
	return contracts.Message{
		Type:      contracts.MessageAttachment,
		UUID:      contracts.NewID(),
		SessionID: sessionID,
		Subtype:   CommandPermissionsSubtype,
		Raw: map[string]any{
			"attachment": payload,
		},
	}
}

func CommandPermissionsFromMessages(messages []contracts.Message) CommandPermissions {
	var out CommandPermissions
	seen := map[string]struct{}{}
	for _, message := range messages {
		item, ok := CommandPermissionsFromMessage(message)
		if !ok {
			continue
		}
		for _, tool := range item.AllowedTools {
			if _, ok := seen[tool]; ok {
				continue
			}
			seen[tool] = struct{}{}
			out.AllowedTools = append(out.AllowedTools, tool)
		}
		if item.Model != "" {
			out.Model = item.Model
		}
	}
	return out
}

func CommandPermissionsFromMessage(message contracts.Message) (CommandPermissions, bool) {
	if message.Type != contracts.MessageAttachment && message.Subtype != CommandPermissionsSubtype {
		return CommandPermissions{}, false
	}
	raw := message.Raw
	payload, _ := raw["attachment"].(map[string]any)
	if payload == nil {
		payload = raw
	}
	rawType, _ := payload["type"].(string)
	if message.Subtype != CommandPermissionsSubtype && rawType != CommandPermissionsSubtype {
		return CommandPermissions{}, false
	}
	result := CommandPermissions{
		AllowedTools: ParseToolList(firstStringSlice(payload, "allowedTools", "allowed_tools", "tools")),
		Model:        strings.TrimSpace(firstString(payload, "model")),
	}
	if len(result.AllowedTools) == 0 && result.Model == "" {
		return CommandPermissions{}, false
	}
	return result, true
}

func ParseToolList(tools []string) []string {
	var result []string
	for _, toolString := range tools {
		var current strings.Builder
		inParens := false
		for _, r := range toolString {
			switch r {
			case '(':
				inParens = true
				current.WriteRune(r)
			case ')':
				inParens = false
				current.WriteRune(r)
			case ',', ' ', '\t', '\n', '\r':
				if inParens {
					current.WriteRune(r)
					continue
				}
				if part := strings.TrimSpace(current.String()); part != "" {
					result = append(result, part)
				}
				current.Reset()
			default:
				current.WriteRune(r)
			}
		}
		if part := strings.TrimSpace(current.String()); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func FormatCommandInputTags(commandName string, args string) string {
	return strings.Join([]string{
		wrapTag(commandNameTag, "/"+commandName),
		wrapTag(commandMessageTag, commandName),
		wrapTag(commandArgsTag, args),
	}, "\n")
}

func LooksLikeCommand(commandName string) bool {
	if commandName == "" {
		return false
	}
	for _, r := range commandName {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == ':' || r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func slashUserText(text string, sessionID contracts.ID, uuid contracts.ID, meta bool) contracts.Message {
	if uuid == "" {
		uuid = contracts.NewID()
	}
	return contracts.Message{
		Type:      contracts.MessageUser,
		UUID:      uuid,
		SessionID: sessionID,
		IsMeta:    meta,
		Content:   []contracts.ContentBlock{contracts.NewTextBlock(text)},
	}
}

func wrapTag(tag string, text string) string {
	return "<" + tag + ">" + text + "</" + tag + ">"
}

func firstStringSlice(obj map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return append([]string(nil), typed...)
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
			return out
		case string:
			return []string{typed}
		}
	}
	return nil
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value
		}
	}
	return ""
}

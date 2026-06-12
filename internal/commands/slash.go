package commands

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
)

const (
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
	Unknown      bool
	Unsupported  bool
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
		commandMessage := slashUserText(FormatCommandInputTags(cmd.Name, parsed.Args), opts.SessionID, opts.UUID, false)
		return SlashResult{
			Command:      cmd,
			Messages:     []contracts.Message{commandMessage, expanded.Message},
			ShouldQuery:  true,
			Model:        cmd.Model,
			AllowedTools: append([]string(nil), cmd.AllowedTools...),
		}, true, nil
	case contracts.CommandLocal, contracts.CommandLocalJSX:
		args := parsed.Args
		if cmd.Sensitive && strings.TrimSpace(args) != "" {
			args = "***"
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

package commands

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
)

type PromptTemplate struct {
	Command contracts.Command
	Content string
}

type PromptExpansion struct {
	Command       contracts.Command
	Content       string
	ContentBlocks []contracts.ContentBlock
	Message       contracts.Message
}

func (r Registry) ExpandPrompt(name string, args string, sessionID contracts.ID) (PromptExpansion, error) {
	template, ok := r.PromptTemplate(name)
	if !ok {
		return PromptExpansion{}, fmt.Errorf("prompt command %q not found or has no prompt template", strings.TrimSpace(name))
	}
	if template.Command.Type != contracts.CommandPrompt {
		return PromptExpansion{}, fmt.Errorf("command %q is %q, not prompt", template.Command.Name, template.Command.Type)
	}
	content := SubstituteArguments(template.Content, args, true, template.Command.ArgumentNames)
	if template.Command.SkillRoot != "" && template.Command.LoadedFrom != "mcp" {
		content = strings.ReplaceAll(content, "${CLAUDE_SKILL_DIR}", template.Command.SkillRoot)
	}
	content = strings.ReplaceAll(content, "${CLAUDE_SESSION_ID}", string(sessionID))
	blocks := []contracts.ContentBlock{contracts.NewTextBlock(content)}
	message := contracts.Message{
		Type:      contracts.MessageUser,
		UUID:      contracts.NewID(),
		SessionID: sessionID,
		IsMeta:    true,
		Content:   blocks,
	}
	return PromptExpansion{
		Command:       cloneCommand(template.Command),
		Content:       content,
		ContentBlocks: append([]contracts.ContentBlock(nil), blocks...),
		Message:       message,
	}, nil
}

func SubstituteArguments(content string, args string, appendIfNoPlaceholder bool, argumentNames []string) string {
	parsedArgs := ParseArguments(args)
	original := content
	for i, name := range argumentNames {
		if name == "" {
			continue
		}
		value := ""
		if i < len(parsedArgs) {
			value = parsedArgs[i]
		}
		content = replaceNamedArgument(content, name, value)
	}
	content = replaceArgumentsIndexes(content, parsedArgs)
	content = replaceNumericShorthand(content, parsedArgs)
	content = strings.ReplaceAll(content, "$ARGUMENTS", args)
	if content == original && appendIfNoPlaceholder && args != "" {
		content += "\n\nARGUMENTS: " + args
	}
	return content
}

func ParseArguments(args string) []string {
	if strings.TrimSpace(args) == "" {
		return nil
	}
	parsed, ok := parseShellLikeArguments(args)
	if !ok {
		return strings.Fields(args)
	}
	return parsed
}

func parseShellLikeArguments(args string) ([]string, bool) {
	var out []string
	var current strings.Builder
	var quote rune
	escaped := false
	haveToken := false
	for _, r := range args {
		if escaped {
			current.WriteRune(r)
			haveToken = true
			escaped = false
			continue
		}
		if quote != '\'' && r == '\\' {
			escaped = true
			haveToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				haveToken = true
				continue
			}
			current.WriteRune(r)
			haveToken = true
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
			haveToken = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if haveToken {
				out = append(out, current.String())
				current.Reset()
				haveToken = false
			}
		default:
			current.WriteRune(r)
			haveToken = true
		}
	}
	if escaped || quote != 0 {
		return nil, false
	}
	if haveToken {
		out = append(out, current.String())
	}
	return out, true
}

func replaceNamedArgument(content string, name string, value string) string {
	target := "$" + name
	var out strings.Builder
	for len(content) > 0 {
		index := strings.Index(content, target)
		if index < 0 {
			out.WriteString(content)
			break
		}
		out.WriteString(content[:index])
		after := index + len(target)
		if after < len(content) && (content[after] == '[' || wordByte(content[after])) {
			out.WriteString(target)
		} else {
			out.WriteString(value)
		}
		content = content[after:]
	}
	return out.String()
}

func replaceArgumentsIndexes(content string, args []string) string {
	const prefix = "$ARGUMENTS["
	var out strings.Builder
	for len(content) > 0 {
		index := strings.Index(content, prefix)
		if index < 0 {
			out.WriteString(content)
			break
		}
		out.WriteString(content[:index])
		rest := content[index+len(prefix):]
		digitEnd := 0
		for digitEnd < len(rest) && rest[digitEnd] >= '0' && rest[digitEnd] <= '9' {
			digitEnd++
		}
		if digitEnd == 0 || digitEnd >= len(rest) || rest[digitEnd] != ']' {
			out.WriteString(prefix)
			content = rest
			continue
		}
		out.WriteString(indexedArg(args, rest[:digitEnd]))
		content = rest[digitEnd+1:]
	}
	return out.String()
}

func replaceNumericShorthand(content string, args []string) string {
	var out strings.Builder
	for len(content) > 0 {
		index := strings.IndexByte(content, '$')
		if index < 0 {
			out.WriteString(content)
			break
		}
		out.WriteString(content[:index])
		rest := content[index+1:]
		digitEnd := 0
		for digitEnd < len(rest) && rest[digitEnd] >= '0' && rest[digitEnd] <= '9' {
			digitEnd++
		}
		if digitEnd == 0 {
			out.WriteByte('$')
			content = rest
			continue
		}
		if digitEnd < len(rest) && wordByte(rest[digitEnd]) {
			out.WriteByte('$')
			out.WriteString(rest[:digitEnd])
			content = rest[digitEnd:]
			continue
		}
		out.WriteString(indexedArg(args, rest[:digitEnd]))
		content = rest[digitEnd:]
	}
	return out.String()
}

func indexedArg(args []string, rawIndex string) string {
	index := 0
	for _, r := range rawIndex {
		index = index*10 + int(r-'0')
	}
	if index < 0 || index >= len(args) {
		return ""
	}
	return args[index]
}

func wordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

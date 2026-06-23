package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// completionSubcommands is the authoritative list of top-level subcommands
// supported by the claude binary. Keep in sync with main.go dispatch.
var completionSubcommands = []string{
	"plugin",
	"auth",
	"mcp",
	"agents",
	"doctor",
	"update",
	"completion",
}

// completionTopFlags lists the commonly-used top-level flags for completions.
var completionTopFlags = []string{
	"--version",
	"--print",
	"--model",
	"--max-tokens",
	"--max-turns",
	"--permission-mode",
	"--dangerously-skip-permissions",
	"--mcp-config",
	"--stream",
	"--input-format",
	"--output-format",
	"--resume",
	"--continue",
	"--system-prompt",
	"--append-system-prompt",
	"--allowed-tools",
	"--disallowed-tools",
	"--add-dir",
	"--cwd",
}

const bashCompletionTemplate = `# bash completion for claude
# Source this file or add to ~/.bashrc:
#   source <(claude completion bash)

_claude_completions() {
    local cur prev words cword
    _init_completion 2>/dev/null || {
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
    }

    local subcommands="%s"
    local flags="%s"

    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${subcommands} ${flags}" -- "${cur}") )
        return 0
    fi

    case "${prev}" in
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            return 0
            ;;
        --model|-m)
            return 0
            ;;
        --permission-mode|--permissionMode)
            COMPREPLY=( $(compgen -W "default acceptEdits bypassPermissions" -- "${cur}") )
            return 0
            ;;
        --input-format|--inputFormat|--output-format|--outputFormat)
            COMPREPLY=( $(compgen -W "text json stream-json" -- "${cur}") )
            return 0
            ;;
    esac

    COMPREPLY=( $(compgen -W "${flags}" -- "${cur}") )
}

complete -F _claude_completions claude
`

const zshCompletionTemplate = `#compdef claude
# zsh completion for claude
# Add to your ~/.zshrc:
#   source <(claude completion zsh)
# or place in a directory on your $fpath.

_claude() {
    local -a subcommands
    subcommands=(
%s
    )

    local -a flags
    flags=(
%s
    )

    _arguments -C \
        '1: :->subcmd' \
        '*:: :->args'

    case $state in
        subcmd)
            _describe 'subcommand' subcommands
            ;;
        args)
            case $words[1] in
                completion)
                    _values 'shell' bash zsh fish
                    ;;
                *)
                    _describe 'flag' flags
                    ;;
            esac
            ;;
    esac
}

_claude "$@"
`

const fishCompletionTemplate = `# fish completion for claude
# Place in ~/.config/fish/completions/claude.fish

# Disable file completion by default
complete -c claude -f

# Subcommands
%s

# Top-level flags
%s

# completion subcommand shell choices
complete -c claude -n "__fish_seen_subcommand_from completion" -a "bash zsh fish" -d "shell type"

# --input-format / --output-format choices
complete -c claude -l input-format -a "text json stream-json" -d "input format"
complete -c claude -l output-format -a "text json stream-json" -d "output format"
complete -c claude -l permission-mode -a "default acceptEdits bypassPermissions" -d "permission mode"
`

// runCompletionCLI implements "claude completion <shell> [--output <file>]".
// It writes a static completion script for bash, zsh, or fish to stdout,
// or to a file when --output is given (SUBCMD-COMPLETION-06).
// Returns 0 on success, 1 on error.
func runCompletionCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: claude completion <shell>")
		fmt.Fprintln(stderr, "supported shells: bash, zsh, fish")
		return 1
	}

	// Parse flags after the shell argument.
	shell := strings.ToLower(args[0])
	rest := args[1:]

	var outputFile string
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--output" && i+1 < len(rest) {
			outputFile = rest[i+1]
			i++
		} else if strings.HasPrefix(rest[i], "--output=") {
			outputFile = strings.TrimPrefix(rest[i], "--output=")
		}
	}

	var script string
	switch shell {
	case "bash":
		script = fmt.Sprintf(bashCompletionTemplate,
			strings.Join(completionSubcommands, " "),
			strings.Join(completionTopFlags, " "),
		)
	case "zsh":
		var subLines []string
		for _, s := range completionSubcommands {
			subLines = append(subLines, fmt.Sprintf("        '%s'", s))
		}
		var flagLines []string
		for _, f := range completionTopFlags {
			flagLines = append(flagLines, fmt.Sprintf("        '%s'", f))
		}
		script = fmt.Sprintf(zshCompletionTemplate,
			strings.Join(subLines, "\n"),
			strings.Join(flagLines, "\n"),
		)
	case "fish":
		var subLines []string
		for _, s := range completionSubcommands {
			subLines = append(subLines, fmt.Sprintf("complete -c claude -n __fish_use_subcommand -a %s", s))
		}
		var flagLines []string
		for _, f := range completionTopFlags {
			name := strings.TrimLeft(f, "-")
			flagLines = append(flagLines, fmt.Sprintf("complete -c claude -l %s", name))
		}
		script = fmt.Sprintf(fishCompletionTemplate,
			strings.Join(subLines, "\n"),
			strings.Join(flagLines, "\n"),
		)
	default:
		fmt.Fprintf(stderr, "completion: unknown shell %q\n", args[0])
		fmt.Fprintln(stderr, "supported shells: bash, zsh, fish")
		return 1
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(script), 0o644); err != nil {
			fmt.Fprintf(stderr, "completion: cannot write to %q: %v\n", outputFile, err)
			return 1
		}
		return 0
	}

	fmt.Fprint(stdout, script)
	return 0
}

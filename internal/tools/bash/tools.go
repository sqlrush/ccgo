package bashtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	defaultTimeoutMillis = 120_000
	maxTimeoutMillis     = 600_000
)

var gitStatAllowedFlags = map[string]bool{
	"--stat":        true,
	"--numstat":     true,
	"--shortstat":   true,
	"--name-only":   true,
	"--name-status": true,
}

var gitColorAllowedFlags = map[string]bool{
	"--color":    true,
	"--no-color": true,
}

var gitPatchAllowedFlags = map[string]bool{
	"--patch":       true,
	"-p":            true,
	"--no-patch":    true,
	"--no-ext-diff": true,
	"-s":            true,
}

var gitRefSelectionAllowedFlags = map[string]bool{
	"--all":      true,
	"--branches": true,
	"--tags":     true,
	"--remotes":  true,
}

var gitDateFilterFlagsWithArgs = map[string]bool{
	"--since":  true,
	"--after":  true,
	"--until":  true,
	"--before": true,
}

var gitLogDisplayAllowedFlags = map[string]bool{
	"--oneline":       true,
	"--graph":         true,
	"--decorate":      true,
	"--no-decorate":   true,
	"--relative-date": true,
}

var gitLogDisplayFlagsWithArgs = map[string]bool{
	"--date": true,
}

var gitCountFlagsWithArgs = map[string]bool{
	"--max-count": true,
	"-n":          true,
}

var gitAuthorFilterFlagsWithArgs = map[string]bool{
	"--author":    true,
	"--committer": true,
	"--grep":      true,
}

var gitDiffAllowedFlags = mergeBoolMaps(
	gitStatAllowedFlags,
	gitColorAllowedFlags,
	map[string]bool{
		"--dirstat":             true,
		"--summary":             true,
		"--patch-with-stat":     true,
		"--word-diff":           true,
		"--color-words":         true,
		"--no-renames":          true,
		"--no-ext-diff":         true,
		"--check":               true,
		"--full-index":          true,
		"--binary":              true,
		"--break-rewrites":      true,
		"--find-renames":        true,
		"--find-copies":         true,
		"--find-copies-harder":  true,
		"--irreversible-delete": true,
		"--histogram":           true,
		"--patience":            true,
		"--minimal":             true,
		"--ignore-space-at-eol": true,
		"--ignore-space-change": true,
		"--ignore-all-space":    true,
		"--ignore-blank-lines":  true,
		"--function-context":    true,
		"--exit-code":           true,
		"--quiet":               true,
		"--cached":              true,
		"--staged":              true,
		"--pickaxe-regex":       true,
		"--pickaxe-all":         true,
		"--no-index":            true,
		"-p":                    true,
		"-u":                    true,
		"-s":                    true,
		"-M":                    true,
		"-C":                    true,
		"-B":                    true,
		"-D":                    true,
		"-l":                    true,
		"-R":                    true,
	},
)

var gitDiffFlagsWithArgs = map[string]bool{
	"--word-diff-regex":    true,
	"--ws-error-highlight": true,
	"--abbrev":             true,
	"--diff-algorithm":     true,
	"--inter-hunk-context": true,
	"--relative":           true,
	"--diff-filter":        true,
	"-S":                   true,
	"-G":                   true,
	"-O":                   true,
}

var gitLogAllowedFlags = mergeBoolMaps(
	gitLogDisplayAllowedFlags,
	gitRefSelectionAllowedFlags,
	gitStatAllowedFlags,
	gitColorAllowedFlags,
	gitPatchAllowedFlags,
	map[string]bool{
		"--abbrev-commit":     true,
		"--full-history":      true,
		"--dense":             true,
		"--sparse":            true,
		"--simplify-merges":   true,
		"--ancestry-path":     true,
		"--source":            true,
		"--first-parent":      true,
		"--merges":            true,
		"--no-merges":         true,
		"--reverse":           true,
		"--walk-reflogs":      true,
		"--no-min-parents":    true,
		"--no-max-parents":    true,
		"--follow":            true,
		"--no-walk":           true,
		"--left-right":        true,
		"--cherry-mark":       true,
		"--cherry-pick":       true,
		"--boundary":          true,
		"--topo-order":        true,
		"--date-order":        true,
		"--author-date-order": true,
		"--pickaxe-regex":     true,
		"--pickaxe-all":       true,
	},
)

var gitLogFlagsWithArgs = mergeBoolMaps(
	gitLogDisplayFlagsWithArgs,
	gitDateFilterFlagsWithArgs,
	gitCountFlagsWithArgs,
	gitAuthorFilterFlagsWithArgs,
	map[string]bool{
		"--skip":        true,
		"--max-age":     true,
		"--min-age":     true,
		"--pretty":      true,
		"--format":      true,
		"--diff-filter": true,
		"-S":            true,
		"-G":            true,
	},
)

var gitShowAllowedFlags = mergeBoolMaps(
	gitLogDisplayAllowedFlags,
	gitStatAllowedFlags,
	gitColorAllowedFlags,
	gitPatchAllowedFlags,
	map[string]bool{
		"--abbrev-commit": true,
		"--word-diff":     true,
		"--color-words":   true,
		"--first-parent":  true,
		"--raw":           true,
		"-m":              true,
		"--quiet":         true,
	},
)

var gitShowFlagsWithArgs = mergeBoolMaps(
	gitLogDisplayFlagsWithArgs,
	map[string]bool{
		"--word-diff-regex": true,
		"--pretty":          true,
		"--format":          true,
		"--diff-filter":     true,
	},
)

var gitStatusAllowedFlags = map[string]bool{
	"--short":           true,
	"-s":                true,
	"--branch":          true,
	"-b":                true,
	"--porcelain":       true,
	"--long":            true,
	"--verbose":         true,
	"-v":                true,
	"--ignored":         true,
	"--column":          true,
	"--no-column":       true,
	"--ahead-behind":    true,
	"--no-ahead-behind": true,
	"--renames":         true,
	"--no-renames":      true,
}

var gitStatusFlagsWithArgs = map[string]bool{
	"--untracked-files":   true,
	"-u":                  true,
	"--ignore-submodules": true,
	"--find-renames":      true,
	"-M":                  true,
}

var gitLsFilesAllowedFlags = map[string]bool{
	"--cached":             true,
	"-c":                   true,
	"--deleted":            true,
	"-d":                   true,
	"--modified":           true,
	"-m":                   true,
	"--others":             true,
	"-o":                   true,
	"--ignored":            true,
	"-i":                   true,
	"--stage":              true,
	"-s":                   true,
	"--killed":             true,
	"-k":                   true,
	"--unmerged":           true,
	"-u":                   true,
	"--directory":          true,
	"--no-empty-directory": true,
	"--eol":                true,
	"--full-name":          true,
	"--debug":              true,
	"-z":                   true,
	"-t":                   true,
	"-v":                   true,
	"-f":                   true,
	"--exclude-standard":   true,
	"--error-unmatch":      true,
	"--recurse-submodules": true,
}

var gitLsFilesFlagsWithArgs = map[string]bool{
	"--abbrev":                true,
	"--exclude":               true,
	"-x":                      true,
	"--exclude-from":          true,
	"-X":                      true,
	"--exclude-per-directory": true,
}

var gitGrepAllowedFlags = map[string]bool{
	"-E":                    true,
	"--extended-regexp":     true,
	"-G":                    true,
	"--basic-regexp":        true,
	"-F":                    true,
	"--fixed-strings":       true,
	"-P":                    true,
	"--perl-regexp":         true,
	"-i":                    true,
	"--ignore-case":         true,
	"-v":                    true,
	"--invert-match":        true,
	"-w":                    true,
	"--word-regexp":         true,
	"-n":                    true,
	"--line-number":         true,
	"-c":                    true,
	"--count":               true,
	"-l":                    true,
	"--files-with-matches":  true,
	"-L":                    true,
	"--files-without-match": true,
	"-h":                    true,
	"-H":                    true,
	"--heading":             true,
	"--break":               true,
	"--full-name":           true,
	"--color":               true,
	"--no-color":            true,
	"-o":                    true,
	"--only-matching":       true,
	"--and":                 true,
	"--or":                  true,
	"--not":                 true,
	"--untracked":           true,
	"--no-index":            true,
	"--recurse-submodules":  true,
	"--cached":              true,
	"-q":                    true,
	"--quiet":               true,
}

var gitGrepFlagsWithArgs = map[string]bool{
	"-e":               true,
	"-A":               true,
	"--after-context":  true,
	"-B":               true,
	"--before-context": true,
	"-C":               true,
	"--context":        true,
	"--max-depth":      true,
	"--threads":        true,
}

var gitRevParseAllowedFlags = map[string]bool{
	"--verify":                         true,
	"--abbrev-ref":                     true,
	"--symbolic":                       true,
	"--symbolic-full-name":             true,
	"--show-toplevel":                  true,
	"--show-cdup":                      true,
	"--show-prefix":                    true,
	"--git-dir":                        true,
	"--git-common-dir":                 true,
	"--absolute-git-dir":               true,
	"--show-superproject-working-tree": true,
	"--is-inside-work-tree":            true,
	"--is-inside-git-dir":              true,
	"--is-bare-repository":             true,
	"--is-shallow-repository":          true,
	"--is-shallow-update":              true,
	"--path-prefix":                    true,
}

var gitRevParseFlagsWithArgs = map[string]bool{
	"--short": true,
}

var gitLsRemoteAllowedFlags = map[string]bool{
	"--branches":  true,
	"-b":          true,
	"--tags":      true,
	"-t":          true,
	"--heads":     true,
	"-h":          true,
	"--refs":      true,
	"--quiet":     true,
	"-q":          true,
	"--exit-code": true,
	"--get-url":   true,
	"--symref":    true,
}

var gitLsRemoteFlagsWithArgs = map[string]bool{
	"--sort": true,
}

var gitBranchAllowedFlags = map[string]bool{
	"-l":             true,
	"--list":         true,
	"-a":             true,
	"--all":          true,
	"-r":             true,
	"--remotes":      true,
	"-v":             true,
	"-vv":            true,
	"--verbose":      true,
	"--color":        true,
	"--no-color":     true,
	"--column":       true,
	"--no-column":    true,
	"--no-abbrev":    true,
	"--show-current": true,
	"-i":             true,
	"--ignore-case":  true,
}

var gitBranchFlagsWithArgs = map[string]bool{
	"--contains":    true,
	"--no-contains": true,
	"--points-at":   true,
	"--sort":        true,
}

var gitBranchOptionalArgFlags = map[string]bool{
	"--merged":    true,
	"--no-merged": true,
}

var gitTagAllowedFlags = map[string]bool{
	"-l":            true,
	"--list":        true,
	"--column":      true,
	"--no-column":   true,
	"-i":            true,
	"--ignore-case": true,
}

var gitTagFlagsWithArgs = map[string]bool{
	"-n":            true,
	"--contains":    true,
	"--no-contains": true,
	"--merged":      true,
	"--no-merged":   true,
	"--sort":        true,
	"--format":      true,
	"--points-at":   true,
}

var gitReflogFlagsWithArgs = map[string]bool{
	"-n":          true,
	"--max-count": true,
	"--date":      true,
	"--format":    true,
	"--pretty":    true,
	"--grep":      true,
	"--author":    true,
	"--committer": true,
	"--since":     true,
	"--after":     true,
	"--until":     true,
	"--before":    true,
}

var gitStashShowFlagsWithArgs = map[string]bool{
	"--word-diff-regex": true,
	"--diff-filter":     true,
	"--abbrev":          true,
}

var gitStashListAllowedFlags = map[string]bool{
	"--oneline":       true,
	"--graph":         true,
	"--decorate":      true,
	"--no-decorate":   true,
	"--relative-date": true,
	"--all":           true,
	"--branches":      true,
	"--tags":          true,
	"--remotes":       true,
}

var gitStashListFlagsWithArgs = map[string]bool{
	"--date":      true,
	"--max-count": true,
	"-n":          true,
	"--since":     true,
	"--after":     true,
	"--until":     true,
	"--before":    true,
}

var gitStashShowAllowedFlags = map[string]bool{
	"--stat":        true,
	"--numstat":     true,
	"--shortstat":   true,
	"--name-only":   true,
	"--name-status": true,
	"--color":       true,
	"--no-color":    true,
	"--patch":       true,
	"-p":            true,
	"--no-patch":    true,
	"--no-ext-diff": true,
	"-s":            true,
	"--word-diff":   true,
}

var gitWorktreeListAllowedFlags = map[string]bool{
	"--porcelain": true,
	"-v":          true,
	"--verbose":   true,
}

var gitWorktreeListFlagsWithArgs = map[string]bool{
	"--expire": true,
}

var gitMergeBaseAllowedFlags = map[string]bool{
	"--is-ancestor": true,
	"--fork-point":  true,
	"--octopus":     true,
	"--independent": true,
	"--all":         true,
}

var gitDescribeAllowedFlags = map[string]bool{
	"--tags":        true,
	"--long":        true,
	"--always":      true,
	"--contains":    true,
	"--first-match": true,
	"--exact-match": true,
	"--dirty":       true,
	"--broken":      true,
}

var gitDescribeFlagsWithArgs = map[string]bool{
	"--match":      true,
	"--exclude":    true,
	"--abbrev":     true,
	"--candidates": true,
}

var gitCatFileAllowedFlags = map[string]bool{
	"-t":                        true,
	"-s":                        true,
	"-p":                        true,
	"-e":                        true,
	"--batch-check":             true,
	"--allow-undetermined-type": true,
}

var gitForEachRefFlagsWithArgs = map[string]bool{
	"--format":      true,
	"--sort":        true,
	"--count":       true,
	"--contains":    true,
	"--no-contains": true,
	"--merged":      true,
	"--no-merged":   true,
	"--points-at":   true,
}

var gitRevListAllowedFlags = map[string]bool{
	"--all":            true,
	"--branches":       true,
	"--tags":           true,
	"--remotes":        true,
	"--count":          true,
	"--reverse":        true,
	"--first-parent":   true,
	"--ancestry-path":  true,
	"--merges":         true,
	"--no-merges":      true,
	"--no-min-parents": true,
	"--no-max-parents": true,
	"--walk-reflogs":   true,
	"--oneline":        true,
	"--abbrev-commit":  true,
	"--full-history":   true,
	"--dense":          true,
	"--sparse":         true,
	"--source":         true,
	"--graph":          true,
}

var gitRevListFlagsWithArgs = map[string]bool{
	"--since":       true,
	"--after":       true,
	"--until":       true,
	"--before":      true,
	"--max-count":   true,
	"-n":            true,
	"--author":      true,
	"--committer":   true,
	"--grep":        true,
	"--min-parents": true,
	"--max-parents": true,
	"--skip":        true,
	"--max-age":     true,
	"--min-age":     true,
	"--pretty":      true,
	"--format":      true,
	"--abbrev":      true,
}

var gitBlameAllowedFlags = map[string]bool{
	"--color":          true,
	"--no-color":       true,
	"--porcelain":      true,
	"-p":               true,
	"--line-porcelain": true,
	"--incremental":    true,
	"--root":           true,
	"--show-stats":     true,
	"--show-name":      true,
	"--show-number":    true,
	"-n":               true,
	"--show-email":     true,
	"-e":               true,
	"-f":               true,
	"-w":               true,
	"-M":               true,
	"-C":               true,
	"--score-debug":    true,
	"-s":               true,
	"-l":               true,
	"-t":               true,
}

var gitBlameFlagsWithArgs = map[string]bool{
	"-L":                 true,
	"--date":             true,
	"--ignore-rev":       true,
	"--ignore-revs-file": true,
	"--abbrev":           true,
}

var gitShortlogAllowedFlags = map[string]bool{
	"--all":       true,
	"--branches":  true,
	"--tags":      true,
	"--remotes":   true,
	"-s":          true,
	"--summary":   true,
	"-n":          true,
	"--numbered":  true,
	"-e":          true,
	"--email":     true,
	"-c":          true,
	"--committer": true,
	"--no-merges": true,
}

var gitShortlogFlagsWithArgs = map[string]bool{
	"--since":  true,
	"--after":  true,
	"--until":  true,
	"--before": true,
	"--group":  true,
	"--format": true,
	"--author": true,
}

var gitConfigGetAllowedFlags = map[string]bool{
	"--local":       true,
	"--global":      true,
	"--system":      true,
	"--worktree":    true,
	"--bool":        true,
	"--int":         true,
	"--bool-or-int": true,
	"--path":        true,
	"--expiry-date": true,
	"-z":            true,
	"--null":        true,
	"--name-only":   true,
	"--show-origin": true,
	"--show-scope":  true,
}

var gitConfigGetFlagsWithArgs = map[string]bool{
	"--default": true,
	"--type":    true,
}

var bashSafeEnvVars = map[string]bool{
	"GOEXPERIMENT":                   true,
	"GOOS":                           true,
	"GOARCH":                         true,
	"CGO_ENABLED":                    true,
	"GO111MODULE":                    true,
	"RUST_BACKTRACE":                 true,
	"RUST_LOG":                       true,
	"NODE_ENV":                       true,
	"PYTHONUNBUFFERED":               true,
	"PYTHONDONTWRITEBYTECODE":        true,
	"PYTEST_DISABLE_PLUGIN_AUTOLOAD": true,
	"PYTEST_DEBUG":                   true,
	"ANTHROPIC_API_KEY":              true,
	"LANG":                           true,
	"LANGUAGE":                       true,
	"LC_ALL":                         true,
	"LC_CTYPE":                       true,
	"LC_TIME":                        true,
	"CHARSET":                        true,
	"TERM":                           true,
	"COLORTERM":                      true,
	"NO_COLOR":                       true,
	"FORCE_COLOR":                    true,
	"TZ":                             true,
	"LS_COLORS":                      true,
	"LSCOLORS":                       true,
	"GREP_COLOR":                     true,
	"GREP_COLORS":                    true,
	"GCC_COLORS":                     true,
	"TIME_STYLE":                     true,
	"BLOCK_SIZE":                     true,
	"BLOCKSIZE":                      true,
}

type bashInput struct {
	Command               string `json:"command"`
	Timeout               *int   `json:"timeout,omitempty"`
	Description           string `json:"description,omitempty"`
	RunInBackground       bool   `json:"run_in_background,omitempty"`
	RunInBackgroundAlt    bool   `json:"runInBackground,omitempty"`
	hasRunInBackground    bool
	hasRunInBackgroundAlt bool
}

type bashResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	TimedOut   bool
	DurationMS int64
	TimeoutMS  int
}

type bashOutputInput struct {
	BashID       string `json:"bash_id,omitempty"`
	ID           string `json:"id,omitempty"`
	TailLines    *int   `json:"tail_lines,omitempty"`
	TailLinesAlt *int   `json:"tailLines,omitempty"`
}

type bashKillInput struct {
	BashID string `json:"bash_id,omitempty"`
	ID     string `json:"id,omitempty"`
}

func NewBashTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "Bash",
			Description:        "Run a shell command.",
			SearchHint:         "run shell command",
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"command"},
				"properties": map[string]any{
					"command":           map[string]any{"type": "string"},
					"timeout":           map[string]any{"type": "integer"},
					"description":       map[string]any{"type": "string"},
					"run_in_background": map[string]any{"type": "boolean"},
					"runInBackground":   map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Runs a shell command in the current working directory. Provide command, optional timeout in milliseconds, optional short description, and run_in_background for background commands. Full sandbox parity and interrupt controls are not implemented yet.", nil
		},
		ValidateFunc:    validateBash,
		CallFunc:        callBash,
		ReadOnlyFunc:    bashReadOnlyInput,
		ConcurrencyFunc: bashReadOnlyInput,
		DestructiveFunc: bashDestructiveInput,
	}
}

func NewBashOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "BashOutput",
			Description:     "Read output from a background Bash command.",
			SearchHint:      "read background shell command output",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"bash_id":    map[string]any{"type": "string"},
					"id":         map[string]any{"type": "string"},
					"tail_lines": map[string]any{"type": "integer"},
					"tailLines":  map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads stdout, stderr, and status for a Bash command started with run_in_background.", nil
		},
		ValidateFunc:    validateBashOutput,
		CallFunc:        callBashOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewKillBashTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "KillBash",
			Description:     "Cancel a background Bash command.",
			SearchHint:      "kill cancel background shell command",
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"bash_id": map[string]any{"type": "string"},
					"id":      map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Cancels a Bash command started with run_in_background. Use BashOutput to read the final output and status.", nil
		},
		ValidateFunc:    validateKillBash,
		CallFunc:        callKillBash,
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateBash(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeBash(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if input.Timeout != nil {
		if *input.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		if *input.Timeout > maxTimeoutMillis {
			return fmt.Errorf("timeout must be at most %d milliseconds", maxTimeoutMillis)
		}
	}
	return nil
}

func validateBashOutput(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeBashOutput(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("bash_id is required")
	}
	if input.TailLines != nil && *input.TailLines <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	if input.TailLinesAlt != nil && *input.TailLinesAlt <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	return nil
}

func validateKillBash(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeKillBash(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("bash_id is required")
	}
	return nil
}

func callBash(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeBash(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	timeout := bashTimeout(input)
	if input.runInBackground() {
		return startBackgroundBash(ctx, input, timeout)
	}
	result := runBashCommand(ctx, strings.TrimSpace(input.Command), timeout)
	return contracts.ToolResult{
		Content: formatBashContent(result),
		IsError: result.TimedOut || result.ExitCode != 0,
		StructuredContent: map[string]any{
			"type":        "bash",
			"command":     input.Command,
			"description": input.Description,
			"stdout":      result.Stdout,
			"stderr":      result.Stderr,
			"exit_code":   result.ExitCode,
			"timed_out":   result.TimedOut,
			"duration_ms": result.DurationMS,
			"timeout_ms":  result.TimeoutMS,
		},
	}, nil
}

func callBashOutput(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeBashOutput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background bash command not found: %s", input.backgroundID())
	}
	snapshot := task.Snapshot()
	tailLines := bashOutputTailLines(input)
	if tailLines > 0 {
		snapshot.Stdout = tailText(snapshot.Stdout, tailLines)
		snapshot.Stderr = tailText(snapshot.Stderr, tailLines)
	}
	return contracts.ToolResult{
		Content: formatBackgroundOutput(snapshot),
		IsError: !snapshot.Running && (snapshot.TimedOut || snapshot.ExitCode != 0),
		StructuredContent: map[string]any{
			"type":        "bash_output",
			"bash_id":     snapshot.ID,
			"command":     snapshot.Command,
			"description": snapshot.Description,
			"stdout":      snapshot.Stdout,
			"stderr":      snapshot.Stderr,
			"running":     snapshot.Running,
			"exit_code":   snapshot.ExitCode,
			"timed_out":   snapshot.TimedOut,
			"cancelled":   snapshot.Cancelled,
			"duration_ms": snapshot.DurationMS,
			"timeout_ms":  snapshot.TimeoutMS,
			"started_at":  snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
			"ended_at":    formatOptionalTime(snapshot.EndedAt),
			"error":       snapshot.Error,
		},
	}, nil
}

func callKillBash(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeKillBash(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background bash command not found: %s", input.backgroundID())
	}
	killed := task.Cancel()
	snapshot := task.Snapshot()
	content := fmt.Sprintf("Kill requested for background command %s.", snapshot.ID)
	if !killed {
		content = fmt.Sprintf("Background command %s is not running.", snapshot.ID)
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":      "kill_bash",
			"bash_id":   snapshot.ID,
			"command":   snapshot.Command,
			"running":   snapshot.Running,
			"killed":    killed,
			"cancelled": snapshot.Cancelled,
		},
	}, nil
}

func runBashCommand(ctx tool.Context, command string, timeout time.Duration) bashResult {
	baseCtx := ctx.Context
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(baseCtx, timeout)
	defer cancel()

	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	configureBashCommand(cmd)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	durationMS := time.Since(start).Milliseconds()
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if timedOut {
			exitCode = -1
		}
	}
	return bashResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   exitCode,
		TimedOut:   timedOut,
		DurationMS: durationMS,
		TimeoutMS:  int(timeout / time.Millisecond),
	}
}

func startBackgroundBash(ctx tool.Context, input bashInput, timeout time.Duration) (contracts.ToolResult, error) {
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	command := strings.TrimSpace(input.Command)
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	configureBashCommand(cmd)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	task := &BackgroundTask{
		ID:          "bash_" + string(contracts.NewID()),
		Command:     command,
		Description: input.Description,
		StartedAt:   time.Now(),
		TimeoutMS:   int(timeout / time.Millisecond),
		Running:     true,
		ExitCode:    0,
	}
	cmd.Stdout = &task.stdout
	cmd.Stderr = &task.stderr
	task.SetCancel(func() {
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		}
		cancel()
	})
	if err := cmd.Start(); err != nil {
		cancel()
		return contracts.ToolResult{}, err
	}
	state.Add(task)
	go func() {
		defer cancel()
		err := cmd.Wait()
		durationMS := time.Since(task.StartedAt).Milliseconds()
		timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
		cancelled := task.IsCancelled()
		exitCode := 0
		errText := ""
		if err != nil {
			exitCode = 1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else if !timedOut && !cancelled {
				errText = err.Error()
			}
			if timedOut || cancelled {
				exitCode = -1
			}
		}
		if cancelled && errText == "" {
			errText = "cancelled"
		}
		task.Finish(exitCode, timedOut, durationMS, errText, time.Now())
	}()
	return contracts.ToolResult{
		Content: fmt.Sprintf("Command started in background with ID: %s", task.ID),
		StructuredContent: map[string]any{
			"type":        "bash_background",
			"bash_id":     task.ID,
			"command":     command,
			"description": input.Description,
			"running":     true,
			"timeout_ms":  task.TimeoutMS,
			"started_at":  task.StartedAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func configureBashCommand(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killBashProcessGroup(cmd)
	}
}

func killBashProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func formatBashContent(result bashResult) string {
	var b strings.Builder
	if result.Stdout != "" {
		b.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(result.Stderr)
	}
	content := strings.TrimRight(b.String(), "\n")
	if result.TimedOut {
		return appendStatusLine(content, fmt.Sprintf("Command timed out after %dms.", result.TimeoutMS))
	}
	if result.ExitCode != 0 {
		return appendStatusLine(content, fmt.Sprintf("Command exited with code %d.", result.ExitCode))
	}
	if content == "" {
		return "Command completed successfully with no output."
	}
	return content
}

func appendStatusLine(content string, status string) string {
	if content == "" {
		return status
	}
	return content + "\n" + status
}

func formatBackgroundOutput(snapshot BackgroundTaskSnapshot) string {
	var status string
	if snapshot.Running {
		status = fmt.Sprintf("Background command %s is still running.", snapshot.ID)
	} else if snapshot.Cancelled {
		status = fmt.Sprintf("Background command %s was cancelled.", snapshot.ID)
	} else if snapshot.TimedOut {
		status = fmt.Sprintf("Background command %s timed out after %dms.", snapshot.ID, snapshot.TimeoutMS)
	} else {
		status = fmt.Sprintf("Background command %s completed with exit code %d.", snapshot.ID, snapshot.ExitCode)
	}
	output := formatBashContent(bashResult{
		Stdout:    snapshot.Stdout,
		Stderr:    snapshot.Stderr,
		ExitCode:  snapshot.ExitCode,
		TimedOut:  snapshot.TimedOut,
		TimeoutMS: snapshot.TimeoutMS,
	})
	if snapshot.Running && snapshot.Stdout == "" && snapshot.Stderr == "" {
		return status
	}
	if output == "Command completed successfully with no output." {
		return status
	}
	return status + "\n\n" + output
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func bashTimeout(input bashInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func decodeBash(raw json.RawMessage) (bashInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashInput{}, err
	}
	for key := range obj {
		switch key {
		case "command", "timeout", "description", "run_in_background", "runInBackground":
		default:
			return bashInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashInput{}, err
	}
	if _, ok := obj["run_in_background"]; ok {
		input.hasRunInBackground = true
	}
	if _, ok := obj["runInBackground"]; ok {
		input.hasRunInBackgroundAlt = true
	}
	return input, nil
}

func decodeBashOutput(raw json.RawMessage) (bashOutputInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashOutputInput{}, err
	}
	for key := range obj {
		switch key {
		case "bash_id", "id", "tail_lines", "tailLines":
		default:
			return bashOutputInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashOutputInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashOutputInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashOutputInput{}, err
	}
	return input, nil
}

func decodeKillBash(raw json.RawMessage) (bashKillInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashKillInput{}, err
	}
	for key := range obj {
		switch key {
		case "bash_id", "id":
		default:
			return bashKillInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashKillInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashKillInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashKillInput{}, err
	}
	return input, nil
}

func (input bashInput) runInBackground() bool {
	if input.hasRunInBackground {
		return input.RunInBackground
	}
	if input.hasRunInBackgroundAlt {
		return input.RunInBackgroundAlt
	}
	return false
}

func (input bashOutputInput) backgroundID() string {
	if strings.TrimSpace(input.BashID) != "" {
		return strings.TrimSpace(input.BashID)
	}
	return strings.TrimSpace(input.ID)
}

func (input bashKillInput) backgroundID() string {
	if strings.TrimSpace(input.BashID) != "" {
		return strings.TrimSpace(input.BashID)
	}
	return strings.TrimSpace(input.ID)
}

func bashOutputTailLines(input bashOutputInput) int {
	if input.TailLines != nil {
		return *input.TailLines
	}
	if input.TailLinesAlt != nil {
		return *input.TailLinesAlt
	}
	return 0
}

func tailText(text string, lines int) string {
	if lines <= 0 || text == "" {
		return text
	}
	parts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(parts) <= lines {
		return text
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func bashReadOnlyInput(raw json.RawMessage) bool {
	input, err := decodeBash(raw)
	if err != nil {
		return false
	}
	return IsReadOnlyCommand(input.Command)
}

func bashDestructiveInput(raw json.RawMessage) bool {
	input, err := decodeBash(raw)
	if err != nil {
		return false
	}
	return IsDestructiveCommand(input.Command)
}

func IsReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(stripShellLineComments(command))
	if command == "" || !shellSyntaxComplete(command) || hasShellMutationSyntax(command) || IsDestructiveCommand(command) {
		return false
	}
	segments := splitCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		words := shellWords(segment)
		if len(words) == 0 {
			return false
		}
		if !readOnlyWords(words) {
			return false
		}
	}
	return true
}

func IsDestructiveCommand(command string) bool {
	command = stripShellLineComments(command)
	if hasNestedShellDestructiveCommand(command) {
		return true
	}
	for _, segment := range splitCommandSegments(command) {
		words := shellWords(segment)
		if len(words) == 0 {
			continue
		}
		if destructiveWords(words) {
			return true
		}
	}
	return false
}

func hasShellMutationSyntax(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}
		if inDouble {
			if ch == '$' || ch == '`' {
				return true
			}
			continue
		}
		switch ch {
		case '>', '<', '$', '`':
			return true
		case '&':
			if i+1 >= len(command) || command[i+1] != '&' {
				return true
			}
		}
	}
	return false
}

func shellSyntaxComplete(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
	}
	return !inSingle && !inDouble && !escaped
}

func hasNestedShellDestructiveCommand(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}
		if ch == '`' {
			end := findShellClosingBacktick(command, i+1)
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+1 : end]) {
				return true
			}
			i = end
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
			end := findShellClosingParen(command, i+1)
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+2 : end]) {
				return true
			}
			i = end
			continue
		}
		if !inDouble && ch == '(' {
			end := findShellClosingParen(command, i)
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+1 : end]) {
				return true
			}
			i = end
		}
	}
	return false
}

func findShellClosingBacktick(command string, start int) int {
	escaped := false
	for i := start; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '`' {
			return i
		}
	}
	return -1
}

func findShellClosingParen(command string, open int) int {
	depth := 1
	inSingle := false
	inDouble := false
	escaped := false
	for i := open + 1; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripShellLineComments(command string) string {
	var stripped strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	wordStart := true
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			stripped.WriteByte(ch)
			escaped = false
			wordStart = false
			continue
		}
		if ch == '\\' && !inSingle {
			stripped.WriteByte(ch)
			escaped = true
			wordStart = false
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			stripped.WriteByte(ch)
			wordStart = false
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			stripped.WriteByte(ch)
			wordStart = false
			continue
		}
		if !inSingle && !inDouble && ch == '#' && wordStart {
			for i+1 < len(command) && command[i+1] != '\n' && command[i+1] != '\r' {
				i++
			}
			continue
		}
		stripped.WriteByte(ch)
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ';' || ch == '|' || ch == '&') {
			wordStart = true
		} else {
			wordStart = false
		}
	}
	return stripped.String()
}

func splitCommandSegments(command string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			current.WriteByte(ch)
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}
		if !inSingle && !inDouble {
			switch ch {
			case ';', '|', '\n', '\r':
				segments = appendNonemptySegment(segments, current.String())
				current.Reset()
				continue
			case '&':
				if i+1 < len(command) && command[i+1] == '&' {
					segments = appendNonemptySegment(segments, current.String())
					current.Reset()
					i++
					continue
				}
				segments = appendNonemptySegment(segments, current.String())
				current.Reset()
				continue
			}
		}
		current.WriteByte(ch)
	}
	return appendNonemptySegment(segments, current.String())
}

func appendNonemptySegment(segments []string, segment string) []string {
	segment = strings.TrimSpace(segment)
	if segment != "" {
		segments = append(segments, segment)
	}
	return segments
}

func shellWords(command string) []string {
	var words []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n') {
			flush()
			continue
		}
		current.WriteByte(ch)
	}
	flush()
	return words
}

func stripSafeWrapperWords(words []string) []string {
	words = stripLeadingSafeAssignments(words)
	for len(words) > 0 {
		switch filepathBase(words[0]) {
		case "time", "nohup":
			next := 1
			if len(words) > 1 && words[1] == "--" {
				next = 2
			}
			words = words[next:]
		case "timeout":
			duration := timeoutDurationIndex(words)
			if duration < 0 || duration+1 >= len(words) {
				return words
			}
			words = words[duration+1:]
		case "nice":
			commandIndex := niceCommandIndex(words)
			if commandIndex < 0 {
				return words
			}
			words = words[commandIndex:]
		case "stdbuf":
			commandIndex := stdbufCommandIndex(words)
			if commandIndex < 0 {
				return words
			}
			words = words[commandIndex:]
		case "env":
			commandIndex := envCommandIndex(words)
			if commandIndex < 0 {
				return words
			}
			words = words[commandIndex:]
		default:
			return words
		}
	}
	return words
}

func stripLeadingSafeAssignments(words []string) []string {
	for len(words) > 0 && isSafeEnvAssignment(words[0]) {
		words = words[1:]
	}
	return words
}

func stripLeadingAssignments(words []string) []string {
	for len(words) > 0 && isShellAssignment(words[0]) {
		words = words[1:]
	}
	return words
}

func timeoutDurationIndex(words []string) int {
	for i := 1; i < len(words); {
		arg := words[i]
		next := ""
		if i+1 < len(words) {
			next = words[i+1]
		}
		switch {
		case arg == "--foreground" || arg == "--preserve-status" || arg == "--verbose" || arg == "-v":
			i++
		case strings.HasPrefix(arg, "--kill-after=") || strings.HasPrefix(arg, "--signal="):
			if !isSafeWrapperValue(strings.SplitN(arg, "=", 2)[1]) {
				return -1
			}
			i++
		case (arg == "--kill-after" || arg == "--signal" || arg == "-k" || arg == "-s") && next != "":
			if !isSafeWrapperValue(next) {
				return -1
			}
			i += 2
		case (strings.HasPrefix(arg, "-k") || strings.HasPrefix(arg, "-s")) && len(arg) > 2:
			if !isSafeWrapperValue(arg[2:]) {
				return -1
			}
			i++
		case arg == "--":
			i++
			if i < len(words) && isTimeoutDuration(words[i]) {
				return i
			}
			return -1
		case strings.HasPrefix(arg, "-"):
			return -1
		default:
			if isTimeoutDuration(arg) {
				return i
			}
			return -1
		}
	}
	return -1
}

func niceCommandIndex(words []string) int {
	i := 1
	if i < len(words) && words[i] == "-n" {
		if i+1 >= len(words) || !isSignedInteger(words[i+1]) {
			return -1
		}
		i += 2
	} else if i < len(words) && strings.HasPrefix(words[i], "-") && isSignedInteger(words[i]) {
		i++
	}
	if i < len(words) && words[i] == "--" {
		i++
	}
	if i < len(words) {
		return i
	}
	return -1
}

func stdbufCommandIndex(words []string) int {
	consumed := false
	i := 1
	for i < len(words) {
		arg := words[i]
		switch {
		case (arg == "-i" || arg == "-o" || arg == "-e") && i+1 < len(words):
			consumed = true
			i += 2
		case len(arg) > 2 && arg[0] == '-' && (arg[1] == 'i' || arg[1] == 'o' || arg[1] == 'e'):
			consumed = true
			i++
		case strings.HasPrefix(arg, "--input=") || strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--error="):
			consumed = true
			i++
		case strings.HasPrefix(arg, "-"):
			return -1
		default:
			if consumed {
				return i
			}
			return -1
		}
	}
	return -1
}

func envCommandIndex(words []string) int {
	for i := 1; i < len(words); {
		arg := words[i]
		switch {
		case isSafeEnvAssignment(arg):
			i++
		case arg == "-i" || arg == "-0" || arg == "-v":
			i++
		case arg == "-u" && i+1 < len(words):
			if !isShellIdentifier(words[i+1]) {
				return -1
			}
			i += 2
		case strings.HasPrefix(arg, "-"):
			return -1
		default:
			return i
		}
	}
	return -1
}

func isSafeEnvAssignment(word string) bool {
	name, value, ok := strings.Cut(word, "=")
	return ok && bashSafeEnvVars[name] && isShellIdentifier(name) && isSafeEnvValue(value)
}

func isShellAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	return ok && isShellIdentifier(name)
}

func isShellIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				continue
			}
			return false
		}
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func isSafeEnvValue(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '/' || r == ':' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func isSafeWrapperValue(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '+' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func isTimeoutDuration(value string) bool {
	if value == "" {
		return false
	}
	if last := value[len(value)-1]; last == 's' || last == 'm' || last == 'h' || last == 'd' {
		value = value[:len(value)-1]
	}
	if value == "" {
		return false
	}
	if value[0] < '0' || value[0] > '9' {
		return false
	}
	seenDigit := false
	seenDot := false
	digitAfterDot := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
			if seenDot {
				digitAfterDot = true
			}
		case r == '.' && !seenDot:
			seenDot = true
		default:
			return false
		}
	}
	return seenDigit && (!seenDot || digitAfterDot)
}

func isSignedInteger(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readOnlyWords(words []string) bool {
	words = stripSafeWrapperWords(words)
	if len(words) == 0 {
		return false
	}
	cmd := filepathBase(words[0])
	switch cmd {
	case "ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "rg", "find", "stat", "file", "du", "df":
		return readOnlyPathCommand(words)
	case "pwd", "printf", "echo", "date", "whoami", "id", "uname", "printenv", "which", "type":
		return true
	case "env":
		return readOnlyEnv(words)
	case "git":
		return readOnlyGit(words)
	case "go":
		return len(words) >= 2 && words[1] == "list"
	default:
		return false
	}
}

func readOnlyPathCommand(words []string) bool {
	for _, word := range words[1:] {
		if word == "--" {
			continue
		}
		if strings.HasPrefix(word, "-") {
			if name, value, ok := strings.Cut(word, "="); ok {
				if name == "" || !safeRelativeShellPathArg(value) {
					return false
				}
			}
			continue
		}
		if !safeRelativeShellPathArg(word) {
			return false
		}
	}
	return true
}

func safeRelativeShellPathArg(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" || strings.ContainsAny(value, "$`\x00") {
		return false
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") {
		return false
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func readOnlyEnv(words []string) bool {
	for i := 1; i < len(words); {
		arg := words[i]
		switch {
		case isSafeEnvAssignment(arg):
			i++
		case arg == "-i" || arg == "-0" || arg == "-v":
			i++
		case arg == "-u" && i+1 < len(words) && isShellIdentifier(words[i+1]):
			i += 2
		default:
			return false
		}
	}
	return true
}

func readOnlyGit(words []string) bool {
	if len(words) < 2 {
		return false
	}
	switch words[1] {
	case "diff":
		return argsAllowPositionals(words[2:], gitDiffAllowedFlags, gitDiffFlagsWithArgs, -1)
	case "log":
		return argsAllowPositionals(words[2:], gitLogAllowedFlags, gitLogFlagsWithArgs, -1)
	case "show":
		return argsAllowPositionals(words[2:], gitShowAllowedFlags, gitShowFlagsWithArgs, -1)
	case "status":
		return argsAllowPositionals(words[2:], gitStatusAllowedFlags, gitStatusFlagsWithArgs, -1)
	case "ls-files":
		return argsAllowPositionals(words[2:], gitLsFilesAllowedFlags, gitLsFilesFlagsWithArgs, -1)
	case "grep":
		return argsAllowPositionals(words[2:], gitGrepAllowedFlags, gitGrepFlagsWithArgs, -1)
	case "rev-parse":
		return argsAllowPositionals(words[2:], gitRevParseAllowedFlags, gitRevParseFlagsWithArgs, -1)
	case "ls-remote":
		return readOnlyGitLsRemote(words[2:])
	case "branch":
		return readOnlyGitBranch(words[2:])
	case "tag":
		return readOnlyGitTag(words[2:])
	case "remote":
		return readOnlyGitRemote(words[2:])
	case "reflog":
		return readOnlyGitReflog(words[2:])
	case "stash":
		return readOnlyGitStash(words[2:])
	case "worktree":
		return readOnlyGitWorktree(words[2:])
	case "merge-base":
		return argsAllowPositionals(words[2:], gitMergeBaseAllowedFlags, nil, -1)
	case "describe":
		return argsAllowPositionals(words[2:], gitDescribeAllowedFlags, gitDescribeFlagsWithArgs, -1)
	case "cat-file":
		return argsAllowPositionals(words[2:], gitCatFileAllowedFlags, nil, 1)
	case "for-each-ref":
		return argsAllowPositionals(words[2:], nil, gitForEachRefFlagsWithArgs, -1)
	case "rev-list":
		return argsAllowPositionals(words[2:], gitRevListAllowedFlags, gitRevListFlagsWithArgs, -1)
	case "blame":
		return argsAllowPositionals(words[2:], gitBlameAllowedFlags, gitBlameFlagsWithArgs, -1)
	case "shortlog":
		return argsAllowPositionals(words[2:], gitShortlogAllowedFlags, gitShortlogFlagsWithArgs, -1)
	case "config":
		return readOnlyGitConfig(words[2:])
	default:
		return false
	}
}

func readOnlyGitBranch(args []string) bool {
	return argsAllowListModePositionals(args, gitBranchAllowedFlags, gitBranchFlagsWithArgs, gitBranchOptionalArgFlags)
}

func readOnlyGitTag(args []string) bool {
	return argsAllowListModePositionals(args, gitTagAllowedFlags, gitTagFlagsWithArgs, nil)
}

func readOnlyGitLsRemote(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			for _, positional := range args[i+1:] {
				if !safeGitLsRemoteArg(positional) {
					return false
				}
			}
			return true
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				name := strings.SplitN(arg, "=", 2)[0]
				if !gitLsRemoteFlagsWithArgs[name] {
					return false
				}
				continue
			}
			if gitLsRemoteFlagsWithArgs[arg] && i+1 < len(args) {
				i++
				continue
			}
			if !gitLsRemoteAllowedFlags[arg] {
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !gitLsRemoteAllowedFlags[arg] {
				return false
			}
			continue
		}
		if !safeGitLsRemoteArg(arg) {
			return false
		}
	}
	return true
}

func safeGitLsRemoteArg(arg string) bool {
	return arg != "" && !strings.Contains(arg, "://") && !strings.Contains(arg, "@") && !strings.Contains(arg, ":") && !strings.Contains(arg, "$")
}

func readOnlyGitRemote(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 1 && (args[0] == "-v" || args[0] == "--verbose") {
		return true
	}
	switch args[0] {
	case "show":
		return readOnlyGitRemoteShow(args[1:])
	case "get-url":
		return readOnlyGitRemoteGetURL(args[1:])
	default:
		return false
	}
}

func readOnlyGitRemoteShow(args []string) bool {
	positionals := 0
	for _, arg := range args {
		if arg == "-n" {
			continue
		}
		positionals++
		if positionals > 1 || !safeGitRemoteName(arg) {
			return false
		}
	}
	return positionals == 1
}

func readOnlyGitRemoteGetURL(args []string) bool {
	positionals := 0
	for _, arg := range args {
		if arg == "--push" || arg == "--all" {
			continue
		}
		positionals++
		if positionals > 1 || !safeGitRemoteName(arg) {
			return false
		}
	}
	return positionals == 1
}

func safeGitRemoteName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func readOnlyGitReflog(args []string) bool {
	first, ok := gitReflogFirstPositional(args)
	if !ok {
		return true
	}
	return first != "expire" && first != "delete" && first != "exists"
}

func readOnlyGitStash(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "list":
		return argsAreAllowedFlagsOnly(args[1:], gitStashListAllowedFlags, gitStashListFlagsWithArgs)
	case "show":
		return argsAllowPositionals(args[1:], gitStashShowAllowedFlags, gitStashShowFlagsWithArgs, 1)
	default:
		return false
	}
}

func readOnlyGitWorktree(args []string) bool {
	if len(args) == 0 || args[0] != "list" {
		return false
	}
	return argsAreAllowedFlagsOnly(args[1:], gitWorktreeListAllowedFlags, gitWorktreeListFlagsWithArgs)
}

func readOnlyGitConfig(args []string) bool {
	if len(args) == 0 || args[0] != "--get" {
		return false
	}
	return argsAllowPositionals(args[1:], gitConfigGetAllowedFlags, gitConfigGetFlagsWithArgs, 1)
}

func destructiveWords(words []string) bool {
	words = stripSafeWrapperWords(words)
	words = stripLeadingAssignments(words)
	if len(words) == 0 {
		return false
	}
	cmd := filepathBase(words[0])
	switch cmd {
	case "rm", "rmdir", "dd", "mkfs", "shutdown", "reboot", "halt", "poweroff", "kill", "pkill", "killall", "sudo", "su":
		return true
	case "git":
		return destructiveGit(words)
	case "find":
		return destructiveFind(words)
	case "xargs":
		return destructiveXargs(words)
	case "chmod", "chown", "chgrp":
		return hasRecursiveFlag(words)
	default:
		return false
	}
}

func destructiveFind(words []string) bool {
	for i, word := range words[1:] {
		switch word {
		case "-delete":
			return true
		case "-exec", "-execdir", "-ok", "-okdir":
			if i+2 < len(words) && destructiveExecutable(words[i+2]) {
				return true
			}
		}
	}
	return false
}

func destructiveXargs(words []string) bool {
	for _, word := range words[1:] {
		if destructiveExecutable(word) {
			return true
		}
	}
	return false
}

func destructiveExecutable(word string) bool {
	switch filepathBase(word) {
	case "rm", "rmdir", "dd", "mkfs", "shutdown", "reboot", "halt", "poweroff", "kill", "pkill", "killall", "sudo", "su":
		return true
	default:
		return false
	}
}

func destructiveGit(words []string) bool {
	if len(words) < 2 {
		return false
	}
	switch words[1] {
	case "reset":
		return containsWord(words[2:], "--hard")
	case "clean":
		return true
	case "branch":
		return containsAnyWord(words[2:], "-d", "-D", "--delete", "--force")
	case "tag":
		return containsAnyWord(words[2:], "-d", "--delete")
	case "remote":
		return len(words) >= 3 && (words[2] == "remove" || words[2] == "rm")
	case "push":
		return containsAnyWord(words[2:], "-f", "--force", "--force-with-lease", "--delete")
	case "reflog":
		first, ok := gitReflogFirstPositional(words[2:])
		return ok && (first == "expire" || first == "delete")
	case "stash":
		return len(words) >= 3 && (words[2] == "drop" || words[2] == "pop" || words[2] == "clear")
	case "worktree":
		return len(words) >= 3 && (words[2] == "remove" || words[2] == "prune")
	case "checkout", "restore":
		return containsWord(words[2:], ".") || containsWord(words[2:], "--")
	default:
		return false
	}
}

func gitReflogFirstPositional(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}
			if gitReflogFlagsWithArgs[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if gitReflogFlagsWithArgs[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		return arg, true
	}
	return "", false
}

func argsAreAllowedFlagsOnly(args []string, allowedFlags map[string]bool, flagsWithArgs map[string]bool) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return i+1 == len(args)
		}
		if !strings.HasPrefix(arg, "-") {
			return false
		}
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			name := strings.SplitN(arg, "=", 2)[0]
			if !flagsWithArgs[name] {
				return false
			}
			continue
		}
		if flagsWithArgs[arg] && i+1 < len(args) {
			i++
			continue
		}
		if !allowedFlags[arg] {
			return false
		}
	}
	return true
}

func argsAllowPositionals(args []string, allowedFlags map[string]bool, flagsWithArgs map[string]bool, maxPositionals int) bool {
	positionals := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals += len(args) - i - 1
			return maxPositionals < 0 || positionals <= maxPositionals
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				name := strings.SplitN(arg, "=", 2)[0]
				if !flagsWithArgs[name] {
					return false
				}
				continue
			}
			if flagsWithArgs[arg] && i+1 < len(args) {
				i++
				continue
			}
			if !allowedFlags[arg] {
				return false
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if flagsWithArgs[arg] && i+1 < len(args) {
				i++
				continue
			}
			if !allowedFlags[arg] {
				return false
			}
			continue
		}
		positionals++
		if maxPositionals >= 0 && positionals > maxPositionals {
			return false
		}
	}
	return true
}

func argsAllowListModePositionals(args []string, allowedFlags map[string]bool, flagsWithArgs map[string]bool, optionalArgFlags map[string]bool) bool {
	seenListFlag := false
	optionalArgOpen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return seenListFlag
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				name := strings.SplitN(arg, "=", 2)[0]
				switch {
				case flagsWithArgs[name]:
					optionalArgOpen = false
				case allowedFlags[name]:
					seenListFlag = seenListFlag || name == "--list"
					optionalArgOpen = false
				case optionalArgFlags[name]:
					optionalArgOpen = false
				default:
					return false
				}
				continue
			}
			if flagsWithArgs[arg] && i+1 < len(args) {
				i++
				optionalArgOpen = false
				continue
			}
			if optionalArgFlags[arg] {
				optionalArgOpen = true
				continue
			}
			if allowedFlags[arg] {
				seenListFlag = seenListFlag || arg == "--list"
				optionalArgOpen = false
				continue
			}
			return false
		}
		if strings.HasPrefix(arg, "-") {
			if flagsWithArgs[arg] && i+1 < len(args) {
				i++
				optionalArgOpen = false
				continue
			}
			if allowedFlags[arg] {
				seenListFlag = seenListFlag || arg == "-l"
				optionalArgOpen = false
				continue
			}
			return false
		}
		if seenListFlag || optionalArgOpen {
			optionalArgOpen = false
			continue
		}
		return false
	}
	return true
}

func mergeBoolMaps(maps ...map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for _, entries := range maps {
		for key, value := range entries {
			merged[key] = value
		}
	}
	return merged
}

func hasRecursiveFlag(words []string) bool {
	for _, word := range words[1:] {
		if word == "-R" || word == "--recursive" || strings.Contains(word, "R") && strings.HasPrefix(word, "-") {
			return true
		}
	}
	return false
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

func containsAnyWord(words []string, candidates ...string) bool {
	for _, candidate := range candidates {
		if containsWord(words, candidate) {
			return true
		}
	}
	return false
}

func filepathBase(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.Trim(name, `"'`)
	name = strings.TrimSuffix(name, ".exe")
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

package powershelltools

import (
	"strings"

	"ccgo/internal/tool"
)

// PowerShellPrompt composes the full PowerShell tool prompt, mirroring CC's
// getPrompt() (src/tools/PowerShellTool/prompt.ts:73-145). Unlike Bash it has
// no git/PR ceremony, and it adds Windows quoting + non-interactive guards.
func PowerShellPrompt(_ tool.PromptContext) (string, error) {
	return strings.TrimRight(strings.Join([]string{
		powerShellPreamble(),
		"",
		powerShellEditionSection(),
		"",
		powerShellDirectoryVerification(),
		"",
		powerShellSyntaxNotes(),
		"",
		powerShellNonInteractiveGuards(),
		"",
		powerShellHereStrings(),
		"",
		powerShellUsageNotes(),
	}, "\n"), "\n"), nil
}

func powerShellPreamble() string {
	return strings.Join([]string{
		"Executes a given PowerShell command with optional timeout. Working directory persists between commands; shell state (variables, functions) does not.",
		"",
		"IMPORTANT: This tool is for terminal operations via PowerShell: git, npm, docker, and PS cmdlets. DO NOT use it for file operations (reading, writing, editing, searching, finding files) - use the specialized tools for this instead.",
	}, "\n")
}

func powerShellEditionSection() string {
	// Detection not yet resolved — give the conservative 5.1-safe guidance.
	// Mirrors getEditionSection(null) from prompt.ts:67-70.
	return strings.Join([]string{
		"PowerShell edition: unknown — assume Windows PowerShell 5.1 for compatibility",
		"   - Do NOT use `&&`, `||`, ternary `?:`, null-coalescing `??`, or null-conditional `?.`. These are PowerShell 7+ only and parser-error on 5.1.",
		"   - To chain commands conditionally: `A; if ($?) { B }`. Unconditionally: `A; B`.",
	}, "\n")
}

func powerShellDirectoryVerification() string {
	return strings.Join([]string{
		"Before executing the command, please follow these steps:",
		"",
		"1. Directory Verification:",
		"   - If the command will create new directories or files, first use `Get-ChildItem` (or `ls`) to verify the parent directory exists and is the correct location",
		"",
		"2. Command Execution:",
		"   - Always quote file paths that contain spaces with double quotes",
		"   - Capture the output of the command.",
	}, "\n")
}

func powerShellSyntaxNotes() string {
	return strings.Join([]string{
		"PowerShell Syntax Notes:",
		"   - Variables use $ prefix: $myVar = \"value\"",
		"   - Escape character is backtick (`), not backslash",
		"   - Use Verb-Noun cmdlet naming: Get-ChildItem, Set-Location, New-Item, Remove-Item",
		"   - Common aliases: ls (Get-ChildItem), cd (Set-Location), cat (Get-Content), rm (Remove-Item)",
		"   - Pipe operator | works similarly to bash but passes objects, not text",
		"   - Use Select-Object, Where-Object, ForEach-Object for filtering and transformation",
		"   - String interpolation: \"Hello $name\" or \"Hello $($obj.Property)\"",
		"   - Registry access uses PSDrive prefixes: `HKLM:\\SOFTWARE\\...`, `HKCU:\\...` — NOT raw `HKEY_LOCAL_MACHINE\\...`",
		"   - Environment variables: read with `$env:NAME`, set with `$env:NAME = \"value\"` (NOT `Set-Variable` or bash `export`)",
		"   - Call native exe with spaces in path via call operator: `& \"C:\\Program Files\\App\\app.exe\" arg1 arg2`",
	}, "\n")
}

func powerShellNonInteractiveGuards() string {
	return strings.Join([]string{
		"Interactive and blocking commands (will hang — this tool runs with -NonInteractive):",
		"   - NEVER use `Read-Host`, `Get-Credential`, `Out-GridView`, `$Host.UI.PromptForChoice`, or `pause`",
		"   - Destructive cmdlets (`Remove-Item`, `Stop-Process`, `Clear-Content`, etc.) may prompt for confirmation. Add `-Confirm:$false` when you intend the action to proceed. Use `-Force` for read-only/hidden items.",
		"   - Never use `git rebase -i`, `git add -i`, or other commands that open an interactive editor",
	}, "\n")
}

func powerShellHereStrings() string {
	return strings.Join([]string{
		"Passing multiline strings (commit messages, file content) to native executables:",
		"   - Use a single-quoted here-string so PowerShell does not expand `$` or backticks inside. The closing `'@` MUST be at column 0 (no leading whitespace) on its own line — indenting it is a parse error:",
		"<example>",
		"git commit -m @'",
		"Commit message here.",
		"Second line with $literal dollar signs.",
		"'@",
		"</example>",
		"   - Use `@'...'@` (single-quoted, literal) not `@\"...\"@` (double-quoted, interpolated) unless you need variable expansion",
		"   - For arguments containing `-`, `@`, or other characters PowerShell parses as operators, use the stop-parsing token: `git log --% --format=%H`",
	}, "\n")
}

func powerShellUsageNotes() string {
	return strings.Join([]string{
		"Usage notes:",
		"  - The command argument is required.",
		"  - It is very helpful if you write a clear, concise description of what this command does.",
		"  - Avoid using PowerShell to run commands that have dedicated tools, unless explicitly instructed:",
		"    - File search: Use Glob (NOT Get-ChildItem -Recurse)",
		"    - Content search: Use Grep (NOT Select-String)",
		"    - Read files: Use Read (NOT Get-Content)",
		"    - Write files: Use Write (NOT Set-Content/Out-File)",
		"    - Communication: Output text directly (NOT Write-Output/Write-Host)",
		"  - When issuing multiple commands:",
		"    - If the commands are independent and can run in parallel, make multiple PowerShell tool calls in a single message.",
		"    - If the commands depend on each other and must run sequentially, chain them in a single PowerShell call (see edition-specific chaining syntax above).",
		"    - Use `;` only when you need to run commands sequentially but don't care if earlier commands fail.",
		"    - DO NOT use newlines to separate commands (newlines are ok in quoted strings and here-strings)",
		"  - Do NOT prefix commands with `cd` or `Set-Location` -- the working directory is already set to the correct project directory automatically.",
		"  - For git commands:",
		"    - Prefer to create a new commit rather than amending an existing commit.",
		"    - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal. Only use destructive operations when they are truly the best approach.",
		"    - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign, -c commit.gpgsign=false) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.",
	}, "\n")
}

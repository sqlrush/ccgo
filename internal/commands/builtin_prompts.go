package commands

import "ccgo/internal/contracts"

// BuiltinPromptTemplates returns the builtin CommandPrompt templates that are
// sourced identically to bundled skill prompts but registered as builtin so that
// they are never overridden by user-defined commands with the same name.
func BuiltinPromptTemplates() []PromptTemplate {
	return []PromptTemplate{
		{
			Command: contracts.Command{
				Type:        contracts.CommandPrompt,
				Name:        "init",
				Description: "Initialize a CLAUDE.md file for this project",
				Source:      contracts.CommandSourceBuiltin,
			},
			Content: initPrompt,
		},
		{
			Command: contracts.Command{
				Type:         contracts.CommandPrompt,
				Name:         "review",
				Description:  "Review a pull request",
				ArgumentHint: "[pr-number]",
				Source:       contracts.CommandSourceBuiltin,
			},
			Content: reviewPrompt,
		},
	}
}

// initPrompt is ported from CC src/commands/init.ts OLD_INIT_PROMPT.
const initPrompt = `Please analyze this codebase and create a CLAUDE.md file, which will be given to future instances of Claude Code to operate in this repository.

What to add:
1. Commands that will be commonly used, such as how to build, lint, and run tests. Include the necessary commands to develop in this codebase, such as how to run a single test.
2. High-level code architecture and structure so that future instances can be productive more quickly. Focus on the "big picture" architecture that requires reading multiple files to understand.

Usage notes:
- If there's already a CLAUDE.md, suggest improvements to it.
- When you make the initial CLAUDE.md, do not repeat yourself and do not include obvious instructions like "Provide helpful error messages to users", "Write unit tests for all new utilities", "Never include sensitive information (API keys, tokens) in code or commits".
- Avoid listing every component or file structure that can be easily discovered.
- Don't include generic development practices.
- If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include the important parts.
- If there is a README.md, make sure to include the important parts.
- Do not make up information such as "Common Development Tasks", "Tips for Development", "Support and Documentation" unless this is expressly included in other files that you read.
- Be sure to prefix the file with the following text:

` + "```" + `
# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
` + "```"

// reviewPrompt is ported from CC src/commands/review.ts LOCAL_REVIEW_PROMPT.
// $ARGUMENTS is substituted with the PR number provided by the user.
const reviewPrompt = `
      You are an expert code reviewer. Follow these steps:

      1. If no PR number is provided in the args, run ` + "`gh pr list`" + ` to show open PRs
      2. If a PR number is provided, run ` + "`gh pr view <number>`" + ` to get PR details
      3. Run ` + "`gh pr diff <number>`" + ` to get the diff
      4. Analyze the changes and provide a thorough code review that includes:
         - Overview of what the PR does
         - Analysis of code quality and style
         - Specific suggestions for improvements
         - Any potential issues or risks

      Keep your review concise but thorough. Focus on:
      - Code correctness
      - Following project conventions
      - Performance implications
      - Test coverage
      - Security considerations

      Format your review with clear sections and bullet points.

      PR number: $ARGUMENTS
    `

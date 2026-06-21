package main

import (
	"fmt"
	"io"
	"strings"

	"ccgo/internal/agentfile"
)

// runAgentsCLI implements the `claude agents` subcommand (list-only).
// It prints agents grouped by scope (project, then user) to stdout.
// args is reserved for future subcommands; currently only listing is supported.
// Returns 0 on success, 1 on error.
func runAgentsCLI(cwd string, args []string, stdout, stderr io.Writer) int {
	projectDir := agentfile.ProjectDir(cwd)
	projectAgents, err := agentfile.List(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "agents: %v\n", err)
		return 1
	}

	userDir, userDirErr := agentfile.UserDir()
	var userAgents []agentfile.AgentFile
	if userDirErr == nil {
		userAgents, err = agentfile.List(userDir)
		if err != nil {
			fmt.Fprintf(stderr, "agents: %v\n", err)
			return 1
		}
	}

	printAgentList(stdout, projectAgents, userAgents)
	return 0
}

// printAgentList prints agents grouped by scope to w.
func printAgentList(w io.Writer, project, user []agentfile.AgentFile) {
	fmt.Fprintln(w, "Agents:")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Project agents:")
	if len(project) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, a := range project {
			desc := a.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(w, "  %s — %s\n", a.Name, desc)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "User agents:")
	if len(user) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, a := range user {
			desc := a.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(w, "  %s — %s\n", a.Name, desc)
		}
	}
}

// agentsCLIUsage returns the usage string for the agents subcommand.
func agentsCLIUsage() string {
	return strings.TrimSpace(`
Usage: claude agents

List all agents grouped by scope (project and user).

Agent files live in:
  Project: <cwd>/.claude/agents/*.md
  User:    ~/.claude/agents/*.md
`)
}

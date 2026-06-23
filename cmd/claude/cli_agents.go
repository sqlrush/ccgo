package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ccgo/internal/agentfile"
)

// runAgentsCLI implements the `claude agents` subcommand.
// Subcommands: (no arg) list — show agents; create <name> — create agent file;
// delete <name> — delete agent file; show <name> — show agent detail.
// Flag: --setting-sources project|user|all  — restrict output to given scope(s).
// Returns 0 on success, 1 on error.
func runAgentsCLI(cwd string, args []string, stdout, stderr io.Writer) int {
	return runAgentsCLIWithUserDir(cwd, "", args, stdout, stderr)
}

// runAgentsCLIWithUserDir is like runAgentsCLI but allows overriding the home
// directory root for testability. When userDirRoot is empty, ~/.claude/agents is used.
func runAgentsCLIWithUserDir(cwd, userDirRoot string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claude agents", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingSources := fs.String("setting-sources", "all", "Restrict output to given scope(s): project, user, or all")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	remaining := fs.Args()

	// Subcommand dispatch.
	var sub string
	if len(remaining) > 0 {
		sub = strings.ToLower(remaining[0])
	}
	subArgs := ""
	if len(remaining) > 1 {
		subArgs = strings.TrimSpace(strings.Join(remaining[1:], " "))
	}

	switch sub {
	case "create":
		return agentsCLICreate(cwd, subArgs, stdout, stderr)
	case "delete", "rm", "remove":
		return agentsCLIDelete(cwd, subArgs, stdout, stderr)
	case "show", "info", "detail":
		return agentsCLIShow(cwd, subArgs, stdout, stderr)
	default:
		// list (no subcommand or "list")
		return agentsCLIList(cwd, userDirRoot, *settingSources, stdout, stderr)
	}
}

// agentsCLIList lists agents from project and/or user scope.
// userDirRoot, when non-empty, overrides the home directory root so tests can
// inject a temp directory without touching ~/.claude.
func agentsCLIList(cwd, userDirRoot string, sources string, stdout, stderr io.Writer) int {
	sources = strings.ToLower(strings.TrimSpace(sources))
	showProject := sources == "all" || sources == "project" || sources == ""
	showUser := sources == "all" || sources == "user" || sources == ""

	var projectAgents, userAgents []agentfile.AgentFile

	if showProject {
		projectDir := agentfile.ProjectDir(cwd)
		agents, err := agentfile.List(projectDir)
		if err != nil {
			fmt.Fprintf(stderr, "agents: %v\n", err)
			return 1
		}
		projectAgents = agents
	}

	if showUser {
		userDir, err := resolveAgentUserDir(userDirRoot)
		if err == nil {
			agents, err := agentfile.List(userDir)
			if err != nil {
				fmt.Fprintf(stderr, "agents: %v\n", err)
				return 1
			}
			userAgents = agents
		}
	}

	printAgentList(stdout, projectAgents, userAgents)
	return 0
}

// resolveAgentUserDir returns the user-scoped agents directory.
// When root is non-empty, it returns <root>/.claude/agents instead of ~/.claude/agents.
func resolveAgentUserDir(root string) (string, error) {
	if root != "" {
		return root + "/.claude/agents", nil
	}
	return agentfile.UserDir()
}

// agentsCLICreate creates a new agent file in the project scope.
func agentsCLICreate(cwd, name string, stdout, stderr io.Writer) int {
	name = strings.TrimSpace(name)
	if name == "" {
		fmt.Fprintln(stderr, "Usage: claude agents create <name>")
		return 1
	}
	dir := agentfile.ProjectDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "agents create: mkdir %s: %v\n", dir, err)
		return 1
	}
	a := agentfile.AgentFile{
		Name:        name,
		Description: "",
		Prompt:      "# " + name + "\n",
	}
	if err := agentfile.Save(dir, a); err != nil {
		fmt.Fprintf(stderr, "agents create: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Created agent %q in %s\n", name, dir)
	return 0
}

// agentsCLIDelete removes an agent file from the project scope.
func agentsCLIDelete(cwd, name string, stdout, stderr io.Writer) int {
	name = strings.TrimSpace(name)
	if name == "" {
		fmt.Fprintln(stderr, "Usage: claude agents delete <name>")
		return 1
	}
	dir := agentfile.ProjectDir(cwd)
	if err := agentfile.Delete(dir, name); err != nil {
		fmt.Fprintf(stderr, "agents delete: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Deleted agent %q\n", name)
	return 0
}

// agentsCLIShow prints detail for a named agent.
func agentsCLIShow(cwd, name string, stdout, stderr io.Writer) int {
	name = strings.TrimSpace(name)
	if name == "" {
		fmt.Fprintln(stderr, "Usage: claude agents show <name>")
		return 1
	}
	// Search project scope first, then user scope.
	projectDir := agentfile.ProjectDir(cwd)
	agents, err := agentfile.List(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "agents show: %v\n", err)
		return 1
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			printAgentDetail(stdout, a)
			return 0
		}
	}
	userDir, err := agentfile.UserDir()
	if err == nil {
		userAgents, err := agentfile.List(userDir)
		if err == nil {
			for _, a := range userAgents {
				if strings.EqualFold(a.Name, name) {
					printAgentDetail(stdout, a)
					return 0
				}
			}
		}
	}
	fmt.Fprintf(stderr, "agents show: no agent found with name %q\n", name)
	return 1
}

// printAgentList prints agents grouped by scope to w.
// It shows the model field when present (SUBCMD-AGENTS-04) and marks agents
// that are shadowed by a higher-priority same-named agent (SUBCMD-AGENTS-05).
// Priority order: project > user. Project agents win over user agents.
func printAgentList(w io.Writer, project, user []agentfile.AgentFile) {
	// Build a set of project agent names so we can detect user-agent shadows.
	projectNames := make(map[string]bool, len(project))
	for _, a := range project {
		projectNames[strings.ToLower(a.Name)] = true
	}

	totalActive := 0
	hasAny := len(project) > 0 || len(user) > 0

	if !hasAny {
		fmt.Fprintln(w, "(none)")
		return
	}

	// Count active (non-shadowed) agents for header.
	for range project {
		totalActive++
	}
	for _, a := range user {
		if !projectNames[strings.ToLower(a.Name)] {
			totalActive++
		}
	}
	fmt.Fprintf(w, "%d active agents\n\n", totalActive)

	fmt.Fprintln(w, "Project agents:")
	if len(project) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, a := range project {
			fmt.Fprintf(w, "  %s\n", formatAgentLine(a))
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "User agents:")
	if len(user) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		for _, a := range user {
			if projectNames[strings.ToLower(a.Name)] {
				fmt.Fprintf(w, "  (shadowed by project) %s\n", formatAgentLine(a))
			} else {
				fmt.Fprintf(w, "  %s\n", formatAgentLine(a))
			}
		}
	}
}

// formatAgentLine renders a single agent line: "name [· model] — description".
func formatAgentLine(a agentfile.AgentFile) string {
	var parts []string
	parts = append(parts, a.Name)
	if a.Model != "" {
		parts = append(parts, "· "+a.Model)
	}
	label := strings.Join(parts, " ")
	desc := a.Description
	if desc == "" {
		desc = "(no description)"
	}
	return label + " — " + desc
}

// printAgentDetail prints detailed info for a single agent to w.
func printAgentDetail(w io.Writer, a agentfile.AgentFile) {
	fmt.Fprintf(w, "Agent: %s\n", a.Name)
	if a.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", a.Description)
	}
	if a.Model != "" {
		fmt.Fprintf(w, "Model: %s\n", a.Model)
	}
	if a.Effort != "" {
		fmt.Fprintf(w, "Effort: %s\n", a.Effort)
	}
	if a.Color != "" {
		fmt.Fprintf(w, "Color: %s\n", a.Color)
	}
	if len(a.Tools) > 0 {
		fmt.Fprintf(w, "Tools: %s\n", strings.Join(a.Tools, ", "))
	}
	if a.Path != "" {
		fmt.Fprintf(w, "Path: %s\n", a.Path)
	}
}

// agentsCLIUsage returns the usage string for the agents subcommand.
func agentsCLIUsage() string {
	return strings.TrimSpace(`
Usage: claude agents [subcommand] [--setting-sources project|user|all]

Subcommands:
  (no arg) / list           List all agents grouped by scope
  create <name>             Create a new agent file in the project scope
  delete <name>             Delete an agent file from the project scope
  show <name>               Show detail for a named agent

Flags:
  --setting-sources <scope> Restrict listing to project, user, or all (default: all)

Agent files live in:
  Project: <cwd>/.claude/agents/*.md
  User:    ~/.claude/agents/*.md
`)
}

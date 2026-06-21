package repl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"ccgo/internal/agentfile"
)

// agentsHandler returns a CommandHandler for the /agents slash command.
// Subcommands:
//
//	(no arg) / list  – list agents grouped by scope (project, user)
//	create <name>    – create a new stub agent file in the project scope
//	delete <name>    – delete an agent file from the project scope
//	show   <name>    – print detail for a named agent
func agentsHandler(cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		args := strings.TrimSpace(cc.Args)
		sub, rest, _ := strings.Cut(args, " ")
		rest = strings.TrimSpace(rest)

		switch strings.ToLower(sub) {
		case "create":
			return agentsCreate(cwd, rest)
		case "delete":
			return agentsDelete(cwd, rest)
		case "show":
			return agentsShow(cwd, rest)
		default:
			// "list" or no subcommand
			return agentsList(cwd)
		}
	}
}

func agentsCreate(cwd, name string) (CommandOutcome, error) {
	if name == "" {
		return CommandOutcome{Handled: true, Status: "Usage: /agents create <name>"}, nil
	}
	dir := agentfile.ProjectDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CommandOutcome{}, fmt.Errorf("agents create: mkdir %s: %w", dir, err)
	}
	a := agentfile.AgentFile{
		Name:        name,
		Description: "",
		Prompt:      "# " + name + "\n",
	}
	if err := agentfile.Save(dir, a); err != nil {
		return CommandOutcome{}, fmt.Errorf("agents create: %w", err)
	}
	return CommandOutcome{
		Handled: true,
		Status:  fmt.Sprintf("Created agent %q in %s", name, dir),
	}, nil
}

func agentsDelete(cwd, name string) (CommandOutcome, error) {
	if name == "" {
		return CommandOutcome{Handled: true, Status: "Usage: /agents delete <name>"}, nil
	}
	dir := agentfile.ProjectDir(cwd)
	if err := agentfile.Delete(dir, name); err != nil {
		return CommandOutcome{}, fmt.Errorf("agents delete: %w", err)
	}
	return CommandOutcome{
		Handled: true,
		Status:  fmt.Sprintf("Deleted agent %q", name),
	}, nil
}

func agentsShow(cwd, name string) (CommandOutcome, error) {
	if name == "" {
		return CommandOutcome{Handled: true, Status: "Usage: /agents show <name>"}, nil
	}
	dir := agentfile.ProjectDir(cwd)
	agents, err := agentfile.List(dir)
	if err != nil {
		return CommandOutcome{}, fmt.Errorf("agents show: %w", err)
	}
	for _, a := range agents {
		if strings.EqualFold(a.Name, name) {
			return CommandOutcome{
				Handled: true,
				Status:  formatAgentDetail(a),
			}, nil
		}
	}
	// Also check user scope
	userDir, userDirErr := agentfile.UserDir()
	if userDirErr == nil {
		userAgents, listErr := agentfile.List(userDir)
		if listErr == nil {
			for _, a := range userAgents {
				if strings.EqualFold(a.Name, name) {
					return CommandOutcome{
						Handled: true,
						Status:  formatAgentDetail(a),
					}, nil
				}
			}
		}
	}
	return CommandOutcome{
		Handled: true,
		Status:  fmt.Sprintf("No agent found with name %q.", name),
	}, nil
}

func agentsList(cwd string) (CommandOutcome, error) {
	projectDir := agentfile.ProjectDir(cwd)
	projectAgents, err := agentfile.List(projectDir)
	if err != nil {
		return CommandOutcome{}, fmt.Errorf("agents list: %w", err)
	}

	userDir, userDirErr := agentfile.UserDir()
	var userAgents []agentfile.AgentFile
	if userDirErr == nil {
		userAgents, err = agentfile.List(userDir)
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("agents list (user): %w", err)
		}
	}

	status := formatAgentList(projectAgents, userAgents)
	return CommandOutcome{Handled: true, Status: status}, nil
}

// formatAgentList renders agents grouped by scope.
func formatAgentList(project, user []agentfile.AgentFile) string {
	var sb strings.Builder
	sb.WriteString("Agents:\n")
	sb.WriteString("\nProject agents:\n")
	if len(project) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, a := range project {
			desc := a.Description
			if desc == "" {
				desc = "(no description)"
			}
			sb.WriteString(fmt.Sprintf("  %s — %s\n", a.Name, desc))
		}
	}
	sb.WriteString("\nUser agents:\n")
	if len(user) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, a := range user {
			desc := a.Description
			if desc == "" {
				desc = "(no description)"
			}
			sb.WriteString(fmt.Sprintf("  %s — %s\n", a.Name, desc))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatAgentDetail renders a single AgentFile for /agents show.
func formatAgentDetail(a agentfile.AgentFile) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Agent: %s\n", a.Name))
	if a.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", a.Description))
	}
	if a.Model != "" {
		sb.WriteString(fmt.Sprintf("Model: %s\n", a.Model))
	}
	if len(a.Tools) > 0 {
		sb.WriteString(fmt.Sprintf("Tools: %s\n", strings.Join(a.Tools, ", ")))
	}
	if a.Path != "" {
		sb.WriteString(fmt.Sprintf("Path: %s\n", a.Path))
	}
	if a.Prompt != "" {
		sb.WriteString(fmt.Sprintf("\nPrompt:\n%s", a.Prompt))
	}
	return strings.TrimRight(sb.String(), "\n")
}

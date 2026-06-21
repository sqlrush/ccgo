package repl

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/config"
)

// permissionsAdder adds a permission rule to the settings file at path.
// Production: config.AddPermissionRule. Tests: in-memory recorder.
type permissionsAdder func(path, behavior, rule string) error

// permissionsRemover removes a permission rule from the settings file at path.
// Production: config.RemovePermissionRule. Tests: in-memory recorder.
type permissionsRemover func(path, behavior, rule string) error

// permissionsHandlerWith returns a CommandHandler for /permissions backed by
// the given mutators. The path parameter is the settings file to read/write.
//
// Subcommands:
//
//	(no arg)              — list current allow/deny/ask rules
//	list                  — same as no-arg
//	allow <rule>          — add rule to permissions.allow
//	deny  <rule>          — add rule to permissions.deny
//	ask   <rule>          — add rule to permissions.ask
//	remove <behavior> <rule> — remove rule from the named behavior list
func permissionsHandlerWith(path string, add permissionsAdder, remove permissionsRemover) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		args := strings.TrimSpace(cc.Args)

		// No-arg or explicit "list" → display current rules.
		if args == "" || args == "list" {
			return listPermissions(path)
		}

		parts := strings.Fields(args)
		if len(parts) == 0 {
			return listPermissions(path)
		}

		sub := parts[0]
		switch sub {
		case "allow", "deny", "ask":
			rule := strings.TrimSpace(strings.TrimPrefix(args, sub))
			if rule == "" {
				return CommandOutcome{
					Handled: true,
					Status:  fmt.Sprintf("Usage: /permissions %s <rule>  (e.g. Bash(git status:*))", sub),
				}, nil
			}
			if err := add(path, sub, rule); err != nil {
				return CommandOutcome{}, fmt.Errorf("permissions add: %w", err)
			}
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Added %s rule: %s", sub, rule),
			}, nil

		case "remove":
			// Expect: remove <behavior> <rule>
			if len(parts) < 3 {
				return CommandOutcome{
					Handled: true,
					Status:  "Usage: /permissions remove <allow|deny|ask> <rule>",
				}, nil
			}
			behavior := parts[1]
			rule := strings.TrimSpace(strings.Join(parts[2:], " "))
			if err := remove(path, behavior, rule); err != nil {
				return CommandOutcome{}, fmt.Errorf("permissions remove: %w", err)
			}
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Removed %s rule: %s", behavior, rule),
			}, nil

		default:
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /permissions [list | allow <rule> | deny <rule> | ask <rule> | remove <allow|deny|ask> <rule>]",
			}, nil
		}
	}
}

// listPermissions reads the settings document at path and returns a formatted
// summary of current allow/deny/ask rules.
func listPermissions(path string) (CommandOutcome, error) {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return CommandOutcome{}, fmt.Errorf("permissions list: %w", err)
	}

	perms, _ := doc["permissions"].(map[string]any)

	var sb strings.Builder
	sb.WriteString("Permissions:\n")
	for _, behavior := range []string{"allow", "deny", "ask"} {
		rules := rulesFromDoc(perms, behavior)
		sb.WriteString(fmt.Sprintf("  %s (%d):", behavior, len(rules)))
		if len(rules) == 0 {
			sb.WriteString(" (none)\n")
			continue
		}
		sb.WriteByte('\n')
		for _, r := range rules {
			sb.WriteString(fmt.Sprintf("    %s\n", r))
		}
	}
	return CommandOutcome{Handled: true, Status: sb.String()}, nil
}

// rulesFromDoc extracts the string slice for a behavior key from a permissions
// map, returning nil when the key is absent or holds no strings.
func rulesFromDoc(perms map[string]any, behavior string) []string {
	if perms == nil {
		return nil
	}
	list, ok := perms[behavior].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// permissionsHandler is the production handler using the user settings file.
func permissionsHandler() CommandHandler {
	path := config.UserSettingsPath()
	return permissionsHandlerWith(path, config.AddPermissionRule, config.RemovePermissionRule)
}

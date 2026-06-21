package repl

import (
	"context"
	"strings"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// CommandContext is the live state a REPL command handler may read.
type CommandContext struct {
	Args    string
	Screen  *tui.REPLScreen
	History []contracts.Message
	CWD     string
}

// CommandOutcome reports what a handler did. Handled=false means the input was
// not a registered live-effect command and must fall through to the model.
type CommandOutcome struct {
	Handled        bool
	ReplaceHistory bool
	NewHistory     []contracts.Message
	Status         string
	SendToModel    bool
}

// CommandHandler runs a single live-effect slash command.
type CommandHandler func(ctx context.Context, cc CommandContext) (CommandOutcome, error)

// CommandRouter maps slash command names to live-effect handlers.
type CommandRouter struct {
	handlers map[string]CommandHandler
}

// NewCommandRouter returns an empty router ready for registration.
func NewCommandRouter() *CommandRouter {
	return &CommandRouter{handlers: make(map[string]CommandHandler)}
}

// Register adds a handler for the given command name (without leading slash).
// Duplicate registrations replace the previous handler.
func (r *CommandRouter) Register(name string, h CommandHandler) {
	r.handlers[strings.TrimSpace(name)] = h
}

// Names returns the set of command names registered on the router (without leading slash).
// It is used by tests to enumerate the production router's command surface.
func (r *CommandRouter) Names() []string {
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	return names
}

// Dispatch routes a raw input line. If it is a slash command with a registered
// handler, the handler runs with cc.Args set to the parsed arguments.
// If the input is not a slash command, or the command is not registered,
// Dispatch returns {Handled: false} so the loop falls through to the model.
func (r *CommandRouter) Dispatch(ctx context.Context, input string, cc CommandContext) (CommandOutcome, error) {
	parsed, ok := commands.ParseSlashCommand(input)
	if !ok {
		return CommandOutcome{Handled: false}, nil
	}
	handler, found := r.handlers[parsed.CommandName]
	if !found {
		return CommandOutcome{Handled: false}, nil
	}
	cc.Args = parsed.Args
	return handler(ctx, cc)
}

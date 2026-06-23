package repl

import (
	"context"

	"ccgo/internal/contracts"
)

// helpHandlerWith is the dependency-injected help handler. It opens the help
// overlay listing the visible commands in the supplied registry.
func helpHandlerWith(registry []contracts.Command) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Overlay: NewHelpScreen(registry)}, nil
	}
}

// helpHandler builds the production handler injecting the command registry.
func helpHandler(registry []contracts.Command) CommandHandler {
	return helpHandlerWith(registry)
}

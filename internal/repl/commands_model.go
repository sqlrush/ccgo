package repl

import (
	"context"
)

// builtinModels is the default model list shown in the /model picker.
// Mirrors the list from CC src/commands/model/index.ts.
var builtinModels = []string{
	"claude-opus-4-5",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
	"claude-opus-4",
	"claude-sonnet-4",
	"claude-haiku-3-5",
	"claude-3-7-sonnet-latest",
}

// modelHandlerWith is the dependency-injected model handler.
func modelHandlerWith(models []string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		// No-arg: open picker overlay.
		if cc.Args == "" {
			return CommandOutcome{Handled: true, Overlay: NewModelPicker(models)}, nil
		}
		// With arg: model switching at runtime is not yet wired.
		return CommandOutcome{
			Handled: true,
			Status:  "Model selection with argument not yet wired. Use /model with no argument to open the picker.",
		}, nil
	}
}

// modelHandler builds the production handler.
func modelHandler() CommandHandler {
	return modelHandlerWith(builtinModels)
}

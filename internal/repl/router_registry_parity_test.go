package repl

import (
	"testing"

	"ccgo/internal/commands"
)

// TestProductionRouterCommandsAreInBuiltinRegistry ensures that every command
// name registered on the production CommandRouter (the one wired by
// RunInteractiveWithOptions) can be found in the commands.BuiltinCommands
// registry — either as a direct name or as an alias.
//
// This guards against "registered-in-router-but-not-in-registry" drift that
// causes /help and slash autocomplete to omit commands and makes headless mode
// return "Unknown skill: <name>". The same class of drift previously bit
// /permissions and /agents.
func TestProductionRouterCommandsAreInBuiltinRegistry(t *testing.T) {
	router := newProductionRouter("")
	registry := commands.FromSources(commands.Sources{Builtins: commands.BuiltinCommands()})

	for _, name := range router.Names() {
		name := name // capture for subtests
		t.Run(name, func(t *testing.T) {
			_, found := registry.Find(name)
			if !found {
				t.Errorf(
					"router command %q is not present in BuiltinCommands: "+
						"add it to commands.BuiltinCommands() so /help and "+
						"headless mode can discover it",
					name,
				)
			}
		})
	}
}

// TestBuiltinRegistryCommandsHaveRouterOrExecutor verifies that every builtin
// command of type CommandLocal/CommandLocalJSX either has an executor in
// ExecuteBuiltinLocalCommand OR is registered on the production router.
// This is the inverse guard: it catches registry entries that are missing a
// runtime handler entirely.
func TestBuiltinRegistryCommandsHaveRouterOrExecutor(t *testing.T) {
	router := newProductionRouter("")
	routerNames := make(map[string]struct{})
	for _, n := range router.Names() {
		routerNames[n] = struct{}{}
	}
	registry := commands.FromSources(commands.Sources{Builtins: commands.BuiltinCommands()})

	for _, cmd := range commands.BuiltinCommands() {
		cmd := cmd // capture
		t.Run(cmd.Name, func(t *testing.T) {
			if _, ok := commands.ExecuteBuiltinLocalCommand(registry, cmd, ""); ok {
				return // has an executor — fine
			}
			if _, ok := routerNames[cmd.Name]; ok {
				return // handled by production router — fine
			}
			// Check aliases too
			for _, alias := range cmd.Aliases {
				if _, ok := routerNames[alias]; ok {
					return
				}
			}
			t.Errorf(
				"builtin command %q has no executor in ExecuteBuiltinLocalCommand "+
					"and is not registered on the production router",
				cmd.Name,
			)
		})
	}
}

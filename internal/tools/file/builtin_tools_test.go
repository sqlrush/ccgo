package filetools

import "testing"

func TestBuiltinToolsIncludesToolSearch(t *testing.T) {
	for _, tool := range BuiltinTools() {
		if tool.Name() == "ToolSearch" {
			return
		}
	}
	t.Fatal("builtin tools did not include ToolSearch")
}

func TestDefaultRegistryHasPhase5Tools(t *testing.T) {
	tools := BuiltinTools()
	names := make(map[string]bool, len(tools))
	for _, tl := range tools {
		names[tl.Name()] = true
	}
	for _, name := range []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode", "LSP"} {
		if !names[name] {
			t.Fatalf("builtin tools missing %q", name)
		}
	}
}

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

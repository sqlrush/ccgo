package conversation

import (
	"ccgo/internal/tool"
	lsptools "ccgo/internal/tools/lsp"
)

func (r Runner) withAdvancedTools() (Runner, error) {
	settings := r.mergedSettings()
	if settings.Advanced == nil {
		return r, nil
	}
	var extra []tool.Tool
	if advancedBoolEnabled(settings.Advanced.LSP) {
		extra = appendMissingTool(extra, r.Tools.Registry, lsptools.NewDiagnosticsTool())
	}
	if len(extra) == 0 {
		return r, nil
	}
	registry, err := mergeToolRegistry(r.Tools.Registry, extra)
	if err != nil {
		return r, err
	}
	r.Tools.Registry = registry
	return r, nil
}

func appendMissingTool(out []tool.Tool, registry *tool.Registry, candidate tool.Tool) []tool.Tool {
	if candidate == nil {
		return out
	}
	if registry != nil {
		if _, ok := registry.Lookup(candidate.Name()); ok {
			return out
		}
	}
	return append(out, candidate)
}

func advancedBoolEnabled(value *bool) bool {
	return value != nil && *value
}

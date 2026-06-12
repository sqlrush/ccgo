package mcp

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type Root struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

type RootsListResult struct {
	Roots []Root `json:"roots"`
}

type RootsProvider func(context.Context) ([]Root, error)

func FileRoot(path string, name string) (Root, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Root{}, fmt.Errorf("mcp root path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Root{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(abs)
	}
	name = strings.TrimSpace(name)
	if name == "." || name == string(filepath.Separator) {
		name = abs
	}
	return Root{
		URI:  (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String(),
		Name: name,
	}, nil
}

func StaticRootsProvider(roots []Root) RootsProvider {
	roots = normalizeRoots(roots)
	return func(context.Context) ([]Root, error) {
		return append([]Root(nil), roots...), nil
	}
}

func RootsListRequestHandler(provider RootsProvider, fallback RPCRequestHandler) RPCRequestHandler {
	return func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		if IsRootsListMethod(request.Method) {
			if provider == nil {
				return nil, &RPCError{Code: -32601, Message: "method not found"}
			}
			roots, err := provider(ctx)
			if err != nil {
				return nil, &RPCError{
					Code:    -32603,
					Message: "failed to list MCP client roots",
					Data:    err.Error(),
				}
			}
			return RootsListResult{Roots: normalizeRoots(roots)}, nil
		}
		if fallback == nil {
			fallback = DefaultRPCRequestHandler
		}
		return fallback(ctx, request)
	}
}

func (c *ProtocolClient) NotifyRootsListChanged(ctx context.Context) error {
	return c.SendNotification(ctx, "notifications/roots/list_changed", nil)
}

func IsRootsListMethod(method string) bool {
	normalized := strings.TrimSpace(method)
	normalized = strings.ReplaceAll(normalized, ".", "/")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.Trim(normalized, "/")
	normalized = strings.ToLower(normalized)
	switch normalized {
	case "roots/list", "root/list", "roots_list", "root_list", "rootslist", "rootlist":
		return true
	default:
		return false
	}
}

func CapabilitiesWithRoots(capabilities map[string]any, listChanged bool) map[string]any {
	out := copyCapabilityMap(capabilities)
	roots := map[string]any{}
	if existing, ok := out["roots"].(map[string]any); ok {
		for key, value := range existing {
			roots[key] = value
		}
	}
	if listChanged {
		roots["listChanged"] = true
	}
	out["roots"] = roots
	return out
}

func copyCapabilityMap(capabilities map[string]any) map[string]any {
	out := make(map[string]any, len(capabilities)+1)
	for key, value := range capabilities {
		out[key] = value
	}
	return out
}

func normalizeRoots(roots []Root) []Root {
	out := make([]Root, 0, len(roots))
	for _, root := range roots {
		uri := strings.TrimSpace(root.URI)
		if uri == "" {
			continue
		}
		out = append(out, Root{
			URI:  uri,
			Name: strings.TrimSpace(root.Name),
		})
	}
	return out
}

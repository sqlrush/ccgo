package tool

import (
	"ccgo/internal/contracts"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	aliases map[string]string
	order   []string
}

func NewRegistry(tools ...Tool) (*Registry, error) {
	registry := &Registry{
		tools:   map[string]Tool{},
		aliases: map[string]string{},
	}
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("cannot register nil tool")
	}
	name := strings.TrimSpace(t.Name())
	if name == "" {
		return fmt.Errorf("cannot register tool with empty name")
	}
	key := normalizeName(name)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[key]; ok {
		return fmt.Errorf("tool %q already registered", name)
	}
	if target, ok := r.aliases[key]; ok {
		return fmt.Errorf("tool name %q conflicts with alias for %q", name, target)
	}
	for _, alias := range t.Aliases() {
		aliasKey := normalizeName(alias)
		if aliasKey == "" {
			return fmt.Errorf("tool %q has empty alias", name)
		}
		if _, ok := r.tools[aliasKey]; ok {
			return fmt.Errorf("alias %q for %q conflicts with registered tool", alias, name)
		}
		if target, ok := r.aliases[aliasKey]; ok {
			return fmt.Errorf("alias %q for %q conflicts with alias for %q", alias, name, target)
		}
	}

	r.tools[key] = t
	r.order = append(r.order, key)
	for _, alias := range t.Aliases() {
		r.aliases[normalizeName(alias)] = key
	}
	return nil
}

func (r *Registry) Lookup(name string) (Tool, bool) {
	key := normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.tools[key]; ok {
		return t, true
	}
	if target, ok := r.aliases[key]; ok {
		t, ok := r.tools[target]
		return t, ok
	}
	return nil, false
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for _, key := range r.order {
		if t := r.tools[key]; t != nil {
			names = append(names, t.Name())
		}
	}
	return names
}

func (r *Registry) Definitions(ctx PromptContext) ([]contracts.ToolDefinition, error) {
	r.mu.RLock()
	tools := make([]Tool, 0, len(r.tools))
	for _, key := range r.order {
		tools = append(tools, r.tools[key])
	}
	r.mu.RUnlock()

	definitions := make([]contracts.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		def, err := Definition(ctx, t)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, def)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions, nil
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

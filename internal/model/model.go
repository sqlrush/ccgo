package model

import (
	"strings"
)

const (
	Claude35Haiku  = "claude-3-5-haiku-20241022"
	Claude45Haiku  = "claude-haiku-4-5-20251001"
	Claude35Sonnet = "claude-3-5-sonnet-20241022"
	Claude37Sonnet = "claude-3-7-sonnet-20250219"
	Claude40Sonnet = "claude-sonnet-4-20250514"
	Claude45Sonnet = "claude-sonnet-4-5-20250929"
	Claude46Sonnet = "claude-sonnet-4-6"
	Claude40Opus   = "claude-opus-4-20250514"
	Claude41Opus   = "claude-opus-4-1-20250805"
	Claude45Opus   = "claude-opus-4-5-20251101"
	Claude46Opus   = "claude-opus-4-6"
)

type Capability struct {
	Name                string  `json:"name"`
	CanonicalName       string  `json:"canonical_name,omitempty"`
	DisplayName         string  `json:"display_name,omitempty"`
	ContextWindowTokens int     `json:"context_window_tokens"`
	MaxOutputTokens     int     `json:"max_output_tokens"`
	SupportsThinking    bool    `json:"supports_thinking"`
	SupportsEffort      bool    `json:"supports_effort"`
	Supports1MContext   bool    `json:"supports_1m_context,omitempty"`
	AlwaysOnThinking    bool    `json:"always_on_thinking,omitempty"`
	InputUSDPerMTok     float64 `json:"input_usd_per_mtok,omitempty"`
	OutputUSDPerMTok    float64 `json:"output_usd_per_mtok,omitempty"`
}

type Registry struct {
	Aliases map[string]string
	Models  map[string]Capability
}

func DefaultRegistry() Registry {
	return Registry{
		Aliases: map[string]string{
			"sonnet":    Claude46Sonnet,
			"sonnet4":   Claude40Sonnet,
			"sonnet4.5": Claude45Sonnet,
			"sonnet4.6": Claude46Sonnet,
			"opus":      Claude46Opus,
			"opus4":     Claude40Opus,
			"opus4.1":   Claude41Opus,
			"opus4.5":   Claude45Opus,
			"opus4.6":   Claude46Opus,
			"haiku":     Claude45Haiku,
			"haiku3.5":  Claude35Haiku,
			"haiku4.5":  Claude45Haiku,
			"best":      Claude46Opus,
			"opusplan":  Claude46Sonnet,
		},
		Models: map[string]Capability{
			Claude35Haiku:  capability(Claude35Haiku, "claude-3-5-haiku", "Haiku 3.5", 200000, 8192, false, false, false),
			Claude45Haiku:  capability(Claude45Haiku, "claude-haiku-4-5", "Haiku 4.5", 200000, 8192, false, false, false),
			Claude35Sonnet: capability(Claude35Sonnet, "claude-3-5-sonnet", "Sonnet 3.5", 200000, 8192, false, false, false),
			Claude37Sonnet: capability(Claude37Sonnet, "claude-3-7-sonnet", "Sonnet 3.7", 200000, 64000, true, true, false),
			Claude40Sonnet: capability(Claude40Sonnet, "claude-sonnet-4", "Sonnet 4", 200000, 64000, true, true, true),
			Claude45Sonnet: capability(Claude45Sonnet, "claude-sonnet-4-5", "Sonnet 4.5", 200000, 64000, true, true, true),
			Claude46Sonnet: capability(Claude46Sonnet, "claude-sonnet-4-6", "Sonnet 4.6", 200000, 64000, true, true, true),
			Claude40Opus:   capability(Claude40Opus, "claude-opus-4", "Opus 4", 200000, 64000, true, true, true),
			Claude41Opus:   capability(Claude41Opus, "claude-opus-4-1", "Opus 4.1", 200000, 64000, true, true, true),
			Claude45Opus:   capability(Claude45Opus, "claude-opus-4-5", "Opus 4.5", 200000, 64000, true, true, true),
			Claude46Opus:   capability(Claude46Opus, "claude-opus-4-6", "Opus 4.6", 200000, 64000, true, true, true),
		},
	}
}

func (r Registry) Resolve(name string) (Capability, bool) {
	key := strings.TrimSpace(strings.ToLower(name))
	has1m := strings.HasSuffix(key, "[1m]")
	if has1m {
		key = strings.TrimSpace(strings.TrimSuffix(key, "[1m]"))
	}
	if key == "" {
		key = Claude46Sonnet
	}
	if target, ok := r.Aliases[key]; ok {
		key = target
	}
	capability, ok := r.Models[key]
	if ok && has1m && capability.Supports1MContext {
		capability.Name += "[1m]"
		capability.ContextWindowTokens = 1000000
		capability.DisplayName += " (1M context)"
	}
	return capability, ok
}

func capability(name, canonical, display string, context, maxOutput int, thinking, effort, oneMillion bool) Capability {
	return Capability{
		Name:                name,
		CanonicalName:       canonical,
		DisplayName:         display,
		ContextWindowTokens: context,
		MaxOutputTokens:     maxOutput,
		SupportsThinking:    thinking,
		SupportsEffort:      effort,
		Supports1MContext:   oneMillion,
	}
}

func CanonicalName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "claude-opus-4-6"):
		return "claude-opus-4-6"
	case strings.Contains(lower, "claude-opus-4-5"):
		return "claude-opus-4-5"
	case strings.Contains(lower, "claude-opus-4-1"):
		return "claude-opus-4-1"
	case strings.Contains(lower, "claude-opus-4"):
		return "claude-opus-4"
	case strings.Contains(lower, "claude-sonnet-4-6"):
		return "claude-sonnet-4-6"
	case strings.Contains(lower, "claude-sonnet-4-5"):
		return "claude-sonnet-4-5"
	case strings.Contains(lower, "claude-sonnet-4"):
		return "claude-sonnet-4"
	case strings.Contains(lower, "claude-haiku-4-5"):
		return "claude-haiku-4-5"
	case strings.Contains(lower, "claude-3-7-sonnet"):
		return "claude-3-7-sonnet"
	case strings.Contains(lower, "claude-3-5-sonnet"):
		return "claude-3-5-sonnet"
	case strings.Contains(lower, "claude-3-5-haiku"):
		return "claude-3-5-haiku"
	default:
		return lower
	}
}

func (r Registry) RenderName(name string) string {
	capability, ok := r.Resolve(name)
	if !ok || capability.DisplayName == "" {
		return name
	}
	return capability.DisplayName
}

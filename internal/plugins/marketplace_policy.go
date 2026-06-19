package plugins

import (
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

type MarketplaceDecision struct {
	Name    string
	Allowed bool
	Reason  string
}

type MarketplacePolicy struct {
	extra   map[string]string
	strict  map[string]string
	blocked map[string]string
}

func NewMarketplacePolicy(settings contracts.Settings) MarketplacePolicy {
	return MarketplacePolicy{
		extra:   marketplaceNameSetFromMapKeys(settings.ExtraKnownMarketplaces),
		strict:  marketplaceNameSetFromList(settings.StrictKnownMarketplaces),
		blocked: marketplaceNameSetFromList(settings.BlockedMarketplaces),
	}
}

func (p MarketplacePolicy) Decision(name string) MarketplaceDecision {
	name = strings.TrimSpace(name)
	if name == "" {
		return MarketplaceDecision{Allowed: false, Reason: "marketplace name is empty"}
	}
	key := marketplaceNameKey(name)
	if canonical, ok := p.blocked[key]; ok {
		return MarketplaceDecision{Name: canonical, Allowed: false, Reason: "blocked by settings blockedMarketplaces"}
	}
	if len(p.strict) > 0 {
		if canonical, ok := p.strict[key]; ok {
			return MarketplaceDecision{Name: canonical, Allowed: true}
		}
		return MarketplaceDecision{Name: name, Allowed: false, Reason: "not listed in settings strictKnownMarketplaces"}
	}
	if canonical, ok := p.extra[key]; ok {
		return MarketplaceDecision{Name: canonical, Allowed: true}
	}
	return MarketplaceDecision{Name: name, Allowed: true}
}

func (p MarketplacePolicy) ExtraNames() []string {
	return marketplaceSortedNames(p.extra)
}

func (p MarketplacePolicy) StrictNames() []string {
	return marketplaceSortedNames(p.strict)
}

func (p MarketplacePolicy) BlockedNames() []string {
	return marketplaceSortedNames(p.blocked)
}

func (p MarketplacePolicy) StrictMode() bool {
	return len(p.strict) > 0
}

func marketplaceNameSetFromMapKeys(values map[string]any) map[string]string {
	out := map[string]string{}
	for name := range values {
		addMarketplaceName(out, name)
	}
	return out
}

func marketplaceNameSetFromList(values []any) map[string]string {
	out := map[string]string{}
	for _, value := range values {
		addMarketplaceName(out, marketplaceNameFromAny(value))
	}
	return out
}

func addMarketplaceName(values map[string]string, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	values[marketplaceNameKey(name)] = name
}

func marketplaceNameFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return marketplaceNameFromAnyMap(typed)
	case map[string]string:
		for _, key := range marketplaceNameFields() {
			if value := strings.TrimSpace(typed[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func marketplaceNameFromAnyMap(value map[string]any) string {
	for _, key := range marketplaceNameFields() {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func marketplaceNameFields() []string {
	return []string{"name", "id", "marketplace", "url", "repo", "package", "path", "hostPattern", "pathPattern", "source"}
}

func marketplaceNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func marketplaceSortedNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for _, name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

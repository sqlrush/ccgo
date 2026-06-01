package model

import "testing"

func TestResolveDefaultAndAlias(t *testing.T) {
	registry := DefaultRegistry()
	capability, ok := registry.Resolve("")
	if !ok {
		t.Fatal("default model did not resolve")
	}
	if capability.Name != Claude46Sonnet {
		t.Fatalf("default = %q", capability.Name)
	}
	capability, ok = registry.Resolve("OPUS")
	if !ok || capability.Name != Claude46Opus {
		t.Fatalf("opus resolve = %#v %v", capability, ok)
	}
}

func TestResolveOneMillionContext(t *testing.T) {
	registry := DefaultRegistry()
	capability, ok := registry.Resolve("sonnet[1m]")
	if !ok {
		t.Fatal("sonnet[1m] did not resolve")
	}
	if capability.ContextWindowTokens != 1000000 || capability.Name != Claude46Sonnet+"[1m]" {
		t.Fatalf("capability = %#v", capability)
	}
}

func TestCanonicalName(t *testing.T) {
	got := CanonicalName("us.anthropic.claude-opus-4-6-v1:0")
	if got != "claude-opus-4-6" {
		t.Fatalf("canonical = %q", got)
	}
}

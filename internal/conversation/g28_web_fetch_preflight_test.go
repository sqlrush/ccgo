package conversation

import (
	"testing"

	"ccgo/internal/contracts"
)

// CFG-38: settings.skipWebFetchPreflight → tool metadata.
// Given: settings with SkipWebFetchPreflight=true
// When:  injectWebFetchSkipPreflight is called
// Then:  metadata["ccgo.tools.web.fetch.skip_preflight"] == true
func TestInjectWebFetchSkipPreflightTrue(t *testing.T) {
	t.Parallel()
	skip := true
	meta := map[string]any{}
	injectWebFetchSkipPreflight(meta, &skip)
	v, ok := meta["ccgo.tools.web.fetch.skip_preflight"]
	if !ok {
		t.Fatal("expected skip_preflight key to be present in metadata")
	}
	if v != true {
		t.Fatalf("expected true, got %v", v)
	}
}

// Given: settings with SkipWebFetchPreflight=false
// When:  injectWebFetchSkipPreflight is called
// Then:  key is NOT injected (falsy → no effect)
func TestInjectWebFetchSkipPreflightFalse(t *testing.T) {
	t.Parallel()
	skip := false
	meta := map[string]any{}
	injectWebFetchSkipPreflight(meta, &skip)
	if _, ok := meta["ccgo.tools.web.fetch.skip_preflight"]; ok {
		t.Fatal("expected skip_preflight key NOT to be present when false")
	}
}

// Given: settings with SkipWebFetchPreflight=nil
// When:  injectWebFetchSkipPreflight is called
// Then:  key is NOT injected
func TestInjectWebFetchSkipPreflightNil(t *testing.T) {
	t.Parallel()
	meta := map[string]any{}
	injectWebFetchSkipPreflight(meta, nil)
	if _, ok := meta["ccgo.tools.web.fetch.skip_preflight"]; ok {
		t.Fatal("expected skip_preflight key NOT to be present when nil")
	}
}

// Given: a Runner with SkipWebFetchPreflight=true in settings (via settingsOverride)
// When:  toolMetadata() is called
// Then:  returned metadata contains the skip_preflight key (CFG-38 production path)
func TestToolMetadataInjectsSkipWebFetchPreflight(t *testing.T) {
	t.Parallel()
	skip := true
	s := contracts.Settings{
		SkipWebFetchPreflight: &skip,
	}
	r := Runner{settingsOverride: &s}
	meta := r.toolMetadata()
	v, ok := meta["ccgo.tools.web.fetch.skip_preflight"]
	if !ok {
		t.Fatal("toolMetadata() should inject skip_preflight key when setting is true")
	}
	if v != true {
		t.Fatalf("expected true, got %v", v)
	}
}

// Given: a Runner with SkipWebFetchPreflight=false in settings
// When:  toolMetadata() is called
// Then:  returned metadata does NOT contain the skip_preflight key
func TestToolMetadataNoSkipWebFetchPreflightWhenFalse(t *testing.T) {
	t.Parallel()
	skip := false
	s := contracts.Settings{
		SkipWebFetchPreflight: &skip,
	}
	r := Runner{settingsOverride: &s}
	meta := r.toolMetadata()
	if _, ok := meta["ccgo.tools.web.fetch.skip_preflight"]; ok {
		t.Fatal("toolMetadata() should NOT inject skip_preflight key when setting is false")
	}
}

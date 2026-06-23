package repl

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
)

// TestConfigHandlerReturnsHandled verifies that configHandler always returns
// Handled=true so /config doesn't fall through to the model (CMD-CONFIG-01).
func TestConfigHandlerReturnsHandled(t *testing.T) {
	h := configHandler(func() contracts.Settings { return contracts.Settings{} }, "/tmp/work")
	out, err := h(context.Background(), CommandContext{CWD: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Handled {
		t.Fatal("configHandler must return Handled=true")
	}
	if out.Status == "" {
		t.Fatal("configHandler must return non-empty Status")
	}
}

// TestConfigHandlerIncludesModel verifies that when a model is set in settings,
// it appears in the config summary.
func TestConfigHandlerIncludesModel(t *testing.T) {
	h := configHandler(func() contracts.Settings {
		return contracts.Settings{Model: "claude-opus-4"}
	}, "/tmp/work")
	out, err := h(context.Background(), CommandContext{CWD: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out.Status, "claude-opus-4") {
		t.Fatalf("configHandler must include model in status; got: %s", out.Status)
	}
}

// TestConfigHandlerIncludesPermissions verifies that allow/deny permission rules
// are reflected in the config summary.
func TestConfigHandlerIncludesPermissions(t *testing.T) {
	h := configHandler(func() contracts.Settings {
		perms := &contracts.PermissionsSetting{
			Allow: []string{"Bash(git:*)"},
			Deny:  []string{"Bash(rm:*)"},
		}
		return contracts.Settings{Permissions: perms}
	}, "/tmp/work")
	out, err := h(context.Background(), CommandContext{CWD: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out.Status, "Bash(git:*)") {
		t.Fatalf("configHandler must include allow rule; got: %s", out.Status)
	}
	if !contains(out.Status, "Bash(rm:*)") {
		t.Fatalf("configHandler must include deny rule; got: %s", out.Status)
	}
}

// TestPluginHandlerReturnsHandled verifies that pluginHandler always returns
// Handled=true so /plugin doesn't fall through to the model (CMD-PLUGIN-01).
func TestPluginHandlerReturnsHandled(t *testing.T) {
	h := pluginHandler(func() contracts.Settings { return contracts.Settings{} })
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Handled {
		t.Fatal("pluginHandler must return Handled=true")
	}
}

// TestPluginHandlerNoPlugins verifies the empty-plugins message.
func TestPluginHandlerNoPlugins(t *testing.T) {
	h := pluginHandler(func() contracts.Settings { return contracts.Settings{} })
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out.Status, "No plugins") {
		t.Fatalf("pluginHandler must say 'No plugins' when none; got: %s", out.Status)
	}
}

// TestPluginHandlerListsPlugins verifies that configured plugins appear in the
// summary.
func TestPluginHandlerListsPlugins(t *testing.T) {
	h := pluginHandler(func() contracts.Settings {
		return contracts.Settings{
			PluginConfigs: map[string]contracts.PluginConfig{
				"my-plugin": {},
			},
		}
	})
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out.Status, "my-plugin") {
		t.Fatalf("pluginHandler must list plugin name; got: %s", out.Status)
	}
}



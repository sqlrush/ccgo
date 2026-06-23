package conversation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"ccgo/internal/auth"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	"ccgo/internal/mcp/reconnect"
	"ccgo/internal/mcp/remoteauth"
	pluginpkg "ccgo/internal/plugins"
)

func LoadMCPConfigFromSettingsFiles(cwd string) (*MCPConfig, error) {
	resolvedCWD, err := resolveMCPConfigCWD(cwd)
	if err != nil {
		return nil, err
	}
	userSettings, err := loadOptionalSettings(config.UserSettingsPath())
	if err != nil {
		return nil, err
	}
	projectSettings, err := loadOptionalSettings(config.ProjectSettingsPath(resolvedCWD))
	if err != nil {
		return nil, err
	}
	policySettings, err := config.LoadPolicySettings()
	if err != nil {
		return nil, err
	}
	localSettings, err := loadOptionalSettings(config.LocalSettingsPath(resolvedCWD))
	if err != nil {
		return nil, err
	}
	mergedSettings := config.MergeSettings(userSettings, projectSettings, localSettings, policySettings)
	settingsFileDetector, err := config.NewSettingsChangeDetector(mcpConfigSettingsFilePaths(resolvedCWD))
	if err != nil {
		return nil, err
	}
	return &MCPConfig{
		UserSettings:         userSettings,
		ProjectSettings:      projectSettings,
		LocalSettings:        localSettings,
		PolicySettings:       policySettings,
		PluginServers:        pluginpkg.LoadMCPServersWithSettings(pluginpkg.InstalledPluginDirs(resolvedCWD), mergedSettings),
		CWD:                  resolvedCWD,
		settingsFileDetector: settingsFileDetector,
		ToolOptions: mcp.ServerToolOptions{
			// MCP-39..44: CombinedAccessTokenProvider handles both first-time
			// interactive OAuth acquisition (via BrowserAuthorizer) and silent
			// token refresh from the per-server credential file. The Authorizer
			// field uses the OS browser opener so the user is directed to the
			// authorization server on first connect.
			AccessTokenProvider: remoteauth.CombinedAccessTokenProvider(remoteauth.CombinedOptions{
				StoreFor: func(name string, _ contracts.MCPServer) auth.CredentialStore {
					return auth.NewFileCredentialStore(mcp.DefaultMCPServerCredentialsPath(name))
				},
				Authorizer: remoteauth.NewBrowserAuthorizer(),
			}),
			// MCP-43: for remote transports (HTTP/SSE/WS), wrap OpenServerClient
			// with reconnect.Run so dropped connections are retried with
			// exponential backoff (up to DefaultMaxAttempts).
			OpenClient: reconnectingOpenClient,
		},
	}, nil
}

func (c *MCPConfig) RefreshSettingsFiles() (bool, error) {
	if c == nil || c.settingsFileDetector == nil {
		return false, nil
	}
	changes, err := c.settingsFileDetector.DetectChanges(mcpConfigSettingsFilePaths(c.CWD))
	if err != nil {
		return false, err
	}
	if len(changes) == 0 {
		return false, nil
	}
	userSettings, projectSettings, localSettings, err := loadMCPConfigSettingsFiles(c.CWD)
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(c.UserSettings, userSettings) ||
		!reflect.DeepEqual(c.ProjectSettings, projectSettings) ||
		!reflect.DeepEqual(c.LocalSettings, localSettings)
	c.UserSettings = userSettings
	c.ProjectSettings = projectSettings
	c.LocalSettings = localSettings
	c.refreshPluginServers()
	return changed, nil
}

func (c *MCPConfig) RefreshPolicySettings() (bool, error) {
	if c == nil {
		return false, nil
	}
	policySettings, err := config.LoadPolicySettings()
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(c.PolicySettings, policySettings)
	c.PolicySettings = policySettings
	c.refreshPluginServers()
	return changed, nil
}

func (c *MCPConfig) refreshPluginServers() {
	if c == nil {
		return
	}
	if c.CWD != "" {
		c.PluginServers = pluginpkg.LoadMCPServersWithSettings(pluginpkg.InstalledPluginDirs(c.CWD), c.MergedSettings())
	} else {
		c.PluginServers = nil
	}
}

func resolveMCPConfigCWD(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, nil
}

func loadOptionalSettings(path string) (contracts.Settings, error) {
	settings, err := config.LoadSettingsFile(path)
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return contracts.Settings{}, nil
	}
	return contracts.Settings{}, fmt.Errorf("load settings %s: %w", path, err)
}

func loadMCPConfigSettingsFiles(cwd string) (contracts.Settings, contracts.Settings, contracts.Settings, error) {
	userSettings, err := loadOptionalSettings(config.UserSettingsPath())
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	if cwd == "" {
		return userSettings, contracts.Settings{}, contracts.Settings{}, nil
	}
	projectSettings, err := loadOptionalSettings(config.ProjectSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	localSettings, err := loadOptionalSettings(config.LocalSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	return userSettings, projectSettings, localSettings, nil
}

func mcpConfigSettingsFilePaths(cwd string) []string {
	paths := []string{config.UserSettingsPath()}
	if cwd != "" {
		paths = append(paths, config.ProjectSettingsPath(cwd), config.LocalSettingsPath(cwd))
	}
	return paths
}

// reconnectingOpenClient is a ClientOpenFunc that wraps the default
// OpenServerClientWithOptions in reconnect.Run for remote transports (MCP-43).
// Local transports (stdio/sdk) are opened directly without reconnect.
// For remote transports, the function retries both the open AND the initialize
// step with exponential backoff so dropped SSE/HTTP/WS connections are healed.
func reconnectingOpenClient(ctx context.Context, name string, server contracts.MCPServer) (mcp.ClientHandle, error) {
	if !reconnect.ShouldReconnect(server.Type) {
		return mcp.OpenServerClientWithOptions(ctx, name, server, mcp.ServerToolOptions{})
	}
	var handle mcp.ClientHandle
	err := reconnect.Run(ctx, func(rctx context.Context) error {
		h, rerr := mcp.OpenServerClientWithOptions(rctx, name, server, mcp.ServerToolOptions{})
		if rerr != nil {
			return rerr
		}
		if init, ok := h.Client.(mcp.InitializingClient); ok {
			if ierr := init.EnsureInitialized(rctx); ierr != nil {
				if h.Close != nil {
					_ = h.Close()
				}
				return ierr
			}
		}
		handle = h
		return nil
	}, reconnect.Options{})
	return handle, err
}

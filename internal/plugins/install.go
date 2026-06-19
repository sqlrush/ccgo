package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type PluginInstallResult struct {
	Plugin           LoadedPlugin
	TargetPath       string
	AlreadyInstalled bool
}

type PluginUpdateResult struct {
	MarketplacePluginCount int
	Updated                []PluginUpdateItem
}

type PluginUpdateItem struct {
	Plugin     LoadedPlugin
	TargetPath string
}

type PluginUninstallResult struct {
	Plugin     LoadedPlugin
	TargetPath string
	Scope      string
}

func InstallMarketplacePlugin(name string, cwd string, settings contracts.Settings) (PluginInstallResult, error) {
	return InstallMarketplacePluginInScope(name, cwd, "project", settings)
}

func InstallMarketplacePluginInScope(name string, cwd string, scope string, settings contracts.Settings) (PluginInstallResult, error) {
	plugin, ok := findLoadedPlugin(LoadPluginDirsWithSettings(nil, settings), name)
	if !ok {
		return PluginInstallResult{}, fmt.Errorf("not found in configured marketplace sources")
	}
	resolvedScope, err := resolvePluginInstallScope(scope, plugin, settings, false)
	if err != nil {
		return PluginInstallResult{}, err
	}
	targetPluginsDir, err := installPluginsDirForScope(cwd, resolvedScope)
	if err != nil {
		return PluginInstallResult{}, err
	}
	targetRoot := filepath.Join(targetPluginsDir, SafePluginInstallDirName(plugin))
	if SameResolvedPath(plugin.Root, targetRoot) {
		return PluginInstallResult{Plugin: plugin, TargetPath: targetRoot, AlreadyInstalled: true}, nil
	}
	if _, err := os.Stat(targetRoot); err == nil {
		if installed, loadErr := LoadPluginDir(targetRoot); loadErr == nil && strings.TrimSpace(installed.Name) == strings.TrimSpace(plugin.Name) {
			return PluginInstallResult{Plugin: plugin, TargetPath: targetRoot, AlreadyInstalled: true}, nil
		}
		return PluginInstallResult{}, fmt.Errorf("target path already exists: %s", targetRoot)
	} else if err != nil && !os.IsNotExist(err) {
		return PluginInstallResult{}, err
	}
	if err := CopyPluginDir(plugin.Root, targetRoot); err != nil {
		return PluginInstallResult{}, err
	}
	return PluginInstallResult{Plugin: plugin, TargetPath: targetRoot}, nil
}

func UpdateInstalledMarketplacePlugins(name string, cwd string, settings contracts.Settings) (PluginUpdateResult, error) {
	return UpdateInstalledMarketplacePluginsInScope(name, cwd, "project", settings)
}

func UpdateInstalledMarketplacePluginsInScope(name string, cwd string, scope string, settings contracts.Settings) (PluginUpdateResult, error) {
	marketplacePlugins := LoadPluginDirsWithSettings(nil, settings)
	resolvedScope, err := resolvePluginUpdateScope(scope, name, marketplacePlugins, settings)
	if err != nil {
		return PluginUpdateResult{}, err
	}
	installedRoots, err := installedPluginDirsForScope(cwd, resolvedScope)
	if err != nil {
		return PluginUpdateResult{}, err
	}
	result := PluginUpdateResult{MarketplacePluginCount: len(marketplacePlugins)}
	installedPlugins := LoadPluginDirs(installedRoots)
	name = strings.TrimSpace(name)
	if name != "" && !strings.EqualFold(name, "all") {
		installed, ok := findLoadedPlugin(installedPlugins, name)
		if !ok {
			return result, fmt.Errorf("installed plugin %s was not found", name)
		}
		marketplacePlugin, ok := findLoadedPlugin(marketplacePlugins, installed.Name)
		if !ok {
			return result, fmt.Errorf("plugin %s was not found in configured marketplace sources", installed.Name)
		}
		if SameResolvedPath(marketplacePlugin.Root, installed.Root) {
			return result, nil
		}
		if err := ReplacePluginDir(marketplacePlugin.Root, installed.Root); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, PluginUpdateItem{Plugin: marketplacePlugin, TargetPath: installed.Root})
		return result, nil
	}
	sort.SliceStable(installedPlugins, func(i, j int) bool {
		if installedPlugins[i].Name == installedPlugins[j].Name {
			return installedPlugins[i].Root < installedPlugins[j].Root
		}
		return installedPlugins[i].Name < installedPlugins[j].Name
	})
	for _, installed := range installedPlugins {
		marketplacePlugin, ok := findLoadedPlugin(marketplacePlugins, installed.Name)
		if !ok || SameResolvedPath(marketplacePlugin.Root, installed.Root) {
			continue
		}
		if err := ReplacePluginDir(marketplacePlugin.Root, installed.Root); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, PluginUpdateItem{Plugin: marketplacePlugin, TargetPath: installed.Root})
	}
	return result, nil
}

func UninstallInstalledPluginInScope(name string, cwd string, scope string) (PluginUninstallResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return PluginUninstallResult{}, fmt.Errorf("plugin name is required")
	}
	searchScope := strings.ToLower(strings.TrimSpace(scope))
	switch searchScope {
	case "":
		searchScope = "all"
	case "project", "user", "local":
	default:
		return PluginUninstallResult{}, fmt.Errorf("scope %q is not supported; use project, user, or local", scope)
	}
	installedRoots, err := installedPluginDirsForScope(cwd, searchScope)
	if err != nil {
		return PluginUninstallResult{}, err
	}
	installedPlugins := LoadPluginDirs(installedRoots)
	plugin, ok := findLoadedPlugin(installedPlugins, name)
	if !ok {
		return PluginUninstallResult{}, fmt.Errorf("installed plugin %s was not found", name)
	}
	if !pluginRootInInstalledRoots(plugin.Root, installedRoots) {
		return PluginUninstallResult{}, fmt.Errorf("refusing to uninstall plugin outside installed plugin directories: %s", plugin.Root)
	}
	targetPath := plugin.Root
	if strings.TrimSpace(targetPath) == "" || cleanAbs(targetPath) == string(filepath.Separator) {
		return PluginUninstallResult{}, fmt.Errorf("refusing to uninstall unsafe plugin path: %s", targetPath)
	}
	if err := os.RemoveAll(targetPath); err != nil {
		return PluginUninstallResult{}, err
	}
	return PluginUninstallResult{Plugin: plugin, TargetPath: targetPath, Scope: InstalledPluginScope(cwd, targetPath)}, nil
}

func installPluginsDirForScope(cwd string, scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "project":
		if strings.TrimSpace(cwd) == "" {
			return "", fmt.Errorf("working directory is unavailable")
		}
		return filepath.Join(cwd, ".claude", "plugins"), nil
	case "local":
		if strings.TrimSpace(cwd) == "" {
			return "", fmt.Errorf("working directory is unavailable")
		}
		return filepath.Join(cwd, ".claude", "plugins"), nil
	case "user":
		return filepath.Join(platform.ClaudeHomeDir(), "plugins"), nil
	default:
		return "", fmt.Errorf("scope %q is not supported; use project, user, or local", scope)
	}
}

func installedPluginDirsForScope(cwd string, scope string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "project":
		if strings.TrimSpace(cwd) == "" {
			return nil, fmt.Errorf("working directory is unavailable")
		}
		return ProjectPluginDirs(cwd), nil
	case "local":
		if strings.TrimSpace(cwd) == "" {
			return nil, fmt.Errorf("working directory is unavailable")
		}
		return ProjectPluginDirs(cwd), nil
	case "user":
		return UserPluginDirs(), nil
	case "all":
		if strings.TrimSpace(cwd) == "" {
			return nil, fmt.Errorf("working directory is unavailable")
		}
		return InstalledPluginDirs(cwd), nil
	default:
		return nil, fmt.Errorf("scope %q is not supported; use project, user, local, or all", scope)
	}
}

func resolvePluginInstallScope(scope string, plugin LoadedPlugin, settings contracts.Settings, allowAll bool) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = preferredMarketplaceInstallScope(plugin, settings)
	}
	if scope == "" {
		scope = "project"
	}
	switch scope {
	case "project", "user", "local":
		return scope, nil
	case "all":
		if allowAll {
			return scope, nil
		}
	}
	return "", fmt.Errorf("scope %q is not supported; use project, user, or local", scope)
}

func resolvePluginUpdateScope(scope string, name string, marketplacePlugins []LoadedPlugin, settings contracts.Settings) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" && strings.TrimSpace(name) != "" && !strings.EqualFold(strings.TrimSpace(name), "all") {
		if plugin, ok := findLoadedPlugin(marketplacePlugins, name); ok {
			scope = preferredMarketplaceInstallScope(plugin, settings)
		}
	}
	if scope == "" {
		scope = "project"
	}
	if scope == "all" {
		return scope, nil
	}
	return resolvePluginInstallScope(scope, LoadedPlugin{}, settings, false)
}

func pluginRootInInstalledRoots(root string, installedRoots []string) bool {
	rootClean := cleanAbs(root)
	for _, installedRoot := range installedRoots {
		if rootClean == cleanAbs(installedRoot) || SameResolvedPath(root, installedRoot) {
			return true
		}
	}
	return false
}

func preferredMarketplaceInstallScope(plugin LoadedPlugin, settings contracts.Settings) string {
	marketplace := strings.TrimSpace(plugin.Marketplace)
	if marketplace == "" {
		return ""
	}
	for name, raw := range settings.ExtraKnownMarketplaces {
		if !strings.EqualFold(strings.TrimSpace(name), marketplace) {
			continue
		}
		entry, _ := raw.(map[string]any)
		return strings.ToLower(strings.TrimSpace(stringFromAnyMap(entry, "installLocation")))
	}
	return ""
}

func SafePluginInstallDirName(plugin LoadedPlugin) string {
	name := strings.TrimSpace(plugin.Name)
	if name == "" {
		name = filepath.Base(plugin.Root)
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		allowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' ||
			r == '_' ||
			r == '.'
		if allowed {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	name = strings.Trim(b.String(), ".-")
	if name == "" {
		return "plugin"
	}
	return name
}

func SameResolvedPath(a string, b string) bool {
	a = cleanResolvedPath(a)
	b = cleanResolvedPath(b)
	return a != "" && b != "" && a == b
}

func cleanResolvedPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func CopyPluginDir(src string, dst string) error {
	src = cleanResolvedPath(src)
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source plugin is not a directory")
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("target path already exists: %s", dst)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, cleanup, err := stagePluginDir(src, parent, filepath.Base(dst), info.Mode().Perm())
	if err != nil {
		return err
	}
	defer cleanup(true)
	if err := os.Rename(temp, dst); err != nil {
		return err
	}
	cleanup(false)
	return nil
}

func ReplacePluginDir(src string, dst string) error {
	src = cleanResolvedPath(src)
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source plugin is not a directory")
	}
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return CopyPluginDir(src, dst)
	} else if err != nil {
		return err
	}
	parent := filepath.Dir(dst)
	temp, cleanupTemp, err := stagePluginDir(src, parent, filepath.Base(dst), info.Mode().Perm())
	if err != nil {
		return err
	}
	defer cleanupTemp(true)
	backup, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".old-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		_ = os.RemoveAll(backup)
		return err
	}
	backupActive := false
	defer func() {
		if backupActive {
			_ = os.RemoveAll(backup)
		}
	}()
	if err := os.Rename(dst, backup); err != nil {
		return err
	}
	backupActive = true
	if err := os.Rename(temp, dst); err != nil {
		if restoreErr := os.Rename(backup, dst); restoreErr != nil {
			backupActive = false
			return fmt.Errorf("%w; failed to restore backup %s: %v", err, backup, restoreErr)
		}
		backupActive = false
		return err
	}
	cleanupTemp(false)
	_ = os.RemoveAll(backup)
	backupActive = false
	return nil
}

func stagePluginDir(src string, parent string, base string, mode os.FileMode) (string, func(bool), error) {
	temp, err := os.MkdirTemp(parent, "."+base+".tmp-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func(remove bool) {
		if remove {
			_ = os.RemoveAll(temp)
		}
	}
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Chmod(temp, mode); err != nil {
		cleanup(true)
		return "", nil, err
	}
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(temp, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to install plugin symlink %s", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to install non-regular plugin file %s", rel)
		}
		return copyPluginFile(path, target, info.Mode().Perm())
	}); err != nil {
		cleanup(true)
		return "", nil, err
	}
	return temp, cleanup, nil
}

func copyPluginFile(src string, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	return os.WriteFile(dst, data, mode)
}

func findLoadedPlugin(plugins []LoadedPlugin, name string) (LoadedPlugin, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, plugin := range plugins {
		if strings.ToLower(strings.TrimSpace(plugin.Name)) == key {
			return plugin, true
		}
	}
	return LoadedPlugin{}, false
}

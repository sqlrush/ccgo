package plugins

// InstallFromNpm and InstallFromGitHub implement direct plugin installation
// without requiring a configured marketplace catalog.
//
// PLUGIN-21: InstallFromNpm downloads a package via `npm install` and copies
//            the result into targetPath.
// PLUGIN-22: InstallFromGitHub expands an owner/repo shorthand to a GitHub
//            HTTPS URL and clones it.
//
// CC ref: src/utils/plugins/pluginLoader.ts:installFromNpm / installFromGitHub.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// errInvalidShorthand is returned when a GitHub shorthand is empty or
// structurally invalid.
var errInvalidShorthand = errors.New("invalid GitHub shorthand")

// InstallFromNpmOptions configures an npm direct install.
type InstallFromNpmOptions struct {
	// Registry overrides the npm registry URL (e.g. a private registry).
	Registry string
	// Version pins the package version (e.g. "1.2.3").
	Version string
}

const installNPMTimeout = 120 * time.Second
const installGitTimeout = 60 * time.Second

// InstallFromNpm installs an npm package directly into targetPath using
// `npm install --prefix <npmCacheDir>` then copies the installed package.
// ⚠️ Live npm network required for a real install; test with stub.
// CC ref: src/utils/plugins/pluginLoader.ts:installFromNpm.
func InstallFromNpm(packageName string, targetPath string, opts InstallFromNpmOptions) error {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return fmt.Errorf("npm package name is required")
	}

	packageSpec := packageName
	if strings.TrimSpace(opts.Version) != "" {
		packageSpec = packageName + "@" + strings.TrimSpace(opts.Version)
	}

	npmCacheDir := filepath.Join(filepath.Dir(targetPath), ".npm-install-cache")
	if err := os.MkdirAll(npmCacheDir, 0o700); err != nil {
		return fmt.Errorf("InstallFromNpm: create npm cache dir: %w", err)
	}

	// Build npm install args.
	args := []string{"install", packageSpec, "--prefix", npmCacheDir}
	if strings.TrimSpace(opts.Registry) != "" {
		args = append(args, "--registry", strings.TrimSpace(opts.Registry))
	}

	ctx, cancel := newTimeoutContext(installNPMTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", args...) //nolint:gosec
	cmd.Env = append(os.Environ(),
		"NPM_CONFIG_AUDIT=false",
		"NPM_CONFIG_FUND=false",
		"NPM_CONFIG_UPDATE_NOTIFIER=false",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("InstallFromNpm: npm install %s: %w: %s", packageSpec, err, strings.TrimSpace(string(output)))
	}

	// The installed package lives at npmCacheDir/node_modules/<packageName>.
	// For scoped packages (@scope/pkg) the dir is @scope/pkg.
	installedPath := filepath.Join(npmCacheDir, "node_modules", packageName)
	if _, err := os.Stat(installedPath); err != nil {
		return fmt.Errorf("InstallFromNpm: installed package path not found at %s after npm install", installedPath)
	}

	return CopyPluginDir(installedPath, targetPath)
}

// InstallFromGitHub clones a GitHub repository into targetPath.
// The repo argument may be either "owner/repo" shorthand (expanded to a
// github.com HTTPS URL) or a full HTTPS/SSH URL.
// ref pins a branch or tag; sha checks out a specific commit after clone.
// PLUGIN-22: GitHub shorthand `owner/repo` auto-detection.
// CC ref: src/utils/plugins/pluginLoader.ts:installFromGitHub.
func InstallFromGitHub(repo string, targetPath string, ref string, sha string) error {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return fmt.Errorf("%w: repo is empty", errInvalidShorthand)
	}

	gitURL := expandGitHubShorthand(repo)

	ctx, cancel := newTimeoutContext(installGitTimeout)
	defer cancel()

	// Always start with a shallow clone for efficiency.
	args := []string{"clone", "--depth", "1", "--recurse-submodules", "--shallow-submodules"}
	if strings.TrimSpace(ref) != "" {
		args = append(args, "--branch", strings.TrimSpace(ref))
	}
	if strings.TrimSpace(sha) != "" {
		args = append(args, "--no-checkout")
	}
	args = append(args, gitURL, targetPath)

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("InstallFromGitHub: git clone %s: %w: %s", gitURL, err, strings.TrimSpace(string(output)))
	}

	// Check out specific commit if sha is specified.
	if strings.TrimSpace(sha) != "" {
		checkoutCtx, checkoutCancel := newTimeoutContext(installGitTimeout)
		defer checkoutCancel()
		checkCmd := exec.CommandContext(checkoutCtx, "git", "-C", targetPath, "checkout", strings.TrimSpace(sha)) //nolint:gosec
		checkCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, checkErr := checkCmd.CombinedOutput(); checkErr != nil {
			return fmt.Errorf("InstallFromGitHub: git checkout %s: %w: %s", sha, checkErr, strings.TrimSpace(string(out)))
		}
	}

	return nil
}

func newTimeoutContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// expandGitHubShorthand converts "owner/repo" to a GitHub HTTPS URL.
// Full URLs (http:// / https:// / git@) are returned unchanged.
// CC ref: src/utils/plugins/pluginLoader.ts:installFromGitHub
//
//	(source.source === 'github' → `https://github.com/${source.repo}.git`).
func expandGitHubShorthand(repo string) string {
	lower := strings.ToLower(repo)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "git@") {
		return repo
	}
	// Strip common prefixes users might add.
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimPrefix(repo, "www.github.com/")
	return "https://github.com/" + repo + ".git"
}

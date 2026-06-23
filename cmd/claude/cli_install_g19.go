package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"ccgo/internal/updater"
)

// installOptions carries injected state for runInstallCLIv2, enabling
// deterministic unit tests without touching the filesystem or network.
type installOptions struct {
	// Target is "latest", "stable", or a specific version string.
	Target string
	// Force causes reinstall even when the installed version matches latest.
	Force bool
	// CurrentVersion is the currently-installed version (for up-to-date check).
	// When empty, runInstallCLIv2 reads the package-level version variable.
	CurrentVersion string
	// TargetPath is the destination binary path.
	// When empty, defaults to ~/.local/bin/claude (or .exe on Windows).
	TargetPath string
	// CurrentInstType is the current install type (e.g. "npm-global", "native").
	// When empty, runInstallCLIv2 calls resolveInstallType().
	CurrentInstType string
}

// runInstallCLIv2 implements the real install/upgrade logic for "claude install":
//  1. Resolves the target version from the release server.
//  2. If already up to date and not --force, reports and exits 0.
//  3. If npm-global/npm-local, prints migration notice.
//  4. Downloads + atomically replaces the target binary.
//
// ReleaseBaseURL is read from the CLAUDE_RELEASE_BASE_URL env var (tests set it
// to an httptest server URL); production uses the real GCS bucket.
func runInstallCLIv2(_ []string, opts installOptions, stdout, stderr io.Writer) int {
	baseURL := updater.ResolveBaseURL()

	channel := opts.Target
	if channel == "" {
		channel = "latest"
	}

	// Resolve the target version.
	fmt.Fprintf(stdout, "Checking release server for %s version...\n", channel)
	latestVer, err := updater.CheckLatestVersion(channel, baseURL)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to check release server: %v\n", err)
		fmt.Fprintln(stderr, "Try running 'claude doctor' for diagnostics.")
		return 1
	}
	fmt.Fprintf(stdout, "Latest version: %s\n", latestVer)

	// Determine current version.
	curVer := opts.CurrentVersion
	if curVer == "" {
		curVer = version
	}

	// Already up to date and not forced.
	if curVer == latestVer && !opts.Force {
		fmt.Fprintf(stdout, "Claude is up to date (%s)\n", curVer)
		return 0
	}

	// Migration notice for npm installs.
	instType := opts.CurrentInstType
	if instType == "" {
		instType = resolveInstallType()
	}
	if strings.HasPrefix(instType, "npm") {
		fmt.Fprintln(stdout, "Detected npm installation — migrating to native binary.")
		fmt.Fprintln(stdout, "The npm package will be superseded by the native binary.")
		fmt.Fprintln(stdout, "(To remove the old npm installation, run: npm uninstall -g @anthropic-ai/claude-code)")
	}

	// Determine target path.
	targetPath := opts.TargetPath
	if targetPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "Error: cannot determine home dir: %v\n", err)
			return 1
		}
		targetPath = homeLocalBinClaude(home)
	}

	if opts.Force && curVer == latestVer {
		fmt.Fprintf(stdout, "Reinstalling %s (--force)...\n", latestVer)
	} else {
		fmt.Fprintf(stdout, "Installing %s → %s...\n", curVer, latestVer)
	}

	if err := updater.DownloadAndInstall(latestVer, baseURL, targetPath); err != nil {
		fmt.Fprintf(stderr, "Error: install failed: %v\n", err)
		fmt.Fprintln(stderr, "Try running 'claude doctor' for diagnostics.")
		return 1
	}

	fmt.Fprintf(stdout, "Successfully installed claude %s to %s\n", latestVer, targetPath)
	return 0
}

// homeLocalBinClaude returns the default native binary installation path for
// the given home directory, matching CC's ~/.local/bin/claude convention.
func homeLocalBinClaude(home string) string {
	binaryName := updater.BinaryName()
	if binaryName == "claude.exe" {
		// Windows: %LOCALAPPDATA%\Claude\claude.exe equivalent approximation.
		return home + `\.local\bin\` + binaryName
	}
	return home + "/.local/bin/" + binaryName
}

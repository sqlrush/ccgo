package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/mcp"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

const version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version")
	flags.BoolVar(showVersion, "v", false, "print version")
	cwd := flags.String("cwd", "", "working directory for local tools")
	allowMutating := flags.Bool("allow-mutating-tools", false, "allow write and command tools")
	flags.BoolVar(allowMutating, "allowMutatingTools", false, "allow write and command tools")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "%s (ccgo mcp)\n", version)
		return 0
	}
	workingDirectory, err := resolveWorkingDirectory(*cwd)
	if err != nil {
		fmt.Fprintf(stderr, "claude-mcp: %v\n", err)
		return 1
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		fmt.Fprintf(stderr, "claude-mcp: %v\n", err)
		return 1
	}
	server, err := mcp.NewBuiltinServer(mcp.BuiltinServerOptions{
		Registry:           registry,
		Executor:           tool.NewExecutor(registry),
		WorkingDirectory:   workingDirectory,
		AllowMutatingTools: *allowMutating,
	})
	if err != nil {
		fmt.Fprintf(stderr, "claude-mcp: %v\n", err)
		return 1
	}
	if err := server.Run(context.Background(), stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "claude-mcp: %v\n", err)
		return 1
	}
	return 0
}

func resolveWorkingDirectory(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		cwd = wd
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("invalid --cwd %q: %w", raw, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("invalid --cwd %q: %w", raw, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid --cwd %q: not a directory", raw)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, nil
}

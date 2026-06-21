package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"ccgo/internal/mcp"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

// newBuiltinMCPServer constructs a stdio MCP server exposing the local tool
// registry — the same tools the agent uses (mirrors CC entrypoints/mcp.ts).
// The cwd parameter is the working directory exposed to tools.
func newBuiltinMCPServer(cwd string) (*mcp.BuiltinServer, error) {
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		return nil, fmt.Errorf("build tool registry: %w", err)
	}
	srv, err := mcp.NewBuiltinServer(mcp.BuiltinServerOptions{
		Registry:           registry,
		Executor:           tool.NewExecutor(registry),
		WorkingDirectory:   cwd,
		AllowMutatingTools: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create builtin MCP server: %w", err)
	}
	return srv, nil
}

// mcpServe implements the `claude mcp serve` subcommand.  It accepts -d /
// --debug / --verbose for CC flag parity but ignores them.
func mcpServe(args []string, _, stderr io.Writer) int {
	for _, a := range args {
		switch a {
		case "-d", "--debug", "--verbose":
			// accepted and ignored for CC parity
		default:
			fmt.Fprintf(stderr, "ccgo mcp serve: unknown flag %s\n", a)
			return 1
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	server, err := newBuiltinMCPServer(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	if err := server.Run(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	return 0
}

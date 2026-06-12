package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

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
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "%s (ccgo mcp)\n", version)
		return 0
	}
	workingDirectory := *cwd
	if workingDirectory == "" {
		var err error
		workingDirectory, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "claude-mcp: %v\n", err)
			return 1
		}
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

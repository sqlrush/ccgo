package main

import (
	"flag"
	"fmt"
	"os"

	"ccgo/internal/bootstrap"
)

const version = "0.0.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	flag.BoolVar(showVersion, "v", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s (ccgo)\n", version)
		return
	}

	state, err := bootstrap.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccgo: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "ccgo scaffold ready\nsession_id=%s\ncwd=%s\n", state.SessionID(), state.CWD())
}

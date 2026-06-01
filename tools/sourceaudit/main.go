package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	source := flag.String("source", "/Users/sqlrush/agent/claude-code", "Claude Code source snapshot root")
	out := flag.String("out", "-", "output JSON file, or - for stdout")
	flag.Parse()

	audit, err := AuditSource(*source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sourceaudit: %v\n", err)
		os.Exit(1)
	}
	if err := WriteAuditJSON(audit, *out); err != nil {
		fmt.Fprintf(os.Stderr, "sourceaudit: %v\n", err)
		os.Exit(1)
	}
}

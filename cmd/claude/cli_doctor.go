package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"ccgo/internal/doctor"
)

// runDoctorCommand implements "claude doctor": runs the shared diagnostic engine
// and prints the formatted report. Exits 1 if any check has StatusError.
//
// SUBCMD-DOCTOR-10: supports --check-network <url> to opt into an HTTP connectivity
// check. The check is skipped when the flag is not provided (network-free default).
// CC ref: src/screens/Doctor.tsx:131 distTagsPromise + getDoctorDiagnostic.
func runDoctorCommand(args []string, cwd string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("claude doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkNetwork := fs.String("check-network", "", "URL to probe for network connectivity (opt-in)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	in := doctor.Input{
		Version:              version,
		CWD:                  cwd,
		NetworkCheckEndpoint: strings.TrimSpace(*checkNetwork),
	}

	report := doctor.Run(in)

	fmt.Fprintln(stdout, doctor.Format(report))

	if report.HasErrors() {
		return 1
	}
	return 0
}

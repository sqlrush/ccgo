package main

import (
	"fmt"
	"io"

	"ccgo/internal/doctor"
)

// runDoctorCommand implements "claude doctor": runs the shared diagnostic engine
// and prints the formatted report. Exits 1 if any check has StatusError.
func runDoctorCommand(args []string, cwd string, stdout io.Writer, stderr io.Writer) int {
	_ = args // no subcommands yet

	report := doctor.Run(doctor.Input{
		Version: version,
		CWD:     cwd,
	})

	fmt.Fprintln(stdout, doctor.Format(report))

	if report.HasErrors() {
		return 1
	}
	return 0
}

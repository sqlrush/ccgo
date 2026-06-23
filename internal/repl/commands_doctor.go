package repl

import (
	"context"

	"ccgo/internal/doctor"
)

// doctorRunner is the DI interface: returns the formatted doctor report string.
type doctorRunner func() string

// doctorHandlerWith is the dependency-injected doctor handler (testable without
// touching the real environment).
func doctorHandlerWith(run doctorRunner) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: run()}, nil
	}
}

// doctorHandler builds the production handler using the real doctor engine.
func doctorHandler(cwd, version string) CommandHandler {
	return doctorHandlerWith(func() string {
		report := doctor.Run(doctor.Input{Version: version, CWD: cwd})
		return doctor.Format(report)
	})
}

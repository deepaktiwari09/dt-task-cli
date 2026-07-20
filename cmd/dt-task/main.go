package main

import (
	"os"
	"time"

	"github.com/deepaktiwari09/dt-task-cli/internal/cli"
)

func main() {
	root, app, err := cli.New()
	if err != nil {
		os.Stderr.WriteString("error: " + err.Error() + "\n")
		os.Exit(1)
	}
	started := time.Now()
	if err := root.Execute(); err != nil {
		app.Log.Error("command failed", "operation", "cli", "duration_ms", time.Since(started).Milliseconds(), "result", "error", "error_type", cli.ExitCode(err))
		app.WriteError(err)
		os.Exit(cli.ExitCode(err))
	}
}

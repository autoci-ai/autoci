package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/autoci-ai/autoci/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if cli.IsDoctorUnhealthy(err) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

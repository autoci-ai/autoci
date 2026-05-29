package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

func RunDepotCI(ctx context.Context, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "depot", "ci", "run")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr
	}
	return err
}

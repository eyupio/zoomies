package silent

import (
	"context"
	"errors"
	"os/exec"
)

// Exec runs the real wsl.exe, hidden.
type Exec struct{ Path string }

func (e Exec) cmd(ctx context.Context, args []string) *exec.Cmd {
	c := exec.CommandContext(ctx, e.Path, args...)
	Hide(c)
	return c
}

func (e Exec) Output(ctx context.Context, args ...string) ([]byte, error) {
	return e.cmd(ctx, args).Output()
}

func (e Exec) Run(ctx context.Context, args ...string) (int, error) {
	err := e.cmd(ctx, args).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

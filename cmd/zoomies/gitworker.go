package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/eyupio/zoomies/internal/gitmerge"
)

// runGitWorker is the bounded Git merge worker: one JSON merge request on
// standard input, one JSON answer on standard output, then exit.
//
// It is a subcommand of the one binary and a separate image, which is the point
// of decision 0016. The controller is distroless and runs no repository code, so
// the merge happens here, in an image whose only contents worth taking are Git
// and this binary: no database, no GitHub App key, no model provider key. Every
// field of the request crosses a process boundary the controller drew, which is
// why the request carries three commits and three limits and nothing else.
func runGitWorker(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies git-worker [--check]",
		"The bounded Git merge worker that conflict repair runs in its own image. It reads one JSON merge request on standard input and writes one JSON answer on standard output. It holds no credentials and runs no repository code.")
	check := fs.Bool("check", false, "say whether this image's Git can run the bounded merge, then exit: the worker image's last build step")
	fs.example(
		`echo '{"dir":"/work/repo","ancestor":"<sha>","base":"<sha>","head":"<sha>"}' | zoomies git-worker`,
		"zoomies git-worker --check",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}

	if *check {
		toolchain, err := gitmerge.Check(ctx)
		if err != nil {
			return workerError{err}
		}
		fmt.Fprintf(e.out, "git %s can run the bounded merge, which needs %s or newer\n", toolchain.Git, gitmerge.MinimumGitVersion)
		return nil
	}

	// A person at a terminal has not written a request, and waiting for one
	// they cannot see the shape of is a command that hangs. Say how it is
	// started instead of reading until they give up.
	if f, ok := e.in.(*os.File); ok && stdTerminal(f) {
		return usagef("git-worker", "reads a JSON merge request on standard input, so the controller starts it with a request written to it; --check says whether this image can merge")
	}
	if err := gitmerge.Serve(ctx, e.in, e.out); err != nil {
		return workerError{err}
	}
	return nil
}

// workerError drops the package prefix from the merge package's messages, which
// reads as a stutter under "zoomies git-worker:", while keeping the cause wrapped
// so a cancelled context is still recognised and exits quietly.
type workerError struct{ err error }

func (w workerError) Error() string { return strings.TrimPrefix(w.err.Error(), "gitmerge: ") }

func (w workerError) Unwrap() error { return w.err }

package main

import (
	"context"
	"fmt"
	"io"
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
	fs := newFlagSet(e, "zoomies git-worker [--request path] [--answer path] [--check]",
		"The bounded Git merge worker that conflict repair runs in its own image. It reads one JSON merge request, on standard input or from --request, and writes one JSON answer, on standard output or to --answer, then exits. It holds no credentials and runs no repository code.")
	check := fs.Bool("check", false, "say whether this image's Git can run the bounded merge, then exit: the worker image's last build step")
	requestPath := fs.String("request", "", "read the request from this file instead of standard input")
	answerPath := fs.String("answer", "", "write the answer to this file instead of standard output")
	fs.example(
		`echo '{"dir":"/work/repo","ancestor":"<sha>","base":"<sha>","head":"<sha>"}' | zoomies git-worker`,
		"zoomies git-worker --request /work/request.json --answer /work/answer.json",
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

	// The agent runs this as a one-shot container with the workspace mounted, so
	// the request and the answer are files beside the repository. Reading them
	// from standard input would mean plumbing an attach into a container runtime
	// for a process that runs once and exits.
	in, out, closeFiles, err := workerStreams(e, *requestPath, *answerPath)
	if err != nil {
		return err
	}
	defer closeFiles()

	// A person at a terminal has not written a request, and waiting for one they
	// cannot see the shape of is a command that hangs. Say how it is started
	// instead of reading until they give up.
	if *requestPath == "" {
		if f, ok := e.in.(*os.File); ok && stdTerminal(f) {
			return usagef("git-worker", "reads a JSON merge request on standard input, so the controller starts it with a request written to it; --request and --answer name files instead, and --check says whether this image can merge")
		}
	}
	if err := gitmerge.Serve(ctx, in, out); err != nil {
		return workerError{err}
	}
	return nil
}

// workerStreams opens the files the request and the answer live in, falling back
// to the process's own streams when neither is named.
func workerStreams(e *env, requestPath, answerPath string) (io.Reader, io.Writer, func(), error) {
	nothing := func() {}
	in, out := e.in, e.out
	if requestPath != "" {
		file, err := os.Open(requestPath)
		if err != nil {
			return nil, nil, nothing, usagef("git-worker", "cannot read the request from %s: %v", requestPath, err)
		}
		in = file
		previous := nothing
		nothing = func() { _ = file.Close(); previous() }
	}
	if answerPath != "" {
		file, err := os.OpenFile(answerPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, nothing, usagef("git-worker", "cannot write the answer to %s: %v", answerPath, err)
		}
		out = file
		previous := nothing
		nothing = func() { _ = file.Close(); previous() }
	}
	return in, out, nothing, nil
}

// workerError drops the package prefix from the merge package's messages, which
// reads as a stutter under "zoomies git-worker:", while keeping the cause wrapped
// so a cancelled context is still recognised and exits quietly.
type workerError struct{ err error }

func (w workerError) Error() string { return strings.TrimPrefix(w.err.Error(), "gitmerge: ") }

func (w workerError) Unwrap() error { return w.err }

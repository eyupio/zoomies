// Command fakegithub serves the in-process GitHub fake on a loopback port, so
// that a browser can reach it.
//
// The Playwright suite drives the real binary, and everything below the browser
// already runs against `internal/github`'s fake -- but that fake is an
// in-process test server, so the one client that could never reach it was the
// one an operator uses. Connecting an App, verifying it, and watching a runner
// appear were therefore tested at every layer except the page they happen on.
//
// This is that fake with a port. It is a test-only program: it mints nothing,
// verifies no signature, and answers every App as though the credentials were
// perfect, which is exactly what makes it useless to anything but a test and
// safe to point a browser at.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/eyupio/zoomies/internal/github"
)

func main() {
	flag.Parse()

	fake := github.NewFake()
	defer fake.Close()

	// Something for a fleet to see. The names match the demo seed's, so a
	// spec that connects this fake and one that reads the seeded fixture are
	// looking at the same organisation.
	for _, repo := range []string{"acme/widgets", "acme/site", "acme/infra"} {
		fake.AddRepo(repo)
	}

	// One line, parsed by the harness that started it: the URL to hand to an
	// installation, and the two identifiers the connect form asks for.
	//
	// No flush: os.Stdout is unbuffered, and asking a pipe to sync fails with
	// "invalid argument" -- which, guarded, exited this program the instant it
	// had said where it was, so the harness read an address nothing was
	// listening on.
	fmt.Printf("fakegithub url=%s app_id=%d installation_id=%d\n",
		fake.URL(), fake.AppID(), fake.InstallationID())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

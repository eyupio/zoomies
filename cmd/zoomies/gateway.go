package main

import (
	"context"
	"fmt"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/gateway"
	"github.com/eyupio/zoomies/internal/logging"
)

// runGateway is `zoomies gateway`: the provider-side end of a private
// connection. It runs beside a hypervisor the controller cannot reach -- a
// Proxmox cluster on a home network, typically -- and forwards what arrives
// through the tunnel to that one API. The address it prints is what the
// controller's provider form asks for.
func runGateway(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies gateway --target <host:port> [--state-dir path] [--quiet]",
		"Publish a private provider API, such as a Proxmox cluster on your home network, to a controller over Tailcat.")
	target := fs.String("target", "", "the provider's API as this machine reaches it, e.g. 192.168.1.10:8006 (required)")
	stateDir := fs.String("state-dir", "", "where the gateway keeps its identity, which is its address (default: "+config.StateDir()+")")
	quiet := fs.Bool("quiet", false, "do not print the address; for a service whose log is not the place for a credential")
	logFormat := fs.String("log-format", "text", "log format: text or json")
	fs.example(
		"zoomies gateway --target 192.168.1.10:8006",
		"zoomies gateway --target pve.lan:8006 --state-dir /var/lib/zoomies",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	if _, err := gateway.ParseTarget(*target); err != nil {
		return usagef("gateway", "%v", err)
	}
	dir := *stateDir
	if dir == "" {
		dir = config.StateDir()
	}
	log := logging.Setup(logging.Options{Level: "info", Format: *logFormat})

	g, err := gateway.Start(ctx, gateway.Options{Target: *target, StateDir: dir, Logger: log})
	if err != nil {
		return err
	}
	defer g.Close()

	if *quiet {
		fmt.Fprintf(e.err, "Private connection ready; forwarding to %s. The address is in %s.\n", g.Target(), gateway.StatePath(dir))
	} else {
		// The address is the capability. It is printed once, to the terminal
		// the operator is sitting at, and never logged: a service should
		// run with --quiet, and the state file is where to read it back.
		fmt.Fprintf(e.out, "Private connection ready; forwarding to %s.\n\n", g.Target())
		fmt.Fprintf(e.out, "On the controller, open the provider, choose Private connection (Tailcat), and paste this address:\n\n  %s\n\n", g.Address())
		fmt.Fprintln(e.out, "Anyone holding that address can open connections to the target. Keep it out of screenshots, issues and source control.")
		fmt.Fprintln(e.out, "Leave this running; ctrl-C stops the connection and the controller reports the provider unreachable.")
	}
	<-ctx.Done()
	return ctx.Err()
}

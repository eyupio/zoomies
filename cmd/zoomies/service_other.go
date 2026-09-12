//go:build !windows

package main

import "context"

// runAsService reports that this process is not under a service manager that
// needs answering: systemd and launchd run a plain process and speak to it
// with signals, which main already handles.
func runAsService(context.Context, *env) (int, bool) { return 0, false }

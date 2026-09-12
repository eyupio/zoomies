//go:build windows

package main

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/eyupio/zoomies/internal/installer"
)

// runAsService runs the command under the Windows service control dispatcher
// when the service manager started this process, and reports false otherwise
// so that main runs it as a plain command.
//
// A service that does not connect to the dispatcher is killed with error 1053
// about thirty seconds after it starts, so this is not optional for a process
// sc.exe launches. The command itself is unchanged: the arguments the service
// was registered with are dispatched exactly as a terminal's would be, and a
// Stop request becomes the same context cancellation a SIGTERM is on Linux.
func runAsService(ctx context.Context, e *env) (int, bool) {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return 0, false
	}
	h := &serviceHandler{ctx: ctx, e: e}
	if err := svc.Run(installer.WindowsServiceName(installer.UnitAgent), h); err != nil {
		return exitError, true
	}
	return h.code, true
}

// stopWaitHint is how long the service manager is told a stop may take. An
// agent stopping tidily waits for the jobs it is running, which is minutes,
// and the manager's default patience is thirty seconds.
const stopWaitHint = 10 * time.Minute

type serviceHandler struct {
	ctx  context.Context
	e    *env
	code int
}

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- dispatch(ctx, h.e, os.Args[1:]) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case code := <-done:
			h.code = code
			status <- svc.Status{State: svc.Stopped}
			return false, uint32(code)
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: uint32(stopWaitHint / time.Millisecond)}
				cancel()
				h.code = <-done
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}

package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"time"
)

// How long an operator may ask a CPU profile to run for. A profile is a sample
// of what the process is doing, so a few seconds of a busy controller is
// enough, and a long one would hold the route and the profiler for as long as
// somebody forgot it.
const (
	MinCPUProfileSeconds     = 1
	DefaultCPUProfileSeconds = 10
	MaxCPUProfileSeconds     = 30
)

// ErrCPUProfileBusy is the refusal for a profile asked for while one is being
// taken. The runtime allows one at a time, and a second request would either
// fail inside it or, worse, cut the first one short.
var ErrCPUProfileBusy = errors.New("controller: a CPU profile is already being taken")

// CaptureCPUProfile samples where the controller's CPU goes for d, and returns
// the profile in the format `go tool pprof` reads.
//
// It is for the question the metrics cannot answer. They say which loop and
// which step took the time; a profile says what inside it did, down to the
// function, and each loop's goroutines carry a "loop" label so the profile can
// be read by loop (`go tool pprof -tagfocus=loop=housekeeping`).
//
// It is asked for, not left running: sampling costs a little CPU of its own,
// which is the wrong thing to add to a controller that is already busy, and a
// profile nobody asked for has nobody to read it. Cancelling ctx ends it at
// once, so a client that gives up does not keep the profiler on.
func (c *Controller) CaptureCPUProfile(ctx context.Context, d time.Duration) ([]byte, error) {
	if !c.profiling.CompareAndSwap(false, true) {
		return nil, ErrCPUProfileBusy
	}
	defer c.profiling.Store(false)

	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		// The runtime has one CPU profiler and something else holds it, which a
		// test run with -cpuprofile is the usual case of.
		return nil, fmt.Errorf("starting the CPU profile: %w", err)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	// Stopped before anything else is decided, so that every way out of here
	// has turned the profiler off.
	pprof.StopCPUProfile()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CPUProfileInProgress reports whether a profile is being taken right now.
func (c *Controller) CPUProfileInProgress() bool { return c.profiling.Load() }

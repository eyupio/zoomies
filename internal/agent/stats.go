package agent

import (
	"context"
	"sync"
	"time"
)

// Sampling has its own bounded budget. A busy host must not spend a second per
// container in the same loop that notices exited jobs and releases disk space.
const (
	statsInterval = 30 * time.Second
	statsBudget   = 10 * time.Second
	statsTimeout  = 2 * time.Second
	statsWorkers  = 4
)

func (a *Agent) statsLoop(ctx context.Context) error {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()
	for {
		a.sampleStats(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (a *Agent) sampleStats(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, statsBudget)
	defer cancel()
	jobs := make(chan tracked)
	var workers sync.WaitGroup
	for range statsWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for r := range jobs {
				if ctx.Err() != nil {
					continue
				}
				b, err := a.opts.Backends.Get(r.kind)
				if err != nil {
					continue
				}
				sctx, stop := context.WithTimeout(ctx, statsTimeout)
				stats, err := b.Stats(sctx, r.handle)
				stop()
				if err != nil {
					a.log.Debug("runner stats are unavailable; retaining the last sample",
						"runner", r.runnerID, "error", err)
					continue
				}
				a.mu.Lock()
				if current := a.runners[r.runnerID]; current != nil &&
					current.handle == r.handle && !current.terminal && !current.hostRemoved {
					current.stats = stats
				}
				a.mu.Unlock()
			}
		}()
	}
send:
	for _, r := range a.trackedRunners() {
		if r.terminal || r.hostRemoved || r.handle == "" {
			continue
		}
		select {
		case jobs <- r:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	workers.Wait()
}

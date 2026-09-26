package agent

import (
	"context"
	"time"
)

type logMaintainer interface {
	PruneLogs(context.Context, int64) error
}

func (a *Agent) nativeLogLoop(ctx context.Context) error {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		for _, kind := range a.opts.Backends.Kinds() {
			b, err := a.opts.Backends.Get(kind)
			if err != nil {
				continue
			}
			if m, ok := b.(logMaintainer); ok {
				work, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := m.PruneLogs(work, 10<<20)
				cancel()
				if err != nil {
					a.log.Warn("could not bound native runner logs", "backend", kind, "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

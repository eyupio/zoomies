package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// toolFillTimeout bounds one fill. Several JDKs over a slow link are minutes
// each; a fill still running after this long is stuck, and the next scan
// offers it again.
const toolFillTimeout = 45 * time.Minute

// handleToolFill fills a pool's tool cache on this host and reports what
// became of each request.
func (a *Agent) handleToolFill(ctx context.Context, task Task) {
	a.fill.Lock()
	defer a.fill.Unlock()
	if ctx.Err() != nil {
		a.reportNotStarted(ctx, task)
		return
	}
	kind := task.Backend
	if kind == "" {
		kind = a.opts.DefaultBackend
	}
	b, err := a.opts.Backends.Get(kind)
	if err != nil {
		a.reportToolFill(ctx, task, nil, err)
		return
	}
	filler, ok := b.(backend.ToolCacheFiller)
	if !ok {
		a.reportToolFill(ctx, task, nil, fmt.Errorf("the %s backend keeps no tool cache to fill", kind))
		return
	}
	fctx, cancel := context.WithTimeout(ctx, toolFillTimeout)
	defer cancel()
	started := a.now()
	fills, err := filler.FillToolCache(fctx, *task.Spec, task.Tools)
	a.log.Info("filled a pool's tool cache", "pool", task.Spec.PoolID, "requests", len(task.Tools),
		"answered", len(fills), "took", a.now().Sub(started).Round(time.Second), "error", err)
	a.reportToolFill(ctx, task, fills, err)
}

func (a *Agent) reportToolFill(ctx context.Context, task Task, fills []backend.ToolFill, err error) {
	res := TaskResult{TaskID: task.ID, Kind: task.Kind, OK: err == nil, ToolFills: fills, CompletedAt: a.now()}
	if err != nil {
		res.Error = err.Error()
		res.Fault = backend.Fault(err)
		if res.Fault == "" {
			res.Fault = store.FaultBackend
		}
	}
	a.report(ctx, res)
}

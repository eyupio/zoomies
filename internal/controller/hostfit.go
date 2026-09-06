package controller

import (
	"context"

	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// HostFit is what the pool wizard asks before a pool exists: how many hosts
// could run it as configured, and, when none can, the first host's own account
// of why and the backends those hosts do offer.
//
// Zero is worth saying out loud before a pool is created: a pool whose
// selector matches nothing looks completely healthy and never starts a runner.
// The explanation matters as much as the count, because the usual cause is not
// a missing machine but a daemon the agent on an existing one could not reach,
// and the backends those same hosts do offer are the other way out.
type HostFit struct {
	Count  int
	Detail string
	// Alternatives are the backends offered by the hosts that match this pool
	// in every way except its backend, in the order a pool would move to them.
	// The wizard turns them into the second half of its warning, in the same
	// words the scheduler uses once the pool is real.
	Alternatives []string
}

// HostFit counts the hosts that could run a pool, using the scheduler's own
// placement rule so the wizard can never disagree with the fleet.
func (c *Controller) HostFit(ctx context.Context, p *store.Pool) (HostFit, error) {
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return HostFit{}, err
	}
	now := c.Now()
	fit := HostFit{}
	offered := map[string]int{}
	for _, h := range hosts {
		if !scheduler.HostAvailable(h, now) || !scheduler.HostSelects(h, p) {
			continue
		}
		for _, kind := range h.Backends {
			if kind != string(p.Backend) {
				offered[kind]++
			}
		}
		if !scheduler.HostOffers(h, p) {
			if fit.Detail == "" {
				if info, ok := h.BackendInfo.Find(p.Backend); ok && !info.Available && info.Detail != "" {
					fit.Detail = h.Name + " reports: " + info.Detail
				}
			}
			continue
		}
		fit.Count++
	}
	for _, kind := range []store.BackendKind{store.BackendDocker, store.BackendPodman, store.BackendProcess} {
		if offered[string(kind)] > 0 {
			fit.Alternatives = append(fit.Alternatives, string(kind))
		}
	}
	return fit, nil
}

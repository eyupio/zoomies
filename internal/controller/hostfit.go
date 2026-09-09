package controller

import (
	"context"
	"time"

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
	// PlatformMismatch counts the hosts that fit in every other way but are
	// not the machine the pool asked for. It is called out separately because
	// it is the one mismatch no backend change can fix: the answer is a
	// different host, or a different platform on the pool.
	PlatformMismatch int
	// Selected is how many hosts the pool's host selector reaches at all,
	// whatever became of them afterwards. It is the number the wizard's
	// placement step shows while a selector is being typed, and Count is the
	// number its review step shows -- so when the two differ, something
	// happened between them that an operator has to be told about.
	Selected int
	// Excluded is that difference, host by host: everything the selector
	// reaches and the fleet cannot run, with the reason in the operator's
	// terms. It is what turns "2 hosts match" followed by "1 host can run
	// this pool" from a contradiction into an explanation.
	Excluded []HostExclusion
}

// HostExclusion is one host a pool's selector reaches that could not run it,
// and why. The reason is a sentence about the host with the host's name left
// out, so a caller can put the name where its own layout wants it.
type HostExclusion struct {
	Host string `json:"host"`
	// Code is which of the placement rules turned this host down, for a caller
	// that wants to group or colour them: unavailable, backend, platform, size.
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// The reasons a host the selector reached cannot run the pool.
const (
	ExcludedUnavailable = "unavailable"
	ExcludedBackend     = "backend"
	ExcludedPlatform    = "platform"
	ExcludedSize        = "size"
)

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
		if !scheduler.HostSelects(h, p) {
			continue
		}
		fit.Selected++
		exclude := func(code, reason string) {
			fit.Excluded = append(fit.Excluded, HostExclusion{Host: h.Name, Code: code, Reason: reason})
		}
		// A host that is there and not taking work is still an answer to
		// "where did my other host go", so it is excluded with a reason rather
		// than passed over. The order below is the scheduler's own, so the
		// reason given is the first rule the host actually failed.
		if !scheduler.HostAvailable(h, now) {
			exclude(ExcludedUnavailable, unavailableReason(h, now))
			continue
		}
		for _, kind := range h.Backends {
			if kind != string(p.Backend) {
				offered[kind]++
			}
		}
		if !scheduler.HostOffers(h, p) {
			reason := "its agent does not offer the " + string(p.Backend) + " backend"
			if info, ok := h.BackendInfo.Find(p.Backend); ok && !info.Available && info.Detail != "" {
				// The agent's own probe usually names the fix -- a socket that
				// is not readable, a daemon that is not running -- and the
				// backend's name alone sends an operator looking in the wrong
				// place.
				reason += " -- it reports: " + info.Detail
				if fit.Detail == "" {
					fit.Detail = h.Name + " reports: " + info.Detail
				}
			}
			exclude(ExcludedBackend, reason)
			continue
		}
		if !scheduler.HostIsPlatform(h, p) {
			fit.PlatformMismatch++
			exclude(ExcludedPlatform, platformReason(h, p))
			continue
		}
		// A host that could never hold one runner of this pool cannot run it,
		// and saying so before the pool exists is the whole point of the
		// wizard's count. It goes in the same sentence the backend probe uses,
		// because it has the same shape: a machine that is there and cannot
		// take the work, with the reason attached.
		if short := scheduler.HostShortfall(h, p); short != "" {
			exclude(ExcludedSize, short)
			if fit.Detail == "" {
				fit.Detail = h.Name + " cannot run it: " + short + "."
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

// unavailableReason says which of the three ways a host takes no new runner it
// is in. They read alike on a Hosts page and are three different jobs: wait for
// an agent, uncordon a machine, upgrade a release.
func unavailableReason(h *store.Host, now time.Time) string {
	switch {
	case !h.Healthy(now):
		return "it is not heartbeating, so nothing is placed there until its agent checks in again"
	case h.Cordoned:
		return "it is cordoned, so it takes no new runners until it is uncordoned"
	default:
		return "its agent speaks a protocol this controller does not, so it takes no new runners until it is upgraded"
	}
}

// platformReason names both machines, because the fix is a different host or a
// different pool and an operator cannot choose between them without the pair.
func platformReason(h *store.Host, p *store.Pool) string {
	has := h.Platform().Describe()
	if has == "" {
		has = "an unreported platform"
	}
	return "it is running " + has + ", and this pool asks for " + p.Platform.Describe()
}

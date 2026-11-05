package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
)

// statsEventWindow is the window a `stats` frame summarises. It is the
// window GET /stats defaults to and the one the Overview asks for, so a frame
// arriving over the stream carries the same numbers a fetch would -- a
// different window here would make the tiles jump on every event.
const statsEventWindow = time.Hour

// publishDerived sends what is computed rather than stored: the Overview's
// statistics, the problems list, and any host whose card has moved.
//
// Nothing writes a row when a job ages out of the wait percentiles or a host
// stops sending heartbeats, so nothing in the store can announce those. They
// are worked out here after every reconcile pass and every housekeeping tick,
// and published only when the JSON actually differs from what was last sent.
// Without this the Overview's four numbers and the problems bell were the
// only parts of the product that stayed as they were until the browser was
// reloaded.
//
// It costs a handful of SQLite queries per pass, so each piece is worked out
// only when some subscriber asked for the kind of event it makes: a controller
// nobody is watching should not do the watchers' work, and neither should one
// whose only watcher is following jobs.
func (c *Controller) publishDerived(ctx context.Context) { c.runDerived(ctx, false) }

// publishDerivedPaced is publishDerived for the callers that come round on their
// own, the scheduling pass and the housekeeping tick: it leaves the figures alone
// when they were worked out less than derivedGap ago. What an operator did, a
// Kennel action or an update plan, calls publishDerived and is shown at once,
// because somebody is looking at the page for it.
func (c *Controller) publishDerivedPaced(ctx context.Context) { c.runDerived(ctx, true) }

func (c *Controller) runDerived(ctx context.Context, paced bool) {
	if c.bus == nil || ctx.Err() != nil {
		return
	}
	want := c.derivedWanted()
	if !want.any() {
		// Counted, so that a controller doing this work every pass can be told
		// from one that is skipping it: the pair of this and the subscriber
		// gauge says which of the two a busy instance is.
		c.metrics.derivedSkipped.Inc()
		return
	}

	c.derivedMu.Lock()
	defer c.derivedMu.Unlock()
	// Decided under the lock, so that two paced callers arriving together do not
	// both work the figures out: the second waits, finds the first has just
	// finished and has nothing to add.
	if paced && !c.derivedDue() {
		c.metrics.derivedPaced.Inc()
		return
	}
	// Called from several places, so the whole is timed here and not by each of
	// them, and after the decision, so that a call that left the figures alone is
	// not counted as a run of them. The parts are timed apart because they are
	// very different sizes, and the one that is large on a big fleet is the one
	// worth caching.
	defer c.stepTimer(loopDerived, stepAll)()
	started := c.derivedNow()
	defer func() {
		finished := c.derivedNow()
		c.derivedPace.last, c.derivedPace.took = finished, max(finished.Sub(started), 0)
	}()
	// One set of reads for the figures below, so that the statistics, the
	// problems list, the update status and the host views read the fleet once
	// between them and not once each.
	ctx = withFleetReads(ctx)

	if want.stats {
		done := c.stepTimer(loopDerived, stepStats)
		if stats, err := c.Stats(ctx, statsEventWindow); err != nil {
			c.log.Warn("could not compute fleet statistics for the event stream", "error", err)
		} else if raw, changed := c.derivedChanged(&c.lastStats, stats); changed {
			c.bus.Publish(events.KindStats, "", json.RawMessage(raw))
		}
		done()
	}

	if want.problems {
		done := c.stepTimer(loopDerived, stepProblems)
		if problems, err := c.SharedProblems(ctx); err != nil {
			c.log.Warn("could not gather the current problems for the event stream", "error", err)
		} else if raw, changed := c.derivedChanged(&c.lastProblems, NewProblemsView(problems)); changed {
			c.bus.Publish(events.KindProblems, "", json.RawMessage(raw))
		}
		done()
	}

	// The Overview of Kennel Club is computed, not stored: nothing writes a row
	// when an evaluation grows older, so nothing could announce that it did. With
	// the feature off this costs no query -- the document is the same "off" one
	// each time -- and is sent once when it flips.
	if want.kennel {
		done := c.stepTimer(loopDerived, stepKennel)
		if overview, err := c.KennelOverview(ctx); err != nil {
			c.log.Warn("could not compute the Kennel Club overview for the event stream", "error", err)
		} else if raw, changed := c.derivedChanged(&c.lastKennel, overview); changed {
			c.bus.Publish(events.KindKennelSummary, "", json.RawMessage(raw))
		}
		done()
	}

	// What an update would take is computed too: a soak ends when the clock passes
	// it and nothing is written, so no row could announce that auto may now act.
	// The sentence moves as time does, by the hour and then by the minute near the
	// end of a wait, and each move is sent once.
	//
	// A request and a closed attempt send their own frame through the same
	// memory, so this one says nothing a button has just said.
	if want.updates {
		done := c.stepTimer(loopDerived, stepUpdates)
		if status, err := c.UpdatesView(ctx); err != nil {
			c.log.Warn("could not work out the update status for the event stream", "error", err)
		} else {
			c.sendUpdates(status)
		}
		done()
	}

	// A transfer's preparation is the same again: every count in it moves as
	// runners are confirmed gone and jobs finish, and no row says so.
	if want.transfer {
		done := c.stepTimer(loopDerived, stepTransfer)
		c.sendTransfer(ctx)
		done()
	}

	if want.hosts {
		done := c.stepTimer(loopDerived, stepHosts)
		c.publishHostChanges(ctx)
		done()
	}
	if want.machines {
		done := c.stepTimer(loopDerived, stepMachines)
		c.publishMachineChanges(ctx)
		done()
	}

	// The machine the controller runs on is sampled on every housekeeping tick and
	// nothing writes a row when it is, so nothing in the store could announce it.
	// It is sent when its bytes move, which is each tick while one is being taken.
	if want.controllerMachine {
		done := c.stepTimer(loopDerived, stepControllerMachine)
		if raw, changed := c.derivedChanged(&c.lastMachine, c.ControllerMachine()); changed {
			c.bus.Publish(events.KindControllerMachine, "", json.RawMessage(raw))
		}
		done()
	}
}

// How long the scheduling pass and the housekeeping tick leave the figures alone
// after working them out.
//
// A busy fleet nudges the scheduler every few seconds, and the figures cost a
// tenth of a second to a second to work out, so doing it after every pass spent a
// quarter of the controller's time on frames that said what the last one had. The
// gap is derivedIdleFactor times as long as the work took, which keeps the work to
// a tenth of the controller's time however large the fleet is, and never less than
// derivedMinGap, so that a small fleet's cheap work is still not repeated every
// pass. It is never more than derivedMaxGap, so that a change waits at most about
// as long as the housekeeping tick takes to come round and say so.
const (
	derivedMinGap     = 5 * time.Second
	derivedMaxGap     = 30 * time.Second
	derivedIdleFactor = 9
)

// derivedPacing is what the pacing remembers: the clock it is measured on (the
// controller's, unless a test replaces it, as a benchmark does to move pacing
// time without ageing every host's heartbeat with it), when the last run
// finished, and how long that run took. It is guarded by derivedMu.
type derivedPacing struct {
	now  func() time.Time
	last time.Time
	took time.Duration
}

// derivedGap is how long after a run that took this long the paced callers leave
// the figures alone.
func derivedGap(took time.Duration) time.Duration {
	return min(max(derivedIdleFactor*took, derivedMinGap), derivedMaxGap)
}

func (c *Controller) derivedNow() time.Time {
	if c.derivedPace.now != nil {
		return c.derivedPace.now()
	}
	return c.Now()
}

// derivedDue says whether the paced callers may work the figures out now. Called
// with derivedMu held. A clock that has gone back is no reason to trust a
// moment that is now in its future.
func (c *Controller) derivedDue() bool {
	p := &c.derivedPace
	if p.last.IsZero() {
		return true
	}
	now := c.derivedNow()
	return now.Before(p.last) || now.Sub(p.last) >= derivedGap(p.took)
}

// derivedWant is which of the figures a pass works out for the stream some
// subscriber could be sent. Each is the one kind of event it makes.
type derivedWant struct {
	stats, problems, kennel, updates, transfer, hosts, machines, controllerMachine bool
}

func (w derivedWant) any() bool {
	return w.stats || w.problems || w.kennel || w.updates || w.transfer || w.hosts || w.machines || w.controllerMachine
}

// derivedWanted asks the bus, once per run, which figures anyone could be sent.
// A subscriber that named no kinds, as the web UI's stream does, is owed all of
// them; one that named some is owed those and is not charged for the rest.
func (c *Controller) derivedWanted() derivedWant {
	b := c.bus
	return derivedWant{
		stats:             b.Wants(events.KindStats),
		problems:          b.Wants(events.KindProblems),
		kennel:            b.Wants(events.KindKennelSummary),
		updates:           b.Wants(events.KindUpdates),
		transfer:          b.Wants(events.KindTransfer),
		hosts:             b.Wants(events.KindHostUpdated),
		machines:          b.Wants(events.KindMachineUpdated),
		controllerMachine: b.Wants(events.KindControllerMachine),
	}
}

// publishMachineChanges announces every machine whose rendered view has moved
// since it was last sent. Called with derivedMu held.
//
// It exists for the same reason publishHostChanges does, and for one more. A
// machine spends most of its life waiting -- for a clone, for a guest to boot,
// for an agent to join -- and what moves on the page while it waits is elapsed
// time and the counts around it, neither of which writes a row. Without this
// the one screen an operator watches while a machine is being built would be
// the one screen that did not move.
//
// Machines are in the tens, like hosts, so diffing the rendered view is cheap;
// this is deliberately not how the runners are done.
func (c *Controller) publishMachineChanges(ctx context.Context) {
	if !c.cfg().Provider.Enabled {
		return
	}
	view, err := c.MachineRenderer(ctx)
	if err != nil {
		c.log.Warn("could not read the providers for the event stream", "error", err)
		return
	}
	machines, _, err := c.st.ListMachines(ctx, store.MachineFilter{}, store.Page{Limit: 500})
	if err != nil {
		c.log.Warn("could not read the machines for the event stream", "error", err)
		return
	}
	if c.lastMachines == nil {
		c.lastMachines = make(map[string][]byte, len(machines))
	}
	live := make(map[string]struct{}, len(machines))
	for _, m := range machines {
		live[m.ID] = struct{}{}
		last := c.lastMachines[m.ID]
		raw, changed := c.derivedChanged(&last, view.View(m))
		if !changed {
			continue
		}
		c.lastMachines[m.ID] = last
		c.bus.Publish(events.KindMachineUpdated, "machine:"+m.ID, json.RawMessage(raw))
	}
	for id := range c.lastMachines {
		if _, ok := live[id]; !ok {
			delete(c.lastMachines, id)
		}
	}
}

// publishHostChanges announces every host whose rendered view has moved since
// it was last sent. Called with derivedMu held.
//
// The two numbers an operator actually reads off the Hosts page are the two
// nothing announces. `active_runners` is counted from the runners table when a
// host is read, so a runner starting moves the "4 of 6 slots in use" bar with
// no host row written and nothing to publish; and the heartbeat behind "last
// heartbeat 12s ago" is a bare UPDATE of one column that no publisher watches.
// Both sat still until the browser reconciled, which is why the page felt dead
// while the fleet moved underneath it -- the operator was looking at the one
// screen in the product that was not live.
//
// Diffing the rendered view is what stops that becoming a frame per host per
// pass: a host nobody has touched marshals to the same bytes and says nothing,
// and a host that is merely alive says something once per heartbeat rather
// than once per pass. The cost is bounded by how many hosts a fleet has, which
// is tens -- this is deliberately not how the runners are done, because there
// are thousands of those and the same idea would be too expensive.
func (c *Controller) publishHostChanges(ctx context.Context) {
	hosts, err := c.fleetHosts(ctx)
	if err != nil {
		c.log.Warn("could not read the hosts for the event stream", "error", err)
		return
	}
	if c.lastHosts == nil {
		c.lastHosts = make(map[string][]byte, len(hosts))
	}
	live := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		live[h.ID] = struct{}{}
		last := c.lastHosts[h.ID]
		raw, changed := c.derivedChanged(&last, c.HostView(h).For(true))
		if !changed {
			continue
		}
		c.lastHosts[h.ID] = last
		c.bus.Publish(events.KindHostUpdated, "host:"+h.ID, json.RawMessage(raw))
	}
	// A host that has gone announced itself on the way out. Forgetting it here
	// only keeps the map from growing for the life of the process.
	for id := range c.lastHosts {
		if _, ok := live[id]; !ok {
			delete(c.lastHosts, id)
		}
	}
}

// derivedChanged marshals v, records it as the last published form, and says
// whether it differs from the one before. Comparing the bytes rather than the
// values means a new field is compared the day it is added.
func (c *Controller) derivedChanged(last *[]byte, v any) ([]byte, bool) {
	raw, err := json.Marshal(v)
	if err != nil {
		c.log.Error("could not marshal a payload for the event stream", "error", err)
		return nil, false
	}
	if bytes.Equal(raw, *last) {
		return raw, false
	}
	*last = raw
	return raw, true
}

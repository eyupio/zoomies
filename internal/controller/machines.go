package controller

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
)

// The machine loop is the half of the fleet that spends money.
//
// Everything here follows one rule, and it is worth stating before any of the
// code: every arrow between two machine states is an OBSERVATION, never an
// intent. A create whose answer never arrived is resolved by asking the
// provider about the identity the row already carries, never by creating a
// second machine; a delete is complete when an inspect cannot find the
// resource, never when the delete call returned 200. A controller that guessed
// in either direction would leave somebody paying for a machine nothing in the
// fleet knows about, which is the failure this whole design is arranged around.
//
// The loop is its own spawn rather than a step of Reconcile. reconcileMu is
// held for a whole scheduling pass and a clone takes minutes, so a machine step
// inside it would stop the fleet placing runners for as long as a hypervisor
// felt like taking -- the argument that already moved capacity-demand delivery
// out of the pass.

const (
	// machineFirstPass is how long after start the first machine pass runs.
	// Late enough that agents have had a chance to say they are back -- a pass
	// that ran the instant the process came up would see an empty fleet and
	// buy machines for work the hosts it has were about to take.
	machineFirstPass = 15 * time.Second
	// machineRetryBase and machineRetryMax bound one machine's own backoff.
	machineRetryBase = 20 * time.Second
	machineRetryMax  = 15 * time.Minute
	// machineMaxAttempts is how many times a step is tried before the machine
	// is failed and left visible. Failing it is not tidying up: the row stays,
	// with its reason, because the resource behind it may still exist.
	machineMaxAttempts = 5
	// observationMaxAge is how old an observation may be and still be evidence
	// for a delete. A minute: long enough that a sweep and a delete in the same
	// pass agree, short enough that nothing acts on what was true at breakfast.
	observationMaxAge = 60 * time.Second
	// machineSweepBatch bounds one recovery pass, so a machine stuck behind an
	// unreachable node cannot starve the rest.
	machineSweepBatch = 100
	// machineClaimMargin is how much longer a claim lasts than the call it
	// covers, so that a claim never expires underneath the request it is there
	// to make exclusive.
	machineClaimMargin = 30 * time.Second
	// breakerThreshold is how many machines may fail before ready in a row
	// before the provider is stood down. Three is enough to tell a bad template
	// from an unlucky one, and few enough that the fourth is not paid for.
	breakerThreshold = 3
	breakerBase      = time.Minute
	breakerMax       = 15 * time.Minute
)

// machineRuntime is everything the machine loop owns.
type machineRuntime struct {
	// mu makes a machine pass mutually exclusive with itself, exactly as
	// reconcileMu does for the scheduling pass. It is a separate mutex because
	// the two passes have nothing to say to each other and a machine step must
	// never be able to hold up a scheduling one.
	mu sync.Mutex
	// calls tracks the provider calls a pass issued, which outlive the pass:
	// Stop waits on it beside deliveries, because exiting under a create
	// leaves a machine whose outcome nobody recorded.
	calls  sync.WaitGroup
	nudges chan struct{}
	// cursor is the recovery sweep's keyset position, owned by the pass that
	// mu serialises.
	cursor string

	cacheMu sync.Mutex
	cache   map[string]*builtProvider

	// noteMu guards what the problems drawer reads about each provider: the
	// last failure and the untracked resources the last sweep found. Both are
	// in memory, like blocked and runnerGroups, because both are re-learnt by
	// the next pass -- and neither is a fact about the fleet worth a row.
	noteMu  sync.Mutex
	trouble map[string]providerTrouble
	orphans map[string][]string
}

// providerTrouble is the last failure a provider gave us, kept for the
// sentence the problems drawer shows.
type providerTrouble struct {
	Kind    provider.FailureKind
	Detail  string
	Remedy  string
	At      time.Time
	Machine string
}

// builtProvider is one configuration row's live client, cached until the row
// changes. updated_at is the whole cache key beyond the id: an operator who
// rotates a credential expects the next pass to use it, and nothing else in
// the row moves without moving that column.
type builtProvider struct {
	p         provider.Provider
	updatedAt time.Time
}

func newMachineRuntime() *machineRuntime {
	return &machineRuntime{
		nudges:  make(chan struct{}, 1),
		cache:   map[string]*builtProvider{},
		trouble: map[string]providerTrouble{},
		orphans: map[string][]string{},
	}
}

// NudgeMachines wakes the machine loop immediately. Like Nudge it never blocks
// and never queues: the channel holds one token, so a burst of changes costs
// one pass.
func (c *Controller) NudgeMachines() {
	select {
	case c.machines.nudges <- struct{}{}:
	default:
	}
}

// machineLoop runs a pass on the configured interval, on every nudge, and
// whenever the settings change.
func (c *Controller) machineLoop(ctx context.Context) {
	timer := time.NewTimer(machineFirstPass)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-c.machines.nudges:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-c.settingsChanged:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if err := c.ReconcileMachines(ctx); err != nil && ctx.Err() == nil {
			// Logged rather than propagated: a transient database or provider
			// error must not stop the loop, and the machines it did not reach
			// this pass are reached by the next one.
			c.log.Error("a machine pass failed", "error", err)
		}
		timer.Reset(c.machineInterval())
	}
}

// machineInterval is how often machines are reconciled, with a floor so that a
// misconfigured zero does not spin the loop.
func (c *Controller) machineInterval() time.Duration {
	if d := c.cfg().Provider.Interval; d > 0 {
		return d
	}
	return 30 * time.Second
}

// ReconcileMachines runs exactly one machine pass. It is exported so that
// tests and the API can force a pass and know it has finished, which a nudge
// deliberately cannot promise.
//
// The order is the design. Recovery runs before demand on every pass, so a
// restart is simply the pass where nothing was in memory and there is no
// separate startup path to get wrong. Ownership is swept before anything is
// decided, so a resource that vanished under us is known about before the
// fleet works out whether to buy another. And nothing is created until every
// machine already on the way has been counted.
func (c *Controller) ReconcileMachines(ctx context.Context) error {
	c.machines.mu.Lock()
	defer c.machines.mu.Unlock()

	// Two controllers each cloning a VM is the expensive failure this design
	// exists for. Nothing else in this codebase stops on lease loss, because
	// nothing else spends money: a duplicate runner is a wasted slot and a
	// duplicate machine is an invoice. The problems drawer already says so
	// permanently, so this says nothing more.
	if c.leaseLost.Load() != nil {
		return nil
	}

	providers, err := c.st.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("listing providers: %w", err)
	}
	if len(providers) == 0 {
		return nil
	}

	env, err := c.machineSnapshot(ctx, providers)
	if err != nil {
		return err
	}

	c.recoverMachineOperations(ctx, env)
	c.sweepProviders(ctx, env)
	c.applyMachinePlan(ctx, env, DecideMachines(env.demand()))
	c.stepMachines(ctx, env)
	c.publishDerived(ctx)
	return nil
}

// machineEnv is one pass's view of the world, gathered once.
type machineEnv struct {
	now    time.Time
	holder string
	cfg    *config.Config
	// snap and plan are the scheduler's own snapshot and decision over the
	// same rows. The machine demand calculation reads them rather than forming
	// a second opinion about which pool wants what.
	snap  scheduler.Snapshot
	plan  scheduler.Plan
	rows  []*store.Provider
	byID  map[string]*store.Provider
	list  []*store.Machine
	hosts map[string]*store.Host
	// live is how many runners each host is actually running, which is what
	// says whether a machine may be drained and whether a drain has finished.
	live map[string]int
}

func (e *machineEnv) provider(id string) *store.Provider { return e.byID[id] }

// demand renders the snapshot the pure calculation reads.
func (e *machineEnv) demand() MachineSnapshot {
	return MachineSnapshot{
		Now:       e.now,
		Plan:      e.plan,
		Pools:     e.snap.Pools,
		Hosts:     e.snap.Hosts,
		Runners:   e.snap.Runners,
		Jobs:      e.snap.Jobs,
		Machines:  e.list,
		Providers: e.rows,
		Limits: MachineLimits{
			MaxMachines:        e.cfg.Provider.MaxMachines,
			MaxCreatesInFlight: e.cfg.Provider.MaxCreatesInFlight,
			IdleTimeout:        e.cfg.Provider.IdleTimeout,
			ScaleDownCooldown:  e.cfg.Provider.ScaleDownCooldown,
		},
	}
}

// machineSnapshot reads everything one pass decides on.
func (c *Controller) machineSnapshot(ctx context.Context, providers []*store.Provider) (*machineEnv, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	cfg := c.cfg()
	// A machine takes minutes to arrive, so it starts before the delay that
	// exists to damp runner churn has expired: the delay has already been paid
	// by the hypervisor being slow.
	snap.Policy.ScaleUpDelay = cfg.Provider.ScaleUpDelay

	env := &machineEnv{
		now:    c.Now(),
		holder: c.controllerID(),
		cfg:    cfg,
		snap:   snap,
		plan:   scheduler.Decide(snap),
		rows:   providers,
		byID:   make(map[string]*store.Provider, len(providers)),
		hosts:  make(map[string]*store.Host, len(snap.Hosts)),
		live:   map[string]int{},
	}
	for _, p := range providers {
		env.byID[p.ID] = p
		ms, err := c.st.ListMachinesForProvider(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("listing machines for provider %s: %w", p.Name, err)
		}
		env.list = append(env.list, ms...)
	}
	for _, h := range snap.Hosts {
		env.hosts[h.ID] = h
	}
	for _, rs := range snap.Runners {
		for _, r := range rs {
			if r != nil && r.State.Live() {
				env.live[r.HostID]++
			}
		}
	}
	return env, nil
}

// controllerID is what this controller stamps on the resources it creates and
// claims operations under. The lease holder where there is one, because that is
// the identity another controller would see; a fixed local name otherwise, for
// the embedded and test controllers that hold no lease.
func (c *Controller) controllerID() string {
	if c.lease != nil && c.lease.Holder != "" {
		return c.lease.Holder
	}
	return "ctl_local"
}

// ---------------------------------------------------------------------------
// The guards -- the kill switch, in three layers
// ---------------------------------------------------------------------------

// mutationsHeld says why no provider may be changed at all right now, or ""
// when one may.
//
// Only the recovery fence answers here, and it is deliberately stricter than
// the operator's kill switch: it blocks deletes as well as creates. A fenced
// database may be a RESTORED COPY, so its machine rows may describe resources
// a different, live controller owns -- and deleting somebody else's running
// machine on the strength of a restored row is the worst thing this system
// could do. Inspecting, listing, polling an operation and recording what was
// seen all continue, because none of them changes anything.
func (c *Controller) mutationsHeld() string {
	if f := c.Fenced(); f.Fenced {
		return "this fleet is fenced for recovery, so nothing is created or deleted at any provider: " +
			"a restored database's machines may belong to a controller that is still running"
	}
	return ""
}

// provisioningHeld says why no new machine may be created right now, or "" when
// one may.
//
// Drains, deletes, recovery and sweeps never consult it: draining and removing
// a machine ask the provider for nothing new, so a held provider still scales
// down -- holdWhileRateLimited's asymmetry, for the same reason. A kill switch
// that also stopped those would strand running machines nobody is watching.
func (c *Controller) provisioningHeld(p *store.Provider, now time.Time) string {
	if held := c.mutationsHeld(); held != "" {
		return held
	}
	if c.cfg().Provider.Paused {
		return "provider.paused is set, so no new machine is created anywhere; " +
			"draining, deleting and recovery continue"
	}
	if p == nil {
		return ""
	}
	switch {
	case p.Paused:
		reason := p.PausedReason
		if reason == "" {
			reason = "an operator paused it"
		}
		return fmt.Sprintf("%s is paused: %s", p.Name, reason)
	case !p.Enabled:
		return fmt.Sprintf("%s is disabled, so it is offered no new demand", p.Name)
	case p.PausedUntil != nil && now.Before(*p.PausedUntil):
		return fmt.Sprintf("%s is standing down after %s in a row; trying again in %s",
			p.Name, plural(p.ConsecutiveFailures, "failure"), roundDuration(p.PausedUntil.Sub(now)))
	}
	return ""
}

// ---------------------------------------------------------------------------
// Building providers
// ---------------------------------------------------------------------------

// machineProvider is one configuration row and the client built from it.
type machineProvider struct {
	row *store.Provider
	p   provider.Provider
}

// providerFor builds the client for one row, cached until the row changes.
//
// The credential is unsealed here and handed to the factory for the life of
// the client it builds. It is never written anywhere else and has no field in
// anything that reaches a guest: the type MachineSpec has nowhere to put one.
func (c *Controller) providerFor(ctx context.Context, row *store.Provider) (*machineProvider, error) {
	c.machines.cacheMu.Lock()
	if got, ok := c.machines.cache[row.ID]; ok && got.updatedAt.Equal(row.UpdatedAt) {
		c.machines.cacheMu.Unlock()
		return &machineProvider{row: row, p: got.p}, nil
	}
	c.machines.cacheMu.Unlock()

	credential, err := c.unsealString(row.CredentialsEnc, "provider "+row.Name+"'s credential")
	if err != nil {
		return nil, err
	}
	p, err := c.providers.New(ctx, row.Kind, provider.Config{
		ProviderID: row.ID,
		Owner:      provider.Owner{ControllerID: c.controllerID(), ProviderID: row.ID},
		Endpoint:   row.Endpoint,
		Settings:   row.Settings,
		Credential: credential,
		CAPEM:      row.CAPEM,
		Insecure:   row.InsecureSkipVerify,
		Deadlines:  c.providerDeadlines(),
		HTTPClient: c.httpClient,
		Logger:     c.log.With("provider", row.Name),
	})
	if err != nil {
		return nil, err
	}
	c.machines.cacheMu.Lock()
	c.machines.cache[row.ID] = &builtProvider{p: p, updatedAt: row.UpdatedAt}
	c.machines.cacheMu.Unlock()
	return &machineProvider{row: row, p: p}, nil
}

// providerDeadlines is the operator's half of the budget. A provider may ask
// for longer than these and never for less supervision.
func (c *Controller) providerDeadlines() provider.Deadlines {
	cfg := c.cfg().Provider
	return provider.Deadlines{
		Call:      cfg.CallTimeout,
		Allocate:  cfg.CallTimeout,
		Create:    cfg.CreateTimeout,
		Bootstrap: cfg.BootstrapTimeout,
		Delete:    cfg.DeleteTimeout,
	}
}

// stepTimeout is how long one step's requests get. It is not how long the work
// takes: a create is asynchronous, and the deadline that bounds the whole clone
// is provider.create_timeout, checked against the row rather than against a
// context. Two call timeouts, because the longest step makes two requests.
func (c *Controller) stepTimeout(pr *machineProvider) time.Duration {
	d := c.cfg().Provider.CallTimeout
	if d <= 0 {
		d = 30 * time.Second
	}
	if ask := pr.p.Capabilities().Deadlines.Call; ask > d {
		d = ask
	}
	return 2 * d
}

// ---------------------------------------------------------------------------
// Applying the plan
// ---------------------------------------------------------------------------

// applyMachinePlan writes this pass's reservations and starts this pass's
// drains. A failure on one provider never abandons the rest.
func (c *Controller) applyMachinePlan(ctx context.Context, env *machineEnv, plan MachinePlan) {
	for _, pp := range plan.Providers {
		row := env.provider(pp.ProviderID)
		if row == nil {
			continue
		}
		if pp.Create > 0 {
			if held := c.provisioningHeld(row, env.now); held != "" {
				c.log.Debug("machines were wanted and nothing may be bought", "provider", row.Name,
					"wanted", pp.Create, "held", held)
			} else {
				c.reserveMachines(ctx, env, row, pp)
			}
		}
		for _, id := range pp.Drain {
			c.beginDrain(ctx, env, id)
		}
	}
}

// reserveMachines writes the planned rows for one provider's purchase.
//
// The row is written before the credential is minted and before the provider
// is called, so that a failure has somewhere to be recorded -- the createRunner
// ordering, for the same reason. Deleting the row instead would leave a
// provider silently one machine short with nothing to explain it and, worse
// here, a machine nobody has a record of.
func (c *Controller) reserveMachines(ctx context.Context, env *machineEnv, row *store.Provider, pp ProviderPlan) {
	expires := env.now.Add(c.cfg().Provider.CreateTimeout)
	for range pp.Create {
		m := &store.Machine{
			ProviderID:           row.ID,
			State:                store.MachinePlanned,
			Message:              pp.Reason,
			Capacity:             row.MachineCapacity,
			Labels:               row.MachineLabels,
			ReservationExpiresAt: &expires,
		}
		if err := c.st.CreateMachine(ctx, m); err != nil {
			c.log.Error("could not reserve a machine", "provider", row.Name, "error", err)
			return
		}
		env.list = append(env.list, m)
		c.publishMachine(ctx, m)
		c.log.Info("a machine was reserved", "machine", m.ID, "name", m.Name,
			"provider", row.Name, "reason", pp.Reason)
	}
}

// beginDrain cordons a machine's host and moves the machine to draining, which
// is reversible right up until the delete starts.
func (c *Controller) beginDrain(ctx context.Context, env *machineEnv, id string) {
	m := env.machine(id)
	if m == nil || m.State != store.MachineReady {
		return
	}
	if m.HostID != "" {
		if err := c.st.SetHostCordoned(ctx, m.HostID, true); err != nil {
			c.log.Warn("could not cordon a draining machine's host", "machine", id, "host", m.HostID, "error", err)
			return
		}
		if h, err := c.st.GetHost(ctx, m.HostID); err == nil {
			c.publishHost(h)
		}
	}
	c.transitionMachine(ctx, env, m, store.MachineDraining,
		"this machine's pools have had no queued work for long enough that it is no longer worth paying for")
}

func (e *machineEnv) machine(id string) *store.Machine {
	for _, m := range e.list {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// transitionMachine moves a machine and publishes the result, keeping the
// pass's own copy of the row in step so that a later step in the same pass is
// not deciding from a state that has already moved.
func (c *Controller) transitionMachine(ctx context.Context, env *machineEnv, m *store.Machine, to store.MachineState, message string) {
	out, err := c.st.TransitionMachine(ctx, m.ID, to, message)
	if err != nil {
		// Two passes racing over one machine is normal rather than an error,
		// and so is a machine an operator moved underneath this pass.
		if errors.Is(err, store.ErrInvalidTransition) || errors.Is(err, store.ErrNotFound) {
			c.log.Debug("a machine could not be moved", "machine", m.ID, "to", to, "error", err)
			return
		}
		c.log.Error("could not move a machine", "machine", m.ID, "to", to, "error", err)
		return
	}
	*m = *out
	c.publishMachine(ctx, m)
}

// ---------------------------------------------------------------------------
// Stepping
// ---------------------------------------------------------------------------

// stepMachines advances every machine by at most one step, so a pass is
// bounded and a restart never finds a half-applied multi-step sequence.
//
// The steps that talk to a provider run in their own goroutine on a detached,
// bounded context: a hypervisor that accepts a connection and never answers
// must hold up one machine rather than the pass, and the answer must still be
// recorded if the process is asked to stop while the request is out. Stop waits
// for them.
func (c *Controller) stepMachines(ctx context.Context, env *machineEnv) {
	for _, m := range env.list {
		row := env.provider(m.ProviderID)
		if row == nil {
			continue
		}
		switch m.State {
		case store.MachineDeleted, store.MachineQuarantined, store.MachineFailed:
			// A quarantined machine moves when a person moves it, and never
			// otherwise. A failed one keeps whatever resource it has until a
			// sweep or an operator says what to do with it.
			continue
		}
		if m.NextAttemptAt != nil && env.now.Before(*m.NextAttemptAt) {
			continue
		}

		// The states that are answered by looking at our own rows need no
		// provider call, no claim and no goroutine.
		switch m.State {
		case store.MachineEnrolling:
			c.stepEnrolling(ctx, env, m)
			continue
		case store.MachineReady:
			c.stepReady(ctx, env, m)
			continue
		case store.MachineDraining:
			c.stepDraining(ctx, env, m)
			continue
		}

		pr, err := c.providerFor(ctx, row)
		if err != nil {
			c.noteProviderTrouble(row.ID, err, m.ID, env.now)
			continue
		}
		kind := machineOperationFor(m.State)
		if kind == store.MachineOpNone {
			continue
		}
		timeout := c.stepTimeout(pr)
		// The claim outlasts the call it covers. Held for exactly the call's
		// own deadline, it would expire at the instant the call gives up --
		// while this pass is still classifying the failure and writing the
		// outcome down -- and another pass arriving in that window would take
		// the machine and act on a row that is about to be rewritten.
		claimed, err := c.st.ClaimMachineOperation(ctx, m.ID, store.MachineOperation{
			Kind: kind, Holder: env.holder, Timeout: timeout + machineClaimMargin,
		}, env.now)
		if err != nil {
			if !errors.Is(err, store.ErrMachineBusy) {
				c.log.Warn("could not claim a machine's next step", "machine", m.ID, "error", err)
			}
			continue
		}
		*m = *claimed

		c.machines.calls.Add(1)
		go func(m *store.Machine) {
			defer c.machines.calls.Done()
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()
			c.runMachineStep(cctx, env, pr, m)
		}(claimed)
	}
}

// machineOperationFor is the one operation a machine in this state can be
// doing. It is the claim's kind, so that a machine stuck mid-step says which
// step it is stuck in.
func machineOperationFor(s store.MachineState) store.MachineOpKind {
	switch s {
	case store.MachinePlanned, store.MachineCreating:
		return store.MachineOpCreate
	case store.MachineStarting:
		return store.MachineOpStart
	case store.MachineBootstrapping:
		return store.MachineOpBootstrap
	case store.MachineDeleting:
		return store.MachineOpDelete
	}
	return store.MachineOpNone
}

// runMachineStep makes one machine's provider call and records what it learnt.
func (c *Controller) runMachineStep(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) {
	opID := m.OpID
	started := time.Now()

	var err error
	switch m.State {
	case store.MachinePlanned:
		err = c.stepPlanned(ctx, env, pr, m)
	case store.MachineCreating:
		err = c.stepCreating(ctx, env, pr, m)
	case store.MachineStarting:
		err = c.stepStarting(ctx, env, pr, m)
	case store.MachineBootstrapping:
		err = c.stepBootstrapping(ctx, env, pr, m)
	case store.MachineDeleting:
		err = c.stepDeleting(ctx, env, pr, m)
	}
	c.metrics.providerOperations.WithLabelValues(string(m.OpKind), operationOutcome(err)).Inc()
	c.metrics.providerOperationSeconds.WithLabelValues(string(m.OpKind)).Observe(time.Since(started).Seconds())

	switch {
	case errors.Is(err, errMachineHeld):
		// A guard refused after the claim was taken. Released rather than
		// failed: nothing about the machine went wrong, and counting it would
		// walk the backoff up towards giving up on a machine nobody has tried.
		c.finishMachineOperation(ctx, m, opID, store.MachineOpReleased, "", nil)
	case errors.Is(err, errMachineWaiting):
		c.finishMachineOperation(ctx, m, opID, store.MachineOpReleased, "", nil)
	case errors.Is(err, errMachineUnknownOutcome):
		// Deliberately not finished. The operation is not over -- nobody knows
		// whether it happened -- and the claim expiring on its own is what lets
		// the next pass ASK rather than act. Finishing here would clear the one
		// flag that says the answer was never heard.
	case err != nil:
		c.recordMachineFailure(ctx, env, pr, m, opID, err)
	default:
		c.finishMachineOperation(ctx, m, opID, store.MachineOpSucceeded, "", nil)
		c.noteProviderSuccess(ctx, pr.row)
	}
}

// The three outcomes that are not failures, as sentinels so that each step can
// simply return one.
var (
	// errMachineHeld is a guard that refused after the claim was taken.
	errMachineHeld = errors.New("this machine may not be changed right now")
	// errMachineWaiting is an operation that has not finished yet, which is
	// the normal answer to most polls.
	errMachineWaiting = errors.New("this machine's operation has not finished")
	// errMachineUnknownOutcome is the one that matters: the request went out
	// and we never heard. It is never retried, only looked into.
	errMachineUnknownOutcome = errors.New("this machine's operation ended without an answer")
)

// operationOutcome is the metric label for how a call went. The categories are
// the ones an operator would separate: it worked, it was refused for capacity,
// we could not reach the provider, it refused, or we never found out.
func operationOutcome(err error) string {
	switch {
	case err == nil, errors.Is(err, errMachineWaiting), errors.Is(err, errMachineHeld):
		return "ok"
	case errors.Is(err, errMachineUnknownOutcome):
		return "ambiguous"
	}
	switch provider.KindOf(err) {
	case provider.FailureQuota:
		return "quota"
	case provider.FailureUnreachable:
		return "unreachable"
	}
	return "refused"
}

func (c *Controller) finishMachineOperation(ctx context.Context, m *store.Machine, opID string, out store.MachineOpOutcome, message string, next *time.Time) {
	if err := c.st.FinishMachineOperation(ctx, m.ID, opID, out, message, next); err != nil {
		c.log.Warn("could not record a machine operation's outcome", "machine", m.ID, "error", err)
	}
	c.publishMachineByID(ctx, m.ID)
}

// publishMachineByID re-reads a machine and publishes it, for the changes that
// are column writes rather than transitions.
func (c *Controller) publishMachineByID(ctx context.Context, id string) {
	m, err := c.st.GetMachine(ctx, id)
	if err != nil {
		return
	}
	c.publishMachine(ctx, m)
}

// recordMachineFailure settles one failed step: the provider's own sentence on
// the row, the backoff, and the provider's breaker.
//
// A failure nothing but a person can fix -- a wrong credential, a missing
// privilege, a setting -- goes straight to failed with the provider's words.
// There is nothing to wait for, and retrying it five times only delays the
// moment somebody reads the reason.
func (c *Controller) recordMachineFailure(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine, opID string, err error) {
	kind := provider.KindOf(err)
	detail := err.Error()
	now := c.Now()

	terminal := !provider.Retryable(err) || m.Attempts+1 >= machineMaxAttempts
	next := now.Add(c.machineBackoff(m.Attempts + 1))
	if at, ok := provider.RetryAfter(err, now); ok {
		// The provider named a time, and its own rate limiter knows better
		// than our backoff. A time already in the past is no answer, which
		// RetryAfter has already refused.
		next = at
	}
	c.finishMachineOperation(ctx, m, opID, store.MachineOpFailed, detail, &next)
	c.noteProviderTrouble(pr.row.ID, err, m.ID, now)
	c.noteProviderFailure(ctx, pr.row, m)

	if terminal {
		c.transitionMachine(ctx, env, m, store.MachineFailed, detail)
		c.log.Warn("a machine was given up on", "machine", m.ID, "name", m.Name,
			"provider", pr.row.Name, "kind", kind, "error", detail)
		return
	}
	c.log.Warn("a machine's step failed and will be tried again", "machine", m.ID,
		"provider", pr.row.Name, "kind", kind, "attempts", m.Attempts+1, "next", next, "error", detail)
}

// machineBackoff is how long to wait before attempt n+1.
//
// Jittered down by up to a quarter, and the number is drawn here rather than
// in the decision function for the same reason poolJitter is: a decision has to
// be reproducible from the snapshot that produced it, and one that reached for
// rand inside could not be put in front of a test.
func (c *Controller) machineBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	wait := machineRetryBase
	for range attempts - 1 {
		wait *= 2
		if wait >= machineRetryMax {
			wait = machineRetryMax
			break
		}
	}
	return wait - time.Duration(rand.Float64()*float64(wait)/4)
}

// ---------------------------------------------------------------------------
// The steps
// ---------------------------------------------------------------------------

// stepPlanned takes a machine from a reservation to a machine being built.
//
// The ordering is everything: the identity is allocated, written to the row,
// and only then is anything created. A create whose answer is lost is therefore
// answered by asking about an identity we already hold, rather than by creating
// a second machine.
func (c *Controller) stepPlanned(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	if held := c.provisioningHeld(pr.row, c.Now()); held != "" {
		return fmt.Errorf("%w: %s", errMachineHeld, held)
	}
	owner := c.machineOwner(pr.row, m)
	spec, err := c.machineSpec(ctx, env, pr, m, owner)
	if err != nil {
		return err
	}
	ref, err := pr.p.Allocate(ctx, spec)
	if err != nil {
		return err
	}
	if ref.ID == "" && ref.Name == "" {
		return fmt.Errorf("%s allocated nothing to identify a machine by; a provider must return an id, a name, or both", pr.row.Name)
	}
	if ref.Name == "" {
		ref.Name = m.Name
	}
	// The identity, before anything exists to lose. SetMachineResource refuses
	// to move one already set, so a pass that lost a race leaves the first
	// identity standing rather than pointing the row away from a live machine.
	id := ref.ID
	if id == "" {
		id = ref.Name
	}
	if err := c.st.SetMachineResource(ctx, m.ID, ref.Zone, id, owner.Fingerprint, owner.ControllerID); err != nil {
		return err
	}
	c.transitionMachine(ctx, env, m, store.MachineCreating,
		fmt.Sprintf("building %s at %s", ref.Name, pr.row.Name))

	op, err := pr.p.Create(ctx, ref, spec)
	if err != nil {
		return c.noteAmbiguity(ctx, m, err)
	}
	if !op.Zero() {
		if err := c.st.SetMachineOperationHandle(ctx, m.ID, m.OpID, op.Handle); err != nil {
			c.log.Warn("could not record a create's handle", "machine", m.ID, "error", err)
		}
	}
	return nil
}

// noteAmbiguity records a call whose outcome was never heard, and turns
// anything else straight back into a failure.
//
// It leaves the claim in place: the operation is not finished, nobody knows
// whether it happened, and the one thing that must not follow is a retry that
// treats silence as failure and creates a second machine.
func (c *Controller) noteAmbiguity(ctx context.Context, m *store.Machine, err error) error {
	if provider.KindOf(err) != provider.FailureAmbiguous {
		return err
	}
	if mErr := c.st.MarkMachineOutcomeUnknown(ctx, m.ID, m.OpID, err.Error()); mErr != nil {
		c.log.Warn("could not record that an operation's outcome is unknown", "machine", m.ID, "error", mErr)
	}
	c.publishMachineByID(ctx, m.ID)
	c.log.Warn("a machine operation ended without an answer; it will be resolved by looking, never by trying again",
		"machine", m.ID, "operation", m.OpKind, "error", err)
	return fmt.Errorf("%w: %s", errMachineUnknownOutcome, err)
}

// stepCreating follows a create to its end, or works out what became of one
// whose answer was never heard.
func (c *Controller) stepCreating(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	if m.OpHandle != "" && !m.OpOutcomeUnknown {
		status, err := pr.p.Operation(ctx, provider.OperationRef{Kind: provider.OpCreate, Handle: m.OpHandle})
		switch {
		case errors.Is(err, provider.ErrNotFound):
			// The provider has forgotten the task -- a restart at its end, or
			// a task log that has rolled over. What became of the machine is
			// still a question for Inspect.
		case err != nil:
			return err
		case !status.Done:
			return errMachineWaiting
		case !status.OK:
			return &provider.Error{Kind: provider.FailureRefused, Op: "create", Ref: m.Name,
				Message: status.Detail, Remedy: "the provider's own task log carries the rest of it"}
		}
	}
	return c.resolveByInspection(ctx, env, pr, m)
}

// resolveByInspection is the whole of ambiguity recovery: ask what exists, and
// let the answer decide.
//
// It never creates on a guess. A machine that cannot be found is looked for
// twice, a call timeout apart, before the identity is reused -- and past the
// ambiguity timeout it is quarantined rather than retried, because at that
// point the honest answer is that nobody knows and a person should look.
func (c *Controller) resolveByInspection(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	ref := machineRef(m)
	got, err := pr.p.Inspect(ctx, ref)
	switch {
	case err == nil:
	case errors.Is(err, provider.ErrNotFound):
		return c.resolveMissing(ctx, env, pr, m)
	default:
		return err
	}

	if why := c.ownershipComplaint(m, got); why != "" {
		return c.quarantineMachine(ctx, env, m, why)
	}
	if got.Locked != "" {
		// Something else holds the resource. A reason to wait, not a failure,
		// and never a reason to create a second machine.
		return errMachineWaiting
	}
	if err := c.st.SetMachineOwnershipVerified(ctx, m.ID, c.Now()); err != nil {
		c.log.Warn("could not record a machine's ownership", "machine", m.ID, "error", err)
	}
	c.recordMachineAddress(ctx, m, got)

	switch got.Phase {
	case provider.PhaseRunning:
		c.transitionMachine(ctx, env, m, store.MachineBootstrapping, "the machine is running; installing the agent")
	case provider.PhaseStopped:
		c.transitionMachine(ctx, env, m, store.MachineStarting, "the machine exists and is being started")
	case provider.PhaseCreating:
		return errMachineWaiting
	default:
		// PhaseUnknown and PhaseDeleting both mean we cannot say what this is,
		// and what we cannot recognise we leave alone.
		return errMachineWaiting
	}
	return nil
}

// resolveMissing decides what to do about an identity the provider cannot find.
func (c *Controller) resolveMissing(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	cfg := c.cfg().Provider
	since := m.CreatedAt
	if m.CreateStartedAt != nil {
		since = *m.CreateStartedAt
	}
	if c.Now().Sub(since) > cfg.AmbiguityTimeout {
		return c.quarantineMachine(ctx, env, m, fmt.Sprintf(
			"this machine's create was issued %s ago and %s still cannot find %s. Nothing has been created or deleted: "+
				"look in the provider's console, then either release this row or delete the resource by hand",
			roundDuration(c.Now().Sub(since)), pr.row.Name, m.ResourceID))
	}
	if m.Attempts == 0 {
		// Looked for once. A provider that has just accepted a create often
		// cannot answer about it yet, so one absence is not evidence that
		// nothing was created.
		return &provider.Error{Kind: provider.FailureNotFound, Op: "inspect", Ref: m.Name,
			Message: fmt.Sprintf("%s cannot find %s yet; looking again before anything is created", pr.row.Name, m.ResourceID),
			Remedy:  "nothing to do: the machine is looked for once more before its identity is reused"}
	}
	// Twice, a call timeout apart. The identity is reused rather than a new one
	// allocated: that is what makes this a retry of one machine rather than the
	// purchase of a second.
	if held := c.provisioningHeld(pr.row, c.Now()); held != "" {
		return fmt.Errorf("%w: %s", errMachineHeld, held)
	}
	owner := c.machineOwner(pr.row, m)
	spec, err := c.machineSpec(ctx, env, pr, m, owner)
	if err != nil {
		return err
	}
	op, err := pr.p.Create(ctx, machineRef(m), spec)
	if err != nil {
		return c.noteAmbiguity(ctx, m, err)
	}
	if !op.Zero() {
		if err := c.st.SetMachineOperationHandle(ctx, m.ID, m.OpID, op.Handle); err != nil {
			c.log.Warn("could not record a create's handle", "machine", m.ID, "error", err)
		}
	}
	return nil
}

// stepStarting powers a machine on and waits for it to be running.
func (c *Controller) stepStarting(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	if m.OpHandle != "" {
		status, err := pr.p.Operation(ctx, provider.OperationRef{Kind: provider.OpStart, Handle: m.OpHandle})
		switch {
		case errors.Is(err, provider.ErrNotFound):
		case err != nil:
			return err
		case !status.Done:
			return errMachineWaiting
		}
	}
	got, err := pr.p.Inspect(ctx, machineRef(m))
	if err != nil {
		return err
	}
	if why := c.ownershipComplaint(m, got); why != "" {
		return c.quarantineMachine(ctx, env, m, why)
	}
	c.recordMachineAddress(ctx, m, got)
	if got.Phase == provider.PhaseRunning {
		c.transitionMachine(ctx, env, m, store.MachineBootstrapping, "the machine is running; installing the agent")
		return nil
	}
	power, ok := pr.p.(provider.PowerController)
	if !ok || got.Phase != provider.PhaseStopped {
		return errMachineWaiting
	}
	if held := c.mutationsHeld(); held != "" {
		return fmt.Errorf("%w: %s", errMachineHeld, held)
	}
	op, err := power.Start(ctx, machineRef(m))
	if err != nil {
		return c.noteAmbiguity(ctx, m, err)
	}
	if !op.Zero() {
		if err := c.st.SetMachineOperationHandle(ctx, m.ID, m.OpID, op.Handle); err != nil {
			c.log.Warn("could not record a start's handle", "machine", m.ID, "error", err)
		}
	}
	return errMachineWaiting
}

// stepReady is an observation and nothing else: a ready machine is a host, and
// the host's own lifecycle is the fleet's.
//
// What it writes is when the machine last had nothing to do, which is the clock
// scale-down counts from. It is stamped once and cleared the moment a runner
// lands, so a machine that has been idle all afternoon does not look like one
// that went idle a moment ago.
func (c *Controller) stepReady(ctx context.Context, env *machineEnv, m *store.Machine) {
	if m.HostID == "" {
		// The host row has gone -- deleted by an operator, or lost with a
		// restore. The machine is still rented, so it is drained and deleted
		// rather than forgotten.
		c.transitionMachine(ctx, env, m, store.MachineDraining,
			"the host this machine enrolled as no longer exists, so the machine is being released")
		return
	}
	host := env.hosts[m.HostID]
	if host == nil {
		c.transitionMachine(ctx, env, m, store.MachineDraining,
			"the host this machine enrolled as no longer exists, so the machine is being released")
		return
	}
	switch {
	case env.live[host.ID] > 0:
		if m.IdleSince != nil {
			if err := c.st.SetMachineIdleSince(ctx, m.ID, nil); err != nil {
				c.log.Warn("could not clear a machine's idle time", "machine", m.ID, "error", err)
			}
			m.IdleSince = nil
			c.publishMachine(ctx, m)
		}
	case m.IdleSince == nil:
		at := env.now
		if err := c.st.SetMachineIdleSince(ctx, m.ID, &at); err != nil {
			c.log.Warn("could not record a machine's idle time", "machine", m.ID, "error", err)
			return
		}
		m.IdleSince = &at
		c.publishMachine(ctx, m)
	}
}

// stepDraining waits for a machine's runners to finish, and lets demand take it
// back until the moment the delete starts.
func (c *Controller) stepDraining(ctx context.Context, env *machineEnv, m *store.Machine) {
	if m.HostID != "" && env.live[m.HostID] > 0 {
		return
	}
	c.transitionMachine(ctx, env, m, store.MachineDeleting,
		"the machine is empty and is being released")
}

// stepDeleting removes a machine, and is not finished until the provider says
// the resource is gone.
//
// A removal is not complete until the provider says the resource is gone. A 200
// from the delete call is not that; an Inspect that answers ErrNotFound is.
// recoverHostCleanup's header comment is the same sentence about a host.
func (c *Controller) stepDeleting(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine) error {
	if m.ResourceID == "" {
		// Nothing was ever created, so there is nothing to confirm gone.
		return c.confirmDeleted(ctx, env, m)
	}
	got, err := pr.p.Inspect(ctx, machineRef(m))
	switch {
	case errors.Is(err, provider.ErrNotFound):
		return c.confirmDeleted(ctx, env, m)
	case err != nil:
		return err
	}
	if why := c.ownershipComplaint(m, got); why != "" {
		return c.quarantineMachine(ctx, env, m, why)
	}
	if !got.Phase.AuthorisesDelete() {
		// PhaseUnknown above all: what we cannot recognise we leave alone,
		// which costs a stuck machine and saves somebody else's.
		return errMachineWaiting
	}
	if err := c.st.SetMachineOwnershipVerified(ctx, m.ID, c.Now()); err != nil {
		c.log.Warn("could not record a machine's ownership", "machine", m.ID, "error", err)
	}

	if m.OpHandle != "" {
		status, err := pr.p.Operation(ctx, provider.OperationRef{Kind: provider.OpDelete, Handle: m.OpHandle})
		switch {
		case errors.Is(err, provider.ErrNotFound):
		case err != nil:
			return err
		case !status.Done:
			return errMachineWaiting
		}
	}
	if held := c.mutationsHeld(); held != "" {
		return fmt.Errorf("%w: %s", errMachineHeld, held)
	}
	if got.Locked != "" {
		return errMachineWaiting
	}
	op, err := pr.p.Delete(ctx, machineRef(m))
	if err != nil {
		return c.noteAmbiguity(ctx, m, err)
	}
	if !op.Zero() {
		if err := c.st.SetMachineOperationHandle(ctx, m.ID, m.OpID, op.Handle); err != nil {
			c.log.Warn("could not record a delete's handle", "machine", m.ID, "error", err)
		}
	}
	// Not done: the next pass inspects, and only an inspect that cannot find
	// the resource records the deletion.
	return errMachineWaiting
}

// confirmDeleted stamps the confirmation exactly once, and takes the host row
// with it.
func (c *Controller) confirmDeleted(ctx context.Context, env *machineEnv, m *store.Machine) error {
	stamped, err := c.st.ConfirmMachineDeleted(ctx, m.ID, c.Now())
	if err != nil {
		return err
	}
	c.transitionMachine(ctx, env, m, store.MachineDeleted, "the provider confirms this machine's resource is gone")
	if stamped && m.HostID != "" {
		if err := c.DeleteHost(ctx, m.HostID); err != nil && !errors.Is(err, store.ErrNotFound) {
			c.log.Warn("a machine's resource is gone but its host row could not be removed",
				"machine", m.ID, "host", m.HostID, "error", err)
		}
	}
	if stamped {
		c.log.Info("a machine was released", "machine", m.ID, "name", m.Name)
	}
	return nil
}

// quarantineMachine stops a machine being acted on at all, with a sentence
// written for the person who now has to look.
func (c *Controller) quarantineMachine(ctx context.Context, env *machineEnv, m *store.Machine, why string) error {
	if err := c.st.SetMachineOwnershipError(ctx, m.ID, why); err != nil {
		c.log.Warn("could not record why a machine was quarantined", "machine", m.ID, "error", err)
	}
	c.transitionMachine(ctx, env, m, store.MachineQuarantined, why)
	c.log.Error("a machine was quarantined and nothing will act on it until a person does",
		"machine", m.ID, "name", m.Name, "reason", why)
	return nil
}

// ownershipComplaint reports which of the ownership facts disagreed, or "" when
// they all hold.
//
// The marks are not authenticated -- anybody with audit rights on the
// infrastructure can read and forge them -- so this is tamper-evidence rather
// than a credential. Its job is to tell "the machine I created at this
// identifier" apart from "a machine somebody else created at the same
// identifier after mine was destroyed", which is the accident that actually
// happens.
func (c *Controller) ownershipComplaint(m *store.Machine, got provider.Machine) string {
	want := provider.Owner{
		ControllerID: m.OwnerControllerID,
		ProviderID:   m.ProviderID,
		MachineID:    m.ID,
		Fingerprint:  m.OwnerFingerprint,
	}
	switch {
	case got.Owner.Zero():
		// An unmarked resource is not evidence of a second fleet: a provider
		// that cannot write marks leaves every resource like this, and the
		// store is then the only record of who made it.
		return ""
	case got.Owner.MachineID != "" && got.Owner.MachineID != m.ID:
		return fmt.Sprintf("machine %s names %s on %s, but that resource's ownership record says machine %s. "+
			"Nothing has been deleted. Check the provider's console, then either release this row -- which forgets "+
			"the resource without touching it -- or delete the resource by hand",
			m.ID, m.ResourceID, m.ResourceZone, got.Owner.MachineID)
	case !got.Owner.Matches(want):
		return fmt.Sprintf("machine %s names %s on %s, but that resource was created by another controller. "+
			"Nothing has been deleted. Check the provider's console before releasing this row",
			m.ID, m.ResourceID, m.ResourceZone)
	case got.Ref.Name != "" && got.Ref.Name != m.Name:
		return fmt.Sprintf("machine %s names %s on %s, but that resource is called %q rather than %q. "+
			"Nothing has been deleted", m.ID, m.ResourceID, m.ResourceZone, got.Ref.Name, m.Name)
	}
	return ""
}

// machineRef is the durable identity the row carries, in the provider's terms.
func machineRef(m *store.Machine) provider.MachineRef {
	ref := provider.MachineRef{Zone: m.ResourceZone, ID: m.ResourceID, Name: m.Name}
	if ref.ID == ref.Name {
		// A provider whose identity is only knowable after creation has its
		// name recorded as its identifier; handing that back as a native id
		// would ask it to look up something it does not have.
		ref.ID = ""
	}
	return ref
}

// machineOwner is the marks this controller stamps on a machine's resource.
func (c *Controller) machineOwner(row *store.Provider, m *store.Machine) provider.Owner {
	fingerprint := m.OwnerFingerprint
	if fingerprint == "" {
		fingerprint = store.NewSecret(12)
	}
	controller := m.OwnerControllerID
	if controller == "" {
		controller = c.controllerID()
	}
	return provider.Owner{
		ControllerID: controller,
		ProviderID:   row.ID,
		MachineID:    m.ID,
		Fingerprint:  fingerprint,
		CreatedAt:    m.CreatedAt,
	}
}

// machineSpec is what to build, including the enrolment payload when the
// provider carries it in the machine's creation metadata rather than pushing it
// into a running guest.
func (c *Controller) machineSpec(ctx context.Context, env *machineEnv, pr *machineProvider, m *store.Machine, owner provider.Owner) (provider.MachineSpec, error) {
	spec := provider.MachineSpec{
		Name:  m.Name,
		Owner: owner,
		Shape: provider.Shape{
			CPUs:      pr.row.MachineCPUs,
			MemoryMB:  pr.row.MachineMemoryMB,
			DiskMB:    pr.row.MachineDiskMB,
			Platform:  pr.row.MachinePlatform,
			Placement: pr.row.Settings,
		},
	}
	if pr.p.Capabilities().Bootstrap != provider.BootstrapMetadata {
		return spec, nil
	}
	// A provider that cannot reach into a running guest gets the payload with
	// the create, which means it is readable by anyone who can read the
	// machine's metadata. The credential in it is a single-use, minutes-long,
	// name-scoped join token and nothing else, which is exactly why that is
	// tolerable.
	payload, err := c.machineBootstrap(ctx, env, pr.row, m)
	if err != nil {
		return spec, err
	}
	spec.Bootstrap = payload
	return spec, nil
}

// recordMachineAddress keeps the address the provider reports, which is what an
// operator needs when a machine boots and never joins.
func (c *Controller) recordMachineAddress(ctx context.Context, m *store.Machine, got provider.Machine) {
	if got.Address == "" || got.Address == m.Address {
		return
	}
	if err := c.st.SetMachineAddress(ctx, m.ID, got.Address); err != nil {
		c.log.Debug("could not record a machine's address", "machine", m.ID, "error", err)
		return
	}
	m.Address = got.Address
}

// ---------------------------------------------------------------------------
// The breaker, and what the problems drawer reads
// ---------------------------------------------------------------------------

// noteProviderFailure counts a machine that failed before it was ever ready,
// and stands the provider down once enough have in a row.
//
// This is what stops a bad template burning fifty machines: three failures is
// enough to tell an unlucky create from a broken one, and the window widens so
// that a provider which is simply down is not hammered while it recovers.
func (c *Controller) noteProviderFailure(ctx context.Context, row *store.Provider, m *store.Machine) {
	if m.ReadyAt != nil {
		// A machine that worked once and failed later says nothing about the
		// provider's ability to build one.
		return
	}
	failures := row.ConsecutiveFailures + 1
	row.ConsecutiveFailures = failures
	var until *time.Time
	if failures >= breakerThreshold {
		wait := breakerBase << min(failures-breakerThreshold, 4)
		if wait > breakerMax {
			wait = breakerMax
		}
		at := c.Now().Add(wait)
		until = &at
		row.PausedUntil = until
		c.log.Warn("a provider was stood down after failing to build machines",
			"provider", row.Name, "failures", failures, "until", at)
	}
	if err := c.st.SetProviderBreaker(ctx, row.ID, failures, until); err != nil {
		c.log.Warn("could not record a provider's failures", "provider", row.ID, "error", err)
	}
	c.publishProvider(ctx, row)
}

// noteProviderSuccess clears the breaker. One machine that worked is the whole
// of the evidence needed: the breaker exists to stop a run of failures, not to
// punish a provider for having had one.
func (c *Controller) noteProviderSuccess(ctx context.Context, row *store.Provider) {
	c.clearProviderTrouble(row.ID)
	if row.ConsecutiveFailures == 0 && row.PausedUntil == nil {
		return
	}
	row.ConsecutiveFailures, row.PausedUntil = 0, nil
	if err := c.st.SetProviderBreaker(ctx, row.ID, 0, nil); err != nil {
		c.log.Warn("could not clear a provider's failures", "provider", row.ID, "error", err)
	}
	c.publishProvider(ctx, row)
}

// noteProviderTrouble remembers the last failure a provider gave us, for the
// sentence the problems drawer shows. It is in memory, like the pools' blocked
// reasons, because the next pass re-learns it and it is a fact about now.
func (c *Controller) noteProviderTrouble(providerID string, err error, machineID string, now time.Time) {
	if err == nil {
		return
	}
	t := providerTrouble{Kind: provider.KindOf(err), Detail: err.Error(), At: now, Machine: machineID}
	var pe *provider.Error
	if errors.As(err, &pe) {
		t.Remedy = pe.Remedy
		if pe.Message != "" {
			t.Detail = pe.Message
		}
	}
	c.machines.noteMu.Lock()
	c.machines.trouble[providerID] = t
	c.machines.noteMu.Unlock()
}

func (c *Controller) clearProviderTrouble(providerID string) {
	c.machines.noteMu.Lock()
	delete(c.machines.trouble, providerID)
	c.machines.noteMu.Unlock()
}

func (c *Controller) providerTroubles() map[string]providerTrouble {
	c.machines.noteMu.Lock()
	defer c.machines.noteMu.Unlock()
	out := make(map[string]providerTrouble, len(c.machines.trouble))
	for k, v := range c.machines.trouble {
		out[k] = v
	}
	return out
}

// noteOrphans records the untracked resources one sweep found, which the orphan
// page lists and nothing ever deletes.
func (c *Controller) noteOrphans(providerID string, names []string) {
	c.machines.noteMu.Lock()
	defer c.machines.noteMu.Unlock()
	if len(names) == 0 {
		delete(c.machines.orphans, providerID)
		return
	}
	c.machines.orphans[providerID] = names
}

// ProviderOrphans is the untracked resources the last sweep found, by provider.
func (c *Controller) ProviderOrphans() map[string][]string {
	c.machines.noteMu.Lock()
	defer c.machines.noteMu.Unlock()
	out := make(map[string][]string, len(c.machines.orphans))
	for k, v := range c.machines.orphans {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// unservableProviders names the providers whose machines no pool could ever
// place a runner on: money for nothing, and the one provider fault that looks
// like nothing being wrong at all.
func unservableProviders(providers []*store.Provider, pools []*store.Pool, now time.Time) []string {
	var out []string
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		synth := providerSynthHost(p, now)
		served := false
		for _, pool := range pools {
			if pool.Enabled && scheduler.HostCanRun(synth, pool, now) {
				served = true
				break
			}
		}
		if !served {
			out = append(out, p.ID)
		}
	}
	return out
}

// providerLabelFor names a provider in a metric or a sentence, falling back to
// the id when the row has gone.
func providerLabelFor(providers []*store.Provider, id string) string {
	for _, p := range providers {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// machineLabelsFor renders a machine's labels the way the agent's environment
// file wants them.
func machineLabelsFor(labels store.StringMap) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	// Sorted, so that two renderings of one machine's labels are the same
	// bytes: an environment file that reshuffled itself would look like a
	// change every time it was written.
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

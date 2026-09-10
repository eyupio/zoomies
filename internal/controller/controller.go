// Package controller is the wiring between everything else: it owns the
// reconcile loop, the GitHub client cache, webhook ingest, the agent task
// queue, the log relay and the background housekeeping.
//
// Nothing in here decides how many runners a pool should have -- that is
// internal/scheduler, and it is a pure function -- and nothing in here writes
// SQL, which is internal/store. What this package does is turn a decision into
// GitHub calls, database rows and tasks for an agent, in an order that leaves
// the fleet in a defensible state when any single step fails.
//
// The controller never dials an agent. Agents connect outbound, long-poll for
// tasks and POST their results, which is why the task queue and the log relay
// are shaped the way they are.
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// SeedEnvVar names the environment variable that turns demo seeding on. The
// Playwright suite sets it (see web/tests/support/serve.mjs) so the UI has a
// fixture fleet to render.
const SeedEnvVar = "ZOOMIES_SEED_DEMO"

// Options are the collaborators the controller needs. Everything except the
// store and the configuration has a defensible default, because a caller that
// forgets the event bus should get a working controller rather than a nil
// dereference three loops later.
type Options struct {
	// Store is the only writer of persistent state.
	Store *store.Store
	// Config is the validated configuration; the controller reads the
	// scheduler tunables, the GitHub settings and the retention windows.
	Config *config.Config
	// Key unseals the GitHub App private keys and webhook secrets held in the
	// database. Without it no installation can be used.
	Key *cryptox.Key
	// Auth mints agent tokens, redeems join tokens and records audit rows.
	Auth *auth.Service
	// Events carries every state change to the UI's SSE stream.
	Events *events.Bus
	// GitHub builds a client per installation; nil uses the real GitHub App
	// factory, and tests pass one backed by github.NewFake.
	GitHub github.Factory
	// Backends is only needed when this process also runs the embedded agent.
	Backends *backend.Registry
	Logger   *slog.Logger
	// Clock is injectable so tests can freeze time.
	Clock func() time.Time
	// HTTPClient delivers capacity-demand events. Tests may inject a transport.
	HTTPClient *http.Client
	// LogLevel is the gate the process logger is filtered at, when the caller
	// built one that can move. UpdateConfig sets it from log.level, so that a
	// level changed through PATCH /settings or SIGHUP is the level the process
	// actually logs at, not merely the one the settings page shows.
	LogLevel *slog.LevelVar
	// Lease is this controller's claim on the database, taken before the
	// controller was built. A nil lease means nothing renews and nothing is
	// reported, which is what an embedded or test controller wants.
	Lease *store.ControllerLease
}

// Controller owns the control plane's moving parts and their lifecycles.
type Controller struct {
	st *store.Store
	// live is the configuration as this process currently sees it. It is a
	// snapshot behind an atomic pointer rather than a struct shared by
	// reference, because PATCH /settings changes it while every loop in here
	// is reading it; see config.Live. Read it through cfg().
	live       *config.Live
	logLevel   *slog.LevelVar
	key        *cryptox.Key
	authsvc    *auth.Service
	bus        *events.Bus
	factory    github.Factory
	backends   *backend.Registry
	log        *slog.Logger
	clock      func() time.Time
	httpClient *http.Client

	metrics *metrics
	clients *clientCache
	queues  *taskQueues
	relay   *logRelay

	// pollsInFlight is how many agent task polls are being held right now.
	// An idle fleet still costs one held connection per host, so this is the
	// number the controller sheds against -- see shedFor.
	pollsInFlight atomic.Int64

	// nudges is capacity 1 on purpose: it is a "there is work to look at"
	// flag, not a queue. Fifty webhooks in a second leave one token behind and
	// therefore cause one reconcile pass, not fifty.
	nudges chan struct{}
	// reconcileMu makes a pass mutually exclusive with itself, so a timer tick
	// landing on top of a nudge cannot double-create runners.
	reconcileMu sync.Mutex
	// passes counts completed reconciles; tests assert on coalescing with it.
	passes atomic.Uint64
	// polls counts completed poller sweeps, for the same reason.
	polls atomic.Uint64
	// settingsChanged wakes the loops whose timers are built from the
	// configuration, so that a new interval is in force from the moment it is
	// accepted rather than from the next restart. Capacity 1, like nudges: it
	// is a flag, and the loop re-reads every tunable when it wakes.
	settingsChanged chan struct{}

	// pollingOnly records that no webhook has ever arrived, which the Overview
	// says out loud because a fleet scaling on the poller looks healthy until
	// somebody wonders why it is slow.
	pollingOnly atomic.Bool

	// webhookProbes bounds what a stream of unverifiable deliveries from one
	// address can write to the database, the log and the event stream. Only
	// the rejected path consults it: a delivery that verifies came from
	// GitHub, and GitHub is never throttled here.
	webhookProbes *auth.RateLimiter
	// lastPollAt is when a poller sweep last finished, as UnixNano, or zero if
	// none has. It is what says the safety net is still there: a poller that
	// has stopped sweeping looks exactly like a poller with nothing to do, and
	// on a fleet whose webhooks are also broken the difference is every job.
	lastPollAt atomic.Int64
	// fence is the recovery fence, read from the database at start and kept
	// here so that a decision made ten times a minute is not ten database
	// reads. It is a pointer so the zero value -- no fence -- costs nothing to
	// express and a lift is one atomic store.
	fence atomic.Pointer[store.RecoveryFence]
	// githubMu guards githubPaused, which is a map rather than one deadline
	// because GitHub's quota is per installation: one organisation spending
	// its hour must not stop the fleet polling another's, which is the whole
	// point of the fallback poller on a controller serving several.
	githubMu sync.Mutex
	// githubPaused is the rate-limit backoff for each installation: the moment
	// its API may be used again. An installation absent from the map is not
	// held. Every sweep that spends quota on its own schedule shares it -- the
	// fallback poller and the registration reap -- because a hold one of them
	// earned is a hold the other would otherwise spend the same refused window
	// discovering for itself.
	githubPaused map[string]time.Time

	mu sync.Mutex
	// lastPlan is the most recent scheduler decision, kept so that Problems
	// can report the queued jobs no pool claimed without deciding again.
	lastPlan   *scheduler.Plan
	lastPlanAt time.Time
	// reserved is what each host's live runners had promised away as of the
	// last pass, keyed by host id.
	//
	// It is recorded rather than recomputed for the view, and from the same
	// snapshot the pass decided on: a page that showed a different figure from
	// the one the scheduler placed against would be worse than showing none,
	// because an operator would trust it. It is at most one pass old, which is
	// the same age as the plan the problems drawer already reports.
	reserved map[string]scheduler.Reservation
	// lease is this controller's claim on the database, renewed on a timer.
	// leaseLost is set when a renewal found somebody else holding it, which is
	// the fleet's worst state: two schedulers, both certain they are the only
	// one. It is never cleared -- the operator has to look.
	lease     *store.ControllerLease
	leaseLost atomic.Pointer[store.ControllerLease]
	// runnerGroups remembers the pools whose runner group could not be
	// resolved, so that the fallback to GitHub's Default group is a standing
	// warning rather than a log line nobody reads. Keyed by pool ID.
	runnerGroups map[string]runnerGroupNote
	// blocked remembers the reason each pool could not place a runner, so that
	// a fleet that cannot scale says so once rather than every tick.
	blocked map[string]string
	// hostHealthy remembers each host's last known health so that only a flip
	// publishes an event.
	hostHealthy map[string]bool
	// resynced records the hosts that have heartbeat since this process
	// started; the first heartbeat from each asks for a full runner report.
	resynced map[string]bool
	// release is what the last update check learned about the current release
	// of Zoomies, or nil until one has answered.
	release  *releaseState
	embedded *agent.Agent
	// embeddedCancel stops the in-process agent, which may have been started
	// with a context the controller does not otherwise control.
	embeddedCancel context.CancelFunc

	// derivedMu guards the last published form of the stats and problems
	// payloads, which the reconcile loop and the housekeeping loop both
	// compare against, and of each host, which every publisher records so the
	// pass's diff does not send a frame that has just gone out.
	derivedMu    sync.Mutex
	lastStats    []byte
	lastProblems []byte
	lastHosts    map[string][]byte

	startMu sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	// deliveries tracks capacity-demand requests in flight, which run apart
	// from the reconcile pass that decided them; Stop waits for them, and so
	// do the tests.
	deliveries sync.WaitGroup

	// loopRestartDelay is how long a loop that panicked waits before it is
	// started again. It doubles on each further panic up to loopRestartMax,
	// and a test sets it short.
	loopRestartDelay time.Duration
	// loopPanics is what the problems drawer reports about a loop that has
	// crashed: how often, and what the last panic said.
	loopPanics map[string]loopPanic
}

// loopPanic is the record of one background loop's crashes.
type loopPanic struct {
	Count int
	Last  string
	At    time.Time
}

// loopRestartMax caps the wait between restarts of a loop that keeps
// panicking; a minute is long enough not to flood the log and short enough
// that a bug which clears itself -- a bad row that was since pruned -- does
// not leave scaling stopped for the rest of the day.
const loopRestartMax = time.Minute

// New validates the options and builds a controller that is not yet running.
func New(opts Options) (*Controller, error) {
	if opts.Store == nil {
		return nil, errors.New("controller: no store; open one with store.Open before building the controller")
	}
	if opts.Config == nil {
		return nil, errors.New("controller: no configuration; pass the result of config.Load, or config.Default for an in-process controller")
	}
	if opts.Key == nil {
		return nil, errors.New("controller: no encryption key; GitHub App private keys and webhook secrets are sealed with it, " +
			"so set security.encryption_key_file (or ZOOMIES_ENCRYPTION_KEY) and pass the parsed key")
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "controller")

	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}

	bus := opts.Events
	if bus == nil {
		bus = events.New()
	}

	authsvc := opts.Auth
	if authsvc == nil {
		authsvc = auth.New(opts.Store, opts.Config, bus, auth.WithLogger(log), auth.WithClock(clock))
	}

	factory := opts.GitHub
	if factory == nil {
		factory = github.NewAppFactory(nil)
	}

	c := &Controller{
		st:              opts.Store,
		lease:           opts.Lease,
		live:            config.NewLive(opts.Config),
		logLevel:        opts.LogLevel,
		key:             opts.Key,
		authsvc:         authsvc,
		bus:             bus,
		factory:         factory,
		backends:        opts.Backends,
		log:             log,
		clock:           clock,
		httpClient:      opts.HTTPClient,
		nudges:          make(chan struct{}, 1),
		settingsChanged: make(chan struct{}, 1),
		hostHealthy:     map[string]bool{},
		// Generous next to what GitHub sends and mean next to what a probe
		// wants: a real delivery never reaches this limiter, and a prober gets
		// sixty rows a minute rather than as many as it can open connections.
		webhookProbes:    auth.NewRateLimiter(60, time.Minute, clock),
		loopRestartDelay: time.Second,
		resynced:         map[string]bool{},
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	c.metrics = newMetrics(c)
	c.clients = newClientCache(c)
	c.queues = newTaskQueues()
	c.relay = newLogRelay(c)
	return c, nil
}

// Start launches the background loops and returns as soon as they are running.
// It does not block until they finish; Stop does that.
func (c *Controller) Start(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return errors.New("controller: already started")
	}

	if seedRequested() {
		if err := c.SeedDemo(ctx); err != nil {
			// Seeding is a development and test convenience; refusing to start
			// because of it would be worse than saying so and carrying on.
			c.log.Warn("demo seeding was requested but did not run", "env", SeedEnvVar, "error", err)
		} else if stuckSeedRequested() {
			// Only on top of a seed that just succeeded: the diagnostics
			// fixture ages the demo's own runners, so it has nothing to work
			// with otherwise.
			if err := c.SeedStuck(ctx); err != nil {
				c.log.Warn("the diagnostics fixture was requested but did not run", "env", StuckSeedEnvVar, "error", err)
			}
		}
	}

	// The fence, before any loop starts. A controller that began reconciling
	// and then discovered it was fenced would already have created the runners
	// the fence exists to prevent.
	if err := c.LoadFence(ctx); err != nil {
		return fmt.Errorf("controller: reading the recovery fence: %w", err)
	}
	if f := c.Fenced(); f.Fenced {
		c.log.Warn("this fleet is fenced for recovery",
			"reason", f.Reason,
			"detail", "the scheduler will decide as normal and apply nothing: no runner is created, drained or removed, nothing is reaped, and the poller does not sweep",
			"fix", "check what the restore did not bring with it, then lift the fence")
	}

	// Knowing up front whether a webhook has ever arrived means the Overview
	// can answer "are we event-driven?" without waiting for the first poll.
	if last, err := c.st.LastAcceptedDeliveryAt(ctx); err == nil {
		c.pollingOnly.Store(last.IsZero())
	}

	loopCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.started = true

	c.spawn("reconcile", loopCtx, c.reconcileLoop)
	c.spawn("reap", loopCtx, c.reapLoop)
	c.spawn("poller", loopCtx, c.pollLoop)
	c.spawn("installations", loopCtx, c.probeLoop)
	c.spawn("background", loopCtx, c.backgroundLoop)
	if c.lease != nil {
		c.spawn("lease", loopCtx, c.leaseLoop)
	}
	if seedRequested() {
		c.spawn("demo-heartbeat", loopCtx, c.demoHeartbeatLoop)
	}

	c.log.Info("controller started",
		"interval", c.schedulerInterval(),
		"webhook_path", c.cfg().GitHub.WebhookPath,
		"poll_fallback", c.cfg().GitHub.PollFallback,
		"version", version.Short())
	return nil
}

// spawn runs one named loop until its context is cancelled, tracking it so
// Stop can wait for it.
//
// A panic in one loop must not take the whole controller with it, but a loop
// that has stopped is not something the process can be allowed to be quiet
// about either: a scheduler that died on one bad snapshot used to leave
// /readyz answering 200 and every job queued for good. The loop is started
// again after a wait that doubles with each crash, and the crash is on the
// problems drawer until the process restarts.
func (c *Controller) spawn(name string, ctx context.Context, fn func(context.Context)) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		delay := c.loopRestartDelay
		for {
			value, stack := runGuarded(ctx, fn)
			if value == nil || ctx.Err() != nil {
				return
			}
			c.notePanic(name, value)
			c.log.Error("controller loop panicked; it will be restarted",
				"loop", name, "panic", value, "restart_in", delay, "stack", stack)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(2*delay, loopRestartMax)
		}
	}()
}

// runGuarded runs fn and reports the value it panicked with, or nil when it
// returned. The stack is captured at the panic, which is the only place it is
// still there to capture.
func runGuarded(ctx context.Context, fn func(context.Context)) (value any, stack string) {
	defer func() {
		if r := recover(); r != nil {
			value, stack = r, string(debug.Stack())
		}
	}()
	fn(ctx)
	return nil, ""
}

// notePanic records a loop's crash for the problems drawer.
func (c *Controller) notePanic(name string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loopPanics == nil {
		c.loopPanics = map[string]loopPanic{}
	}
	p := c.loopPanics[name]
	p.Count++
	p.Last = fmt.Sprint(value)
	p.At = c.Now()
	c.loopPanics[name] = p
}

// LoadFence reads the recovery fence from the database into this controller.
//
// It is called at start and again whenever the fence is changed, rather than
// on every pass: the fence changes once in the life of a recovery, and a
// database read in the hot path of a loop that runs every few seconds would
// cost more than it could ever save.
func (c *Controller) LoadFence(ctx context.Context) error {
	f, err := c.st.RecoveryFenced(ctx)
	if err != nil {
		return err
	}
	c.fence.Store(&f)
	return nil
}

// Fence is the recovery fence as the API renders it. It is an alias so the
// transport layer names a controller type rather than reaching into the store.
type Fence = store.RecoveryFence

// Fenced reports whether this fleet is held for recovery, and why.
func (c *Controller) Fenced() store.RecoveryFence {
	if f := c.fence.Load(); f != nil {
		return *f
	}
	return store.RecoveryFence{}
}

// Unfence lifts the fence and lets the fleet act again.
//
// It writes the database first and the in-memory copy second, so a failed
// write leaves a fenced controller rather than one that believes it is free.
func (c *Controller) Unfence(ctx context.Context) error {
	if err := c.st.SetRecoveryFence(ctx, false, ""); err != nil {
		return err
	}
	if err := c.LoadFence(ctx); err != nil {
		return err
	}
	c.log.Info("the recovery fence was lifted; this fleet will create, drain and remove runners again")
	// The fleet has been standing still: decide now rather than at the next
	// tick, because everything queued during the fence is waiting on this.
	c.Nudge()
	return nil
}

// LoopPanics returns the crashes recorded against each background loop since
// the process started, for the problems drawer.
func (c *Controller) LoopPanics() map[string]loopPanic {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.loopPanics)
}

// Stop shuts the loops down gracefully and flushes what is worth keeping.
//
// It deliberately does not tear down runners. Restarting a controller must not
// kill jobs: the runners keep working, their agents keep reporting, and the
// next reconcile picks up where this one left off.
func (c *Controller) Stop(ctx context.Context) error {
	c.startMu.Lock()
	if !c.started {
		c.startMu.Unlock()
		return nil
	}
	c.started = false
	cancel := c.cancel
	c.startMu.Unlock()

	if cancel != nil {
		cancel()
	}
	c.stopEmbeddedAgent()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		// A capacity-demand request decided in the last pass finishes on its
		// own bounded context; letting the process exit under it would lose
		// the record of how it went.
		c.deliveries.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		c.log.Warn("controller shutdown timed out waiting for its loops; exiting anyway, runners are unaffected")
	}

	// One last sample so the Overview's sparkline does not show a gap that
	// looks like an outage when it was a restart.
	if err := c.sample(context.WithoutCancel(ctx)); err != nil {
		c.log.Debug("could not write a final fleet sample", "error", err)
	}
	c.relay.closeAll()
	c.log.Info("controller stopped; runners on every host are still running")
	return nil
}

// Store returns the database handle the API reads through.
func (c *Controller) Store() *store.Store { return c.st }

// Config returns the configuration as it currently stands: what the controller
// was built with, as changed since by UpdateConfig. It is a snapshot and must
// not be written through; the next UpdateConfig would silently discard the
// write, and until then every loop would be reading it unsynchronised.
func (c *Controller) Config() *config.Config { return c.live.Load() }

// cfg is Config for the controller's own code, kept short because it is read
// on every pass.
func (c *Controller) cfg() *config.Config { return c.live.Load() }

// UpdateConfig changes the running configuration.
//
// fn is applied to a copy of the current snapshot, which then replaces it, so
// a loop in the middle of a pass finishes on the values it started with and
// the next pass sees the new ones. Anything built once from the configuration
// -- the log level's gate, the scheduler's and poller's timers -- is retuned
// here, which is what lets PATCH /settings promise that an accepted change is
// in effect and not merely recorded.
func (c *Controller) UpdateConfig(fn func(*config.Config)) *config.Config {
	before, after := c.live.Update(fn)
	if c.logLevel != nil && before.Log.Level != after.Log.Level {
		c.logLevel.Set(config.ParseLogLevel(after.Log.Level))
	}
	select {
	case c.settingsChanged <- struct{}{}:
	default:
	}
	// The scheduler tunables change what the next pass decides, and a new
	// interval takes effect once a pass has run and reset the timer.
	c.Nudge()
	return after
}

// Auth returns the authentication and audit service.
func (c *Controller) Auth() *auth.Service { return c.authsvc }

// Events returns the bus the API's SSE endpoint subscribes to.
func (c *Controller) Events() *events.Bus { return c.bus }

// Backends returns the backend registry, or nil in a controller that runs no
// embedded agent.
func (c *Controller) Backends() *backend.Registry { return c.backends }

// Now returns the controller's clock, always in UTC.
func (c *Controller) Now() time.Time { return c.clock().UTC() }

// Nudge wakes the reconcile loop immediately.
//
// It never blocks and never queues: the channel holds a single token, so a
// burst of webhooks costs one reconcile pass rather than one per delivery.
func (c *Controller) Nudge() {
	select {
	case c.nudges <- struct{}{}:
	default:
	}
}

// PollingOnly reports whether scaling is running on the fallback poller
// because no webhook has ever been received.
func (c *Controller) PollingOnly() bool { return c.pollingOnly.Load() }

// PollerEnabled reports whether the fallback poller is running at all. It is
// off by configuration, never by failure, so a fleet with no safety net is a
// choice somebody made and the UI should say which.
func (c *Controller) PollerEnabled() bool { return c.cfg().GitHub.PollFallback }

// LastPollAt is when the fallback poller last completed a sweep, or the zero
// time if it has not completed one since this controller started.
func (c *Controller) LastPollAt() time.Time {
	ns := c.lastPollAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// schedulerInterval is the reconcile period, with a floor so that a
// misconfigured zero does not spin the loop.
func (c *Controller) schedulerInterval() time.Duration {
	if d := c.cfg().Scheduler.Interval; d > 0 {
		return d
	}
	return 10 * time.Second
}

// policy converts the configured tunables into the scheduler's Policy.
func (c *Controller) policy() scheduler.Policy {
	return scheduler.Policy{
		ScaleUpDelay:      c.cfg().Scheduler.ScaleUpDelay,
		MaxRunnerLifetime: c.cfg().Scheduler.MaxRunnerLifetime,
		ProvisionTimeout:  c.cfg().Scheduler.ProvisionTimeout,
		DrainTimeout:      c.cfg().Scheduler.DrainTimeout,
		MaxCreatesPerTick: c.cfg().Scheduler.MaxCreatesPerTick,
	}
}

// publish sends one event to the UI, ignoring the absence of a bus.
func (c *Controller) publish(kind events.Kind, topic string, payload any) {
	if c.bus == nil {
		return
	}
	c.bus.Publish(kind, topic, payload)
}

// publishRunner announces a runner change so the Runners page updates live.
//
// The frame carries the same JSON as GET /runners/{id}, not the store row: the
// UI drops it straight into its cache, and a row without the pool and host
// names would blank those columns until the next full fetch.
func (c *Controller) publishRunner(ctx context.Context, kind events.Kind, r *store.Runner) {
	if r == nil || c.bus == nil {
		return
	}
	c.publish(kind, "runner:"+r.ID, c.runnerView(ctx, r))
}

// publishHost announces a host change, which is how the Hosts page shows an
// agent going quiet without the operator refreshing. The frame is the API's
// host shape, with `healthy` worked out, because that is the field the event
// exists to change.
//
// Every host frame goes through here, and each one records the bytes it sent:
// the reconcile pass diffs against that record (see publishHostChanges), so a
// change announced the moment it happened is not announced a second time a few
// seconds later.
func (c *Controller) publishHost(h *store.Host) {
	if h == nil || c.bus == nil {
		return
	}
	raw, err := json.Marshal(c.HostView(h))
	if err != nil {
		// Not reachable with a HostView, and not worth dropping the frame
		// over if it ever were: the operator needs the event more than the
		// pass needs its record.
		c.log.Error("could not marshal a host for the event stream", "host", h.ID, "error", err)
		c.publish(events.KindHostUpdated, "host:"+h.ID, c.HostView(h))
		return
	}
	c.rememberHost(h.ID, raw)
	c.publish(events.KindHostUpdated, "host:"+h.ID, json.RawMessage(raw))
}

// rememberHost records the form a host was last published in.
//
// Best-effort, and deliberately so: a caller that publishes a host it has
// changed in memory without saving records bytes the pass will not match --
// the store keeps timestamps to the millisecond and a Go value does not --
// and the next pass then repeats the frame once. Repeating a frame costs a
// browser one repaint of a row it already has; a scheme that could not be
// wrong here would cost every publisher a re-read.
func (c *Controller) rememberHost(id string, raw []byte) {
	c.derivedMu.Lock()
	defer c.derivedMu.Unlock()
	if c.lastHosts == nil {
		c.lastHosts = map[string][]byte{}
	}
	c.lastHosts[id] = raw
}

// publishJob announces a job change in the shape GET /jobs/{id} returns.
func (c *Controller) publishJob(ctx context.Context, j *store.Job) {
	if j == nil || c.bus == nil {
		return
	}
	c.publish(events.KindJobUpdated, "job:"+j.ID, c.jobView(ctx, j))
}

// publishInstallation announces an installation change in the shape
// GET /installations/{id} returns.
func (c *Controller) publishInstallation(ctx context.Context, inst *store.Installation) {
	if inst == nil || c.bus == nil {
		return
	}
	c.publish(events.KindInstallation, "installation:"+inst.ID, c.installationView(ctx, inst))
}

// PublishPool announces a pool an operator created or changed, in the shape
// GET /pools/{id} returns. The API calls it after its own write: the operator
// who made the change is looking at the response, but every other open
// dashboard learns about it from here.
func (c *Controller) PublishPool(ctx context.Context, kind events.Kind, p *store.Pool) {
	if p == nil || c.bus == nil {
		return
	}
	view, err := c.PoolRenderer(ctx)
	if err != nil {
		c.log.Warn("could not render a pool for the event stream", "pool", p.ID, "error", err)
		return
	}
	c.publish(kind, "pool:"+p.ID, view.View(p))
}

// PublishPoolDeleted announces that a pool is gone.
func (c *Controller) PublishPoolDeleted(id string) {
	c.publish(events.KindPoolDeleted, "pool:"+id, deletedPayload{ID: id})
}

// publishRunnerDeleted announces that a runner row is gone. A runner that was
// removed already left the page on its runner.updated frame; this is for the
// rows that vanish without one -- pruned, or cascaded away with their pool,
// host or installation -- which the Runners page otherwise kept until a reload.
func (c *Controller) publishRunnerDeleted(id string) {
	c.publish(events.KindRunnerDeleted, "runner:"+id, deletedPayload{ID: id})
}

// announceEach is the most individual deletions worth putting on the bus in one
// go.
//
// A subscriber's queue is 256 deep and the bus drops a subscriber that falls
// behind it. The hourly prune deletes everything past the retention window in
// one pass, which on a busy fleet is thousands of rows -- so announcing each of
// them cut off every open tab at once, and every one of them reconnected and
// refetched six endpoints. The storm was the announcement, not the deletion.
//
// Sixty-four leaves the queue most of its room for the frames that are actually
// about the fleet, and is far more precise deletions than a person is watching
// disappear.
const announceEach = 64

// publishRunnersDeleted announces a set of deleted runner rows, or -- when
// there are more than a page can usefully be told about one at a time -- one
// resync, which is the frame that already means "fetch the resources again".
//
// A tab that is told to resync refetches once. A tab that is cut off for
// falling behind refetches too, but only after showing itself as disconnected,
// and every other tab does the same thing at the same moment.
func (c *Controller) publishRunnersDeleted(ids []string) {
	if len(ids) > announceEach {
		c.publish(events.KindResync, "", map[string]any{
			"reason": "many runner rows were removed at once; fetch the resources again",
			"count":  len(ids),
		})
		return
	}
	for _, id := range ids {
		c.publishRunnerDeleted(id)
	}
}

// DeletePool removes a pool and announces everything that went with it: each
// runner row, then the pool. The runners go first so a page that drops them
// has nothing left to explain when the pool disappears.
func (c *Controller) DeletePool(ctx context.Context, id string) error {
	runners, err := c.st.DeletePool(ctx, id)
	if err != nil {
		return err
	}
	c.publishRunnersDeleted(runners)
	c.PublishPoolDeleted(id)
	return nil
}

// DeleteHost removes a host and announces its runner rows and then the host.
func (c *Controller) DeleteHost(ctx context.Context, id string) error {
	runners, err := c.st.DeleteHost(ctx, id)
	if err != nil {
		return err
	}
	c.publishRunnersDeleted(runners)
	c.PublishHostDeleted(id)
	return nil
}

// DeleteInstallation removes an installation with its pools and their runner
// rows, forgets its GitHub client, and announces each thing that went in the
// order a page wants them: runners, pools, then the installation.
func (c *Controller) DeleteInstallation(ctx context.Context, id string) error {
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return fmt.Errorf("listing the installation's pools: %w", err)
	}
	runners, err := c.st.DeleteInstallation(ctx, id)
	if err != nil {
		return err
	}
	c.Forget(id)
	c.publishRunnersDeleted(runners)
	for _, p := range pools {
		if p.InstallationID == id {
			c.PublishPoolDeleted(p.ID)
		}
	}
	c.PublishInstallationDeleted(id)
	return nil
}

// PublishHost announces a host an operator changed: its capacity, its labels,
// or whether it is cordoned.
func (c *Controller) PublishHost(h *store.Host) { c.publishHost(h) }

// PublishHostDeleted announces that a host is gone.
func (c *Controller) PublishHostDeleted(id string) {
	c.derivedMu.Lock()
	delete(c.lastHosts, id)
	c.derivedMu.Unlock()
	c.publish(events.KindHostDeleted, "host:"+id, deletedPayload{ID: id})
}

// PublishInstallation announces an installation an operator added or edited.
func (c *Controller) PublishInstallation(ctx context.Context, inst *store.Installation) {
	c.publishInstallation(ctx, inst)
}

// PublishInstallationDeleted announces that an installation is gone.
func (c *Controller) PublishInstallationDeleted(id string) {
	c.publish(events.KindInstallationDeleted, "installation:"+id, deletedPayload{ID: id})
}

// deletedPayload is what every *.deleted frame carries: the id, and nothing
// else, because the resource no longer exists to render.
type deletedPayload struct {
	ID string `json:"id"`
}

// setLastPlan records the most recent decision for Problems to report on.
func (c *Controller) setLastPlan(p scheduler.Plan) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPlan = &p
	c.lastPlanAt = c.Now()
}

// setReserved records what each host had promised away in the snapshot the
// pass just decided on.
func (c *Controller) setReserved(snap scheduler.Snapshot) {
	out := make(map[string]scheduler.Reservation, len(snap.Hosts))
	for _, h := range snap.Hosts {
		if h == nil {
			continue
		}
		out[h.ID] = scheduler.Reserved(h, snap.Pools, snap.Runners)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reserved = out
}

// reservedOn is what the last pass found already promised away on a host, and
// whether a pass has run at all: a controller that has not yet decided
// anything reports nothing rather than zero, which would read as an empty
// machine.
func (c *Controller) reservedOn(id string) (scheduler.Reservation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.reserved[id]
	return r, ok
}

// runnerGroupNote is a pool whose runners went to the default runner group
// because the one it asked for could not be resolved.
type runnerGroupNote struct {
	// PoolName and Group are what the sentence names; the reason is the half
	// that differs between "the group is not there" and "GitHub would not say".
	PoolName string
	Group    string
	Reason   string
}

// noteRunnerGroup records that a pool's runners were placed in the default
// runner group, or that they no longer are. Like noteBlocked, each change is
// logged once: a pool creating runners every few seconds would otherwise
// repeat the same sentence until somebody noticed it.
func (c *Controller) noteRunnerGroup(p *store.Pool, group, reason string) {
	c.mu.Lock()
	if c.runnerGroups == nil {
		c.runnerGroups = map[string]runnerGroupNote{}
	}
	was, had := c.runnerGroups[p.ID]
	if reason == "" {
		delete(c.runnerGroups, p.ID)
	} else {
		c.runnerGroups[p.ID] = runnerGroupNote{PoolName: p.Name, Group: group, Reason: reason}
	}
	c.mu.Unlock()

	switch {
	case reason != "" && reason != was.Reason:
		c.log.Warn("a pool's runners are going to the default runner group",
			"pool", p.Name, "group", group, "reason", reason)
	case reason == "" && had:
		c.log.Info("a pool's runner group resolved again", "pool", p.Name, "group", group)
	}
}

// noteBlocked logs a pool that wanted runners and could not place any, and the
// moment it recovers. Both are logged once per change: the reconcile loop runs
// every few seconds, and a fleet that is one host short would otherwise fill
// the log with the same sentence until somebody noticed it.
func (c *Controller) noteBlocked(pp scheduler.PoolPlan) {
	c.mu.Lock()
	if c.blocked == nil {
		c.blocked = map[string]string{}
	}
	was, had := c.blocked[pp.PoolID]
	switch {
	case pp.Blocked == "":
		delete(c.blocked, pp.PoolID)
	default:
		c.blocked[pp.PoolID] = pp.Blocked
	}
	c.mu.Unlock()

	switch {
	case pp.Blocked != "" && pp.Blocked != was:
		c.log.Warn("a pool cannot place the runners it wants",
			"pool", pp.PoolName, "current", pp.Current, "desired", pp.Desired,
			"queued", pp.QueuedMatched, "reason", pp.Blocked, "fix", pp.BlockedFix)
	case pp.Blocked == "" && had:
		c.log.Info("a pool can place runners again", "pool", pp.PoolName)
	}
}

func (c *Controller) getLastPlan() (*scheduler.Plan, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPlan, c.lastPlanAt
}

// seedRequested reports whether the demo fixtures were asked for.
func seedRequested() bool {
	v, ok := os.LookupEnv(SeedEnvVar)
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// unsealString opens a sealed column, turning a key mismatch into a message
// that names the situation rather than "cipher: message authentication failed".
func (c *Controller) unsealString(sealed []byte, what string) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	s, err := c.key.OpenString(sealed)
	if err != nil {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	return s, nil
}

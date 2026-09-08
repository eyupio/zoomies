package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/scheduler"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// Problem is one thing an operator should know about, in the shape the API's
// /problems endpoint returns.
//
// It carries the same four sentences a config.Finding does -- what is true,
// why it matters, and what to change -- because an operator reading the
// problems drawer should never have to go and look up what a code means.
type Problem struct {
	// Code is a stable identifier such as "host.unhealthy", suitable for
	// grouping or suppressing in an alerting rule.
	Code     string          `json:"code"`
	Severity config.Severity `json:"severity"`
	// Setting names the configuration key involved, when there is one.
	Setting string `json:"setting,omitempty"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Fix     string `json:"fix,omitempty"`
	// TargetKind and TargetID let the UI link a problem to the pool, host,
	// runner or installation it is about.
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	// Alternatives are the choices the fix leaves open, when the fix is a
	// choice: for a pool no host can run, the backends its hosts do offer. They
	// are carried apart from the prose so the UI can put the change one click
	// away rather than leaving an operator to find the pool's edit form.
	Alternatives []string `json:"alternatives,omitempty"`
	// Since is when the situation started, where that is knowable.
	Since *time.Time `json:"since,omitempty"`
}

// problemWindow is how far back rejected webhook deliveries are counted. An
// hour is long enough to catch a secret that was changed on one side only, and
// short enough that yesterday's fixed problem is not still on the list.
const problemWindow = time.Hour

// Problems aggregates everything currently wrong and every dangerous setting
// in effect, worst first.
//
// It returns an empty slice rather than nil when there is nothing to say,
// because the UI renders "nothing needs your attention" from exactly that.
//
// A section it cannot gather is reported *in* the list, as
// controller.problems_partial, rather than returned as an error: this is the
// page an operator opens when something is wrong, and one failing query used
// to turn the whole of it into a 500. An empty drawer reads as a healthy
// fleet, which is the one thing it must never say by accident.
func (c *Controller) Problems(ctx context.Context) ([]Problem, error) {
	out := make([]Problem, 0, 8)

	// Configuration: every warning and error the validator produced. These are
	// the settings that trade safety for convenience, and they are listed
	// whether or not anything has gone wrong yet. ForUI drops the handful that
	// only the CLI says, because they are expected in a normal deployment and
	// a list that is never clear stops being read.
	for _, f := range c.cfg().Validate().ForUI() {
		if f.Severity != config.SeverityError && f.Severity != config.SeverityWarning {
			continue
		}
		out = append(out, Problem{
			Code: f.Code, Severity: f.Severity, Setting: f.Setting,
			Title: f.Title, Detail: f.Detail, Fix: f.Fix,
		})
	}

	// Every section below is gathered on its own, and a section that fails
	// costs its own contents and nothing else. The alternative -- what this
	// used to do -- is that one failing query returns a 500 and the operator
	// sees an empty problems drawer at the moment they most need it, which
	// reads as "nothing is wrong" rather than as "I could not look".
	var missing []string
	gather := func(what string, fn func(context.Context, *[]Problem) error) {
		if err := fn(ctx, &out); err != nil {
			missing = append(missing, what)
			c.log.Warn("a section of the problems list could not be gathered",
				"section", what, "error", err)
		}
	}

	gather("pool settings", func(ctx context.Context, out *[]Problem) error {
		pools, err := c.st.ListPools(ctx)
		if err != nil {
			return fmt.Errorf("listing pools: %w", err)
		}
		insts, err := c.st.ListInstallations(ctx)
		if err != nil {
			return fmt.Errorf("listing installations: %w", err)
		}
		installations := make(map[string]*store.Installation, len(insts))
		for _, i := range insts {
			installations[i.ID] = i
		}
		for _, p := range pools {
			// The same sentences the pool's own page shows, from the same place.
			*out = append(*out, PoolWarnings(p, installations[p.InstallationID])...)
		}
		return nil
	})

	gather("hosts", c.hostProblems)
	gather("installations", c.installationProblems)
	gather("webhook deliveries", c.webhookProblems)
	gather("jobs", c.jobProblems)
	out = append(out, c.PoolCapacityProblems()...)
	out = append(out, c.PoolRunnerGroupProblems()...)
	out = append(out, c.leaseProblems()...)
	out = append(out, c.loopProblems()...)
	out = append(out, c.updateProblems()...)
	gather("polling", c.pollerProblems)
	gather("runners", c.runnerProblems)
	gather("runner cleanup", c.cleanupProblems)
	gather("stuck runners", c.notProgressingProblems)
	gather("capacity-demand deliveries", c.capacityDeliveryProblems)

	// An incomplete list says so, at the top, in the same shape as everything
	// else on it. A list that quietly drops a section is worse than an error,
	// because it is indistinguishable from a healthy fleet.
	if len(missing) > 0 {
		out = append(out, Problem{
			Code:     "controller.problems_partial",
			Severity: config.SeverityError,
			Title:    "this list is incomplete",
			Detail: "the controller could not read " + strings.Join(missing, ", ") +
				", so problems from " + section(len(missing)) + " are missing here.",
			Fix: "check the controller log for the query that failed, and that the database is readable and not out of disk.",
		})
	}

	// Errors first, then warnings, then a stable order so the list does not
	// reshuffle itself between refreshes.
	slices.SortStableFunc(out, func(a, b Problem) int {
		if r := severityRank(a.Severity) - severityRank(b.Severity); r != 0 {
			return r
		}
		if r := strings.Compare(a.Code, b.Code); r != 0 {
			return r
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out, nil
}

func (c *Controller) capacityDeliveryProblems(ctx context.Context, out *[]Problem) error {
	rows, err := c.st.ListCapacityDemandDeliveries(ctx)
	if err != nil {
		return fmt.Errorf("listing capacity-demand deliveries: %w", err)
	}
	for _, d := range rows {
		if d.DeliveredAt != nil || d.LastError == "" {
			continue
		}
		since := d.AttemptedAt
		*out = append(*out, Problem{Code: "capacity_demand.delivery_failed", Severity: config.SeverityWarning, Title: "external capacity provisioner did not accept the latest event", Detail: fmt.Sprintf("%s after %d attempts (HTTP %d): %s", d.EventType, d.Attempts, d.StatusCode, d.LastError), Fix: "check capacity_demand.destination_url, receiver availability, and the shared signing secret; Zoomies will retry on reconciliation.", TargetKind: "pool", TargetID: d.PoolID, Since: since})
	}
	return nil
}

func severityRank(s config.Severity) int {
	switch s {
	case config.SeverityError:
		return 0
	case config.SeverityWarning:
		return 1
	default:
		return 2
	}
}

func (c *Controller) hostProblems(ctx context.Context, out *[]Problem) error {
	hosts, err := c.st.ListHosts(ctx)
	if err != nil {
		return fmt.Errorf("listing hosts: %w", err)
	}
	queued, err := c.st.ListQueuedJobs(ctx)
	if err != nil {
		return fmt.Errorf("listing queued jobs: %w", err)
	}
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}
	poolByID := make(map[string]*store.Pool, len(pools))
	for _, p := range pools {
		poolByID[p.ID] = p
	}
	now := c.Now()

	for _, h := range hosts {
		// Raised before health and cordon are considered, and without a
		// `continue`: two agents on one host is true whatever else that host
		// is, and it is very often the explanation for the other two.
		if h.AgentSessionAltAt != nil && now.Sub(*h.AgentSessionAltAt) < duplicateAgentWindow {
			since := *h.AgentSessionAltAt
			*out = append(*out, Problem{
				Code:     "host.duplicate_agent",
				Severity: config.SeverityWarning,
				Title:    fmt.Sprintf("two agents appear to be running as host %s", h.Name),
				Detail: fmt.Sprintf("this host's credentials have been used by two agent sessions in turn %s. "+
					"An agent takes a new session each time it starts and never goes back to an old one, so "+
					"alternation means a second agent holds a copy of this host's token -- usually a cloned VM, "+
					"or a state directory copied to another machine. They are splitting this host's tasks between "+
					"them, so each sees only half of its own work.", plural(h.AgentSessionAlternations, "time")),
				Fix: fmt.Sprintf("find the second machine reporting as %s and stop its agent, then re-join it with "+
					"its own join token so it becomes a host of its own (zoomies agent join). Zoomies does not "+
					"refuse either session, because both are running real jobs and picking one would end the other's.", h.Name),
				TargetKind: "host", TargetID: h.ID, Since: &since,
			})
		}
		if !h.Healthy(now) {
			since := h.LastHeartbeat
			severity := config.SeverityWarning
			detail := fmt.Sprintf("no heartbeat for %s; the agent process may be stopped, or it cannot reach this controller.",
				now.Sub(h.LastHeartbeat).Round(time.Second))
			if h.ActiveRunners > 0 {
				// Runners on a silent host are unaccounted for, which is worse
				// than a spare host being down.
				severity = config.SeverityError
				detail = fmt.Sprintf("no heartbeat for %s, with %s recorded on it, so their state is unknown.",
					now.Sub(h.LastHeartbeat).Round(time.Second), plural(h.ActiveRunners, "runner"))
			}
			*out = append(*out, Problem{
				Code:       "host.unhealthy",
				Severity:   severity,
				Title:      fmt.Sprintf("host %s has stopped sending heartbeats", h.Name),
				Detail:     detail,
				Fix:        fmt.Sprintf("check that the zoomies agent is running on %s and can reach %s.", h.Name, c.controllerAddress()),
				TargetKind: "host", TargetID: h.ID, Since: &since,
			})
			continue
		}
		if !h.Cordoned {
			continue
		}
		// Only the queued work this host could take counts against it: a
		// cordoned arm64 host has nothing to do with a queue of jobs for a
		// pool it never offered, and blaming it sends an operator to uncordon
		// a machine that would change nothing.
		couldRun := 0
		for _, j := range queued {
			if p := poolByID[j.PoolID]; p != nil && scheduler.HostOffers(h, p) && scheduler.HostSelects(h, p) {
				couldRun++
			}
		}
		if couldRun > 0 {
			*out = append(*out, Problem{
				Code:       "host.cordoned_with_work",
				Severity:   config.SeverityWarning,
				Title:      fmt.Sprintf("host %s is cordoned with %s queued that it could run", h.Name, plural(couldRun, "job")),
				Detail:     "a cordoned host keeps its runners but accepts no new ones, so its capacity is not available to the queue.",
				Fix:        fmt.Sprintf("uncordon %s on the Hosts page if the maintenance it was cordoned for is over.", h.Name),
				TargetKind: "host", TargetID: h.ID,
			})
		}
	}
	return nil
}

func (c *Controller) installationProblems(ctx context.Context, out *[]Problem) error {
	insts, err := c.st.ListInstallations(ctx)
	if err != nil {
		return fmt.Errorf("listing installations: %w", err)
	}
	for _, i := range insts {
		if i.Healthy() {
			continue
		}
		*out = append(*out, Problem{
			Code:       "installation.unhealthy",
			Severity:   config.SeverityError,
			Title:      fmt.Sprintf("the GitHub App installation for %s is not usable", i.Target),
			Detail:     i.LastError,
			Fix:        "check the App's installation, permissions and private key on the Installations page; no runner can be created for this target until it works.",
			TargetKind: "installation", TargetID: i.ID, Since: i.LastCheckedAt,
		})
	}
	return nil
}

func (c *Controller) webhookProblems(ctx context.Context, out *[]Problem) error {
	since := c.Now().Add(-problemWindow)
	rejected, err := c.st.CountFailedDeliveries(ctx, since)
	if err != nil {
		return fmt.Errorf("counting failed webhook deliveries: %w", err)
	}
	if rejected > 0 {
		*out = append(*out, Problem{
			Code:     "webhook.rejected",
			Severity: config.SeverityWarning,
			Setting:  "github.webhook_path",
			Title:    fmt.Sprintf("%s rejected in the last hour", pluralDeliveries(rejected)),
			Detail:   "a rejected delivery is one whose signature did not verify. Either the App's webhook secret no longer matches the one Zoomies holds, or something other than GitHub is posting to this endpoint.",
			Fix:      "compare the webhook secret on the GitHub App with the one on the Installations page, then use GitHub's Redeliver button.",
			Since:    &since,
		})
	}

	last, err := c.st.LastDeliveryAt(ctx)
	if err != nil {
		return fmt.Errorf("reading the last webhook delivery time: %w", err)
	}
	// An instance with no installation is not yet configured, and telling its
	// operator that no webhook has arrived would be noise on top of the setup
	// they have not finished. Once an installation exists, silence is a fault.
	insts, err := c.st.ListInstallations(ctx)
	if err != nil {
		return fmt.Errorf("listing installations: %w", err)
	}
	if last.IsZero() && len(insts) > 0 {
		p := Problem{
			Code:     "webhook.never_received",
			Severity: config.SeverityWarning,
			Setting:  "server.external_url",
			Title:    "no webhook has ever arrived, so scaling is running on the poller",
			Detail: fmt.Sprintf("Zoomies has never received a delivery, so it is discovering queued jobs by polling GitHub every %s "+
				"instead of within a second of them being queued.", c.pollInterval()),
			Fix: fmt.Sprintf("point the App's webhook at %s and check that GitHub can reach it.", c.webhookURLOrPath()),
		}
		if !c.cfg().GitHub.PollFallback {
			// With no webhooks and no poller, nothing will ever start a runner.
			p.Severity = config.SeverityError
			p.Title = "no webhook has ever arrived and the fallback poller is off, so nothing is scaling"
			p.Detail = "Zoomies has never received a delivery and github.poll_fallback is false, so no queued job will ever be noticed."
			p.Fix = fmt.Sprintf("point the App's webhook at %s, or set github.poll_fallback to true.", c.webhookURLOrPath())
		}
		*out = append(*out, p)
	}
	return nil
}

// controllerAddress is how an agent reaches this controller, phrased for a
// message even when the external URL has not been set.
func (c *Controller) controllerAddress() string {
	if u := c.cfg().Server.ExternalURL; u != "" {
		return u
	}
	return "this controller (server.external_url is not set, so Zoomies cannot name the address)"
}

// webhookURLOrPath names the delivery target, falling back to the path when
// the external URL has not been configured -- which is itself one of the
// findings above, so the fix stays actionable either way.
func (c *Controller) webhookURLOrPath() string {
	if u := c.cfg().WebhookURL(); u != "" {
		return u
	}
	return c.cfg().GitHub.WebhookPath + " (set server.external_url so Zoomies can tell you the full URL)"
}

// PoolCapacityProblems reports the pools the scheduler wanted to grow and could
// not place anywhere. It is exported because the pool's own page shows it too:
// "why is this pool not running anything?" is asked on the pool, not only on
// the Overview.
//
// This is the failure that looks exactly like health: the pool is enabled, its
// labels match the queue, every host is connected, and no runner is ever
// created because none of those hosts offers the pool's backend or matches its
// host selector. Nothing else in the product says so -- a scaling event is only
// written when the size actually moved -- so a fleet in this state answers
// "why is nothing running?" with silence unless it is reported here.
// leaseProblems reports that this controller no longer holds the database's
// lease, which means another one has taken it and both are now scheduling.
//
// An error rather than a warning, and one that never clears by itself: there
// is no state this process can reach on its own that makes it safe again. Two
// controllers over one database mint runner credentials against each other,
// reclaim each other's hosts and reap each other's workloads, and every
// symptom of it looks like a bug somewhere else -- which is the reason to name
// it here rather than leave an operator to work it out.
func (c *Controller) leaseProblems() []Problem {
	held := c.leaseLost.Load()
	if held == nil {
		return nil
	}
	return []Problem{{
		Code:     "controller.lease_lost",
		Severity: config.SeverityError,
		Title:    "another controller has taken this database",
		Detail: fmt.Sprintf("this controller's lease is now held by %s, so two controllers are running against the same database. Both are scheduling: they will mint runners against each other, reclaim each other's hosts and remove each other's workloads.",
			held.Describe()),
		Fix: "stop one of them. If this one is the survivor, restart it with --takeover once the other is gone; a controller that has lost its lease does not take it back by itself, because two processes fighting over it is the same failure twice.",
	}}
}

// PoolRunnerGroupProblems reports the pools whose runners went into GitHub's
// default runner group because the group they asked for could not be resolved.
//
// It is a warning rather than a log line because of what Default means: it is
// the group every repository the installation covers can reach. A pool put
// into a named group to fence its runners off, and quietly placed in Default
// instead, is running its jobs somewhere wider than its operator asked for --
// and nothing else on the page would say so.
func (c *Controller) PoolRunnerGroupProblems() []Problem {
	c.mu.Lock()
	notes := make([]runnerGroupNote, 0, len(c.runnerGroups))
	ids := make([]string, 0, len(c.runnerGroups))
	for id, n := range c.runnerGroups {
		ids = append(ids, id)
		notes = append(notes, n)
	}
	c.mu.Unlock()

	out := make([]Problem, 0, len(notes))
	for i, n := range notes {
		out = append(out, Problem{
			Code:     "pool.runner_group_unresolved",
			Severity: config.SeverityWarning,
			Title:    fmt.Sprintf("pool %s: its runners are registering in the default runner group", n.PoolName),
			Detail: fmt.Sprintf("%s, so GitHub is placing them in Default, which every repository the installation covers can reach. The pool asked for %s to keep its runners away from work that is not meant for them.",
				capitalise(n.Reason), n.Group),
			Fix: fmt.Sprintf("create the %s group on GitHub and give this pool's target access to it, or clear the pool's runner group; the runners already registered stay in Default until they are replaced.",
				n.Group),
			TargetKind: "pool", TargetID: ids[i],
		})
	}
	// A stable order, because the drawer is re-rendered on every pass and a
	// map's is not.
	slices.SortFunc(out, func(a, b Problem) int { return strings.Compare(a.TargetID, b.TargetID) })
	return out
}

func (c *Controller) PoolCapacityProblems() []Problem {
	plan, at := c.getLastPlan()
	if plan == nil || at.IsZero() {
		return nil
	}
	var out []Problem
	for _, pp := range plan.Pools {
		if pp.QuotaDeferredJobs > 0 {
			repositories := strings.Join(pp.QuotaDeferredRepositories, ", ")
			out = append(out, Problem{
				Code:     "pool.repository_scale_up_deferred",
				Severity: config.SeverityWarning,
				Title: fmt.Sprintf("pool %s deferred %s from scaling", pp.PoolName,
					plural(pp.QuotaDeferredJobs, "job")),
				Detail: fmt.Sprintf("The best-effort repository scale-up limit deferred runner creation for %s (%s). Compatible idle runners may still accept these jobs because GitHub controls assignment.",
					plural(len(pp.QuotaDeferredRepositories), "repository"), repositories),
				Fix:        "increase the pool repository scale-up limit or wait for that repository's active jobs to finish; use repository-specific pools and workflow labels if strict isolation is required",
				TargetKind: "pool", TargetID: pp.PoolID,
			})
		}
		if pp.Failing != "" {
			// The scheduler is holding the pool back because its runners keep
			// dying before they register. The failed runners themselves are
			// listed under runners.failed with their messages; this entry is
			// about the pool, and about the wait, which is otherwise invisible.
			severity := config.SeverityWarning
			if pp.QueuedMatched > 0 {
				severity = config.SeverityError
			}
			out = append(out, Problem{
				Code:     "pool.runners_failing",
				Severity: severity,
				Title:    fmt.Sprintf("pool %s's runners are failing to start", pp.PoolName),
				Detail:   pp.Failing,
				Fix: "the failed runners are on the Runners page with their reasons; the usual causes are an image " +
					"that cannot be pulled, a host whose agent cannot reach GitHub, or a runner version that does " +
					"not exist. The pool tries again on its own, less often with each failure.",
				TargetKind: "pool", TargetID: pp.PoolID,
			})
		}
		if pp.Blocked == "" {
			continue
		}
		// Jobs already waiting make this an outage rather than a warning about
		// a pool that is merely unable to reach its minimum -- unless the fleet
		// is simply full, which is the system working and which the next
		// finished job clears on its own.
		severity := config.SeverityWarning
		title := fmt.Sprintf("pool %s cannot start the runners it wants", pp.PoolName)
		switch {
		case pp.BlockedAtCapacity && pp.QueuedMatched > 0:
			title = fmt.Sprintf("pool %s has %s waiting for a host with room",
				pp.PoolName, plural(pp.QueuedMatched, "job"))
		case pp.QueuedMatched > 0:
			severity = config.SeverityError
			title = fmt.Sprintf("pool %s has %s waiting and nowhere to run them",
				pp.PoolName, plural(pp.QueuedMatched, "job"))
		}
		out = append(out, Problem{
			Code:         "pool.no_capacity",
			Severity:     severity,
			Title:        title,
			Detail:       pp.Blocked,
			Fix:          pp.BlockedFix,
			Alternatives: pp.BlockedAlternatives,
			TargetKind:   "pool", TargetID: pp.PoolID,
		})
	}
	return out
}

// plural writes "1 job" and "3 jobs". Titles are read as headings in the
// problems drawer, so "job(s)" is not an option there.
// section keeps the incomplete-list sentence grammatical without a plural
// helper that takes two nouns, which nothing else here needs.
func section(n int) string {
	if n == 1 {
		return "that section"
	}
	return "those sections"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// pluralDeliveries is plural for the one noun whose plural is not an s.
func pluralDeliveries(n int) string {
	if n == 1 {
		return "1 webhook delivery"
	}
	return fmt.Sprintf("%d webhook deliveries", n)
}

func (c *Controller) jobProblems(ctx context.Context, out *[]Problem) error {
	if err := c.lostRunnerProblems(ctx, out); err != nil {
		return err
	}
	all, err := c.unmatchedQueuedJobs(ctx)
	if err != nil {
		return err
	}
	now := c.Now()
	var unmatched []scheduler.UnmatchedJob
	for _, u := range all {
		// A job on GitHub's own runners or a vendor's is theirs to run however
		// long it queues, and a job unclaimed for seconds may be another
		// provider's, about to start there. Neither is this fleet's problem.
		if hostedJob(u.Job.Labels) || now.Sub(u.Job.QueuedAt) < unmatchedGrace {
			continue
		}
		unmatched = append(unmatched, u)
	}
	if len(unmatched) == 0 {
		return nil
	}
	example := unmatched[0].Job
	labels := strings.Join(example.Labels, ", ")
	// The scheduler's reason is only set for a job something less obvious than
	// its labels refused -- a pool advertising exactly those labels but on
	// another installation. Saying "if another provider serves those labels,
	// this is expected" about such a job would send the operator looking in
	// the wrong place entirely.
	tail := " If another runner provider serves those labels, this is expected."
	fix := "create or enable a pool advertising those labels, or change the workflow's runs-on; if another provider takes these jobs, nothing needs doing."
	if reason := unmatched[0].Reason; reason != "" {
		tail = fmt.Sprintf(" %s.", capitalise(reason))
		fix = "point the workflow at a pool on the installation covering that repository, or add a pool there; a pool only ever runs work in its own GitHub target."
	}
	*out = append(*out, Problem{
		Code:     "jobs.unmatched",
		Severity: config.SeverityWarning,
		Title:    fmt.Sprintf("no enabled pool here claims %s", plural(len(unmatched), "queued job")),
		Detail: fmt.Sprintf("if they are meant for this fleet, nothing will run them. The oldest is %s in %s, asking for [%s], queued for %s.%s",
			example.JobName, example.Repo, labels, roundDuration(now.Sub(example.QueuedAt)), tail),
		Fix:        fix,
		TargetKind: "job", TargetID: example.ID, Since: &example.QueuedAt,
	})
	return nil
}

// capitalise upper-cases the first letter of a sentence written to be joined
// onto another. The reasons are written lowercase so that they read inside a
// clause; here one begins a sentence of its own.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// duplicateAgentWindow is how long after the last alternation the duplicate is
// still reported. It ages the problem out on its own: once the second agent is
// stopped the sessions stop swapping, and an operator who has fixed it should
// not have to clear anything by hand.
const duplicateAgentWindow = time.Hour

// unmatchedGrace is how long a queued job no pool here claims is given before
// it is reported. GitHub's own runners take a job within seconds and another
// provider within its own scale-up delay, so a job still unclaimed after this
// long is either meant for this fleet and mislabelled, or nobody's at all.
const unmatchedGrace = 2 * time.Minute

// lostRunnerProblems reports jobs whose runner stopped under them in the last
// hour. GitHub records these as failures like any test failure, and a team
// that sees "CI is flaky" when the fleet is killing their jobs will blame the
// wrong thing; this is where the fleet owns up.
func (c *Controller) lostRunnerProblems(ctx context.Context, out *[]Problem) error {
	faulted, _, err := c.st.ListJobs(ctx, store.JobFilter{FaultedOnly: true},
		store.Page{Limit: 100, Sort: "queued_at", Desc: true})
	if err != nil {
		return fmt.Errorf("listing jobs that lost their runner: %w", err)
	}
	since := c.Now().Add(-problemWindow)
	recent := faulted[:0]
	for _, j := range faulted {
		// Filtered here rather than in SQL because the moment that matters
		// is when the runner went, and the nearest stored stamp to that is
		// the job's completion -- or, for a job GitHub still thinks is
		// running, now.
		if j.CompletedAt == nil || j.CompletedAt.After(since) {
			recent = append(recent, j)
		}
	}
	if len(recent) == 0 {
		return nil
	}
	example := recent[0]
	at := example.StartedAt
	if at == nil {
		at = &example.QueuedAt
	}
	title := fmt.Sprintf("%d jobs lost the runners they were running on in the last hour", len(recent))
	if len(recent) == 1 {
		title = "1 job lost the runner it was running on in the last hour"
	}
	*out = append(*out, Problem{
		Code:     "jobs.runner_lost",
		Severity: config.SeverityWarning,
		Title:    title,
		Detail: fmt.Sprintf("GitHub records these as ordinary failures. The most recent is %s in %s: %s.",
			example.JobName, example.Repo, example.RunnerFault),
		Fix:        "open the job for its timeline and the runner for its last output; a runner that dies mid-job has usually run out of memory or disk, or was removed with force. Re-run the workflow once the cause is fixed.",
		TargetKind: "job", TargetID: example.ID, Since: at,
	})
	return nil
}

// unmatchedQueuedJobs prefers the last scheduler decision, which is computed
// against the pools as they are now; it falls back to the flag stored on each
// job when no pass has run yet, so a fresh controller still reports honestly.
//
// The fallback carries no reason. The stored flag records that nothing claimed
// the job, not what refused it, and inventing one from a row would be a guess.
func (c *Controller) unmatchedQueuedJobs(ctx context.Context) ([]scheduler.UnmatchedJob, error) {
	if plan, at := c.getLastPlan(); plan != nil && !at.IsZero() {
		return plan.Unmatched, nil
	}
	jobs, _, err := c.st.ListJobs(ctx, store.JobFilter{
		States:        []store.JobState{store.JobQueued},
		UnmatchedOnly: true,
	}, store.Page{Limit: 100, Sort: "queued_at"})
	if err != nil {
		return nil, fmt.Errorf("listing unmatched jobs: %w", err)
	}
	out := make([]scheduler.UnmatchedJob, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, scheduler.UnmatchedJob{Job: j})
	}
	return out, nil
}

// loopProblems reports every background loop that has panicked since the
// process started. The loop was restarted, so the fleet is still being run;
// the entry is here because a crash is a bug, and a bug that fixes itself
// after a minute is one nobody would otherwise report.
func (c *Controller) loopProblems() []Problem {
	panics := c.LoopPanics()
	names := slices.Sorted(maps.Keys(panics))
	out := make([]Problem, 0, len(names))
	for _, name := range names {
		p := panics[name]
		title := fmt.Sprintf("the %s loop crashed and was restarted", name)
		if p.Count > 1 {
			title = fmt.Sprintf("the %s loop has crashed %d times and was restarted each time", name, p.Count)
		}
		at := p.At
		out = append(out, Problem{
			Code:     "controller.loop_panicked",
			Severity: config.SeverityError,
			Title:    title,
			Detail:   "the last panic said: " + p.Last,
			Fix: "this is a bug in Zoomies. The stack is in the controller's log under \"controller loop panicked\"; " +
				"please report it with that stack. Restarting the controller clears this entry.",
			Since: &at,
		})
	}
	return out
}

// pollerProblems says whether the safety net is still there.
//
// The fallback poller is what catches a fleet whose webhooks have stopped
// arriving, and its two failure modes are silent by construction: a sweep that
// has stopped happening looks exactly like a sweep with nothing to find, and an
// installation standing down from GitHub's rate limit looks exactly like an
// installation with no queued jobs. Both are only visible in the log, and
// nobody reads the log of a fleet that appears to be fine.
func (c *Controller) pollerProblems(ctx context.Context, out *[]Problem) error {
	if !c.PollerEnabled() {
		// Off is a choice, and the configuration validator already names it.
		// Saying it twice would be the drawer disagreeing with itself.
		return nil
	}
	now := c.Now()

	// Two intervals of grace, the same window pollOnce uses to decide an
	// installation's webhooks are fresh: one missed tick is a slow query, and
	// a threshold that fires on one would cry wolf on every busy controller.
	if last := c.LastPollAt(); !last.IsZero() && now.Sub(last) > 2*c.pollInterval()+c.pollInterval()/2 {
		since := last
		*out = append(*out, Problem{
			Code:     "poller.stale",
			Severity: config.SeverityWarning,
			Setting:  "github.poll_interval",
			Title:    "the fallback poller has stopped sweeping",
			Detail: fmt.Sprintf("the last sweep finished %s ago, and the interval is %s. Until it resumes, a job is only noticed if its webhook arrives.",
				formatAge(now.Sub(last)), c.pollInterval()),
			Fix:   "check the controller's log for the error that ended the sweep; a controller that cannot reach GitHub or its own database logs it there.",
			Since: &since,
		})
	}

	// The hold is per installation -- GitHub's quota is -- so this names the
	// installation rather than the fleet. One organisation spending its hour
	// does not stop the others being polled, and an operator told "the poller
	// is paused" would go looking for a fleet-wide fault that is not there.
	held := c.heldInstallations(now)
	if len(held) == 0 {
		return nil
	}
	insts, err := c.st.ListInstallations(ctx)
	if err != nil {
		return fmt.Errorf("listing installations to name the ones being held: %w", err)
	}
	targets := make(map[string]string, len(insts))
	for _, i := range insts {
		targets[i.ID] = i.Target
	}
	for _, id := range slices.Sorted(maps.Keys(held)) {
		name := targets[id]
		if name == "" {
			name = id
		}
		*out = append(*out, Problem{
			Code:       "poller.paused",
			Severity:   config.SeverityWarning,
			Title:      "GitHub is rate-limiting " + name,
			Detail:     "every background sweep is standing down from this installation until " + held[id].UTC().Format(time.RFC3339) + ". Webhooks still arrive; nothing polls for what they miss until then.",
			Fix:        "nothing to do -- it clears itself. If it keeps happening, the App is spending its hourly quota: raise github.poll_interval, or narrow what this installation covers.",
			TargetKind: "installation",
			TargetID:   id,
		})
	}
	return nil
}

// formatAge renders a duration the way an operator says one out loud.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return plural(int(d.Seconds()), "second")
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	default:
		return plural(int(d.Hours()), "hour")
	}
}

func (c *Controller) runnerProblems(ctx context.Context, out *[]Problem) error {
	// The count comes from the store's aggregate, not from a page of rows: a
	// page is capped, and "100 runners in the failed state" on a fleet with
	// four hundred understates the day it is having.
	counts, err := c.st.CountRunnersByPool(ctx)
	if err != nil {
		return fmt.Errorf("counting failed runners: %w", err)
	}
	total := 0
	for _, pc := range counts {
		total += pc.Failed
	}
	if total == 0 {
		return nil
	}
	failed, _, err := c.st.ListRunners(ctx, store.RunnerFilter{
		States: []store.RunnerState{store.RunnerFailed},
	}, store.Page{Limit: 1, Sort: "created_at", Desc: true})
	if err != nil {
		return fmt.Errorf("listing failed runners: %w", err)
	}
	if len(failed) == 0 {
		return nil
	}
	example := failed[0]
	*out = append(*out, Problem{
		Code:       "runners.failed",
		Severity:   config.SeverityWarning,
		Title:      fmt.Sprintf("%s in the failed state", plural(total, "runner")),
		Detail:     fmt.Sprintf("the most recent is %s: %s", example.Name, example.Message),
		Fix:        "look at the runner's logs on the Runners page; failed runners are cleaned up automatically but the cause is not.",
		TargetKind: "runner", TargetID: example.ID, Since: &example.CreatedAt,
	})
	return nil
}

// cleanupProblems names the runners Zoomies could not finish taking away.
//
// This is a different fault from runners.failed and worth its own code. A
// failed runner is a job that did not run; a runner that will not clean up is
// something *left behind* -- a container holding disk on a host, or a
// registration sitting on somebody's organisation -- and it does not go away
// on its own. Neither had anywhere to be said before: the task result was
// dropped because the row was already terminal, and a failed registration
// delete was a log line.
func (c *Controller) cleanupProblems(ctx context.Context, out *[]Problem) error {
	stuck, err := c.st.RunnersWithFailedCleanup(ctx)
	if err != nil {
		return fmt.Errorf("listing runners whose cleanup failed: %w", err)
	}
	if len(stuck) == 0 {
		return nil
	}
	example := stuck[0]
	// A registration left on GitHub and a container left on a host need
	// different things done about them, so the fix says which this is.
	fix := fmt.Sprintf("look at %s on the Runners page. If the container is still on %s, remove it there; "+
		"Zoomies retries, and the row clears itself when it succeeds.",
		example.Name, c.hostName(ctx, example.HostID))
	if strings.Contains(example.CleanupError, "registration") {
		fix = fmt.Sprintf("check the target's runner settings page for %s. Zoomies retries the deletion every "+
			"ten minutes and the row clears itself when it succeeds; a registration that stays is usually a "+
			"permission the App has lost.", example.Name)
	}
	*out = append(*out, Problem{
		Code:     "runners.cleanup_failed",
		Severity: config.SeverityWarning,
		Title:    fmt.Sprintf("%s could not be cleaned up", plural(len(stuck), "runner")),
		Detail: fmt.Sprintf("%s, after %s: %s. Something is left behind — a container on its host, or a "+
			"registration on GitHub — and it will not go away on its own.",
			example.Name, plural(example.CleanupAttempts, "attempt"), example.CleanupError),
		Fix:        fix,
		TargetKind: "runner", TargetID: example.ID, Since: example.CleanupFailedAt,
	})
	return nil
}

// notProgressingSample bounds the page of starting runners the diagnosis is
// built from. They are read oldest first, and a runner is only a candidate
// once it is old enough, so the page always holds the worst cases; a fleet
// with more than this many stuck at once is told "at least".
const notProgressingSample = 100

// notProgressingAfter is how long a runner may sit in a starting state before
// the fleet says so: half the provision timeout, which is the point at which
// the scheduler will eventually fail it.
//
// It is deliberately not a setting of its own. An operator who raises
// provision_timeout for a slow image pull has already said how long starting up
// is allowed to take here, and a second number to keep in step with the first
// is a second number to get wrong.
func (c *Controller) notProgressingAfter() time.Duration {
	timeout := c.cfg().Scheduler.ProvisionTimeout
	if timeout <= 0 {
		return 0
	}
	return timeout / 2
}

// notProgressingProblems reports runners that are neither coming up nor being
// failed yet, and says which of the two shapes it is.
//
// The distinction is the whole point. Until the provision timeout expires the
// fleet says nothing at all, so an operator watching a pool that creates
// runners which never arrive has to read a runner timeline, the controller log
// and `docker ps` on the host to learn what Zoomies already knows: whether the
// agent ever reported the workload started. A runner still waiting for its
// container is a backend or image problem on the host; one whose container
// started and has not registered is the runner process failing to reach GitHub,
// and they are not fixed in the same place.
func (c *Controller) notProgressingProblems(ctx context.Context, out *[]Problem) error {
	after := c.notProgressingAfter()
	if after <= 0 {
		// provision_timeout is off, so nothing is stuck by anyone's definition;
		// saying otherwise would second-guess a deliberate setting.
		return nil
	}
	starting, _, err := c.st.ListRunners(ctx, store.RunnerFilter{
		States: []store.RunnerState{store.RunnerProvisioning, store.RunnerRegistering},
	}, store.Page{Limit: notProgressingSample, Sort: "created_at"})
	if err != nil {
		return fmt.Errorf("listing runners that are starting up: %w", err)
	}

	now := c.Now()
	var waitingForContainer, notRegistered int
	var oldest *store.Runner
	var since time.Time
	hosts := map[string]bool{}
	for _, r := range starting {
		// The clock that matters starts when the workload did. A runner whose
		// container came up ten seconds ago is registering normally even if the
		// image took four minutes to pull, and failing to make that distinction
		// would raise this on every cold start.
		from := r.CreatedAt
		if r.ContainerStartedAt != nil {
			from = *r.ContainerStartedAt
		}
		if now.Sub(from) < after {
			continue
		}
		if r.ContainerStartedAt == nil {
			waitingForContainer++
		} else {
			notRegistered++
		}
		// Longest stuck by its own clock, which is not the same as first
		// created: a runner that spent four minutes pulling an image and then
		// registered ten seconds ago is younger here than an older one whose
		// container came up first.
		// A batch created in one pass shares a millisecond, and the fix below
		// is written for whichever runner is named here, so a tie has to break
		// the same way every time: the one still waiting for a container
		// first, because that is the earlier failure, then the ID.
		if oldest == nil || from.Before(since) || (from.Equal(since) && stuckBefore(r, oldest)) {
			oldest, since = r, from
		}
		hosts[r.HostID] = true
	}
	stuck := waitingForContainer + notRegistered
	if stuck == 0 {
		return nil
	}

	count := plural(stuck, "runner")
	if stuck == notProgressingSample {
		count = "at least " + count
	}
	detail := fmt.Sprintf("the oldest is %s, %s in %s.", oldest.Name,
		now.Sub(since).Round(time.Second), oldest.State)
	// The fix follows the runner the detail names, not whichever bucket is
	// larger. A detail that names a runner still waiting for its container and
	// a fix that says to read that container's logs sends an operator looking
	// for something that is not there.
	fix := "the container is up but the runner has not registered: look at its logs on the Runners page. " +
		"The usual causes are the host being unable to reach github.com and a JIT configuration GitHub has already consumed."
	if oldest.ContainerStartedAt == nil {
		fix = "no agent has reported the workload started: check the agent log on the host, and that the pool's image exists and can be pulled. " +
			"A first pull of a large image can legitimately take minutes."
	}
	if waitingForContainer > 0 && notRegistered > 0 {
		detail += fmt.Sprintf(" %s waiting for a container to start, %d with a container that started and has not registered.",
			plural(waitingForContainer, "runner"), notRegistered)
	}
	if len(hosts) == 1 && oldest.HostID != "" {
		if h, err := c.st.GetHost(ctx, oldest.HostID); err == nil {
			detail += fmt.Sprintf(" All of them are on host %s.", h.Name)
		}
	}

	*out = append(*out, Problem{
		Code:     "runners.not_progressing",
		Severity: config.SeverityWarning,
		Title: fmt.Sprintf("%s stuck starting up for over %s",
			count, after.Round(time.Second)),
		Detail:     detail,
		Fix:        fix,
		TargetKind: "runner", TargetID: oldest.ID, Since: &since,
	})
	return nil
}

// stuckBefore orders two runners that have been stuck for the same time: a
// runner with no container yet comes before one whose container started, and
// equal shapes fall back to the ID so the choice is stable across passes.
func stuckBefore(a, b *store.Runner) bool {
	if (a.ContainerStartedAt == nil) != (b.ContainerStartedAt == nil) {
		return a.ContainerStartedAt == nil
	}
	return a.ID < b.ID
}

// updateProblems reports that a newer release of Zoomies exists.
//
// It says nothing at all in the two cases where it would otherwise mislead: a
// controller built from main, which is ahead of the newest release rather than
// behind it, and a check that has not yet answered.
//
// The wording claims no ordering. GitHub's "latest release" excludes drafts and
// prereleases, so it is the release an operator should be on, but comparing
// "0.2-beta" with "0.10-beta" properly means a version parser this does not
// have. Naming both and letting the operator read them is honest; guessing
// which is newer is not.
func (c *Controller) updateProblems() []Problem {
	if c.cfg().Updates.CheckInterval <= 0 {
		return nil
	}
	latest := c.latestRelease()
	if latest == nil {
		return nil
	}
	running, ok := releaseVersion(version.Version)
	if !ok {
		return nil
	}
	newest, ok := releaseVersion(latest.Tag)
	if !ok || newest == running {
		return nil
	}
	fix := "upgrade with the same method you installed by; the release notes are at " + latest.URL
	if latest.URL == "" {
		fix = "upgrade with the same method you installed by."
	}
	at := latest.At
	return []Problem{{
		Code:     "controller.update_available",
		Severity: config.SeverityInfo,
		Title:    fmt.Sprintf("the current release of Zoomies is %s; this controller is running %s", latest.Tag, running),
		Detail: "nothing is wrong: runners, pools and jobs are unaffected by the controller's own version. " +
			"This is here so an upgrade is a decision rather than a surprise.",
		Fix:   fix,
		Since: &at,
	}}
}

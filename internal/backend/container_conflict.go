package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// How long a create waits for a name the daemon is still holding, and how it
// spaces its attempts.
//
// Removing a container is asynchronous on both daemons this backend speaks to:
// the delete call returns once the container is marked, and the name leaves the
// daemon's index only when the last of the container's filesystem has gone. A
// privileged docker-in-docker sidecar carries a whole nested image store and its
// anonymous volumes, so that can take seconds. Recovery that gave up inside a
// second therefore failed runners over a name whose container had already been
// removed -- and told the operator to go looking for a duplicate agent that was
// never there.
const (
	nameReleaseBudget   = 20 * time.Second
	nameReleaseFirstGap = 100 * time.Millisecond
	nameReleaseMaxGap   = 2 * time.Second
	// maxOwnedRemovals bounds the other shape of this failure. Removing a
	// container we own and finding the name taken again is not a name waiting to
	// be released: something else is creating it, which is the duplicate agent
	// the operator should be told about rather than raced against forever.
	maxOwnedRemovals = 3
)

// How long a create waits for a create of its own that the daemon has not
// finished, and how long the fact that there was one is remembered.
//
// A create the client stops waiting on is not a create the daemon stops: it
// reserves the name first and registers the container -- the point at which
// an inspect can see it -- only at the very end, and on a host running several
// nested daemons that end can be minutes away. In between, a second create of
// the name is refused with a 409 naming a container that inspects as absent,
// which is exactly what a name leaked by a duplicate agent looks like. Telling
// the two apart takes remembering that the first create was ours, and that is
// what the abandoned-create record is for.
//
// The settle window is anchored on the record rather than on the call that
// meets the 409, because the agent's busy retries re-enter this loop ten,
// twenty and forty seconds apart and must share one window: three minutes
// each would outrun the agent's create budget before the sidecar readiness
// wait and the runner's own create had had their turn. Three minutes is a
// daemon that is slow, not one that is gone; past it the create fails as busy
// so the scheduler steers the replacement to another host. The record outlives
// the window because a retry that arrives after it must still know the create
// was ours -- that is the difference between a busy host and a conflict
// finding -- and fifteen minutes is the whole of the create budget.
const (
	nameSettleBudget      = 3 * time.Minute
	abandonedCreateMemory = 15 * time.Minute
)

// recordAbandonedCreate remembers that a create of name was given up on at the
// given time, unless a live record already says so: a second timeout on the
// same name must not move the window the settle wait is measured from, or a
// daemon that keeps taking longer than the header timeout would be waited on
// for ever. Records the memory has outgrown go at the same time, so an agent
// that runs for months does not carry every name it ever gave up on.
func (b *DockerBackend) recordAbandonedCreate(name string, at time.Time) {
	b.abandonedMu.Lock()
	defer b.abandonedMu.Unlock()
	now := time.Now()
	for n, t := range b.abandoned {
		if now.Sub(t) >= abandonedCreateMemory {
			delete(b.abandoned, n)
		}
	}
	if _, ok := b.abandoned[name]; ok {
		return
	}
	if b.abandoned == nil {
		b.abandoned = make(map[string]time.Time)
	}
	b.abandoned[name] = at
}

// abandonedCreateAt reports when a create of name was given up on, if that was
// recently enough for the daemon to still be finishing it.
func (b *DockerBackend) abandonedCreateAt(name string) (time.Time, bool) {
	b.abandonedMu.Lock()
	defer b.abandonedMu.Unlock()
	at, ok := b.abandoned[name]
	if !ok || time.Since(at) >= abandonedCreateMemory {
		return time.Time{}, false
	}
	return at, true
}

// forgetAbandonedCreate drops the record for each name. A create that has
// been seen through -- answered with a 201, adopted, or removed by any path of
// ours -- leaves the daemon nothing to finish, and a name still held after that
// is the daemon unlinking, which gets the release budget rather than the settle
// window and is never reported as a create it was slow to make.
func (b *DockerBackend) forgetAbandonedCreate(names ...string) {
	b.abandonedMu.Lock()
	defer b.abandonedMu.Unlock()
	for _, n := range names {
		delete(b.abandoned, n)
	}
}

// removeOwnedForCreate frees a name before a create, when the name is held by a
// container this runner owns and is done with. It never adopts: it tidies
// before or after a create rather than standing in for one.
func (b *DockerBackend) removeOwnedForCreate(ctx context.Context, spec Spec, sidecar bool) error {
	_, _, err := b.removeOwnedOccupant(ctx, spec, sidecar, "", nil)
	return err
}

// removeOwnedOccupant never treats a name as proof of ownership. Delete by the
// inspected immutable ID so a replacement taking the name cannot be removed.
//
// occupant is the container the daemon itself named as holding the contested
// name, empty when nothing has named one. It is inspected in preference to the
// name because the two can disagree: a name can still be indexed for a container
// that an inspect by that name no longer resolves, and in that window the ID is
// the only handle left to prove ownership with.
//
// want, when given, is what the caller was about to create. An owned container
// that passes every ownership check, was never started and matches it is handed
// back to be started instead of removed and made again: it is the create this
// agent stopped waiting on, finished late by a slow daemon, and removing it
// would only ask that daemon to make the same thing a second time. It reports
// the adopted container's ID, or whether a container was removed.
func (b *DockerBackend) removeOwnedOccupant(ctx context.Context, spec Spec, sidecar bool, occupant string, want *ContainerCreateRequest) (string, bool, error) {
	name := containerName(spec.Name)
	role := roleRunner
	if sidecar {
		name = dindName(name)
		role = roleDinD
	}
	ref := name
	if occupant != "" {
		ref = occupant
	}
	insp, err := b.api.ContainerInspect(ctx, ref)
	if errors.Is(err, ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	refuse := func(reason string) error {
		return tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: container name %s is occupied; %s; existing container retained", name, reason)}
	}
	if insp.ID == "" || insp.Config == nil || spec.RunnerID == "" || spec.PoolID == "" {
		return "", false, refuse("ownership could not be verified")
	}
	// An ID came from the daemon's own conflict reply, so it must still answer to
	// the name we are contesting. If it has been renamed since, whatever holds the
	// name now is a different container and this one is not ours to remove.
	if occupant != "" && strings.TrimPrefix(insp.Name, "/") != name {
		return "", false, nil
	}
	labels := insp.Config.Labels
	if labels[LabelManaged] != "true" || labels[LabelRunnerID] != spec.RunnerID || labels[LabelPoolID] != spec.PoolID || labels[LabelRole] != role {
		return "", false, refuse("ownership labels do not match this runner, pool and role")
	}
	expectedName := spec.Name
	if sidecar {
		expectedName = dindName(spec.Name)
	}
	if labels[LabelName] != expectedName || (sidecar && labels[LabelDinDFor] != spec.Name) {
		return "", false, refuse("runner name labels do not match")
	}
	if sidecar {
		// A running sidecar from a lost create response is reclaimable only when
		// there is no parent workload. Never disrupt a runner already using it.
		if _, err := b.api.ContainerInspect(ctx, containerName(spec.Name)); err == nil {
			return "", false, refuse("its parent runner still exists")
		} else if !errors.Is(err, ErrNotFound) {
			return "", false, err
		}
	} else if insp.State == nil || insp.State.Running || insp.State.Restarting || insp.State.Paused {
		return "", false, refuse("the runner may still be active")
	}
	if want != nil && adoptable(insp, *want) {
		return insp.ID, false, nil
	}
	if err := b.api.ContainerRemove(ctx, insp.ID, true); err != nil && !errors.Is(err, ErrNotFound) {
		return "", false, err
	}
	// The record is what says a still-held name is a create of ours the daemon
	// has not finished. Removing the container finishes it: whatever holds the
	// name after this is the daemon unlinking, which is a different wait.
	b.forgetAbandonedCreate(name)
	b.log.Info("removed a stale owned container before creation", "container", insp.ID, "runner", spec.RunnerID, "role", role)
	return "", true, nil
}

// adoptable reports whether an owned container can stand in for the one the
// caller was about to create. It must have been created and never started -- a
// runner that ran may have taken a job, and a sidecar that ran has a daemon's
// state a fresh one would not -- from the same image, and where either side
// binds the container into another's network namespace the two must name the
// same one, because a runner bound to a sidecar that has since gone would start
// into a namespace that is not there. That binding is the one network spelling
// this backend chose and both daemons store verbatim. Every other pair is left
// alone: the daemon rewrites those -- an empty request comes back as "default"
// or "bridge", Podman reports "bridge" for a named network -- and the labels
// have already proved whose container it is. A Podman-normalised image name
// fails the comparison and is removed and recreated, which costs a create and
// never a job.
func adoptable(insp *ContainerInspect, want ContainerCreateRequest) bool {
	st := insp.State
	if st == nil || st.Status != "created" || st.Running || st.Restarting || st.Paused {
		return false
	}
	if insp.Config == nil || insp.Config.Image != want.Image || insp.HostConfig == nil {
		return false
	}
	var mode string
	if want.HostConfig != nil {
		mode = want.HostConfig.NetworkMode
	}
	got := insp.HostConfig.NetworkMode
	if strings.HasPrefix(mode, "container:") || strings.HasPrefix(got, "container:") {
		return got == mode
	}
	return true
}

// createWithConflictRecovery resolves the late-create race: a timed-out daemon
// request may acquire its name after the preflight inspection. Credentials are
// not replayed into an existing runner; only an inactive owned runner or an
// owned sidecar without a parent may be removed and freshly created.
//
// A create the daemon did not answer in time is not a create that failed, so it
// is asked again once, and the daemon's own reply says what became of it: a
// 201 means the first never landed, a 409 means it did. A 409 naming a
// container nobody can inspect is then a create the daemon has not finished
// registering, a removal it has not finished, or a name it has leaked, and
// which of those it is decides how long to wait and how to fail: a name with a
// create of ours on record gets the settle window measured from that record
// and fails as busy, so the agent retries and the scheduler steers the
// replacement elsewhere; a name with none gets the release budget and fails as
// the conflict finding it always was. A container that becomes inspectable is
// judged by its labels as before, with one addition -- one that is ours, was
// never started and matches what this call would make is started rather than
// removed and made again. A name held by a container this runner must not
// touch still fails immediately: waiting on somebody else's workload only
// delays the finding an operator has to act on.
func (b *DockerBackend) createWithConflictRecovery(ctx context.Context, spec Spec, cfg ContainerCreateRequest, sidecar bool) (string, error) {
	name := containerName(spec.Name)
	role := roleRunner
	if sidecar {
		name = dindName(name)
		role = roleDinD
	}
	var (
		started  = time.Now()
		released = started
		gap      = nameReleaseFirstGap
		occupant string
		removals int
		reasked  bool
		// refused is the last 409, kept so the daemon's own words survive in
		// whatever this returns.
		refused error
	)
	// deadline is read afresh each time it is needed, because the evidence it
	// comes from moves: a re-ask writes the record and a removal clears it.
	deadline := func() time.Time {
		d := released.Add(b.nameRelease)
		if at, ok := b.abandonedCreateAt(name); ok {
			d = at.Add(b.nameSettle)
		}
		// Waiting past either only produces a runner the agent will refuse to
		// start.
		for _, limit := range []time.Time{spec.StartBefore, spec.Credentials.ExpiresAt} {
			if !limit.IsZero() && limit.Before(d) {
				d = limit
			}
		}
		return d
	}
	wording := func() string {
		waited := time.Since(started)
		if at, ok := b.abandonedCreateAt(name); ok {
			return slowDaemonReport(name, at, waited)
		}
		return conflictReport(name, occupant, removals, waited)
	}
	// exhausted is the exit when the budget runs out. The fault kind is decided
	// by the evidence, because the kind is what acts on it: a live record says
	// the name is held by a create of ours the daemon is too slow to finish,
	// which the agent retries and the scheduler steers away from; anything else
	// -- a settling name with no create of ours behind it, a name still held
	// after our own removal, a name taken again as fast as we free it -- is the
	// conflict finding an operator has to look at.
	exhausted := func() (string, error) {
		failure := fmt.Errorf("backend: %s: %w", wording(), refused)
		if _, ok := b.abandonedCreateAt(name); ok {
			return "", busyErr(failure)
		}
		return "", tagged{kind: ErrContainerConflict, err: failure}
	}
	// ended is the exit for a context that ran out while the name was being
	// waited on. A deadline was spent waiting on the daemon, so it is busy, with
	// the same wording the budget exit would have used; a cancellation is the
	// caller giving up, which is not a finding about the name at all.
	ended := func() (string, error) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", busyErr(fmt.Errorf("backend: %s: %w (%w)", wording(), refused, ctx.Err()))
		}
		return "", ctx.Err()
	}
	for {
		id, err := b.api.ContainerCreate(ctx, name, cfg)
		if err == nil {
			b.forgetAbandonedCreate(name)
			return id, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			// The daemon does not stop a create because the client stopped
			// waiting for it, so a request that timed out is asked once more:
			// the reply to that is what says whether the first landed. A second
			// timeout goes back unchanged, and the agent's busy retry re-enters
			// here with the record still standing. A context that has ended is
			// not the daemon being slow, whatever the transport called it.
			if errors.Is(err, ErrDaemonBusy) && ctx.Err() == nil {
				// Every timeout is a create the daemon may still finish, so
				// every one is recorded; the record refuses to move a live
				// entry, so a second timeout does not widen the window.
				b.recordAbandonedCreate(name, time.Now())
			}
			if errors.Is(err, ErrDaemonBusy) && ctx.Err() == nil && !reasked {
				reasked = true
				b.log.Warn("the daemon did not answer a create in time; asking it again, because a create the client stopped waiting on is one the daemon may still finish",
					"container", name, "runner", spec.RunnerID, "role", role, "error", err)
				continue
			}
			if refused != nil && ctx.Err() != nil {
				return ended()
			}
			return "", err
		}
		refused = err
		if named := conflictOccupant(apiErr.Message); named != "" {
			occupant = named
		}
		adopted, removed, rerr := b.removeOwnedOccupant(ctx, spec, sidecar, occupant, &cfg)
		switch {
		case adopted != "":
			b.forgetAbandonedCreate(name)
			b.log.Info("adopted a container the daemon finished creating after this agent stopped waiting for it",
				"container", shortID(adopted), "runner", spec.RunnerID, "role", role)
			return adopted, nil
		case ctx.Err() != nil:
			return ended()
		case rerr != nil && errors.Is(rerr, ErrDaemonBusy):
			// An inspect or remove the daemon did not answer is the slowness
			// this loop exists to wait out, not a reason to call the name
			// contested: nothing has been learned about who holds it. A daemon
			// answer that cannot be verified -- a 500, a body with no labels --
			// is a different thing and still fails below.
			b.log.Warn("the daemon did not answer in time while the holder of a container name was being checked; waiting rather than treating the name as contested",
				"container", name, "runner", spec.RunnerID, "role", role, "error", rerr)
			if !time.Now().Before(deadline()) {
				return "", fmt.Errorf("backend: could not establish who holds container name %s: %w", name, rerr)
			}
		case rerr != nil:
			// Preserve conflicting containers even when ownership inspection failed.
			return "", tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: could not safely recover container name %s: %w", name, rerr)}
		case removed:
			removals++
			if removals >= maxOwnedRemovals {
				// The removal just cleared the record, so this is always the
				// conflict wording.
				return exhausted()
			}
			// The late-create race is resolved the moment its container has gone,
			// so retry at once and start the release budget from here: what was
			// spent proving ownership is not what the daemon has had to unlink in.
			released, gap = time.Now(), nameReleaseFirstGap
			continue
		default:
			if !time.Now().Before(deadline()) {
				return exhausted()
			}
		}
		timer := time.NewTimer(gap)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ended()
		case <-timer.C:
		}
		if gap *= 2; gap > nameReleaseMaxGap {
			gap = nameReleaseMaxGap
		}
	}
}

// conflictReport says which conflict an operator is looking at, because the
// three have different answers: a name the daemon has not finished releasing
// needs nothing but time, a name taken again after we removed our own container
// is a second agent driving this daemon, and a name held by a container we can
// still point at is one they can go and inspect. A name nobody can inspect only
// reaches here when no create of ours is on record for it -- a create the
// daemon was slow to finish has slowDaemonReport's wording and fails as busy --
// so the duplicate-agent advice is given only when a duplicate agent is what
// the evidence leaves.
func conflictReport(name, occupant string, removals int, waited time.Duration) string {
	spent := waited.Round(100 * time.Millisecond)
	switch {
	case removals >= maxOwnedRemovals:
		return fmt.Sprintf("container name %s was taken again after removing the container holding it %d times; something else is creating containers with this runner's name -- check for duplicate agents sharing this daemon", name, removals)
	case removals > 0:
		return fmt.Sprintf("container name %s was still held %s after the container holding it was removed; the daemon has not finished releasing the name, and the host may be too busy to unlink it", name, spent)
	case occupant != "":
		return fmt.Sprintf("container name %s was still held by container %s after %s; inspect that container's Zoomies ownership labels, and check for duplicate agents sharing this daemon", name, shortID(occupant), spent)
	default:
		return fmt.Sprintf("container name %s remained occupied for %s, but no container of that name could be inspected; check for duplicate agents sharing this daemon", name, spent)
	}
}

// slowDaemonReport is the exit for a name held by a create of our own that the
// daemon has not finished. It names nothing to inspect and no duplicate agent,
// because there is neither: the container the 409 names is the one this agent
// asked for, and the only thing wrong is how long the host is taking to make
// it. A replacement runner always gets a fresh name, so a name the daemon
// eventually leaks costs one runner row and not the job.
func slowDaemonReport(name string, abandonedAt time.Time, waited time.Duration) string {
	ago := time.Since(abandonedAt).Round(100 * time.Millisecond)
	return fmt.Sprintf("container name %s was reserved to a create this agent gave up waiting on %s ago, and the daemon had still not finished it after %s; the host is too slow to create containers -- look at its load, not for a duplicate agent", name, ago, waited.Round(100*time.Millisecond))
}

// conflictOccupant pulls the occupying container's ID out of a 409 reply. Both
// daemons name it -- Docker as `by container "<id>"`, Podman as `by <id>` --
// and it is the one handle that survives a name whose container an inspect by
// that name no longer resolves. It only says which container to check: whether
// that container may be removed is still decided by its ownership labels.
func conflictOccupant(message string) string {
	const marker = "already in use by"
	i := strings.Index(message, marker)
	if i < 0 {
		return ""
	}
	for _, field := range strings.Fields(message[i+len(marker):]) {
		if id := strings.Trim(field, `"'.,:`); isHexID(id) {
			return id
		}
	}
	return ""
}

// isHexID accepts what a daemon prints for a container ID -- the full 64
// characters or a truncated form -- and nothing that merely looks like a word.
func isHexID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

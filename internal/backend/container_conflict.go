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

// removeOwnedForCreate frees a name before a create, when the name is held by a
// container this runner owns and is done with.
func (b *DockerBackend) removeOwnedForCreate(ctx context.Context, spec Spec, sidecar bool) error {
	_, err := b.removeOwnedOccupant(ctx, spec, sidecar, "")
	return err
}

// removeOwnedOccupant never treats a name as proof of ownership. Delete by the
// inspected immutable ID so a replacement taking the name cannot be removed.
//
// occupant is the container the daemon itself named as holding the contested
// name, empty when nothing has named one. It is inspected in preference to the
// name because the two can disagree: a name can still be indexed for a container
// that an inspect by that name no longer resolves, and in that window the ID is
// the only handle left to prove ownership with. It reports whether a container
// was removed.
func (b *DockerBackend) removeOwnedOccupant(ctx context.Context, spec Spec, sidecar bool, occupant string) (bool, error) {
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
		return false, nil
	}
	if err != nil {
		return false, err
	}
	refuse := func(reason string) error {
		return tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: container name %s is occupied; %s; existing container retained", name, reason)}
	}
	if insp.ID == "" || insp.Config == nil || spec.RunnerID == "" || spec.PoolID == "" {
		return false, refuse("ownership could not be verified")
	}
	// An ID came from the daemon's own conflict reply, so it must still answer to
	// the name we are contesting. If it has been renamed since, whatever holds the
	// name now is a different container and this one is not ours to remove.
	if occupant != "" && strings.TrimPrefix(insp.Name, "/") != name {
		return false, nil
	}
	labels := insp.Config.Labels
	if labels[LabelManaged] != "true" || labels[LabelRunnerID] != spec.RunnerID || labels[LabelPoolID] != spec.PoolID || labels[LabelRole] != role {
		return false, refuse("ownership labels do not match this runner, pool and role")
	}
	expectedName := spec.Name
	if sidecar {
		expectedName = dindName(spec.Name)
	}
	if labels[LabelName] != expectedName || (sidecar && labels[LabelDinDFor] != spec.Name) {
		return false, refuse("runner name labels do not match")
	}
	if sidecar {
		// A running sidecar from a lost create response is reclaimable only when
		// there is no parent workload. Never disrupt a runner already using it.
		if _, err := b.api.ContainerInspect(ctx, containerName(spec.Name)); err == nil {
			return false, refuse("its parent runner still exists")
		} else if !errors.Is(err, ErrNotFound) {
			return false, err
		}
	} else if insp.State == nil || insp.State.Running || insp.State.Restarting || insp.State.Paused {
		return false, refuse("the runner may still be active")
	}
	if err := b.api.ContainerRemove(ctx, insp.ID, true); err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	b.log.Info("removed a stale owned container before creation", "container", insp.ID, "runner", spec.RunnerID, "role", role)
	return true, nil
}

// createWithConflictRecovery resolves the late-create race: a timed-out daemon
// request may acquire its name after the preflight inspection. Credentials are
// not replayed into an existing runner; only an inactive owned runner or an
// owned sidecar without a parent may be removed and freshly created.
//
// Removing the occupant is one half of that; waiting for the daemon to release
// the name is the other, and it is the half that decides whether a job runs.
// Nothing here can hurry an unlink, so a name whose container has gone is
// retried until the budget runs out rather than failed on the spot. A name held
// by a container this runner must not touch still fails immediately: waiting on
// somebody else's workload only delays the finding an operator has to act on.
func (b *DockerBackend) createWithConflictRecovery(ctx context.Context, spec Spec, cfg ContainerCreateRequest, sidecar bool) (string, error) {
	name := containerName(spec.Name)
	if sidecar {
		name = dindName(name)
	}
	var (
		started  = time.Now()
		deadline = started.Add(b.nameRelease)
		gap      = nameReleaseFirstGap
		occupant string
		removals int
	)
	for {
		id, err := b.api.ContainerCreate(ctx, name, cfg)
		if err == nil {
			return id, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			return "", err
		}
		if named := conflictOccupant(apiErr.Message); named != "" {
			occupant = named
		}
		removed, rerr := b.removeOwnedOccupant(ctx, spec, sidecar, occupant)
		if rerr != nil {
			// Preserve conflicting containers even when ownership inspection failed.
			return "", tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: could not safely recover container name %s: %w", name, rerr)}
		}
		stalled := func() (string, error) {
			return "", tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: %s: %w", conflictReport(name, occupant, removals, time.Since(started)), err)}
		}
		if removed {
			removals++
			if removals >= maxOwnedRemovals {
				return stalled()
			}
			// The late-create race is resolved the moment its container has gone,
			// so retry at once and start the release budget from here: what was
			// spent proving ownership is not what the daemon has had to unlink in.
			deadline, gap = time.Now().Add(b.nameRelease), nameReleaseFirstGap
			continue
		}
		if !time.Now().Before(deadline) {
			return stalled()
		}
		timer := time.NewTimer(gap)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
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
// still point at is one they can go and inspect.
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

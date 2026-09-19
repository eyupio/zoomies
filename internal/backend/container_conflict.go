package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// removeOwnedForCreate never treats a name as proof of ownership. Delete by
// the inspected immutable ID so a replacement taking the name cannot be removed.
func (b *DockerBackend) removeOwnedForCreate(ctx context.Context, spec Spec, sidecar bool) error {
	name := containerName(spec.Name)
	role := roleRunner
	if sidecar {
		name = dindName(name)
		role = roleDinD
	}
	insp, err := b.api.ContainerInspect(ctx, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	refuse := func(reason string) error {
		return tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: container name %s is occupied; %s; existing container retained", name, reason)}
	}
	if insp.ID == "" || insp.Config == nil || spec.RunnerID == "" || spec.PoolID == "" {
		return refuse("ownership could not be verified")
	}
	labels := insp.Config.Labels
	if labels[LabelManaged] != "true" || labels[LabelRunnerID] != spec.RunnerID || labels[LabelPoolID] != spec.PoolID || labels[LabelRole] != role {
		return refuse("ownership labels do not match this runner, pool and role")
	}
	expectedName := spec.Name
	if sidecar {
		expectedName = dindName(spec.Name)
	}
	if labels[LabelName] != expectedName || (sidecar && labels[LabelDinDFor] != spec.Name) {
		return refuse("runner name labels do not match")
	}
	if sidecar {
		// A running sidecar from a lost create response is reclaimable only when
		// there is no parent workload. Never disrupt a runner already using it.
		if _, err := b.api.ContainerInspect(ctx, containerName(spec.Name)); err == nil {
			return refuse("its parent runner still exists")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
	} else if insp.State == nil || insp.State.Running || insp.State.Restarting || insp.State.Paused {
		return refuse("the runner may still be active")
	}
	if err := b.api.ContainerRemove(ctx, insp.ID, true); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	b.log.Info("removed a stale owned container before creation", "container", insp.ID, "runner", spec.RunnerID, "role", role)
	return nil
}

// createWithConflictRecovery resolves the late-create race: a timed-out daemon
// request may acquire its name after the preflight inspection. Credentials are
// not replayed into an existing runner; only an inactive owned runner or an
// owned sidecar without a parent may be removed and freshly created.
func (b *DockerBackend) createWithConflictRecovery(ctx context.Context, spec Spec, cfg ContainerCreateRequest, sidecar bool) (string, error) {
	name := containerName(spec.Name)
	if sidecar {
		name = dindName(name)
	}
	for attempt := 0; ; attempt++ {
		id, err := b.api.ContainerCreate(ctx, name, cfg)
		if err == nil {
			return id, nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			return "", err
		}
		if attempt == 2 {
			return "", tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: container name %s remains occupied after bounded recovery; inspect ownership and check for duplicate agents sharing this daemon: %w", name, err)}
		}
		if err := b.removeOwnedForCreate(ctx, spec, sidecar); err != nil {
			// Preserve conflicting containers even when ownership inspection failed.
			return "", tagged{kind: ErrContainerConflict, err: fmt.Errorf("backend: could not safely recover container name %s: %w", name, err)}
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

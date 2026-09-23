package store

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"
)

// HostIncidents is what has recently gone wrong on a host's own machinery, as
// opposed to what the controller decided about it (the throttle) or what the
// host measured (the usage).
//
// Both halves used to exist only somewhere an operator could not see them.
// The runtime cooldown lived in the agent's memory and surfaced as one log
// line on the host; an image that would not pull reached the controller as a
// failed runner, which is cleaned up and gone within minutes, so a host whose
// egress blocks a registry showed only runners that never registered. Kept on
// the host row, each one survives a controller restart and is on the host
// card and in the problems drawer until the evidence says it is over.
//
// The two halves are written by different paths -- the heartbeat and a task
// result -- so each is set by its own statement on its own JSON path and one
// never writes the other back stale.
type HostIncidents struct {
	Runtime   *RuntimeIncident   `json:"runtime,omitempty"`
	ImagePull *ImagePullIncident `json:"image_pull,omitempty"`
}

// RuntimeIncident is the agent's container-runtime cooldown as the controller
// last heard it: the runtime failed, new starts are held, and one recovery
// attempt is due at RetryAt.
type RuntimeIncident struct {
	// Failures is how many runtime failures in a row, capped by the agent.
	Failures int `json:"failures"`
	// Kind is "unavailable" (the daemon could not be reached) or "timeout"
	// (it was reached and did not answer in time).
	Kind  string `json:"kind"`
	Error string `json:"error,omitempty"`
	// RetryAt is on the controller's clock: the agent sends how long is left
	// rather than a time, so a host whose clock is wrong cannot move it.
	RetryAt time.Time `json:"retry_at"`
	// Since is when the controller first heard of this episode, and
	// ObservedAt when it last heard anything new about it. Neither is
	// rewritten on a beat that says nothing new.
	Since      time.Time `json:"since"`
	ObservedAt time.Time `json:"observed_at"`
}

// ImagePullIncident is the last start or prewarm on a host that failed
// because its image could not be made ready, named by the registry it comes
// from and the pool that asked for it.
type ImagePullIncident struct {
	PoolID   string `json:"pool_id"`
	Pool     string `json:"pool"`
	Image    string `json:"image"`
	Registry string `json:"registry"`
	// Source is "start" or "prewarm": which of the agent's results said so.
	Source     string    `json:"source"`
	Error      string    `json:"error,omitempty"`
	Since      time.Time `json:"since"`
	ObservedAt time.Time `json:"observed_at"`
}

func (h HostIncidents) Value() (driver.Value, error) { return marshalJSON(h) }

func (h *HostIncidents) Scan(value any) error {
	*h = HostIncidents{}
	switch v := value.(type) {
	case string:
		return unmarshalJSON(v, h)
	case []byte:
		return unmarshalJSON(string(v), h)
	case nil:
		// A row from before the column, or one nothing has happened to.
		return nil
	default:
		return fmt.Errorf("host incidents: unexpected database value %T", value)
	}
}

// SetHostRuntimeIncident records or, with nil, clears a host's runtime
// cooldown without touching the image-pull half of the column.
func (s *Store) SetHostRuntimeIncident(ctx context.Context, id string, inc *RuntimeIncident) error {
	if inc == nil {
		return s.setHostIncident(ctx, id, "$.runtime", nil)
	}
	return s.setHostIncident(ctx, id, "$.runtime", inc)
}

// SetHostImagePullIncident records or, with nil, clears a host's image-pull
// incident without touching the runtime half of the column.
func (s *Store) SetHostImagePullIncident(ctx context.Context, id string, inc *ImagePullIncident) error {
	if inc == nil {
		return s.setHostIncident(ctx, id, "$.image_pull", nil)
	}
	return s.setHostIncident(ctx, id, "$.image_pull", inc)
}

// setHostIncident takes an untyped nil to clear, which is why both callers
// above unwrap their typed nil first.
func (s *Store) setHostIncident(ctx context.Context, id, path string, inc any) error {
	query := `UPDATE hosts SET incidents=json_remove(COALESCE(incidents,'{}'), ?) WHERE id=?`
	args := []any{path, id}
	if inc != nil {
		body, err := marshalJSON(inc)
		if err != nil {
			return err
		}
		query = `UPDATE hosts SET incidents=json_set(COALESCE(incidents,'{}'), ?, json(?)) WHERE id=?`
		args = []any{path, body, id}
	}
	res, err := s.exec(ctx, query, args...)
	if err != nil {
		return wrapWrite(err)
	}
	return affected(res, "host", id)
}

package store

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"
)

// HostThrottle is how far the controller has stepped a host down after its
// measurements said it was overwhelmed, and why.
//
// It is a ladder rather than a switch. The pressure holds refuse every new
// start while CPU or memory is acutely short, and release the moment a sample
// says otherwise; a host that is overwhelmed on and off for an hour spends that
// hour bouncing in and out of them, taking a fresh runner each time it is let
// back in. The throttle is what outlasts a sample: each rung takes a quarter
// of the host's slots and, down to half, a quarter of every runner's CPU
// quota; it climbs while the pressure keeps coming back, and it comes down one
// rung at a time after a stretch of calm long enough to mean something.
//
// The controller owns every field. A heartbeat carries the measurements the
// decision is made from and never the decision, so a host cannot talk its way
// off its own rung.
type HostThrottle struct {
	// Level is the rung, 0 for none up to MaxThrottleLevel.
	Level int `json:"level,omitempty"`
	// Since is when this episode started: the first step up from zero.
	Since *time.Time `json:"since,omitempty"`
	// ChangedAt is the last step in either direction, which is what the next
	// step up is paced from.
	ChangedAt *time.Time `json:"changed_at,omitempty"`
	// CalmSince is the start of the calm streak the next step down waits on,
	// nil while the host is not calm.
	CalmSince *time.Time `json:"calm_since,omitempty"`
	// Reason is the sentence for the last step up: which measurement did it.
	Reason string `json:"reason,omitempty"`
}

// MaxThrottleLevel is the top rung. Three rungs leave a host a quarter of its
// slots; a fourth would take it to nothing, and a host taking nothing is a
// cordon, which is the operator's to apply and the operator's to see.
const MaxThrottleLevel = 3

// throttleStepFraction is what one rung takes of the slots, and of every
// runner's CPU quota down to MinCPUFactor.
const throttleStepFraction = 0.25

// Active reports whether the host is on any rung at all.
func (t HostThrottle) Active() bool { return t.Level > 0 }

// Factor is the share of a host's slots the current rung leaves: 1 on no
// rung, then 0.75, 0.5 and 0.25.
func (t HostThrottle) Factor() float64 {
	level := min(max(t.Level, 0), MaxThrottleLevel)
	return 1 - throttleStepFraction*float64(level)
}

// MinCPUFactor is the least share of its CPU quota a running job is ever left
// with. Half speed doubles a job's time, which the timeout-minutes most
// workflows set survives; a quarter turns "slow" into "timed out", and a
// throttle that made jobs fail would be doing the thing it exists to prevent.
// The top rung therefore takes slots and nothing else.
const MinCPUFactor = 0.5

// CPUFactor is the share of its CPU quota each runner on the host is left:
// the rung's factor, floored at MinCPUFactor.
func (t HostThrottle) CPUFactor() float64 {
	return max(MinCPUFactor, t.Factor())
}

func (t HostThrottle) Value() (driver.Value, error) { return marshalJSON(t) }

func (t *HostThrottle) Scan(value any) error {
	*t = HostThrottle{}
	switch v := value.(type) {
	case string:
		return unmarshalJSON(v, t)
	case []byte:
		return unmarshalJSON(string(v), t)
	case nil:
		return nil
	default:
		return fmt.Errorf("host throttle: unexpected database value %T", value)
	}
}

// SetHostThrottle records the controller's decision for one host.
//
// Its own statement, like the reserve and the usage: the path a heartbeat's
// facts take through UpdateHost and SetHostReported cannot reach this column,
// and an operator's PATCH cannot either. The throttle is decided from what a
// host measured, never written by what a host said.
func (s *Store) SetHostThrottle(ctx context.Context, id string, throttle HostThrottle) error {
	res, err := s.exec(ctx, `UPDATE hosts SET throttle=? WHERE id=?`, throttle, id)
	if err != nil {
		return wrapWrite(err)
	}
	return affected(res, "host", id)
}

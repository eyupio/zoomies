package store

import (
	"context"
	"database/sql/driver"
	"fmt"
	"math"
	"time"
)

// HostUsage is a recent whole-host measurement, separate from reservations.
// Pointers distinguish an idle/full host from a host that could not measure.
// The controller owns the timestamps and the sustained-CPU hold.
type HostUsage struct {
	CPUPercent        *float64 `json:"cpu_percent,omitempty"`
	MemoryAvailableMB *int64   `json:"memory_available_mb,omitempty"`
	// LoadAverage1 is the kernel's one-minute load average for the whole
	// machine. CPU occupancy stops at 100%; this keeps counting the tasks
	// queued behind the cores, so it is what says how far past its size a
	// host has been pushed rather than merely that it is busy.
	LoadAverage1 *float64   `json:"load_average_1m,omitempty"`
	SampledAt    time.Time  `json:"sampled_at,omitempty"`
	CPUHighSince *time.Time `json:"cpu_high_since,omitempty"`
	CPUHeld      bool       `json:"cpu_held,omitempty"`
}

const HostUsageMaxAge = 90 * time.Second

// CPUHoldWindow is how long CPU has to sit at or above 95% before new starts
// are held. It is a constant rather than a literal because the throttle
// ladder is paced against it: a rung may not be climbed faster than the hold
// that feeds it can trip, and internal/controller pins the two together.
const CPUHoldWindow = 30 * time.Second

func (u HostUsage) Fresh(now time.Time) bool {
	return !u.SampledAt.IsZero() && !now.Before(u.SampledAt) && now.Sub(u.SampledAt) < HostUsageMaxAge
}

// ObserveHostUsage validates just the measurements an agent may supply. A
// forged timestamp cannot keep a reading fresh, or shorten a pressure hold.
// Sustained CPU pressure pauses admission after 30 seconds above 95%; a
// reading below 85% releases it. Busy runners are never changed by this state.
func ObserveHostUsage(previous, measured HostUsage, memoryMB int64, now time.Time) HostUsage {
	u := HostUsage{SampledAt: now}
	if v := measured.CPUPercent; v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 100 {
		value := *v
		u.CPUPercent = &value
		if previous.Fresh(now) && previous.CPUPercent != nil {
			u.CPUHeld = previous.CPUHeld && value >= 85
		}
		if value >= 95 {
			since := now
			if previous.Fresh(now) && previous.CPUHighSince != nil {
				since = *previous.CPUHighSince
			}
			u.CPUHighSince = &since
			u.CPUHeld = u.CPUHeld || now.Sub(since) >= CPUHoldWindow
		}
	}
	if v := measured.MemoryAvailableMB; v != nil && *v >= 0 && memoryMB > 0 && *v <= memoryMB {
		value := *v
		u.MemoryAvailableMB = &value
	}
	// A load average has no ceiling -- that is the point of it -- but it is
	// never negative, and a NaN or an infinity is an agent that read
	// something other than /proc/loadavg. A figure that is only a load
	// average, with no CPU and no memory, is still a measurement.
	if v := measured.LoadAverage1; v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 {
		value := *v
		u.LoadAverage1 = &value
	}
	if u.CPUPercent == nil && u.MemoryAvailableMB == nil && u.LoadAverage1 == nil {
		return HostUsage{}
	}
	return u
}

func (u HostUsage) Value() (driver.Value, error) { return marshalJSON(u) }

func (u *HostUsage) Scan(value any) error {
	*u = HostUsage{}
	switch v := value.(type) {
	case string:
		return unmarshalJSON(v, u)
	case []byte:
		return unmarshalJSON(string(v), u)
	case nil:
		return nil
	default:
		return fmt.Errorf("host usage: unexpected database value %T", value)
	}
}

// SetHostUsage cannot write an operator's capacity, cordon or reserves.
func (s *Store) SetHostUsage(ctx context.Context, id string, usage HostUsage) error {
	res, err := s.exec(ctx, `UPDATE hosts SET usage=? WHERE id=?`, usage, id)
	if err != nil {
		return wrapWrite(err)
	}
	return affected(res, "host", id)
}

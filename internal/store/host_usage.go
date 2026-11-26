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
	// LentCPUPercent is the part of CPUPercent that runners were using out of
	// CPU elastic CPU lent them, as a share of the whole host. The controller
	// works it out from the same heartbeat's runner samples; whatever an agent
	// put here is replaced. A boost doing its job is load the plan chose, and
	// judging admission and the throttle on it would have a boost trip the
	// very hold that then withdraws it.
	LentCPUPercent float64 `json:"lent_cpu_percent,omitempty"`
	// IOWaitPercent is the share of CPU time the machine spent idle with
	// something waiting on disk, as the agent measured it between heartbeats.
	// CPUPercent counts that time as busy, so a build stalled on a saturated disk
	// reads as a host hard at work; this is the figure that says otherwise.
	IOWaitPercent *float64 `json:"io_wait_percent,omitempty"`
	// StealPercent is the share of CPU time the hypervisor kept from this machine,
	// as the agent measured it between heartbeats. CPUPercent counts that time as
	// busy, so a virtual machine that is being starved reads as one hard at work;
	// this is the figure that says otherwise. It is a measurement only: nothing
	// the scheduler decides is judged on it, so a host's room is still what the
	// busy figure says it is.
	StealPercent *float64 `json:"steal_percent,omitempty"`
	// IOWaitHighSince is when I/O wait rose to IOWaitHighPercent and has not
	// fallen below IOWaitCalmPercent since. The controller owns it, for the reason
	// it owns CPUHighSince: an agent that sent its own could keep a host looking
	// disk-bound, or never.
	IOWaitHighSince *time.Time `json:"io_wait_high_since,omitempty"`
	// CPU is the set of CPU figures this measurement was judged by. The
	// controller stamps it from its settings on every observation, so a
	// setting changed while the controller runs takes effect on the next
	// heartbeat, and everything that reads the hold afterwards (admission,
	// paced starts, the throttle ladder, elastic CPU) reads the same figures
	// the hold was decided on. A row written before the figures existed
	// carries none and is read with the defaults.
	CPU CPUThresholds `json:"cpu_thresholds,omitzero"`
}

// CPUThresholds are the CPU occupancy figures a host is judged by, each as a
// share of the whole machine with the CPU lent to runners taken out. A zero
// field means its default, which is what the hold did before the figures
// could be set.
type CPUThresholds struct {
	// HoldPercent is the occupancy at which the clock towards a hold on new
	// starts begins: 95 by default.
	HoldPercent float64 `json:"hold_percent,omitempty"`
	// HoldWindow is how long occupancy has to stay at or above HoldPercent
	// before new starts are held: 30 seconds by default.
	HoldWindow time.Duration `json:"hold_window,omitempty"`
	// ReleasePercent is the occupancy below which a hold is lifted, and the
	// line under which a throttled host counts as calm, so the two never
	// disagree about whether a host has recovered: 85 by default.
	ReleasePercent float64 `json:"release_percent,omitempty"`
	// PacedPercent is the occupancy at or above which runners are started a
	// few at a time, each waiting for the one before to come up: 85 by
	// default.
	PacedPercent float64 `json:"paced_percent,omitempty"`
	// ElasticBusyPercent is the occupancy at or above which a host lends its
	// runners no spare CPU: 85 by default.
	ElasticBusyPercent float64 `json:"elastic_busy_percent,omitempty"`
}

// Default CPU figures, the ones the hold used before they could be set.
const (
	DefaultCPUHoldPercent        = 95.0
	DefaultCPUHoldWindow         = 30 * time.Second
	DefaultCPUReleasePercent     = 85.0
	DefaultCPUPacedPercent       = 85.0
	DefaultCPUElasticBusyPercent = 85.0
)

// OrDefault is t with every unset figure replaced by its default.
func (t CPUThresholds) OrDefault() CPUThresholds {
	if t.HoldPercent <= 0 {
		t.HoldPercent = DefaultCPUHoldPercent
	}
	if t.HoldWindow <= 0 {
		t.HoldWindow = DefaultCPUHoldWindow
	}
	if t.ReleasePercent <= 0 {
		t.ReleasePercent = DefaultCPUReleasePercent
	}
	if t.PacedPercent <= 0 {
		t.PacedPercent = DefaultCPUPacedPercent
	}
	if t.ElasticBusyPercent <= 0 {
		t.ElasticBusyPercent = DefaultCPUElasticBusyPercent
	}
	return t
}

// Thresholds is the set of CPU figures this measurement is judged by.
func (u HostUsage) Thresholds() CPUThresholds { return u.CPU.OrDefault() }

// UnlentCPUPercent is the host's CPU less what runners were using of CPU lent
// to them: the load the plan did not account for. Nil where CPU was not
// measured.
func (u HostUsage) UnlentCPUPercent() *float64 {
	if u.CPUPercent == nil {
		return nil
	}
	v := max(*u.CPUPercent-max(u.LentCPUPercent, 0), 0)
	return &v
}

const HostUsageMaxAge = 90 * time.Second

// MaxCPUHoldWindow is the longest the CPU hold may wait before it trips. The
// throttle ladder is paced against the hold: a rung may not be climbed faster
// than the hold that feeds it can trip, so the window can be no longer than
// the ladder's step, and internal/controller pins the two together.
const MaxCPUHoldWindow = 2 * time.Minute

// I/O wait is judged the way CPU is: one threshold to start the clock and a
// lower one to stop it, so a host hovering around the line is one sustained
// condition rather than a dozen short ones. The advice that rests on it -- that
// a pool's scratch folders could live in memory -- is persistent, and a clock
// that reset on every dip would never reach IOWaitSustained on a busy disk,
// which dips constantly.
const (
	IOWaitHighPercent = 20.0
	IOWaitCalmPercent = 10.0
	// IOWaitSustained is how long a host has to have been waiting on disk before
	// anything is said about it. A build's checkout is a burst of I/O, and a
	// notification for each one would teach an operator to ignore the list.
	IOWaitSustained = 10 * time.Minute
)

// DiskBound reports whether this host has been waiting on its disk, steadily,
// for long enough to say so.
func (u HostUsage) DiskBound(now time.Time) bool {
	return u.Fresh(now) && u.IOWaitHighSince != nil && now.Sub(*u.IOWaitHighSince) >= IOWaitSustained
}

func (u HostUsage) Fresh(now time.Time) bool {
	return !u.SampledAt.IsZero() && !now.Before(u.SampledAt) && now.Sub(u.SampledAt) < HostUsageMaxAge
}

// ObserveHostUsage validates just the measurements an agent may supply. A
// forged timestamp cannot keep a reading fresh, or shorten a pressure hold.
// Sustained CPU pressure pauses admission once CPU has stayed at or above
// cpu.HoldPercent for cpu.HoldWindow; a reading below cpu.ReleasePercent
// releases it. The defaults are 95%, 30 seconds and 85%. Busy runners are
// never changed by this state.
//
// lentPercent is the controller's own figure for the share of the host that
// runners were using out of lent CPU, and both thresholds are judged without
// it. The recorded CPUPercent stays the raw measurement.
func ObserveHostUsage(previous, measured HostUsage, lentPercent float64, memoryMB int64, now time.Time, cpu CPUThresholds) HostUsage {
	cpu = cpu.OrDefault()
	u := HostUsage{SampledAt: now, CPU: cpu}
	if v := measured.CPUPercent; v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 100 {
		raw := *v
		u.CPUPercent = &raw
		if !math.IsNaN(lentPercent) && lentPercent > 0 {
			u.LentCPUPercent = min(lentPercent, raw)
		}
		value := *u.UnlentCPUPercent()
		if previous.Fresh(now) && previous.CPUPercent != nil {
			u.CPUHeld = previous.CPUHeld && value >= cpu.ReleasePercent
		}
		if value >= cpu.HoldPercent {
			since := now
			if previous.Fresh(now) && previous.CPUHighSince != nil {
				since = *previous.CPUHighSince
			}
			u.CPUHighSince = &since
			u.CPUHeld = u.CPUHeld || now.Sub(since) >= cpu.HoldWindow
		}
	}
	if v := measured.MemoryAvailableMB; v != nil && *v >= 0 && memoryMB > 0 && *v <= memoryMB {
		value := *v
		u.MemoryAvailableMB = &value
	}
	if v := measured.IOWaitPercent; v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 100 {
		wait := *v
		u.IOWaitPercent = &wait
		held := previous.Fresh(now) && previous.IOWaitHighSince != nil
		switch {
		case wait >= IOWaitHighPercent && held:
			since := *previous.IOWaitHighSince
			u.IOWaitHighSince = &since
		case wait >= IOWaitHighPercent:
			since := now
			u.IOWaitHighSince = &since
		case wait >= IOWaitCalmPercent && held:
			since := *previous.IOWaitHighSince
			u.IOWaitHighSince = &since
		}
	}
	// Steal is part of what CPUPercent calls busy, taken from the same interval,
	// so it is only a figure beside a CPU one and can never be more than it. One
	// that is would be an agent that read something other than /proc/stat, and is
	// dropped rather than recorded; a tenth of a point is rounding.
	if v := measured.StealPercent; v != nil && u.CPUPercent != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) &&
		*v >= 0 && *v <= 100 && *v <= *u.CPUPercent+0.1 {
		taken := *v
		u.StealPercent = &taken
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

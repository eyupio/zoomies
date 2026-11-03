package controller

import (
	"sync/atomic"
	"time"

	"github.com/eyupio/zoomies/internal/machine"
	"github.com/eyupio/zoomies/internal/store"
)

// ControllerMachineUsage is what the controller measured of the machine it runs on: the
// whole machine, including work that is not Zoomies', as an agent's usage is.
type ControllerMachineUsage struct {
	CPUPercent *float64 `json:"cpu_percent,omitempty"`
	// IOWaitPercent and StealPercent are shares of the same interval as the CPU
	// figure, which counts both as busy: time idle with something waiting on a
	// disk, and time the hypervisor kept the CPU from a virtual machine.
	IOWaitPercent     *float64  `json:"io_wait_percent,omitempty"`
	StealPercent      *float64  `json:"steal_percent,omitempty"`
	LoadAverage1      *float64  `json:"load_average_1m,omitempty"`
	MemoryAvailableMB *int64    `json:"memory_available_mb,omitempty"`
	SampledAt         time.Time `json:"sampled_at"`
}

// ControllerMachineView is the machine the controller itself runs on, as its procfs
// describes it. A controller with an embedded agent is also a host, and that host
// reports the same figures through the hosts routes; this is for the controller
// whose agents are all elsewhere, which has no host of its own to read the
// machine from and would otherwise be a machine nobody can see.
type ControllerMachineView struct {
	CPUs     int   `json:"cpus,omitempty"`
	MemoryMB int64 `json:"memory_mb,omitempty"`
	// Usage is absent until the controller has two samples to take a share of, on
	// a platform with no procfs, and when every figure failed its checks.
	Usage *ControllerMachineUsage `json:"usage,omitempty"`
	// UsageFresh says there is a recent measurement behind Usage, judged as a
	// host's is: a controller that has stopped sampling must not read as one whose
	// last figure still stands.
	UsageFresh bool `json:"usage_fresh"`
}

// machineProbe samples the machine the controller runs on at the housekeeping
// tick and remembers the last answer for the route that serves it.
type machineProbe struct {
	sampler machine.UsageSampler
	// read is a seam for a test, which has no procfs of its own to describe; the
	// real one reads this machine's.
	read func() (cpus int, memoryMB int64, usage machine.Usage, ok bool)
	last atomic.Pointer[ControllerMachineView]
}

func (p *machineProbe) sample() (int, int64, machine.Usage, bool) {
	if p.read != nil {
		return p.read()
	}
	cpus, memoryMB, ok := p.sampler.Machine()
	if !ok {
		return 0, 0, machine.Usage{}, false
	}
	return cpus, memoryMB, p.sampler.Sample(cpus, memoryMB), true
}

// sampleMachine takes one sample of the machine the controller runs on. Every
// figure goes through the checks an agent's does, so a reading that cannot be
// true (a share outside 0 to 100, steal with no CPU figure beside it, more steal
// than busy time) is dropped here as it is there, and the two routes cannot
// disagree about what is possible.
func (c *Controller) sampleMachine() {
	cpus, memoryMB, u, ok := c.machine.sample()
	if !ok {
		c.machine.last.Store(nil)
		return
	}
	now := c.Now()
	seen := store.ObserveHostUsage(store.HostUsage{}, store.HostUsage{
		CPUPercent: u.CPUPercent, MemoryAvailableMB: u.MemoryAvailableMB, LoadAverage1: u.LoadAverage1,
		IOWaitPercent: u.IOWaitPercent, StealPercent: u.StealPercent,
	}, 0, memoryMB, now)
	view := &ControllerMachineView{CPUs: cpus, MemoryMB: memoryMB}
	if !seen.SampledAt.IsZero() && (seen.CPUPercent != nil || seen.MemoryAvailableMB != nil || seen.LoadAverage1 != nil) {
		view.Usage = &ControllerMachineUsage{CPUPercent: seen.CPUPercent, IOWaitPercent: seen.IOWaitPercent, StealPercent: seen.StealPercent,
			LoadAverage1: seen.LoadAverage1, MemoryAvailableMB: seen.MemoryAvailableMB, SampledAt: seen.SampledAt}
	}
	c.machine.last.Store(view)
}

// ControllerMachine is the last reading of the machine the controller runs on. It is read
// at request time and never sampled there: the figures are shares of an interval
// between two samples, and a request would otherwise set the interval.
func (c *Controller) ControllerMachine() ControllerMachineView {
	v := c.machine.last.Load()
	if v == nil {
		return ControllerMachineView{}
	}
	out := *v
	if out.Usage != nil {
		out.UsageFresh = store.HostUsage{SampledAt: out.Usage.SampledAt}.Fresh(c.Now())
	}
	return out
}

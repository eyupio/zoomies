package scheduler

import (
	"math/bits"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/store"
)

// ExecutionPlatform reports the platform the selected backend knows about.
// Keep the native kernel even when the distro is outside the naming catalogue.
func ExecutionPlatform(p *store.Pool, h *store.Host) (os, arch string) {
	if p.Backend == store.BackendProcess {
		return firstNonEmpty(h.OS, h.Distro), h.Arch
	}
	if runtime, ok := h.BackendInfo.Find(p.Backend); ok {
		return runtime.OS, runtime.Arch
	}
	return "", ""
}

// RunnerKernel accepts a kernel from the runtime or a distribution from a pool.
func RunnerKernel(os string) string {
	if strings.EqualFold(strings.TrimSpace(os), "linux") {
		return "linux"
	}
	return naming.Kernel(os)
}

var platformLabels = [...]string{"linux", "macos", "windows", "x64", "arm64", "arm"}

type jobPlatform uint8

type placementDemand struct {
	job      *store.Job
	platform jobPlatform
}

func placementFor(p *store.Pool, j *store.Job) placementDemand {
	d := placementDemand{job: j}
	for _, raw := range j.Labels {
		label := store.NormalizeLabel(raw)
		if i := slices.Index(platformLabels[:], label); i >= 0 && !slices.Contains(p.Labels, label) {
			d.platform |= 1 << i
		}
	}
	return d
}

func (want jobPlatform) labels() string {
	var labels []string
	for i, label := range platformLabels {
		if want&(1<<i) != 0 {
			labels = append(labels, label)
		}
	}
	return strings.Join(labels, ", ")
}

func (want jobPlatform) matches(p *store.Pool, h *store.Host) bool {
	if want == 0 || h == nil {
		return true
	}
	os, arch := ExecutionPlatform(p, h)
	kernel, archLabel := RunnerKernel(os), naming.ArchLabel(arch)
	if declared := RunnerKernel(p.Platform.OS); declared != "" {
		if p.Ephemeral && kernel != "" && kernel != declared {
			return false
		}
		kernel = firstNonEmpty(kernel, declared)
	}
	if declared := naming.ArchLabel(p.Platform.Arch); declared != "" {
		if p.Ephemeral && archLabel != "" && archLabel != declared {
			return false
		}
		archLabel = firstNonEmpty(archLabel, declared)
	}
	if p.Ephemeral && kernel == "" && p.Backend != store.BackendProcess {
		kernel = "linux"
	}
	if archLabel == "" {
		archLabel = naming.ArchLabel(h.Arch)
	}
	for i, label := range platformLabels {
		if want&(1<<i) == 0 {
			continue
		}
		reported := archLabel
		if osLabels[label] {
			reported = kernel
		}
		// Keep older agents eligible when the platform is genuinely unknown.
		if reported != "" && reported != label {
			return false
		}
	}
	return true
}

// Within one priority, give jobs with platform constraints first call on
// runners that a job without constraints could also take.
func preferPlatformDemand(a, b placementDemand) int {
	if a.job.ProvisionNow != b.job.ProvisionNow {
		if a.job.ProvisionNow {
			return -1
		}
		return 1
	}
	return bits.OnesCount8(uint8(b.platform)) - bits.OnesCount8(uint8(a.platform))
}

func (hs *hostSet) host(id string) *store.Host {
	i, ok := slices.BinarySearchFunc(hs.hosts, id, func(h *store.Host, id string) int {
		return strings.Compare(h.ID, id)
	})
	if !ok {
		return nil
	}
	return hs.hosts[i]
}

func (t *tick) matchPlatformSupply(p *store.Pool, plan *PoolPlan, runners []*store.Runner, eligible []placementDemand, live int) {
	if !slices.ContainsFunc(eligible, func(d placementDemand) bool { return d.platform != 0 }) {
		return
	}
	slices.SortStableFunc(eligible, preferPlatformDemand)
	var free []*store.Runner
	for _, r := range runners {
		if r.State.Live() && r.State != store.RunnerBusy && r.State != store.RunnerDraining && !t.stillRunning[r.ID] {
			free = append(free, r)
		}
	}
	plan.supply = 0
	for _, d := range eligible {
		ix := slices.IndexFunc(free, func(r *store.Runner) bool {
			return d.platform.matches(p, t.hosts.host(r.HostID))
		})
		if ix < 0 {
			plan.pending = append(plan.pending, d)
			continue
		}
		free = slices.Delete(free, ix, ix+1)
		plan.supply++
	}
	if len(plan.pending) == 0 {
		return
	}
	plan.Desired = max(plan.Desired, min(live+len(plan.pending), p.MaxRunners))
	// An incompatible warm runner can hold the last pool slot forever.
	// Drain an unused runner only if another host can serve that job.
	short := live + len(plan.pending) - p.MaxRunners
	for _, d := range plan.pending {
		if short <= 0 || len(free) == 0 || live > p.MaxRunners {
			break
		}
		if d.platform == 0 || !slices.ContainsFunc(t.hosts.hosts, func(h *store.Host) bool {
			return d.platform.matches(p, h) && HostCanRun(h, p, t.now)
		}) {
			continue
		}
		r := free[0]
		free = free[1:]
		reason := "making room for a queued job that needs runner labels " + d.platform.labels()
		plan.Actions = append(plan.Actions, t.action(ActionDrain, p, r, reason))
		plan.Reason = scaled(p.Name, live, live-1, reason)
		short--
	}
}

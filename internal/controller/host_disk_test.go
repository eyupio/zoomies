package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// measuredHost gives the host's disk figures the way a real host has them: the
// agent reports the total and the free space (SetHostReported), and the reserve
// is the operator's, which only PatchHost writes. Setting the field on the struct
// and calling UpdateHost leaves the reserve at zero in the database, so the
// scheduler reads the minimum reserve and the host fits a runner it should not.
func measuredHost(t *testing.T, h *harness, host *store.Host, totalMB, freeMB, reserveMB int64) {
	t.Helper()
	host.DiskTotalMB, host.DiskFreeMB = totalMB, freeMB
	if err := h.st.SetHostReported(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := h.st.PatchHost(h.ctx, host.ID, store.HostChanges{ReserveDiskMB: &reserveMB}); err != nil {
		t.Fatal(err)
	}
}

func TestLowDiskRaisesAHostProblemWithoutAQueuedJobAndClearsAfterReclamation(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	measuredHost(t, h, host, 97*1024, 17*1024, 8*1024)
	p := h.problem(t, "host.low_disk")
	if p.TargetKind != "host" || p.TargetID != host.ID || p.Severity != config.SeverityError || p.Audience != AudienceFleet {
		t.Fatalf("wrong target, severity or audience: %+v", p)
	}
	for _, want := range []string{"free", "reserve", "allocatable", "promised", "one runner needs"} {
		if !strings.Contains(p.Detail, want) {
			t.Errorf("detail %q lacks %q", p.Detail, want)
		}
	}
	if p.Remedy != nil {
		t.Fatal("a disk problem must not authorise automatic cleanup")
	}
	if !h.c.HostView(host).Healthy || host.Cordoned {
		t.Fatal("disk admission must not change connectivity or the operator's cordon")
	}
	measuredHost(t, h, host, 97*1024, 58*1024, 8*1024)
	if contains(h.problemCodes(), "host.low_disk") {
		t.Fatal("reclaiming disk did not clear the host problem")
	}
}

func TestLiveRunnersDiskCommitmentsCanBlockAHostThatPassesTheDoctor(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	measuredHost(t, h, host, 144*1024, 34*1024, 16*1024)
	h.runnerRow(pool, host, store.RunnerBusy)
	p := h.problem(t, "host.low_disk")
	if !strings.Contains(p.Detail, "promised to live runners") {
		t.Fatalf("the problem hides the commitment: %s", p.Detail)
	}
}

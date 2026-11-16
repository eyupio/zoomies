package controller

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

func TestLowDiskRaisesAHostProblemWithoutAQueuedJobAndClearsAfterReclamation(t *testing.T) {
	h := newHarness(t)
	_, _, host := h.fleet()
	host.DiskTotalMB, host.DiskFreeMB, host.ReserveDiskMB = 97*1024, 17*1024, 8*1024
	if err := h.st.SetHostReported(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatal(err)
	}
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
	host.DiskFreeMB = 58 * 1024
	if err := h.st.SetHostReported(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	if contains(h.problemCodes(), "host.low_disk") {
		t.Fatal("reclaiming disk did not clear the host problem")
	}
}

func TestLiveRunnersDiskCommitmentsCanBlockAHostThatPassesTheDoctor(t *testing.T) {
	h := newHarness(t)
	_, pool, host := h.fleet()
	host.DiskTotalMB, host.DiskFreeMB, host.ReserveDiskMB = 144*1024, 34*1024, 16*1024
	if err := h.st.SetHostReported(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatal(err)
	}
	h.runnerRow(pool, host, store.RunnerBusy)
	p := h.problem(t, "host.low_disk")
	if !strings.Contains(p.Detail, "promised to live runners") {
		t.Fatalf("the problem hides the commitment: %s", p.Detail)
	}
}

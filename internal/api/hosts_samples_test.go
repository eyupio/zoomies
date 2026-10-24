package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// The capacity map asks for every host at once and a detail page for one, so
// the same route answers both, and a bad window is refused with the field
// named rather than answered with an empty chart.
func TestHostSamplesAreListedForTheFleetOrOneHost(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)
	now := h.ctrl.Now()
	cpu, ioWait := 55.0, 34.0
	if err := h.ctrl.Store().RecordHostSamples(h.ctx, []store.HostSample{
		{HostID: "host_a", At: now.Add(-2 * time.Minute), Capacity: 4, ActiveRunners: 2, CPUPercent: &cpu, IOWaitPercent: &ioWait},
		{HostID: "host_b", At: now.Add(-2 * time.Minute), Capacity: 2},
		{HostID: "host_a", At: now.Add(-3 * time.Hour), Capacity: 4},
	}); err != nil {
		t.Fatalf("RecordHostSamples: %v", err)
	}

	type page struct {
		Items []store.HostSample `json:"items"`
	}
	list := func(path string) page {
		t.Helper()
		res := h.do(request{method: http.MethodGet, path: path, cookie: cookie})
		res.mustStatus(t, http.StatusOK, path)
		var out page
		if err := json.Unmarshal(res.body, &out); err != nil {
			t.Fatalf("%s: %v: %s", path, err, truncate(res.body))
		}
		return out
	}
	all := list("/api/v1/hosts/samples?window=1h")
	if len(all.Items) != 2 {
		t.Fatalf("items in the hour = %d, want the two recent samples: %+v", len(all.Items), all.Items)
	}
	one := list("/api/v1/hosts/samples?window=6h&host_id=host_a")
	if len(one.Items) != 2 || one.Items[0].HostID != "host_a" || one.Items[1].HostID != "host_a" {
		t.Fatalf("items for host_a over 6h = %+v, want both of its samples", one.Items)
	}
	if one.Items[1].CPUPercent == nil || *one.Items[1].CPUPercent != cpu {
		t.Errorf("newest sample = %+v, want its CPU measurement", one.Items[1])
	}
	if one.Items[1].IOWaitPercent == nil || *one.Items[1].IOWaitPercent != ioWait {
		t.Errorf("newest sample = %+v, want its I/O wait measurement", one.Items[1])
	}
	bad := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/samples?window=soon", cookie: cookie})
	bad.mustStatus(t, http.StatusBadRequest, "unparseable window")
}

// The host route is the other half of the pulse: the current judgement, not
// just the history. An assistant reading list_hosts sees the same fields,
// because that tool is a pass-through of GET /hosts.
func TestAHostViewCarriesItsDiskPulse(t *testing.T) {
	h := newHarness(t)
	admin, _ := h.user("root", store.RoleAdmin)
	cookie := h.session(admin)
	host := h.host("vm-disk")
	wait := 33.0
	since := h.ctrl.Now().Add(-11 * time.Minute)
	if err := h.ctrl.Store().SetHostUsage(h.ctx, host.ID, store.HostUsage{
		SampledAt: h.ctrl.Now(), IOWaitPercent: &wait, IOWaitHighSince: &since,
	}); err != nil {
		t.Fatalf("SetHostUsage: %v", err)
	}

	res := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/" + host.ID, cookie: cookie})
	res.mustStatus(t, http.StatusOK, "get the host")
	var view hostResponse
	res.into(t, &view)
	if !view.UsageFresh || !view.DiskBound {
		t.Fatalf("usage_fresh=%v disk_bound=%v, want a fresh disk-bound host", view.UsageFresh, view.DiskBound)
	}
	if view.Usage == nil || view.Usage.IOWaitPercent == nil || *view.Usage.IOWaitPercent != wait {
		t.Fatalf("usage = %+v, want I/O wait %v", view.Usage, wait)
	}
}

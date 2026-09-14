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
	cpu := 55.0
	if err := h.ctrl.Store().RecordHostSamples(h.ctx, []store.HostSample{
		{HostID: "host_a", At: now.Add(-2 * time.Minute), Capacity: 4, ActiveRunners: 2, CPUPercent: &cpu},
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
	bad := h.do(request{method: http.MethodGet, path: "/api/v1/hosts/samples?window=soon", cookie: cookie})
	bad.mustStatus(t, http.StatusBadRequest, "unparseable window")
}

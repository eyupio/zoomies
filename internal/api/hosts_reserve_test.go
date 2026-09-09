package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// The reserve is what a machine keeps for itself, and until this it could be
// set only from inside the store: `SetHostReserve` had no route and no caller,
// so the floors were the only reserve any host ever had and an operator could
// see what a host was without being able to hold any of it back.
func TestAnOperatorCanSetAHostsReserve(t *testing.T) {
	h := newHarness(t)
	host := h.host("vm-1")
	// A machine that has reported itself, which is the only kind a reserve can
	// be held back from.
	host.CPUs, host.MemoryMB = 16, 64*1024
	host.DiskTotalMB, host.DiskFreeMB = 500*1024, 400*1024
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	operator, _ := h.user("operator", store.RoleOperator)

	set := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID,
		cookie: h.session(operator),
		body:   map[string]any{"reserve_cpus": 2, "reserve_memory_mb": 8192, "reserve_disk_mb": 20480}})
	set.mustStatus(t, http.StatusOK, "set the reserve")

	var view hostResponse
	set.into(t, &view)
	if view.ReserveCPUs != 2 || view.ReserveMemoryMB != 8192 || view.ReserveDiskMB != 20480 {
		t.Fatalf("the response does not carry the reserve that was set: %+v", view)
	}
	// And what may be placed on is the machine less what was held back, which
	// is the figure the scheduler fits against.
	if view.AllocatableCPUs != 14 || view.AllocatableMemoryMB != 56*1024 {
		t.Errorf("allocatable = %v CPUs / %d MB, want 14 and %d: the reserve is not coming off the machine",
			view.AllocatableCPUs, view.AllocatableMemoryMB, 56*1024)
	}

	// It survives the write, rather than living in the response alone.
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if stored.ReserveCPUs != 2 || stored.ReserveMemoryMB != 8192 || stored.ReserveDiskMB != 20480 {
		t.Fatalf("the stored host does not carry the reserve: %+v", stored)
	}

	// Capacity and labels still work, and a request that names neither leaves
	// the reserve where it was: every field is independent.
	other := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID,
		cookie: h.session(operator), body: map[string]any{"capacity": 8}})
	other.mustStatus(t, http.StatusOK, "set capacity alone")
	var after hostResponse
	other.into(t, &after)
	if after.Capacity != 8 {
		t.Errorf("capacity = %d, want 8", after.Capacity)
	}
	if after.ReserveMemoryMB != 8192 {
		t.Errorf("changing the capacity cleared the reserve: %+v", after)
	}
}

// A reserve larger than the machine, or one held back from a figure the host
// has never reported, is refused rather than clamped: the first leaves nothing
// placeable and is what typing megabytes for gigabytes looks like, and the
// second would show an operator a reserve the scheduler ignores.
func TestAReserveTheHostCannotHonourIsRefused(t *testing.T) {
	h := newHarness(t)
	measured := h.host("vm-1")
	measured.CPUs, measured.MemoryMB = 8, 16*1024
	if err := h.st.UpdateHost(h.ctx, measured); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	silent := h.host("vm-2") // an agent too old to say what it is
	operator, _ := h.user("operator", store.RoleOperator)

	tooBig := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + measured.ID,
		cookie: h.session(operator), body: map[string]any{"reserve_memory_mb": 16 * 1024}})
	tooBig.mustStatus(t, http.StatusUnprocessableEntity, "a reserve as large as the machine")
	if body := string(tooBig.body); !strings.Contains(body, "nothing to place on") {
		t.Errorf("the refusal does not say what is wrong: %s", body)
	}

	unmeasured := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + silent.ID,
		cookie: h.session(operator), body: map[string]any{"reserve_memory_mb": 1024}})
	unmeasured.mustStatus(t, http.StatusUnprocessableEntity, "a reserve on an unmeasured host")
	if body := string(unmeasured.body); !strings.Contains(body, "upgrade its agent") {
		t.Errorf("the refusal does not say how to fix it: %s", body)
	}

	negative := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + measured.ID,
		cookie: h.session(operator), body: map[string]any{"reserve_cpus": -1}})
	negative.mustStatus(t, http.StatusUnprocessableEntity, "a negative reserve")

	// Nothing was written by any of the three.
	stored, err := h.st.GetHost(h.ctx, measured.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if stored.ReserveMemoryMB != 0 || stored.ReserveCPUs != 0 {
		t.Fatalf("a refused reserve was written anyway: %+v", stored)
	}
}

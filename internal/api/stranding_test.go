package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// sizedHost is a host that has reported what it is, which is the only kind a
// fit can be judged against. The api harness's own host reports nothing --
// an agent too old to measure itself -- and every pool fits on one of those.
func sizedHost(t *testing.T, h *harness, name string, cpus int, memoryMB int64) *store.Host {
	t.Helper()
	host := h.host(name)
	host.CPUs, host.MemoryMB = cpus, memoryMB
	host.DiskTotalMB, host.DiskFreeMB = 500*1024, 400*1024
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	return host
}

// sizedPool is a pool whose runners ask for a definite share, so that raising
// a reserve can take the last machine it fits on away.
func sizedPool(t *testing.T, h *harness, inst *store.Installation, name string, cpus float64, memoryMB int64) *store.Pool {
	t.Helper()
	p := h.pool(inst, name)
	p.Resources = store.Resources{CPUs: cpus, MemoryMB: memoryMB}
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	return p
}

// The mistake this refusal exists for: the Hosts page and the Pools page are
// two halves of one sentence, and holding back most of the machine on one of
// them is a pool whose jobs queue for ever on the other. Nothing was red
// before this check; the fleet simply stopped placing work.
func TestAReserveThatWouldLeaveAPoolNowhereToRunIsRefused(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	host := sizedHost(t, h, "vm-1", 16, 64*1024)
	pool := sizedPool(t, h, inst, "big", 4, 16*1024)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)

	refused := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID, cookie: cookie,
		body: map[string]any{"reserve_memory_mb": 56 * 1024}})
	refused.mustStatus(t, http.StatusConflict, "a reserve that strands a pool")
	body := string(refused.body)
	for _, want := range []string{pool.Name, host.Name, "16 GB", "confirm=true"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not mention %q, so an operator cannot act on it: %s", want, body)
		}
	}

	// And nothing was written: a refusal that half-saved would be worse than
	// no check at all.
	stored, err := h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if stored.ReserveMemoryMB != 0 {
		t.Fatalf("the refused reserve was written anyway: %+v", stored)
	}

	// Shrinking a host before the pool that used it is deleted is a real thing
	// to want, so the refusal is a question rather than a wall.
	confirmed := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + host.ID + "?confirm=true",
		cookie: cookie, body: map[string]any{"reserve_memory_mb": 56 * 1024}})
	confirmed.mustStatus(t, http.StatusOK, "the same reserve, confirmed")
	stored, err = h.st.GetHost(h.ctx, host.ID)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if stored.ReserveMemoryMB != 56*1024 {
		t.Fatalf("confirm=true did not save the reserve: %+v", stored)
	}
}

// The guard is narrow: a pool with somewhere else to go is not stranded, and
// an operator who could not adjust one host of several would stop using the
// page.
func TestAReserveIsSavedWhenAnotherHostCanStillRunThePool(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	first := sizedHost(t, h, "vm-1", 16, 64*1024)
	sizedHost(t, h, "vm-2", 16, 64*1024)
	sizedPool(t, h, inst, "big", 4, 16*1024)
	operator, _ := h.user("operator", store.RoleOperator)

	saved := h.do(request{method: http.MethodPatch, path: "/api/v1/hosts/" + first.ID,
		cookie: h.session(operator), body: map[string]any{"reserve_memory_mb": 56 * 1024}})
	saved.mustStatus(t, http.StatusOK, "a reserve that strands nothing")
}

// The other half of the same guard, typed on the pool's form instead. The
// wizard warns about this before a pool exists; the route has to refuse it for
// every other client, and for the quick edits that never open the wizard.
func TestAPoolEditThatNoHostCouldRunIsRefused(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	host := sizedHost(t, h, "vm-1", 16, 64*1024)
	pool := sizedPool(t, h, inst, "big", 4, 16*1024)
	operator, _ := h.user("operator", store.RoleOperator)
	cookie := h.session(operator)

	refused := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID, cookie: cookie,
		body: map[string]any{"resources": map[string]any{"cpus": 4, "memory_mb": 128 * 1024}}})
	refused.mustStatus(t, http.StatusConflict, "a pool no host could run")
	body := string(refused.body)
	for _, want := range []string{pool.Name, host.Name, "confirm=true"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not mention %q: %s", want, body)
		}
	}

	stored, err := h.st.GetPool(h.ctx, pool.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if stored.Resources.MemoryMB != 16*1024 {
		t.Fatalf("the refused change was written anyway: %+v", stored.Resources)
	}

	// A pool sized for machines that have not joined yet is legitimate, so the
	// refusal can be accepted.
	confirmed := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID + "?confirm=true",
		cookie: cookie, body: map[string]any{"resources": map[string]any{"cpus": 4, "memory_mb": 128 * 1024}}})
	confirmed.mustStatus(t, http.StatusOK, "the same change, confirmed")
}

// An edit to a pool that has somewhere to run and keeps it goes through
// untouched, whatever else it changes.
func TestAPoolEditThatKeepsAHostIsSaved(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	sizedHost(t, h, "vm-1", 16, 64*1024)
	pool := sizedPool(t, h, inst, "big", 4, 16*1024)
	operator, _ := h.user("operator", store.RoleOperator)

	saved := h.do(request{method: http.MethodPatch, path: "/api/v1/pools/" + pool.ID,
		cookie: h.session(operator), body: map[string]any{"max_runners": 8}})
	saved.mustStatus(t, http.StatusOK, "an edit that strands nothing")
}

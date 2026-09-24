package controller

import (
	"slices"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// A containerised agent that cannot hand runners the shared folder keeps no
// tool cache, which costs something only to a pool that keeps one: that is
// when the Problems panel says so, naming the host and the fix, and not
// before.
func TestAnUnmountedSharedFolderIsAProblemOnlyForAPoolThatKeepsAToolCache(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	p := h.pool(inst, "linux")
	host := h.host("zoomies-in-compose")
	host.BackendInfo = store.HostBackends{{
		Kind: store.BackendDocker, Available: true,
		SharedFolder: "the shared folder /var/lib/zoomies/shared is not mounted into this container from the host",
	}}
	if err := h.st.UpdateHost(h.ctx, host); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}
	if slices.Contains(h.problemCodes(), "host.shared_folder_unmounted") {
		t.Fatal("raised with no pool keeping a tool cache")
	}

	p.Cache = store.CacheConfig{Enabled: true, Tools: true, Scope: store.CacheScopePool}
	if err := h.st.UpdatePool(h.ctx, p); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	got := h.problem(t, "host.shared_folder_unmounted")
	if got.TargetID != host.ID || got.Fix == "" {
		t.Errorf("problem = %+v, want it on the host with a fix", got)
	}
}

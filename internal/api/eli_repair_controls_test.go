package api

import (
	"net/http"
	"testing"
)

func TestPausingRepairsDoesNotRequireTheirDeletedProvider(t *testing.T) {
	f := newAutomaticFixture(t)
	if err := f.h.st.DeleteAssistantProvider(f.h.ctx, f.provider.ID); err != nil {
		t.Fatal(err)
	}
	p := f.policy(t, f.auto)
	p.Enabled = false
	f.h.do(request{method: http.MethodPut, path: "/api/v1/assistant/repairs/policy", cookie: f.admin, body: p}).
		mustStatus(t, http.StatusOK, "an unusable policy can always be paused")
	if f.policy(t, f.auto).Enabled {
		t.Fatal("the repair policy stayed enabled")
	}
	p.Enabled = true
	f.h.do(request{method: http.MethodPut, path: "/api/v1/assistant/repairs/policy", cookie: f.admin, body: p}).
		mustStatus(t, http.StatusUnprocessableEntity, "resuming still needs an installation provider")
	if f.policy(t, f.auto).Enabled {
		t.Fatal("an unusable policy was resumed")
	}
}

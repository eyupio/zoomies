package controller

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A repair's question holds what the pull request's author wrote: its text, its
// diff, its failing log. Codex and Copilot keep tools of their own that can read
// the controller's files, and a repair commits what the model answers, so a line
// in a log could carry a file of the controller's out through the pull request.
// Claude Code runs with every tool off and is still allowed.
func TestARepairRefusesAToolThatCanReadTheControllersFiles(t *testing.T) {
	for kind, refused := range map[string]bool{"codex": true, "copilot": true, "claude_code": false, "openai": false} {
		if got := repairKindRefusal(kind) != ""; got != refused {
			t.Errorf("%s: refused = %v, want %v", kind, got, refused)
		}
	}
	h := newHarness(t)
	inst := h.installation()
	// The source read fails, so a repair that got as far as reading it says so.
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), sourceErr: errors.New("the source was read")}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	user := &store.User{Username: "octo", Role: store.RoleAdmin}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	shared := &store.AssistantProvider{Name: "Shared", Kind: "openai", Enabled: true}
	h.st.CreateAssistantProvider(h.ctx, shared)
	mine := &store.AssistantProvider{Name: "Mine", Kind: "codex", Enabled: true, OwnerID: user.ID}
	h.st.CreateAssistantProvider(h.ctx, mine)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: shared.ID, Enabled: true})
	row := &store.EliRepair{DedupKey: "codex", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	h.st.EnqueueEliRepair(h.ctx, row)
	h.c.runEliRepair(h.ctx, row)
	if row.State != "failed" || !strings.Contains(row.Message, "Claude Code") || strings.Contains(row.Message, "the source was read") {
		t.Fatalf("a repair through Codex was not refused before it read anything: state %q, message %q", row.State, row.Message)
	}
}

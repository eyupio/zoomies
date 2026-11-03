package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// installationProvider saves a provider that belongs to the installation, not to
// a person, which is the only kind that may pay for an unattended repair.
func installationProvider(t *testing.T, h *harness, name string, enabled bool) *store.AssistantProvider {
	t.Helper()
	p := &store.AssistantProvider{Name: name, Kind: "openai", Model: name + "-model", Enabled: enabled}
	if err := h.st.CreateAssistantProvider(h.ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func savePolicy(t *testing.T, h *harness, p store.EliRepairPolicy) {
	t.Helper()
	if err := h.st.SetEliRepairPolicy(h.ctx, p); err != nil {
		t.Fatal(err)
	}
}

func overviewOf(t *testing.T, h *harness) map[string]EliAutomaticRepository {
	t.Helper()
	list, err := h.c.EliAutomaticRepairs(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]EliAutomaticRepository{}
	for _, r := range list {
		out[r.Repo] = r
	}
	return out
}

// Only a repository that repairs itself belongs on a page about that. One that
// asks first, one that waits to be asked and one switched off are not automatic,
// whatever else their policies say.
func TestTheAutomaticOverviewListsOnlyEnabledAutomaticPolicies(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true, AllowWorkflows: true})
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/asks", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, AskFirst: true})
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/manual", InstallationID: inst.ID, Enabled: true})
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/off", InstallationID: inst.ID, ProviderID: team.ID, Enabled: false, Automatic: true})

	got := overviewOf(t, h)
	if len(got) != 1 {
		t.Fatalf("the overview lists %d repositories, want only acme/auto: %+v", len(got), got)
	}
	auto := got["acme/auto"]
	if auto.InstallationID != inst.ID || auto.ProviderID != team.ID || auto.ProviderName != "Team" || auto.Model != "Team-model" ||
		!auto.AllowWorkflows {
		t.Errorf("the entry does not carry the policy and its provider: %+v", auto)
	}
	if auto.Problem != "" || auto.Running != 0 || auto.Last != nil {
		t.Errorf("a repository nothing has happened to reads as busy or broken: %+v", auto)
	}
}

// The worker refuses a provider that is gone, disabled or a person's, one failed
// repair at a time. The page says so before the next failure is wasted on it.
func TestTheAutomaticOverviewSaysWhyAProviderCannotPay(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	user := &store.User{Username: "someone", Role: store.RoleViewer}
	if err := h.st.CreateUser(h.ctx, user); err != nil {
		t.Fatal(err)
	}
	gone := installationProvider(t, h, "Gone", true)
	sleepy := installationProvider(t, h, "Sleepy", false)
	personal := &store.AssistantProvider{Name: "Mine", Kind: "openai", Enabled: true, OwnerID: user.ID}
	if err := h.st.CreateAssistantProvider(h.ctx, personal); err != nil {
		t.Fatal(err)
	}
	fine := installationProvider(t, h, "Fine", true)
	for repo, provider := range map[string]string{"acme/gone": gone.ID, "acme/sleepy": sleepy.ID, "acme/personal": personal.ID, "acme/fine": fine.ID} {
		savePolicy(t, h, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: provider, Enabled: true, Automatic: true})
	}
	if err := h.st.DeleteAssistantProvider(h.ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if policies, err := h.st.EliRepairPolicies(h.ctx); err != nil || slices.ContainsFunc(policies, func(p store.EliRepairPolicy) bool { return p.Repo == "acme/gone" && p.ProviderID != "" }) {
		t.Fatalf("deleting a provider should leave its policy with none (policies %+v, error %v)", policies, err)
	}

	got := overviewOf(t, h)
	for _, repo := range []string{"acme/gone", "acme/sleepy", "acme/personal"} {
		if got[repo].Problem == "" {
			t.Errorf("%s has a provider that cannot pay and says nothing", repo)
		}
	}
	if !strings.Contains(got["acme/sleepy"].Problem, "Sleepy") || got["acme/sleepy"].ProviderName != "Sleepy" {
		t.Errorf("a disabled provider is not named: %+v", got["acme/sleepy"])
	}
	if got["acme/gone"].ProviderName != "" || got["acme/gone"].ProviderID != "" {
		t.Errorf("a deleted provider is still named: %+v", got["acme/gone"])
	}
	if got["acme/fine"].Problem != "" {
		t.Errorf("a working provider is reported as a problem: %q", got["acme/fine"].Problem)
	}
}

// What cannot run is what an administrator should read first, and the rest are in
// the order a person looks them up.
func TestTheAutomaticOverviewOrdersProblemsFirstThenByName(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	for _, repo := range []string{"acme/b", "acme/A", "acme/c"} {
		savePolicy(t, h, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})
	}
	broken := installationProvider(t, h, "Broken", false)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/z", InstallationID: inst.ID, ProviderID: broken.ID, Enabled: true, Automatic: true})

	list, err := h.c.EliAutomaticRepairs(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range list {
		order = append(order, r.Repo)
	}
	if want := []string{"acme/z", "acme/A", "acme/b", "acme/c"}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}

func policyOf(t *testing.T, h *harness, repo string) store.EliRepairPolicy {
	t.Helper()
	policies, err := h.st.EliRepairPolicies(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range policies {
		if p.Repo == repo {
			return p
		}
	}
	t.Fatalf("no policy for %s", repo)
	return store.EliRepairPolicy{}
}

// Ask me first is the way back to a person deciding, so the repository keeps
// everything else it was set up with: the provider that will pay again if
// Automatic is chosen later, the allowance and the workflow setting.
func TestStoppingAutomaticRepairsToAskFirstKeepsTheProviderAndLimits(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true, AllowWorkflows: true})

	got, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/auto"}, EliStopAskFirst)
	if err != nil || len(got) != 1 || !got[0].Changed || got[0].Error != "" {
		t.Fatalf("results %+v, error %v", got, err)
	}
	want := store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, AskFirst: true, AllowWorkflows: true}
	if p := policyOf(t, h, "acme/auto"); p != want {
		t.Errorf("policy %+v, want %+v", p, want)
	}
}

// Turning repairs off clears the switch and nothing else, so the switch is also
// how it is turned back on: setup resumes Automatic as it was.
func TestStoppingAutomaticRepairsToOffOnlyClearsTheSwitch(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true, AllowWorkflows: true})

	got, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/auto"}, EliStopOff)
	if err != nil || len(got) != 1 || !got[0].Changed {
		t.Fatalf("results %+v, error %v", got, err)
	}
	want := store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: false, Automatic: true, AllowWorkflows: true}
	if p := policyOf(t, h, "acme/auto"); p != want {
		t.Errorf("policy %+v, want %+v", p, want)
	}
	if overview := overviewOf(t, h); len(overview) != 0 {
		t.Errorf("a repository that is switched off is still listed as automatic: %+v", overview)
	}
}

// The day the control is needed is the day the provider is the problem. Saving a
// policy through setup is refused then, so a stop that went through the same
// check would be unavailable exactly when it was wanted.
func TestStoppingWorksWhenTheProviderIsGone(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/a", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/b", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})
	if err := h.st.DeleteAssistantProvider(h.ctx, team.ID); err != nil {
		t.Fatal(err)
	}

	for repo, then := range map[string]string{"acme/a": EliStopAskFirst, "acme/b": EliStopOff} {
		got, err := h.c.StopEliAutomatic(h.ctx, []string{repo}, then)
		if err != nil || len(got) != 1 || !got[0].Changed || got[0].Error != "" {
			t.Errorf("%s (%s): results %+v, error %v", repo, then, got, err)
		}
	}
	if a := policyOf(t, h, "acme/a"); a.Automatic || !a.AskFirst || !a.Enabled {
		t.Errorf("acme/a was not moved to ask first: %+v", a)
	}
	if b := policyOf(t, h, "acme/b"); b.Enabled {
		t.Errorf("acme/b was not turned off: %+v", b)
	}
}

// A second press, or a second administrator, finds it already done. That is not
// an error, and it must not write the policy again.
func TestStoppingTwiceChangesNothingTheSecondTime(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})
	if _, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/auto"}, EliStopAskFirst); err != nil {
		t.Fatal(err)
	}
	saved := policyOf(t, h, "acme/auto")

	// Asking for off now must not turn an ask-first repository off: it is not
	// automatic, so there is nothing to stop.
	got, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/auto"}, EliStopOff)
	if err != nil || len(got) != 1 || got[0].Changed || got[0].Error != "" {
		t.Fatalf("second stop: %+v, error %v", got, err)
	}
	if p := policyOf(t, h, "acme/auto"); p != saved {
		t.Errorf("the second stop changed the policy: %+v, was %+v", p, saved)
	}
}

func TestStoppingARepositoryWithNoPolicySaysSoAndTakesEachNameOnce(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	team := installationProvider(t, h, "Team", true)
	savePolicy(t, h, store.EliRepairPolicy{Repo: "acme/auto", InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})

	got, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/missing", "acme/auto", "acme/auto"}, EliStopAskFirst)
	if err != nil || len(got) != 2 {
		t.Fatalf("results %+v, error %v", got, err)
	}
	if got[0].Repo != "acme/missing" || got[0].Changed || got[0].Error == "" {
		t.Errorf("a repository with no policy: %+v", got[0])
	}
	if got[1].Repo != "acme/auto" || !got[1].Changed || got[1].Error != "" {
		t.Errorf("the repository stopped: %+v", got[1])
	}
	if _, err := h.c.StopEliAutomatic(h.ctx, []string{"acme/auto"}, "pause"); err == nil {
		t.Error("a choice that is not ask_first or off was accepted")
	}
}

// Telling the administrator that a repair "stops before it changes the branch" is
// only honest because the worker looks at the policy again when it gets to a
// repair. This pins that, for both ways of stopping, so the dialog's promise
// cannot drift from the worker.
func TestAStoppedRepositoryBlocksItsQueuedAutomaticRepair(t *testing.T) {
	for _, then := range []string{EliStopAskFirst, EliStopOff} {
		t.Run(then, func(t *testing.T) {
			s := newCommandSetup(t)
			policy := policyOf(t, s.h, s.repo)
			policy.Automatic = true
			savePolicy(t, s.h, policy)
			row := &store.EliRepair{DedupKey: "automatic:" + s.repo + ":old", Repo: s.repo, InstallationID: s.inst.ID, PullNumber: 1, HeadSHA: "old", RunID: 9, Trigger: "automatic"}
			if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, row); err != nil {
				t.Fatal(err)
			}
			if _, err := s.h.c.StopEliAutomatic(s.h.ctx, []string{s.repo}, then); err != nil {
				t.Fatal(err)
			}

			s.h.c.runEliRepair(s.h.ctx, row)
			if row.State != "blocked" || s.fake.commits != 0 || len(s.fake.comments) != 0 {
				t.Fatalf("a repair queued before the stop ran: state %q, %d commits, comments %q", row.State, s.fake.commits, s.fake.comments)
			}
			// Ask me first leaves repairs on, so "disabled" would be a lie the
			// attempt page repeats beside a card that says the opposite.
			want := "were stopped for this repository"
			if then == EliStopOff {
				want = "disabled"
			}
			if !strings.Contains(row.Message, want) {
				t.Errorf("a repair blocked by a stop to %s says %q, want it to mention %q", then, row.Message, want)
			}
		})
	}
}

// A failed check on a repository whose provider cannot pay used to be queued,
// charged to the allowance, and then failed with a comment on the pull request.
// The panel says automatic repairs cannot start there, so they must not.
func TestAFailedCheckIsNotQueuedWhenTheProviderCannotPay(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, h *harness, instID string) string
		// after runs once the policy exists, for what can only happen to a
		// provider a policy already names.
		after func(t *testing.T, h *harness, providerID string)
	}{
		{"no provider set", func(t *testing.T, h *harness, instID string) string { return "" }, nil},
		{"deleted provider", func(t *testing.T, h *harness, instID string) string {
			return installationProvider(t, h, "Gone", true).ID
		}, func(t *testing.T, h *harness, providerID string) {
			if err := h.st.DeleteAssistantProvider(h.ctx, providerID); err != nil {
				t.Fatal(err)
			}
		}},
		{"disabled provider", func(t *testing.T, h *harness, instID string) string {
			return installationProvider(t, h, "Sleepy", false).ID
		}, nil},
		{"personal provider", func(t *testing.T, h *harness, instID string) string {
			user := &store.User{Username: "octo", Role: store.RoleViewer}
			if err := h.st.CreateUser(h.ctx, user); err != nil {
				t.Fatal(err)
			}
			p := &store.AssistantProvider{Name: "Mine", OwnerID: user.ID, Kind: "openai", Model: "m", Enabled: true}
			if err := h.st.CreateAssistantProvider(h.ctx, p); err != nil {
				t.Fatal(err)
			}
			return p.ID
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			repo := inst.Target + "/repo"
			fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			providerID := tc.setup(t, h, inst.ID)
			savePolicy(t, h, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: providerID, Enabled: true, Automatic: true})
			if tc.after != nil {
				tc.after(t, h, providerID)
			}
			body, _ := json.Marshal(map[string]any{"action": "completed", "repository": map[string]any{"full_name": repo}, "workflow_job": map[string]any{"id": 12, "run_id": 9, "head_sha": "old", "conclusion": "failure"}})
			if err := h.c.enqueueRepairWebhook(h.ctx, inst, "workflow_job", body); err != nil {
				t.Fatal(err)
			}
			rows, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
			if len(rows) != 0 {
				t.Fatalf("a repair was queued with no provider able to pay: %+v", rows)
			}
		})
	}
}

// The dialog promises that a repair which has not pushed yet stops without
// changing the branch. The queued checkpoint is pinned above; this pins the one
// the worker reaches after the model has answered, the last before the first
// write, for both ways of stopping.
func TestAStopWhileTheModelIsWorkingPublishesNothing(t *testing.T) {
	for _, then := range []string{EliStopAskFirst, EliStopOff} {
		t.Run(then, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			repo := inst.Target + "/repo"
			fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "failed"}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// The administrator presses Stop while the model is still answering.
				if _, err := h.c.StopEliAutomatic(h.ctx, []string{repo}, then); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			}))
			defer srv.Close()
			team := &store.AssistantProvider{Name: "Team", Kind: "openai_compatible", BaseURL: srv.URL, Model: "team-model", Enabled: true}
			if err := h.st.CreateAssistantProvider(h.ctx, team); err != nil {
				t.Fatal(err)
			}
			savePolicy(t, h, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: team.ID, Enabled: true, Automatic: true})
			row := &store.EliRepair{DedupKey: "automatic:" + repo + ":old", Repo: repo, InstallationID: inst.ID, PullNumber: 1, HeadSHA: "old", RunID: 9, Trigger: "automatic"}
			if _, err := h.st.EnqueueEliRepair(h.ctx, row); err != nil {
				t.Fatal(err)
			}

			h.c.runEliRepair(h.ctx, row)
			if row.State != "failed" || fake.commits != 0 {
				t.Fatalf("a repair stopped while the model worked ended %q with %d commits: %q", row.State, fake.commits, row.Message)
			}
		})
	}
}

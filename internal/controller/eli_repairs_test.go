package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRepairAdmissionRequiresLinkedHumanCommentAndDeduplicates(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	repo := inst.Target + "/repo"
	p := &store.AssistantProvider{Name: "install", Kind: "openai", Enabled: true}
	if e := h.st.CreateAssistantProvider(h.ctx, p); e != nil {
		t.Fatal(e)
	}
	policy := store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, Automatic: true, DailyLimit: 5}
	if e := h.st.SetEliRepairPolicy(h.ctx, policy); e != nil {
		t.Fatal(e)
	}
	user := &store.User{Username: "requester", Role: store.RoleViewer}
	if e := h.st.CreateUser(h.ctx, user); e != nil {
		t.Fatal(e)
	}
	if e := h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}); e != nil {
		t.Fatal(e)
	}
	if e := h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		action, kind   string
		author, sender int64
		pr             bool
	}{{"created", "User", 9, 9, true}, {"edited", "User", 42, 42, true}, {"created", "Bot", 42, 42, true}, {"created", "User", 42, 9, true}, {"created", "User", 42, 42, false}, {"created", "User", 42, 42, true}, {"created", "User", 42, 42, true}} {
		issue := map[string]any{"number": 1}
		if tc.pr {
			issue["pull_request"] = map[string]any{}
		}
		body, _ := json.Marshal(map[string]any{"action": tc.action, "repository": map[string]any{"full_name": repo}, "issue": issue, "comment": map[string]any{"id": 100, "body": "/eli fix this PR", "user": map[string]any{"id": tc.author, "login": "octo", "type": tc.kind}}, "sender": map[string]any{"id": tc.sender}})
		if rec := h.deliver("issue_comment", body, testWebhookSecret); rec.Code != 202 {
			t.Fatalf("signed delivery: %d %s", rec.Code, rec.Body.String())
		}
		if rec := h.deliver("issue_comment", body, "forged"); rec.Code >= 200 && rec.Code < 300 {
			t.Fatal("forged repair delivery accepted")
		}
	}
	rows, _, e := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if e != nil || len(rows) != 1 || rows[0].UserID != user.ID || rows[0].State != "queued" {
		t.Fatalf("rows %+v %v", rows, e)
	}
}

// The GitHub transport is fake, but the model adapter, owner selection, durable
// progress and publication decision are the same path the worker runs.
type repairFake struct {
	github.Client
	commits        int
	check          string
	onRead         func()
	unsupportedRun bool
	noWrite        bool
	reacted        []int64
	// confused is the comments Eli put a confused face on, because it could not
	// start the repair they asked for.
	confused []int64
	// comments is every text Eli posted or edited on the PR, in order.
	comments []string
	// sourceErr, when set, is what reading the PR's source fails with.
	sourceErr error
	// checkErr, when set, is what asking GitHub for the PR's checks fails with.
	checkErr error
	// title, author, url and base are what the pull request says about itself,
	// for the tests that read it back.
	title, author, url, base string
	// onFailures, when set, answers a read of a job's error output, so a test
	// chooses what comes back and counts the reads.
	onFailures func(jobID int64) ([]string, error)
	// failedJobs and failedJobsErr are what GitHub says the run's failed jobs
	// were, for a failure this fleet has no record of. Nil is the ordinary
	// answer: no failed jobs found.
	failedJobs    *github.RepairFailureRun
	failedJobsErr error
	// failedJobsAsked is the run and head each read was made with, so a test
	// can say which of the two the failure was known by.
	failedJobsAsked [][2]any
}

func (f *repairFake) RepairActor(context.Context, string, string) (github.RepairActor, error) {
	return github.RepairActor{ID: 42, Login: "octo", CanWrite: !f.noWrite}, nil
}
func (f *repairFake) RepairPull(context.Context, string, int) (github.RepairPull, error) {
	head := "old"
	if f.commits > 0 {
		head = "fixed"
	}
	return github.RepairPull{Number: 1, Branch: "feature", HeadSHA: head, Open: true, Title: f.title, Author: f.author, URL: f.url, Base: f.base}, nil
}
func (f *repairFake) RepairPullForRun(ctx context.Context, repo string, _ int64) (github.RepairPull, error) {
	if f.unsupportedRun {
		return github.RepairPull{}, github.ErrRepairUnsupported
	}
	return f.RepairPull(ctx, repo, 1)
}
func (f *repairFake) RepairSource(context.Context, string, github.RepairPull, bool) (*github.RepairSource, error) {
	if f.onRead != nil {
		f.onRead()
	}
	if f.sourceErr != nil {
		return nil, f.sourceErr
	}
	return &github.RepairSource{Snapshot: prrepair.Snapshot{Files: []prrepair.File{{Path: "main.go", Content: "old", Mode: "100644"}}}}, nil
}
func (f *repairFake) RepairFailures(_ context.Context, _ string, _ github.RepairPull, jobID int64) ([]string, error) {
	if f.onFailures != nil {
		return f.onFailures(jobID)
	}
	return []string{"main.go: test failed"}, nil
}
func (f *repairFake) RepairFailedJobs(_ context.Context, _ string, run int64, headSHA string) (*github.RepairFailureRun, error) {
	f.failedJobsAsked = append(f.failedJobsAsked, [2]any{run, headSHA})
	return f.failedJobs, f.failedJobsErr
}
func (f *repairFake) CommitRepair(_ context.Context, _ string, p github.RepairPull, plan prrepair.Plan) (string, error) {
	if p.HeadSHA != "old" || len(plan.Files) != 1 || plan.Files[0].Content != "fixed" {
		return "", fmt.Errorf("bad plan")
	}
	f.commits++
	return "fixed", nil
}
func (f *repairFake) RepairComment(_ context.Context, _ string, _ int, _ int64, text string) (int64, error) {
	f.comments = append(f.comments, text)
	return 1, nil
}
func (f *repairFake) RepairReact(_ context.Context, _ string, id int64, content string) error {
	switch content {
	case "eyes":
		f.reacted = append(f.reacted, id)
	case "confused":
		f.confused = append(f.confused, id)
	default:
		return fmt.Errorf("unexpected reaction %q", content)
	}
	return nil
}
func (f *repairFake) RepairChecks(context.Context, string, string) (string, error) {
	return f.check, f.checkErr
}
func TestRepairWorkerSelectsTheHybridProviderAndRecordsRealCheckOutcome(t *testing.T) {
	for _, trigger := range []string{"mention", "automatic", "revoked", "expired-lease", "cancelled", "queued-old"} {
		t.Run(trigger, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "failed"}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			var models []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Model string `json:"model"`
				}
				json.NewDecoder(r.Body).Decode(&in)
				models = append(models, in.Model)
				w.Header().Set("Content-Type", "text/event-stream")
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			}))
			defer srv.Close()
			user := &store.User{Username: "octo", Role: store.RoleViewer}
			h.st.CreateUser(h.ctx, user)
			h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
			h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
			shared := &store.AssistantProvider{Name: "Shared", Kind: "openai_compatible", BaseURL: srv.URL, Model: "installation-model", Enabled: true}
			personal := &store.AssistantProvider{Name: "Personal", OwnerID: user.ID, Kind: "openai_compatible", BaseURL: srv.URL, Model: "personal-model", Enabled: true}
			for _, p := range []*store.AssistantProvider{shared, personal} {
				if e := h.st.CreateAssistantProvider(h.ctx, p); e != nil {
					t.Fatal(e)
				}
				h.st.SetDefaultAssistantProvider(h.ctx, p.ID)
			}
			repo := "acme/repo"
			h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: shared.ID, Enabled: true, Automatic: true, DailyLimit: 5})
			actualTrigger := "mention"
			if trigger == "automatic" {
				actualTrigger = "automatic"
			}
			if trigger == "revoked" || trigger == "expired-lease" {
				actualTrigger = "mention"
				fake.onRead = func() {
					if trigger == "revoked" {
						h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, false)
					} else {
						h.c.lease = &store.ControllerLease{Holder: "stale", RenewedAt: h.c.Now().Add(-2 * LeaseTTL)}
					}
				}
			}
			dedup := trigger
			if trigger == "mention" {
				dedup = "comment:acme/repo:77"
			}
			row := &store.EliRepair{DedupKey: dedup, Repo: repo, InstallationID: inst.ID, PullNumber: 1, HeadSHA: "old", RunID: 9, Trigger: actualTrigger, UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
			h.st.EnqueueEliRepair(h.ctx, row, 5)
			if trigger == "queued-old" {
				h.advance(2 * time.Hour)
				fake.check = "waiting"
			}
			runCtx := h.ctx
			if trigger == "cancelled" {
				var cancel context.CancelFunc
				runCtx, cancel = context.WithCancel(h.ctx)
				defer cancel()
				fake.onRead = cancel
			}
			h.c.runEliRepair(runCtx, row)
			if trigger == "cancelled" {
				rows, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
				if len(rows) != 1 || rows[0].State != "failed" || fake.commits != 0 {
					t.Fatalf("cancelled repair stuck: %+v", rows)
				}
				return
			}
			if trigger == "expired-lease" {
				if fake.commits != 0 || row.State != "failed" || !strings.Contains(row.Message, "stopped before publication") {
					t.Fatalf("published without a current lease: %+v", row)
				}
				return
			}
			if trigger == "revoked" {
				if fake.commits != 0 || row.State != "failed" || !strings.Contains(row.Message, "access changed") {
					t.Fatalf("published after revocation: %+v", row)
				}
				return
			}
			want := "personal-model"
			if trigger == "automatic" {
				want = "installation-model"
			}
			if len(models) != 1 || models[0] != want || row.State != "checking" || fake.commits != 1 {
				t.Fatalf("models %v repair %+v commits %d", models, row, fake.commits)
			}
			// The comment that asked is acknowledged; a repair nobody asked for in a
			// comment has nothing to react to.
			if trigger == "mention" && (len(fake.reacted) != 1 || fake.reacted[0] != 77) || trigger == "automatic" && len(fake.reacted) != 0 {
				t.Fatalf("reactions %v for a %s repair", fake.reacted, trigger)
			}
			h.advance(31 * time.Second)
			h.c.checkEliRepairs(h.ctx)
			if trigger == "queued-old" {
				rows, _ := h.st.CheckingEliRepairs(h.ctx)
				if len(rows) != 1 {
					t.Fatal("queue age shortened the verification window")
				}
				h.advance(time.Hour)
				h.c.checkEliRepairs(h.ctx)
				all, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
				if all[0].State != "unverified" {
					t.Fatalf("verification never expired: %+v", all[0])
				}
				return
			}
			rows, _ := h.st.CheckingEliRepairs(h.ctx)
			if len(rows) != 0 {
				t.Fatal("failed checks remained pending")
			}
			all, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
			if all[0].State != "checks_failed" {
				t.Fatalf("false success %+v", all[0])
			}
		})
	}
}
func TestRepairWorkerStopsBeforeModelUseWhenPersonalProviderIsMissing(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	p := &store.AssistantProvider{Name: "Shared", Kind: "openai", Enabled: true}
	h.st.CreateAssistantProvider(h.ctx, p)
	h.st.SetDefaultAssistantProvider(h.ctx, p.ID)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, DailyLimit: 5})
	row := &store.EliRepair{DedupKey: "missing", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	h.st.EnqueueEliRepair(h.ctx, row, 5)
	h.c.runEliRepair(h.ctx, row)
	if row.State != "failed" || fake.commits != 0 || !strings.Contains(row.Message, "personal provider") {
		t.Fatalf("installation fallback: %+v", row)
	}
}

func TestAutomaticRepairIgnoresNonPRFailuresBeforeChargingTheBudget(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	repo := inst.Target + "/repo"
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), unsupportedRun: true}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	p := &store.AssistantProvider{Name: "Install", Kind: "openai", Enabled: true}
	h.st.CreateAssistantProvider(h.ctx, p)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, Automatic: true, DailyLimit: 1})
	body, _ := json.Marshal(map[string]any{"action": "completed", "repository": map[string]any{"full_name": repo}, "workflow_job": map[string]any{"id": 12, "run_id": 9, "head_sha": "old", "conclusion": "failure"}})
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "workflow_job", body); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(rows) != 0 {
		t.Fatal("non-PR job charged the budget")
	}
	fake.unsupportedRun = false
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "workflow_job", body); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(rows) != 1 || rows[0].PullNumber != 1 {
		t.Fatalf("PR was not admitted: %+v", rows)
	}
}

func TestLinkedCommentCannotSpendTheBudgetAfterRepositoryAccessIsRemoved(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	repo := inst.Target + "/repo"
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), noWrite: true}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	p := &store.AssistantProvider{Name: "Install", Kind: "openai", Enabled: true}
	h.st.CreateAssistantProvider(h.ctx, p)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true, DailyLimit: 1})
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	body, _ := json.Marshal(map[string]any{"action": "created", "repository": map[string]any{"full_name": repo}, "issue": map[string]any{"number": 1, "pull_request": map[string]any{}}, "comment": map[string]any{"id": 101, "body": "/eli fix this PR", "user": map[string]any{"id": 42, "login": "octo", "type": "User"}}, "sender": map[string]any{"id": 42}})
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "issue_comment", body); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(rows) != 0 {
		t.Fatal("removed collaborator charged the budget")
	}
	fake.noWrite = false
	user.Disabled = true
	h.st.UpdateUser(h.ctx, user)
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "issue_comment", body); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(rows) != 0 {
		t.Fatal("disabled user charged the budget")
	}
	user.Disabled = false
	h.st.UpdateUser(h.ctx, user)
	if err := h.c.enqueueRepairWebhook(h.ctx, inst, "issue_comment", body); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(rows) != 1 {
		t.Fatal("current collaborator was not admitted")
	}
}

// A thread of Eli's updates is read top to bottom by someone deciding whether to
// trust the push, so every stage a repair can be in needs a heading of its own
// and none may fall back to the bare name.
func TestEveryRepairStageHasItsOwnHeading(t *testing.T) {
	seen := map[string]string{}
	for _, state := range []string{"working", "checking", "succeeded", "checks_failed", "failed", "superseded"} {
		h := repairHeading(state)
		if h == repairHeading("") {
			t.Errorf("state %q has no heading of its own", state)
		}
		if other, dup := seen[h]; dup {
			t.Errorf("states %q and %q share the heading %q", state, other, h)
		}
		seen[h] = state
	}
}

func TestTheTriggeringCommentIsReadBackFromTheDedupKey(t *testing.T) {
	for key, want := range map[string]int64{"comment:acme/repo:77": 77, "comment:other/repo:77": 0, "ui:acme/repo:1:abc:u": 0, "comment:acme/repo:x": 0, "comment:acme/repo:-3": 0, "": 0} {
		if got := triggerCommentID(&store.EliRepair{Repo: "acme/repo", DedupKey: key}); got != want {
			t.Errorf("%q gave %d, want %d", key, got, want)
		}
	}
}

// A person who has not marked a provider as their default is still answered in
// chat by the first usable one they own, so a repair they ask for is paid for by
// the same provider rather than refused for want of a tick. A subscription
// provider a viewer may not use is passed over, not tripped on.
func TestARepairFallsBackToThePersonsOnlyUsableProviderWhenNoneIsDefault(t *testing.T) {
	h := newHarness(t)
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	if _, err := h.c.personalChatProvider(h.ctx, "", user.ID); !errors.Is(err, ErrAssistantNoModel) {
		t.Fatalf("with no provider at all: %v", err)
	}
	// Listed by name, so "a-claude" comes first and must be passed over.
	claude := &store.AssistantProvider{Name: "a-claude", Kind: "claude_code", Enabled: true, OwnerID: user.ID}
	h.st.CreateAssistantProvider(h.ctx, claude)
	off := &store.AssistantProvider{Name: "b-off", Kind: "openai", Model: "m", Enabled: false, OwnerID: user.ID}
	h.st.CreateAssistantProvider(h.ctx, off)
	mine := &store.AssistantProvider{Name: "c-mine", Kind: "openai", Model: "m", Enabled: true, OwnerID: user.ID}
	h.st.CreateAssistantProvider(h.ctx, mine)
	theirs := &store.AssistantProvider{Name: "d-theirs", Kind: "openai", Model: "m", Enabled: true, OwnerID: "someone-else"}
	h.st.CreateAssistantProvider(h.ctx, theirs)
	got, err := h.c.personalChatProvider(h.ctx, "", user.ID)
	if err != nil || got.ID != mine.ID {
		t.Fatalf("got %+v, %v; want the person's own enabled API provider", got, err)
	}
}

// When the PR's source cannot be read, the comment says why. "Within Eli's
// repair limits" named neither the limit nor the file, and the person who
// got it could not tell a repository too big to inventory from a token
// without the permission.
func TestARepairThatCannotReadTheSourceSaysWhy(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), sourceErr: errors.New("repository inventory exceeds Eli's repair limit")}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	shared := &store.AssistantProvider{Name: "Shared", Kind: "openai", Enabled: true}
	h.st.CreateAssistantProvider(h.ctx, shared)
	mine := &store.AssistantProvider{Name: "Mine", Kind: "openai", Enabled: true, OwnerID: user.ID}
	h.st.CreateAssistantProvider(h.ctx, mine)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: shared.ID, Enabled: true, DailyLimit: 5})
	row := &store.EliRepair{DedupKey: "unreadable", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	h.st.EnqueueEliRepair(h.ctx, row, 5)
	h.c.runEliRepair(h.ctx, row)
	if row.State != "failed" || row.Message != "The PR source could not be read: repository inventory exceeds Eli's repair limit." {
		t.Fatalf("repair = %+v", row)
	}
}

// Ollama cloud answered the repair prompt with a tool call although it was
// offered no tools, and the repair gave up on the spot with a comment that
// named neither the tool nor the cause. A model told that it has no tools, and
// shown the JSON it was asked for, usually complies on the next turn; one that
// does not is named in the comment, so an operator can tell a model that
// ignores its instructions from a prompt that was wrong. The call here is one
// the repair has no reply for; a call named after a reply is read as that reply
// (see TestARepairReadsAToolCallNamedAfterAReplyAsThatReply).
func TestARepairAnswersAnUnofferedToolCallOnceAndNamesItWhenItRepeats(t *testing.T) {
	for _, relents := range []bool{true, false} {
		name := "the model insists"
		if relents {
			name = "the model relents"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "waiting"}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			var requests []assistant.Request
			h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
				return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(req assistant.Request) []assistant.Event {
					requests = append(requests, req)
					if relents && req.Messages[len(req.Messages)-1].Role == assistant.RoleTool {
						return []assistant.Event{{Delta: `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}}
					}
					return []assistant.Event{{ToolCall: &assistant.ToolCall{ID: "call_1", Name: "run_tests", Arguments: json.RawMessage(`{"command":"go test ./..."}`)}}}
				}}), nil
			}
			user := &store.User{Username: "octo", Role: store.RoleViewer}
			h.st.CreateUser(h.ctx, user)
			h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
			h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
			shared := &store.AssistantProvider{Name: "Shared", Kind: "fake", Model: "scripted", Enabled: true}
			personal := &store.AssistantProvider{Name: "Personal", OwnerID: user.ID, Kind: "fake", Model: "scripted", Enabled: true}
			for _, p := range []*store.AssistantProvider{shared, personal} {
				if e := h.st.CreateAssistantProvider(h.ctx, p); e != nil {
					t.Fatal(e)
				}
				h.st.SetDefaultAssistantProvider(h.ctx, p.ID)
			}
			h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: shared.ID, Enabled: true, DailyLimit: 5})
			row := &store.EliRepair{DedupKey: "comment:acme/repo:77", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
			h.st.EnqueueEliRepair(h.ctx, row, 5)
			h.c.runEliRepair(h.ctx, row)
			if len(requests) != 2 {
				t.Fatalf("the model was asked %d times; a tool call it was not offered is answered once, then the repair gives up", len(requests))
			}
			// The model is answered the way the chat loop answers an unknown
			// tool: as a result of the call it made, saying the tool is not one
			// it was offered, with the turn that made the call kept above it.
			turns := requests[1].Messages
			told := turns[len(turns)-1]
			asked := turns[len(turns)-2]
			if told.Role != assistant.RoleTool || told.ToolCallID != "call_1" || !strings.Contains(told.Content, "run_tests") || !strings.Contains(told.Content, "No tools") || !strings.Contains(told.Content, "cannot_fix") {
				t.Fatalf("the model was told %+v", told)
			}
			if asked.Role != assistant.RoleAssistant || len(asked.ToolCalls) != 1 || asked.ToolCalls[0].ID != "call_1" {
				t.Fatalf("the turn before the result is %+v, not the call", asked)
			}
			if len(row.Details.Steps) != 2 {
				t.Fatalf("%d steps recorded for two model calls", len(row.Details.Steps))
			}
			if relents {
				if row.State != "checking" || fake.commits != 1 {
					t.Fatalf("the corrected model's plan was not published: %+v", row)
				}
				return
			}
			if row.State != "failed" || fake.commits != 0 {
				t.Fatalf("a model that insists on a tool was not stopped: %+v", row)
			}
			last := fake.comments[len(fake.comments)-1]
			for _, want := range []string{"### 🐶 Eli got stuck", "`run_tests` (with `command`)", "no tools", "provider"} {
				if !strings.Contains(last, want) {
					t.Fatalf("the comment does not say %q:\n%s", want, last)
				}
			}
		})
	}
}

// The repair prompt describes its replies as {"read":["path"]} and
// {"windows":[...]}, and Ollama cloud's models took those for functions: the
// repairs on this repository ended in "Eli got stuck" with a tool call named
// `read` or `windows`, twice, one step from reading the file. The call says what
// the reply would have, so the repair goes on as if the model had typed it, with
// no tool turn in the transcript for the model to be confused by.
func TestARepairReadsAToolCallNamedAfterAReplyAsThatReply(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "waiting"}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	var requests []assistant.Request
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(req assistant.Request) []assistant.Event {
			requests = append(requests, req)
			if len(requests) == 1 {
				return []assistant.Event{{ToolCall: &assistant.ToolCall{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)}}}
			}
			return []assistant.Event{{Delta: `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}}
		}}), nil
	}
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: user.ID, Kind: "fake", Model: "scripted", Enabled: true}
	if e := h.st.CreateAssistantProvider(h.ctx, personal); e != nil {
		t.Fatal(e)
	}
	h.st.SetDefaultAssistantProvider(h.ctx, personal.ID)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: personal.ID, Enabled: true, DailyLimit: 5})
	row := &store.EliRepair{DedupKey: "comment:acme/repo:77", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	h.st.EnqueueEliRepair(h.ctx, row, 5)
	h.c.runEliRepair(h.ctx, row)

	if len(requests) != 2 {
		t.Fatalf("the model was asked %d times; a call named after a reply is the reply, so one step reads the file and the next fixes it", len(requests))
	}
	for _, m := range requests[1].Messages {
		if m.Role == assistant.RoleTool || len(m.ToolCalls) > 0 {
			t.Fatalf("the second request carries a tool turn: %+v", m)
		}
	}
	var asked bool
	for _, m := range requests[1].Messages {
		if m.Role == assistant.RoleAssistant && m.Content == `{"read":["main.go"]}` {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the transcript does not hold the read as the model's reply: %+v", requests[1].Messages)
	}
	if row.State != "checking" || fake.commits != 1 {
		t.Fatalf("the repair did not go on to publish the fix: %+v", row)
	}
}

func TestARepairPublishesOnlyAfterTheModelCorrectsAMixedAction(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "waiting"}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	calls := 0
	h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Model: "scripted", Script: func(req assistant.Request) []assistant.Event {
			calls++
			if fake.commits != 0 {
				t.Fatal("published before the model supplied one supported action")
			}
			if calls == 1 {
				return []assistant.Event{{Delta: `{"read":["unread.go"],"summary":"Fix cause","files":[{"path":"main.go","content":"wrong"}]}`}}
			}
			if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "do not combine source requests with changes") {
				t.Fatal("the model did not receive the specific correction")
			}
			return []assistant.Event{{Delta: `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}}
		}}), nil
	}
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	if err := h.st.CreateUser(h.ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true); err != nil {
		t.Fatal(err)
	}
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: user.ID, Kind: "fake", Model: "scripted", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, personal); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetDefaultAssistantProvider(h.ctx, personal.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: personal.ID, Enabled: true, DailyLimit: 5}); err != nil {
		t.Fatal(err)
	}
	row := &store.EliRepair{DedupKey: "mixed-action", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	if _, err := h.st.EnqueueEliRepair(h.ctx, row, 5); err != nil {
		t.Fatal(err)
	}
	h.c.runEliRepair(h.ctx, row)
	if calls != 2 || row.State != "checking" || fake.commits != 1 || len(row.Details.Steps) != 2 {
		t.Fatalf("calls %d, commits %d, repair %+v", calls, fake.commits, row)
	}
	saved, err := h.st.GetEliRepair(h.ctx, row.ID, user.ID, true)
	if err != nil || saved.State != "checking" || len(saved.Details.Steps) != 2 {
		t.Fatalf("repair progress not retained: %+v, %v", saved, err)
	}
	if !saved.Details.Steps[0].ResponseRecorded || !strings.Contains(saved.Details.Steps[0].Response, "unread.go") || !strings.Contains(saved.Details.Steps[0].ValidationError, "do not combine") || saved.Details.Steps[1].ValidationError != "" {
		t.Fatalf("responses or the rejected step's reason were not retained: %+v", saved.Details.Steps)
	}
}

// A provider with no credit left answers with a status and a sentence of its
// own, and the repair filed it as a provider that could not be reached: the
// person reading the comment was told to test a connection that was fine, and
// nothing said the account had run out. The repair says what the provider
// could not do, and records it as its own failure, so the advice on the page
// is about paying for the repair rather than about testing a provider.
func TestARepairThatCannotPaySaysTheProviderIsOutOfCredit(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "waiting"}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":{"type":"server_error","message":"Upstream request failed: Insufficient account funds"}}`))
	}))
	defer srv.Close()
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: user.ID, Kind: "openai_compatible", BaseURL: srv.URL, Model: "paid-model", Enabled: true}
	if e := h.st.CreateAssistantProvider(h.ctx, personal); e != nil {
		t.Fatal(e)
	}
	h.st.SetDefaultAssistantProvider(h.ctx, personal.ID)
	h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, ProviderID: personal.ID, Enabled: true, DailyLimit: 5})
	row := &store.EliRepair{DedupKey: "comment:acme/repo:77", Repo: "acme/repo", InstallationID: inst.ID, PullNumber: 1, Trigger: "mention", UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	h.st.EnqueueEliRepair(h.ctx, row, 5)
	h.c.runEliRepair(h.ctx, row)

	if row.State != "failed" {
		t.Fatalf("the repair did not fail: %+v", row)
	}
	if row.Details.FailureCode != "provider_billing" {
		t.Fatalf("the failure code is %q, not provider_billing: %+v", row.Details.FailureCode, row)
	}
	for _, want := range []string{"cannot pay", "402", "credit"} {
		if !strings.Contains(row.Message, want) {
			t.Errorf("the message does not say %q: %s", want, row.Message)
		}
	}
	last := fake.comments[len(fake.comments)-1]
	if !strings.Contains(last, "cannot pay") {
		t.Errorf("the comment on the pull request does not say what happened:\n%s", last)
	}
}

// A provider says it cannot pay in its own words, and the words differ
// between providers: an account with no credit, a balance too low, a quota
// already spent. Each is a payment the person can make, not a connection to
// test, and each is recorded as one.
func TestARepairNamesAProviderThatCannotPayWhateverTheProviderCalledIt(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		message string
		code    string
	}{
		{"payment required", http.StatusPaymentRequired, "the provider answered 402", "provider_billing"},
		{"no funds", http.StatusInternalServerError, "the provider answered 500: Insufficient account funds", "provider_billing"},
		{"quota spent", http.StatusTooManyRequests, "the provider answered 429: You exceeded your current quota, insufficient_quota", "provider_billing"},
		{"balance too low", http.StatusForbidden, "the provider answered 403: credit balance is too low", "provider_billing"},
		{"rate limited", http.StatusTooManyRequests, "the provider answered 429: rate limit reached", "rate_limited"},
		{"key refused", http.StatusUnauthorized, "the provider answered 401: the provider refused the key", "provider_credentials"},
		{"context", http.StatusBadRequest, "the provider answered 400: maximum context length is 8192 tokens", "context_limit"},
		{"unknown", http.StatusInternalServerError, "the provider answered 500: upstream exploded", "provider_unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			failure := repairProviderError(&providerRefusal{status: tt.status, message: tt.message})
			var coded interface{ RepairFailureCode() string }
			if !errors.As(failure, &coded) || coded.RepairFailureCode() != tt.code {
				t.Fatalf("the failure is %v, recorded as %q, not %q", failure, coded.RepairFailureCode(), tt.code)
			}
		})
	}
}

// providerRefusal is a provider's answer as the adapters hand it over: the
// status it replied with, and the words it used.
type providerRefusal struct {
	status  int
	message string
}

func (e *providerRefusal) Error() string   { return e.message }
func (e *providerRefusal) HTTPStatus() int { return e.status }

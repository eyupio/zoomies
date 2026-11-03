package controller

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type attentionFake struct {
	*repairFake
	pullErr   error
	checksErr error
	runState  github.WorkflowRunState
	runErr    error
	// asked, when set, counts the times GitHub was asked about a run, and
	// onAsk is called as it is, for a test that needs something to happen
	// while the answer is on its way.
	asked *int
	onAsk func()
}

func (f attentionFake) GetWorkflowRun(context.Context, string, int64) (*github.WorkflowRunState, error) {
	if f.asked != nil {
		*f.asked++
	}
	if f.onAsk != nil {
		f.onAsk()
	}
	return &f.runState, f.runErr
}
func (f attentionFake) RepairPullForRun(ctx context.Context, repo string, run int64) (github.RepairPull, error) {
	if f.pullErr != nil {
		return github.RepairPull{}, f.pullErr
	}
	return f.repairFake.RepairPullForRun(ctx, repo, run)
}
func (f attentionFake) RepairChecks(ctx context.Context, repo, sha string) (string, error) {
	if f.checksErr != nil {
		return "", f.checksErr
	}
	return f.repairFake.RepairChecks(ctx, repo, sha)
}

func TestAttentionRechecksGitHubWithoutPersonalRepairSetup(t *testing.T) {
	for _, tc := range []struct {
		name, checks, runConclusion, reason string
		pullErr, checksErr, runErr          error
		known                               bool
	}{
		{name: "still failed", checks: "failed", known: true},
		{name: "passed checks", checks: "passed", reason: "fixed", known: true},
		{name: "passed push run", runConclusion: "success", pullErr: github.ErrRepairUnsupported, reason: "fixed", known: true},
		{name: "closed PR", pullErr: &github.RepairTargetChanged{Code: "closed"}, reason: "closed", known: true},
		{name: "newer commit", pullErr: &github.RepairTargetChanged{Code: "superseded"}, reason: "superseded", known: true},
		{name: "pending checks", checks: "waiting"},
		{name: "cancelled run", checks: "waiting", runConclusion: "cancelled"},
		{name: "push without PR", pullErr: github.ErrRepairUnsupported},
		{name: "checks unavailable", checksErr: errors.New("GitHub unavailable")},
		{name: "run unavailable", runErr: errors.New("GitHub unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := attentionFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: tc.checks}, pullErr: tc.pullErr, checksErr: tc.checksErr, runErr: tc.runErr, runState: github.WorkflowRunState{Status: "completed", Conclusion: tc.runConclusion}}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			repo := inst.Target + "/repo"
			reason, known, deferred := h.c.jobAttentionOnGitHub(h.ctx, repo, 9)
			if reason != tc.reason || known != tc.known || deferred {
				t.Fatalf("reason %q, known %v, deferred %v; want %q, %v, false", reason, known, deferred, tc.reason, tc.known)
			}
			job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Minute)}
			if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			h.c.refreshJobAttention(h.ctx)
			_, count, err := h.st.ListJobs(h.ctx, store.JobFilter{AttentionOnly: true}, store.Page{})
			want := 1
			if tc.reason != "" {
				want = 0
			}
			if err != nil || count != want {
				t.Fatalf("attention count %d, error %v; want %d", count, err, want)
			}
		})
	}
}

func TestRepairRequestRechecksBeforeSpendingAnAttempt(t *testing.T) {
	for _, state := range []string{"passed", "waiting"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: state}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			repo := inst.Target + "/repo"
			if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			user := &store.User{Username: "owner", Role: store.RoleViewer}
			if err := h.st.CreateUser(h.ctx, user); err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}); err != nil {
				t.Fatal(err)
			}
			if err := h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true); err != nil {
				t.Fatal(err)
			}
			_, err := h.c.RequestPinnedEliRepair(h.ctx, user.ID, repo, 1, "", "old", 0, false)
			var changed *github.RepairTargetChanged
			if !errors.As(err, &changed) {
				t.Fatalf("missing changed-target explanation: %v", err)
			}
			rows, _, err := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
			if err != nil || len(rows) != 0 || fake.commits != 0 {
				t.Fatalf("a settled or pending target consumed an attempt: %+v, %v", rows, err)
			}
			ready := h.c.EliReadiness(h.ctx, user.ID, repo, 1, 0)
			if ready.Ready {
				t.Fatal("a repair was offered without failed checks")
			}
			wantCode := "checks_pending"
			if state == "passed" {
				wantCode = "fixed"
			}
			for _, blocker := range ready.Blockers {
				if blocker.Code == wantCode {
					return
				}
			}
			t.Fatalf("missing current check outcome: %+v", ready.Blockers)
		})
	}
}

func TestQueuedRepairStopsBeforeOpeningAProviderWhenChecksAlreadyPassed(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	s.fake.check = "passed"
	row := &store.EliRepair{DedupKey: "queued-fixed", Repo: s.repo, InstallationID: s.inst.ID, PullNumber: 1, HeadSHA: "old", Trigger: "mention", UserID: s.user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, row); err != nil {
		t.Fatal(err)
	}
	s.h.c.runEliRepair(s.h.ctx, row)
	rows := s.rows(t)
	if len(rows) != 1 || rows[0].State != "superseded" || s.fake.commits != 0 {
		t.Fatalf("the already fixed target was repaired: %+v", rows)
	}
}

func TestRepairDoesNotPublishWhenChecksPassWhileTheModelWorks(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	s.fake.check = "failed"
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: s.user.ID, Kind: "openai", Enabled: true}
	if err := s.h.st.CreateAssistantProvider(s.h.ctx, personal); err != nil {
		t.Fatal(err)
	}
	s.fake.onRead = func() { s.fake.check = "passed" }
	s.h.c.openProvider = func(*store.AssistantProvider, string) (assistant.Provider, error) {
		return assistant.NewFake(assistant.FakeOptions{Script: func(assistant.Request) []assistant.Event {
			return []assistant.Event{{Delta: `{"summary":"Fix cause","files":[{"path":"main.go","content":"fixed"}]}`}}
		}}), nil
	}
	row := &store.EliRepair{DedupKey: "fixed-during-model", Repo: s.repo, InstallationID: s.inst.ID, PullNumber: 1, HeadSHA: "old", Trigger: "mention", UserID: s.user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, row); err != nil {
		t.Fatal(err)
	}
	s.h.c.runEliRepair(s.h.ctx, row)
	if row.State != "superseded" || s.fake.commits != 0 {
		t.Fatalf("published over passing checks: %+v", row)
	}
}

func TestPendingChecksDoNotUndoAnObservedFix(t *testing.T) {
	now := time.Now().UTC()
	h := newHarnessAt(t, func() time.Time { return now })
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "waiting"}
	h.c.clients.entries[inst.ID] = &clientEntry{client: attentionFake{repairFake: fake}, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	repo := inst.Target + "/repo"
	job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Hour)}
	if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := h.st.RecordJobAttentionRun(h.ctx, repo, 9, "fixed", h.st.Now(), true); err != nil {
		t.Fatal(err)
	}
	// A settled run is looked at again after an hour, not five minutes.
	now = now.Add(61 * time.Minute)
	h.c.refreshJobAttention(h.ctx)
	_, count, err := h.st.ListJobs(h.ctx, store.JobFilter{AttentionOnly: true}, store.Page{})
	if err != nil || count != 0 {
		t.Fatalf("pending checks resurrected an already fixed failure: %d, %v", count, err)
	}
	fake.check = "failed"
	now = now.Add(61 * time.Minute)
	h.c.refreshJobAttention(h.ctx)
	_, count, err = h.st.ListJobs(h.ctx, store.JobFilter{AttentionOnly: true}, store.Page{})
	if err != nil || count != 1 {
		t.Fatalf("a confirmed failure did not return to the inbox: %d, %v", count, err)
	}
}

// The quota a check spends is the installation's, and runner registration is
// spent from the same one. A check GitHub refuses for it says nothing about the
// run, so it is not an attempt: the run stays first in line instead of waiting
// behind a refusal, and the tick stops asking. The installation is stood down
// the way every other background sweep stands down from one, so the ticks that
// follow do not ask either until the time GitHub named.
func TestAttentionStandsDownFromAnInstallationGitHubRateLimits(t *testing.T) {
	now := time.Now().UTC()
	h := newHarnessAt(t, func() time.Time { return now })
	inst := h.installation()
	repo := inst.Target + "/repo"
	for run := int64(9); run <= 10; run++ {
		job := &store.Job{GitHubJobID: run, GitHubRunID: run, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: now.Add(-time.Hour)}
		if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	client := func(runErr error, checks string) *clientEntry {
		fake := attentionFake{
			repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: checks},
			runErr:     runErr, asked: &asked, runState: github.WorkflowRunState{Status: "completed"},
		}
		return &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	}
	due := func() int {
		t.Helper()
		runs, err := h.st.JobAttentionRunsToCheck(h.ctx, 5)
		if err != nil {
			t.Fatal(err)
		}
		return len(runs)
	}

	h.c.clients.entries[inst.ID] = client(&github.RateLimitedError{ResetAt: now.Add(10 * time.Minute)}, "")
	h.c.refreshJobAttention(h.ctx)
	if asked != 1 {
		t.Fatalf("GitHub was asked %d times in a tick that was refused for quota, want once", asked)
	}
	if !h.c.githubHeld(inst.ID, now) {
		t.Fatal("the installation was not stood down after GitHub said it was out of quota")
	}
	if got := due(); got != 2 {
		t.Fatalf("%d runs are due again after a refused tick, want both: a refusal is not an attempt", got)
	}

	now = now.Add(time.Minute)
	h.c.refreshJobAttention(h.ctx)
	if asked != 1 {
		t.Fatalf("GitHub was asked again (%d times in all) while the installation was stood down", asked)
	}

	now = now.Add(10 * time.Minute)
	h.c.clients.entries[inst.ID] = client(nil, "failed")
	h.c.refreshJobAttention(h.ctx)
	if asked != 3 {
		t.Fatalf("GitHub was asked %d times in all, want both runs checked once the hold had ended", asked)
	}
	if got := due(); got != 0 {
		t.Fatalf("%d runs are still due after both were answered", got)
	}
}

// A loop that fails without a word cannot be told from one with nothing to do.
// On a large database the query behind it outlived its deadline, and the
// failures sat in the inbox, unchecked, with no line in any log to say why.
// Each of its three steps says so at warn level when it fails, as the loops
// beside it do.
func TestTheAttentionLoopSaysWhyItCouldNotDoItsWork(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		// arrange makes the step fail: cancel is the context the tick runs in.
		arrange func(cancel context.CancelFunc, fake *attentionFake)
	}{
		{"listing the runs", "could not list the failed runs to check on GitHub",
			func(cancel context.CancelFunc, _ *attentionFake) { cancel() }},
		{"recording the answer", "could not record what GitHub said about a failed run",
			func(cancel context.CancelFunc, fake *attentionFake) { fake.onAsk = cancel }},
		{"pruning what is no longer needed", "could not prune the records of runs checked on GitHub",
			// A refusal for quota records nothing, so the tick reaches its last
			// step with the context cancelled by then.
			func(cancel context.CancelFunc, fake *attentionFake) {
				fake.onAsk = cancel
				fake.runErr = &github.RateLimitedError{ResetAt: time.Now().Add(time.Hour)}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			var logged bytes.Buffer
			h.c.log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
			inst := h.installation()
			job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: inst.Target + "/repo", State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Hour)}
			if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			fake := attentionFake{
				repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "failed"},
				runState:   github.WorkflowRunState{Status: "completed"},
			}
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			tc.arrange(cancel, &fake)
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}

			h.c.refreshJobAttention(ctx)
			if !strings.Contains(logged.String(), "level=WARN") || !strings.Contains(logged.String(), tc.want) {
				t.Fatalf("a tick whose step failed logged %q, want a warning that says %q", logged.String(), tc.want)
			}
		})
	}
}

// Eli repairs a pull request's branch and nothing else, so what started a failed
// run decides whether anyone is offered a repair. The attention loop is the one
// place every failed run is read from GitHub, so it records the answer.
func TestTheAttentionCheckRecordsWhatStartedAFailedRun(t *testing.T) {
	for _, event := range []string{"push", "pull_request"} {
		t.Run(event, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := attentionFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: "failed"}, pullErr: github.ErrRepairUnsupported, runState: github.WorkflowRunState{Status: "completed", Conclusion: "failure", Event: event}}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
			repo := inst.Target + "/repo"
			job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Minute)}
			if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
				t.Fatal(err)
			}

			h.c.refreshJobAttention(h.ctx)

			got, err := h.st.GetJobByGitHubID(h.ctx, 1)
			if err != nil || got.RunEvent != event {
				t.Fatalf("the failed run's event reads %q (%v), want %q", got.RunEvent, err, event)
			}
		})
	}
}

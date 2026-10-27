package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

func TestAskFirstOffersAreDeduplicatedAndCannotWriteAStaleHead(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	repo := inst.Target + "/repo"
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, Enabled: true, AskFirst: true, DailyLimit: 5}); err != nil {
		t.Fatal(err)
	}
	user := &store.User{Username: "owner", Role: store.RoleViewer}
	h.st.CreateUser(h.ctx, user)
	h.st.SetEliIdentity(h.ctx, store.EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"})
	h.st.ConfirmEliIdentity(h.ctx, user.ID, 42, true)
	for _, job := range []int{1, 2, 1} {
		body, _ := json.Marshal(map[string]any{"action": "completed", "repository": map[string]any{"full_name": repo}, "workflow_job": map[string]any{"id": job, "run_id": 9, "head_sha": "old", "conclusion": "failure", "status": "completed"}})
		if err := h.c.enqueueRepairWebhook(h.ctx, inst, "workflow_job", body); err != nil {
			t.Fatal(err)
		}
	}
	offers, err := h.c.EliOpportunities(h.ctx, user.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %+v %v", offers, err)
	}
	attempts, _, _ := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(attempts) != 0 || fake.commits != 0 {
		t.Fatal("offer spent an attempt or wrote")
	}
	fake.noWrite = true
	visible, err := h.c.EliOpportunities(h.ctx, user.ID)
	if err != nil || len(visible) != 0 {
		t.Fatal("revoked access can see offers")
	}
	fake.noWrite = false
	fake.commits = 1
	if _, err := h.c.ActEliOpportunity(h.ctx, user.ID, offers[0].ID, "accept", false); err == nil {
		t.Fatal("stale offer accepted")
	}
	attempts, _, _ = h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if len(attempts) != 0 {
		t.Fatal("stale offer spent an attempt")
	}
}

type repairEventProvider struct{ events []assistant.Event }

func (p repairEventProvider) Chat(context.Context, assistant.Request) (assistant.Stream, error) {
	return &repairEventStream{events: p.events}, nil
}
func (p repairEventProvider) Check(context.Context) (assistant.CheckResult, error) {
	return assistant.CheckResult{}, nil
}

type repairEventStream struct{ events []assistant.Event }

func (s *repairEventStream) Next(context.Context) (assistant.Event, bool) {
	if len(s.events) == 0 {
		return assistant.Event{}, false
	}
	e := s.events[0]
	s.events = s.events[1:]
	return e, true
}
func (s *repairEventStream) Close() error { return nil }
func TestRepairUsageIsSavedEvenWhenTheProviderCutsItsAnswer(t *testing.T) {
	var steps []store.EliRepairStep
	// An explicit limit, so that a cut is the end of the attempt: an automatic one asks again.
	m := repairModel{provider: repairEventProvider{events: []assistant.Event{{Usage: &assistant.Usage{InputTokens: 21, OutputTokens: 12, Reported: true}}, {Done: true, Cut: true}}}, maxTokens: 12000, record: func(s store.EliRepairStep) { steps = append(steps, s) }}
	_, err := m.Answer(context.Background(), []prrepair.Message{{Role: "user", Content: "evidence"}})
	if err == nil || !strings.Contains(err.Error(), "output limit") || len(steps) != 1 || steps[0].InputTokens != 21 || !steps[0].UsageReported {
		t.Fatalf("steps %+v error %v", steps, err)
	}
	m.provider = repairEventProvider{events: []assistant.Event{{Delta: "{}"}, {Done: true}}}
	if _, err := m.Answer(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if steps[1].UsageReported {
		t.Fatal("missing usage was reported as zero")
	}
}

// roomProvider answers like a model that needs a given amount of room: asked
// for less it is cut at the ceiling, and asked for that much or more it
// answers. It records every ceiling it was asked for, and fails a request when
// refuse says to, so a test can watch the room a repair asks for.
type roomProvider struct {
	needs  int
	asked  *[]int
	refuse func(limit int) error
}

func (p roomProvider) Chat(_ context.Context, req assistant.Request) (assistant.Stream, error) {
	*p.asked = append(*p.asked, req.MaxTokens)
	if p.refuse != nil {
		if err := p.refuse(req.MaxTokens); err != nil {
			return nil, err
		}
	}
	if req.MaxTokens < p.needs {
		return &repairEventStream{events: []assistant.Event{{Usage: &assistant.Usage{InputTokens: 100, OutputTokens: req.MaxTokens, Reported: true}}, {Done: true, Cut: true}}}, nil
	}
	return &repairEventStream{events: []assistant.Event{{Delta: "{}"}, {Done: true}}}, nil
}
func (p roomProvider) Check(context.Context) (assistant.CheckResult, error) {
	return assistant.CheckResult{}, nil
}

// statusFailure is a provider answering with an HTTP status, which is how the
// adapters' errors carry it.
type statusFailure int

func (s statusFailure) Error() string   { return fmt.Sprintf("the provider answered %d", int(s)) }
func (s statusFailure) HTTPStatus() int { return int(s) }

// A thinking model can spend all of a repair's 12000 tokens on reasoning, and a
// repair that failed there could only be rescued by a person editing the
// provider and starting another attempt. An automatic one asks again with room
// enough, and keeps both attempts in its history.
func TestAnAutomaticRepairRaisesItsOwnOutputLimitWhenTheModelRunsOutOfRoom(t *testing.T) {
	var asked []int
	var steps []store.EliRepairStep
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked}, record: func(s store.EliRepairStep) { steps = append(steps, s) }}
	got, err := m.Answer(context.Background(), []prrepair.Message{{Role: "user", Content: "evidence"}})
	if err != nil || got != "{}" {
		t.Fatalf("answer %q error %v", got, err)
	}
	// A repair starts at 12000, and twice that is 24000, which is enough.
	if !slices.Equal(asked, []int{12000, 24000}) {
		t.Fatalf("asked for %v", asked)
	}
	// The cut attempt keeps what the provider counted for it. The one after it
	// reported nothing, which is not zero.
	if len(steps) != 2 || !steps[0].UsageReported || steps[0].OutputTokens != 12000 || steps[1].UsageReported {
		t.Fatalf("steps %+v", steps)
	}
}

// Only the step that needs more room should pay to find out how much: the steps
// after it are the same model reading the same evidence.
func TestARepairKeepsTheRoomItFoundForItsNextStep(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked}}
	for range 2 {
		if _, err := m.Answer(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(asked, []int{12000, 24000, 24000}) {
		t.Fatalf("asked for %v", asked)
	}
}

// The room a repair found it needs is kept for its later steps, but their
// prompts are longer, and the model's context window has to hold the prompt and
// the room together. A step whose prompt no longer fits beside the kept room is
// refused by the provider, and it is not a step that ran out of room: it needs
// no more than it did before, so it asks again from where a repair starts and
// is answered, where it would otherwise have ended the repair on a context limit
// that the room it did not need had caused.
func TestARaisedRoomIsGivenUpWhenALongerPromptNoLongerFitsBesideIt(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked}}
	if _, err := m.Answer(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	// By the next step the window leaves 12000 beside the prompt, and the answer
	// needs 5000.
	m.provider = roomProvider{needs: 5000, asked: &asked, refuse: func(limit int) error {
		if limit > 12000 {
			return statusFailure(400)
		}
		return nil
	}}
	if _, err := m.Answer(context.Background(), nil); err != nil {
		t.Fatalf("the second step failed, though 12000 was room enough: %v", err)
	}
	if !slices.Equal(asked, []int{12000, 24000, 24000, 12000}) {
		t.Fatalf("asked for %v", asked)
	}
}

// Going back to the starting room is one more try and not a loop: a provider
// that refuses the request there as well is refusing something else, and the
// repair says what it said.
func TestARefusalAtTheStartingRoomIsNotAskedAgain(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked}}
	if _, err := m.Answer(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	m.provider = roomProvider{needs: 5000, asked: &asked, refuse: func(int) error { return statusFailure(400) }}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), "output limit") || !slices.Equal(asked, []int{12000, 24000, 24000, 12000}) {
		t.Fatalf("asked for %v, error %v", asked, err)
	}
}

// A number on the provider is the operator's, and may be a cap on what an
// attempt costs, so it is kept exactly and a cut ends the repair as it did.
func TestAnOutputLimitAnOperatorSetIsNeverRaised(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked}, maxTokens: 12000}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "output limit") || !slices.Equal(asked, []int{12000}) {
		t.Fatalf("asked for %v, error %v", asked, err)
	}
}

// Room is not asked for without end. A model still cut at the most a provider
// may be given has nothing more to be asked, and the repair says where it stopped.
func TestAnAutomaticRepairStopsAtTheMostItMayAsk(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 1 << 30, asked: &asked}}
	_, err := m.Answer(context.Background(), nil)
	// 12000 doubled is 24000 and then 48000, and doubled again is 96000, which is
	// held to the 65536 that a provider may be given.
	if !slices.Equal(asked, []int{12000, 24000, 48000, 65536}) {
		t.Fatalf("asked for %v", asked)
	}
	if err == nil || !strings.Contains(err.Error(), "65536") || repairFailureCode(err.Error()) != "output_limit" {
		t.Fatalf("error %v", err)
	}
}

// A provider that took the room it was asked for before and refuses more has
// reached its model's own ceiling, or has no context left beside the prompt.
// The answer was still cut, so the repair ends there, and the account it gives
// is where the cut was (12000), not the amount that was refused (24000).
func TestAProviderThatRefusesMoreRoomLeavesTheCutAsTheAnswer(t *testing.T) {
	var asked []int
	refuse := func(limit int) error {
		if limit > 12000 {
			return statusFailure(400)
		}
		return nil
	}
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked, refuse: refuse}}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "output limit") || !strings.Contains(err.Error(), "12000") || strings.Contains(err.Error(), "24000") {
		t.Fatalf("error %v", err)
	}
	if !slices.Equal(asked, []int{12000, 24000}) {
		t.Fatalf("asked for %v", asked)
	}
}

// Lowering the reasoning effort is advice for the kinds of provider that take
// one. Given to an Anthropic provider it is a dead end: the form has no such
// setting, and the API refuses it, so the person who went looking for it would
// find nothing to change.
func TestAdviceToLowerTheReasoningEffortIsOnlyGivenWhereTheProviderTakesOne(t *testing.T) {
	refuseAbove12000 := func(limit int) error {
		if limit > 12000 {
			return statusFailure(400)
		}
		return nil
	}
	for _, kind := range []assistant.Kind{assistant.KindOpenAICompatible, assistant.KindOpenAI, assistant.KindAnthropic} {
		for name, provider := range map[string]roomProvider{
			"cut at the most it asks": {needs: 1 << 30},
			"refused more room":       {needs: 20000, refuse: refuseAbove12000},
		} {
			t.Run(fmt.Sprintf("%s, %s", kind, name), func(t *testing.T) {
				var asked []int
				provider.asked = &asked
				m := &repairModel{provider: provider, kind: kind}
				_, err := m.Answer(context.Background(), nil)
				if want := assistant.SupportsReasoningEffort(kind); err == nil || strings.Contains(err.Error(), "reasoning effort") != want {
					t.Fatalf("error %v, want advice about the reasoning effort = %v", err, want)
				}
			})
		}
	}
}

// A model whose output ceiling lies between where it was cut and twice that is
// refused the doubled ask every time, so an empty limit can never give it the
// room it does have. Only a person can set the number, and the message that
// ends the repair is the one place that can tell them to.
func TestAModelRefusedMoreRoomIsToldToSetTheOutputLimitToItsMaximum(t *testing.T) {
	var asked []int
	refuseAbove16384 := func(limit int) error {
		if limit > 16384 {
			return statusFailure(400)
		}
		return nil
	}
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked, refuse: refuseAbove16384}}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "set the provider's Output limit to that model's maximum") {
		t.Fatalf("error %v", err)
	}
}

// A refusal means "no more room" only after a cut. On the first request it is
// whatever it says: a model name the provider does not know, or a request it
// cannot take for some other reason.
func TestARefusalOnTheFirstRequestIsNotReadAsAnOutputLimit(t *testing.T) {
	var asked []int
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked, refuse: func(int) error { return statusFailure(400) }}}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), "output limit") || !slices.Equal(asked, []int{12000}) {
		t.Fatalf("asked for %v, error %v", asked, err)
	}
}

// Only a refusal of the request itself says the room was too much. A rate limit
// while asking again is a rate limit, and the person is told so.
func TestAnotherFailureWhileAskingForMoreRoomIsNotHiddenBehindTheCut(t *testing.T) {
	var asked []int
	refuse := func(limit int) error {
		if limit > 12000 {
			return statusFailure(429)
		}
		return nil
	}
	m := &repairModel{provider: roomProvider{needs: 20000, asked: &asked, refuse: refuse}}
	_, err := m.Answer(context.Background(), nil)
	if err == nil || repairFailureCode(err.Error()) != "rate_limited" {
		t.Fatalf("error %v", err)
	}
}

// scriptedRepairModel answers each step with the next reply it was given, and
// with the last one for as long as it is asked.
type scriptedRepairModel struct{ replies []string }

func (m *scriptedRepairModel) Answer(context.Context, []prrepair.Message) (string, error) {
	reply := m.replies[0]
	if len(m.replies) > 1 {
		m.replies = m.replies[1:]
	}
	return reply, nil
}

// repairFiles serves the files a test names, and an empty file for any other
// path, as the GitHub reader does.
type repairFiles map[string]prrepair.File

func (f repairFiles) ReadFile(_ context.Context, name string) (prrepair.File, error) {
	return f[name], nil
}

// What a repair ends with is read from the words of its message, so a message
// that names the source allowance has to land where the allowance does. A repair
// that ran out of rounds on reads it was refused is a context limit, like the
// source budget error it replaced, and the hint under it tells the person to
// narrow the request: asking again sends the same snapshot, and the model asks
// for the same reads.
func TestARepairThatRanOutOfRoundsOnRefusedReadsEndsAsAContextLimit(t *testing.T) {
	line := "0123456789abcde\n"
	reader := repairFiles{"big.go": {Path: "big.go", Content: strings.Repeat(line, (40<<10)/len(line))}}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		reader[name] = prrepair.File{Path: name, Content: strings.Repeat(line, prrepair.MaxFileBytes/len(line))}
	}
	model := &scriptedRepairModel{replies: []string{`{"read":["a.go","b.go","c.go","d.go"]}`, `{"read":["big.go"]}`}}
	_, err := prrepair.Generate(context.Background(), model, reader, prrepair.Snapshot{}, "fix the failing check", false)
	if err == nil || repairFailureCode(err.Error()) != "context_limit" {
		t.Fatalf("error %v ends as %q, want context_limit", err, repairFailureCode(fmt.Sprint(err)))
	}
}

// An edit the model could not apply is the model's plan being unusable, not a
// source that could not be read. The page used to send the person to check the
// App's Contents permission for a preimage the model got wrong.
func TestAnEditOutsideTheSuppliedTextIsNotReportedAsABrokenRead(t *testing.T) {
	const path = "package-lock.json"
	content := strings.Repeat("0123456789abcde\n", prrepair.MaxFileBytes/16) + "not supplied\n"
	reply, err := json.Marshal(prrepair.Plan{Summary: "Fix it", Edits: []prrepair.Edit{{Path: path, Before: "not supplied\n", After: "supplied\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = prrepair.Generate(context.Background(), &scriptedRepairModel{replies: []string{string(reply)}}, repairFiles{}, prrepair.Snapshot{Files: []prrepair.File{{Path: path, Content: content, Mode: "100644"}}}, "fix the failing check", false)
	if err == nil || repairFailureCode(err.Error()) != "unsupported_fix" {
		t.Fatalf("error %v ends as %q, want unsupported_fix", err, repairFailureCode(fmt.Sprint(err)))
	}
}

type unresolvedRepairRun struct {
	*repairFake
	err error
}

func (f unresolvedRepairRun) RepairPullForRun(context.Context, string, int64) (github.RepairPull, error) {
	return github.RepairPull{Number: 313, HeadSHA: "old"}, f.err
}

// A closed PR reaches readiness the way GitHub's client reports it, as a
// changed target with a code of its own, and not as an unsupported one: the
// first is the answer to the question the person asked, and settles the failure
// they asked about; the second is a PR nothing can repair, which settles
// nothing. A fixture that gave a closed PR the second shape would pass whatever
// the first branch did.
func TestReadinessExplainsAClosedPRAndDistinguishesGitHubFailure(t *testing.T) {
	closed := &github.RepairTargetChanged{Code: "closed", Message: "PR #313 is closed or merged. This failure no longer needs a PR repair."}
	for _, tc := range []struct {
		name, code, message string
		err                 error
		// settles is whether the failure the person asked about leaves the
		// attention list because of the answer.
		settles bool
	}{
		{"closed PR", "closed", closed.Message, closed, true},
		{"a newer commit", "superseded", "PR #313 has newer commits than this failed run.", &github.RepairTargetChanged{Code: "superseded", Message: "PR #313 has newer commits than this failed run."}, true},
		{"an unsupported target", "pull", "an Eli repair needs an open PR on a non-default branch in this repository; forks are not writable", fmt.Errorf("%w: an Eli repair needs an open PR on a non-default branch in this repository; forks are not writable", github.ErrRepairUnsupported), false},
		{"GitHub unavailable", "github", "GitHub could not resolve this repair target. Try checking readiness again.", errors.New("transport failed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := unresolvedRepairRun{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}, err: tc.err}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
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
			repo := inst.Target + "/repo"
			job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Hour)}
			if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
				t.Fatal(err)
			}

			ready := h.c.EliReadiness(h.ctx, user.ID, repo, 0, 9)
			if ready.Ready || ready.PullNumber != 313 {
				t.Fatalf("readiness lost the blocked PR: %+v", ready)
			}
			found := false
			for _, blocker := range ready.Blockers {
				found = found || blocker.Code == tc.code && blocker.Message == tc.message
			}
			if !found {
				t.Fatalf("missing actionable blocker: %+v", ready.Blockers)
			}
			assertEveryBlockedCheckSaysWhy(t, ready)
			for _, c := range ready.Checks {
				if c.Code == "pull" && c.Blocker != tc.code {
					t.Errorf("the pull request check says it was stopped by %q, want %q", c.Blocker, tc.code)
				}
			}
			_, left, err := h.st.ListJobs(h.ctx, store.JobFilter{AttentionOnly: true}, store.Page{})
			if want := map[bool]int{true: 0, false: 1}[tc.settles]; err != nil || left != want {
				t.Fatalf("%d failures still need attention (error %v), want %d: a changed target settles the failure it was asked about, and nothing else does", left, err, want)
			}
		})
	}
}

// Asking whether a failed run can be repaired reads the PR's checks, and what
// they say is also the answer to whether the failure still needs anybody: the
// attention list is told, so it does not wait for the loop that checks GitHub
// to find out what the person has just seen. Passed checks settle the failure.
// Failed ones are an observation that undoes an earlier fix, and checks that
// are still running or were cancelled say nothing either way, so an earlier fix
// stands.
func TestReadinessTellsTheAttentionListWhatItLearnedAboutTheFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		// before is what was recorded about the run already, empty for nothing.
		before, checks string
		wantBlocker    string
		// stays is whether the failure is still in the attention list afterwards.
		stays bool
	}{
		{"checks have passed", "", "passed", "fixed", false},
		{"checks are still failing", "", "failed", "", true},
		{"checks are failing again after a fix", "fixed", "failed", "", true},
		{"checks are pending after a fix", "fixed", "waiting", "checks_pending", false},
		{"checks are pending with nothing known", "", "waiting", "checks_pending", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			inst := h.installation()
			fake := attentionFake{repairFake: &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType), check: tc.checks}}
			h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
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
			repo := inst.Target + "/repo"
			job := &store.Job{GitHubJobID: 1, GitHubRunID: 9, Repo: repo, State: store.JobCompleted, Conclusion: "failure", QueuedAt: h.st.Now().Add(-time.Hour)}
			if _, err := h.st.UpsertJob(h.ctx, job); err != nil {
				t.Fatal(err)
			}
			if tc.before != "" {
				if err := h.st.RecordJobAttentionRun(h.ctx, repo, 9, tc.before, h.st.Now(), true); err != nil {
					t.Fatal(err)
				}
			}

			ready := h.c.EliReadiness(h.ctx, user.ID, repo, 0, 9)
			found := tc.wantBlocker == ""
			for _, blocker := range ready.Blockers {
				found = found || blocker.Code == tc.wantBlocker
			}
			if !found {
				t.Fatalf("missing the %q blocker: %+v", tc.wantBlocker, ready.Blockers)
			}
			_, left, err := h.st.ListJobs(h.ctx, store.JobFilter{AttentionOnly: true}, store.Page{})
			if want := map[bool]int{true: 1, false: 0}[tc.stays]; err != nil || left != want {
				t.Fatalf("%d failures still need attention (error %v), want %d", left, err, want)
			}
		})
	}
}

// readinessSetup is a person who could repair: linked and confirmed, with a
// provider of their own, on a repository whose policy allows repairs. A test
// then breaks the one thing it is about.
func readinessSetup(t *testing.T) *commandSetup {
	t.Helper()
	s := newCommandSetup(t, 5)
	s.link(t, true)
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: s.user.ID, Kind: "openai", Enabled: true}
	if err := s.h.st.CreateAssistantProvider(s.h.ctx, personal); err != nil {
		t.Fatal(err)
	}
	s.fake.check = "failed"
	return s
}

// checkSummary is "code:status" for each check, in the order readiness makes them.
func checkSummary(r EliReadiness) []string {
	out := make([]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		out = append(out, c.Code+":"+c.Status)
	}
	return out
}

// A blocked check says which blocker stopped it, because the check alone cannot: GitHub
// not answering is folded into whichever check it was making, and a page that reads only
// the check would send the person to choose another pull request. A check that passed, or
// was never reached, says nothing of the kind. (When the target has changed, the answer's
// blockers hold that change alone, so a check blocked earlier names a blocker that is no
// longer among them, and that is right: it is what stopped that check.)
func assertEveryBlockedCheckSaysWhy(t *testing.T, r EliReadiness) {
	t.Helper()
	for _, c := range r.Checks {
		switch {
		case c.Status == EliCheckBlocked && c.Blocker == "":
			t.Errorf("blocked check %q does not say which blocker stopped it", c.Code)
		case c.Status != EliCheckBlocked && c.Blocker != "":
			t.Errorf("%s check %q names a blocker, %q", c.Status, c.Code, c.Blocker)
		}
	}
}

func blockerCodes(r EliReadiness) []string {
	out := []string{}
	for _, b := range r.Blockers {
		out = append(out, b.Code)
	}
	return out
}

const nineOK = "identity:ok provider:ok installation:ok access:ok policy:ok budget:ok pull:ok checks:ok permissions:ok"

// A page that says what Eli can do needs the pull request's own words and a
// finding for every check, so a person sees what passed and not only what failed.
func TestReadinessReturnsThePullRequestAndEveryCheckWhenReady(t *testing.T) {
	s := readinessSetup(t)
	s.fake.title, s.fake.author, s.fake.url, s.fake.base = "Retry transient 502s", "dana", "https://github.com/acme/repo/pull/1", "main"
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	if !ready.Ready || len(ready.Blockers) != 0 {
		t.Fatalf("not ready: %+v", ready)
	}
	if got := strings.Join(checkSummary(ready), " "); got != nineOK {
		t.Fatalf("checks: %s", got)
	}
	for _, c := range ready.Checks {
		if c.Message == "" || c.Message == "Not checked yet." {
			t.Errorf("an ok check has no finding: %+v", c)
		}
	}
	want := &EliReadinessPull{Number: 1, Title: "Retry transient 502s", Author: "dana", Branch: "feature", Base: "main", URL: "https://github.com/acme/repo/pull/1", Open: true}
	if ready.Pull == nil || *ready.Pull != *want {
		t.Fatalf("pull: %+v, want %+v", ready.Pull, want)
	}
}

// A person whose GitHub account cannot write to the repository is not told what
// is in it, so the pull request is not looked up for them, and the checks after
// the one that stopped them say they were never reached.
func TestReadinessStopsAtWriteAccessAndLeavesTheRestUnchecked(t *testing.T) {
	s := readinessSetup(t)
	s.fake.noWrite = true
	s.fake.title = "A private title"
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	assertEveryBlockedCheckSaysWhy(t, ready)
	want := "identity:ok provider:ok installation:ok access:blocked policy:unchecked budget:unchecked pull:unchecked checks:unchecked permissions:unchecked"
	if got := strings.Join(checkSummary(ready), " "); got != want {
		t.Fatalf("checks: %s", got)
	}
	if ready.Ready || ready.Pull != nil {
		t.Fatalf("readiness gave a pull request to someone who cannot write: %+v", ready)
	}
	if len(ready.Blockers) != 1 || ready.Blockers[0] != (EliBlocker{"access", "Your linked GitHub account needs write access to this repository."}) {
		t.Fatalf("blockers changed: %+v", ready.Blockers)
	}
	if ready.Checks[3].Message != ready.Blockers[0].Message {
		t.Fatalf("the blocked check does not carry the blocker's words: %q", ready.Checks[3].Message)
	}
	if ready.Checks[4].Message != "Not checked yet." {
		t.Fatalf("an unreached check should say so: %q", ready.Checks[4].Message)
	}
}

// With no linked account the provider and the installation are still looked at,
// because they are separate things to fix and a person should fix them in one go.
func TestReadinessWithNoLinkedAccountBlocksIdentityAndStillChecksProvider(t *testing.T) {
	s := newCommandSetup(t, 5)
	personal := &store.AssistantProvider{Name: "Personal", OwnerID: s.user.ID, Kind: "openai", Enabled: true}
	if err := s.h.st.CreateAssistantProvider(s.h.ctx, personal); err != nil {
		t.Fatal(err)
	}
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	assertEveryBlockedCheckSaysWhy(t, ready)
	want := "identity:blocked provider:ok installation:ok access:unchecked policy:unchecked budget:unchecked pull:unchecked checks:unchecked permissions:unchecked"
	if got := strings.Join(checkSummary(ready), " "); got != want {
		t.Fatalf("checks: %s", got)
	}
	if ready.Pull != nil || len(ready.Blockers) != 1 || ready.Blockers[0] != (EliBlocker{"identity", "Link and confirm your GitHub account in repair setup."}) {
		t.Fatalf("blockers or pull changed: %+v", ready)
	}
}

type closedRepairPull struct{ *repairFake }

func (closedRepairPull) RepairPull(context.Context, string, int) (github.RepairPull, error) {
	return github.RepairPull{Number: 7, Title: "Old work", Author: "dana", Branch: "feature", HeadSHA: "old"},
		&github.RepairTargetChanged{Code: "closed", Message: "PR #7 is closed or merged."}
}

// A closed pull request is the answer to what the person asked, and it reads
// better with its title. The blockers keep their old shape: the changed target
// stands alone, as it always has.
func TestReadinessKeepsThePullRequestWhenItIsClosed(t *testing.T) {
	s := readinessSetup(t)
	s.h.c.clients.entries[s.inst.ID] = &clientEntry{client: closedRepairPull{s.fake}, updatedAt: s.inst.UpdatedAt.Truncate(time.Millisecond)}
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 7, 0)
	assertEveryBlockedCheckSaysWhy(t, ready)
	if ready.Pull == nil || ready.Pull.Title != "Old work" || ready.Pull.Open {
		t.Fatalf("the closed pull request was lost: %+v", ready.Pull)
	}
	want := "identity:ok provider:ok installation:ok access:ok policy:ok budget:ok pull:blocked checks:unchecked permissions:unchecked"
	if got := strings.Join(checkSummary(ready), " "); got != want {
		t.Fatalf("checks: %s", got)
	}
	if len(ready.Blockers) != 1 || ready.Blockers[0] != (EliBlocker{"closed", "PR #7 is closed or merged."}) {
		t.Fatalf("blockers: %+v", ready.Blockers)
	}
}

// A repair can be asked for a job this fleet never ran, so readiness names
// what failed from GitHub's report of the run: the jobs, the step each stopped
// at, and every step for the page's disclosure. A GitHub that cannot answer
// leaves the list empty; it never blocks the readiness it rides on.
func TestReadinessNamesTheFailedJobsGitHubReportsWhenTheFleetHasNoRecord(t *testing.T) {
	s := readinessSetup(t)
	s.fake.failedJobs = &github.RepairFailureRun{
		RunID: 9, RunURL: "https://github.com/acme/repo/actions/runs/9", Workflow: "Publish", RunNumber: 1042,
		Jobs: []github.RepairFailedJob{{
			ID: 55, Name: "build-and-push", Conclusion: "failure",
			URL:  "https://github.com/acme/repo/actions/runs/9/job/55",
			Step: "Install dependencies and build", StepNumber: 8,
			Steps: []github.RepairJobStep{{Number: 7, Name: "Checkout", Conclusion: "success"}, {Number: 8, Name: "Install dependencies and build", Conclusion: "failure"}},
		}},
	}

	byRun := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 9)
	if len(byRun.FailedJobs) != 1 {
		t.Fatalf("failed jobs: %+v", byRun.FailedJobs)
	}
	job := byRun.FailedJobs[0]
	if job.ID != 55 || job.Name != "build-and-push" || job.StepNumber != 8 || job.Step != "Install dependencies and build" ||
		job.URL != "https://github.com/acme/repo/actions/runs/9/job/55" || len(job.Steps) != 2 {
		t.Errorf("job: %+v", job)
	}
	if got := s.fake.failedJobsAsked; len(got) != 1 || got[0][0] != int64(9) || got[0][1] != "" {
		t.Errorf("the failure was asked for by %v, want its run alone", got)
	}

	s.fake.failedJobsAsked = nil
	byPull := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	if len(byPull.FailedJobs) != 1 {
		t.Fatalf("failed jobs by pull request: %+v", byPull.FailedJobs)
	}
	if got := s.fake.failedJobsAsked; len(got) != 1 || got[0][0] != int64(0) || got[0][1] != "old" {
		t.Errorf("the failure was asked for by %v, want the pull request's head", got)
	}
}

// The failed jobs sit behind the same write-access check as the pull request:
// a person who may not see the repository is not told what is in it, and
// GitHub is not asked.
func TestReadinessTellsTheFailedJobsToNobodyWhoCannotWrite(t *testing.T) {
	s := readinessSetup(t)
	s.fake.noWrite = true
	s.fake.failedJobs = &github.RepairFailureRun{
		RunID: 9, Workflow: "Publish",
		Jobs: []github.RepairFailedJob{{ID: 55, Name: "build-and-push", Conclusion: "failure"}},
	}
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 9)
	if len(ready.FailedJobs) != 0 {
		t.Fatalf("failed jobs were shown to someone who cannot write: %+v", ready.FailedJobs)
	}
	if len(s.fake.failedJobsAsked) != 0 {
		t.Errorf("GitHub was asked for jobs before access was shown: %v", s.fake.failedJobsAsked)
	}
}

// A GitHub that does not answer the jobs read still answers readiness, and the
// job names and addresses are workflow-author text, held to the same clipping
// and link rules as everything else in the answer.
func TestReadinessFailedJobsAreClippedAndNeverBlock(t *testing.T) {
	s := readinessSetup(t)
	long := strings.Repeat("é", 5000)
	s.fake.failedJobs = &github.RepairFailureRun{
		RunID: 9, Workflow: "CI",
		Jobs: []github.RepairFailedJob{
			{ID: 55, Name: long, Conclusion: "failure", URL: "javascript:alert(1)", Step: long, StepNumber: 8},
			{ID: 56, Name: "second", Conclusion: "failure"},
		},
	}
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 9)
	if len(ready.FailedJobs) != 2 {
		t.Fatalf("failed jobs: %+v", ready.FailedJobs)
	}
	first := ready.FailedJobs[0]
	if n := len([]rune(first.Name)); n != 200 {
		t.Errorf("name is %d runes, want 200", n)
	}
	if n := len([]rune(first.Step)); n != 200 {
		t.Errorf("step is %d runes, want 200", n)
	}
	if first.URL != "" {
		t.Errorf("a scheme that runs script was kept as a link: %q", first.URL)
	}

	s.fake.failedJobsErr = errors.New("github: read failed jobs: not found")
	s.fake.failedJobs = nil
	ready = s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 9)
	if !ready.Ready || len(ready.FailedJobs) != 0 {
		t.Fatalf("an unreadable jobs list blocked readiness: %+v", ready)
	}
}

// Checks that have passed, or have not finished, are not a failure to repair,
// and the checklist says which of the two it is on the check that decided it.
func TestReadinessSaysWhetherTheChecksHavePassedOrAreStillRunning(t *testing.T) {
	for _, tc := range []struct{ check, code string }{{"passed", "fixed"}, {"waiting", "checks_pending"}} {
		t.Run(tc.check, func(t *testing.T) {
			s := readinessSetup(t)
			s.fake.check = tc.check
			ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
			if ready.Ready || ready.Pull == nil {
				t.Fatalf("readiness: %+v", ready)
			}
			var checks EliCheck
			for _, c := range ready.Checks {
				if c.Code == "checks" {
					checks = c
				}
			}
			if checks.Status != EliCheckBlocked || checks.Message == "" {
				t.Fatalf("the checks check: %+v", checks)
			}
			assertEveryBlockedCheckSaysWhy(t, ready)
			if checks.Blocker != tc.code {
				t.Fatalf("the checks check says it was stopped by %q, want %q", checks.Blocker, tc.code)
			}
			if !slices.Contains(blockerCodes(ready), tc.code) {
				t.Fatalf("blockers %v lack %s", blockerCodes(ready), tc.code)
			}
		})
	}
}

// GitHub failing to say whether the checks have passed is not the checks failing, and not
// a pull request to choose: the check says that GitHub did not answer, so a page can offer
// to ask again.
func TestReadinessSaysGitHubDidNotAnswerWhenItCannotReadTheChecks(t *testing.T) {
	s := readinessSetup(t)
	s.fake.checkErr = errors.New("transport failed")
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	want := "identity:ok provider:ok installation:ok access:ok policy:ok budget:ok pull:ok checks:blocked permissions:unchecked"
	if got := strings.Join(checkSummary(ready), " "); got != want {
		t.Fatalf("checks: %s", got)
	}
	if got := blockerCodes(ready); len(got) != 1 || got[0] != "github" {
		t.Fatalf("blockers: %v", got)
	}
	assertEveryBlockedCheckSaysWhy(t, ready)
	if ready.Checks[7].Blocker != "github" {
		t.Fatalf("the checks check says it was stopped by %q, want github", ready.Checks[7].Blocker)
	}
}

// A missing App permission is a check of its own, with the sentence it always had.
func TestReadinessNamesAMissingAppPermission(t *testing.T) {
	s := readinessSetup(t)
	s.h.gh.SetPermissions(map[string]string{"contents": "read", "pull_requests": "write", "actions": "write"})
	ready := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0)
	assertEveryBlockedCheckSaysWhy(t, ready)
	last := ready.Checks[len(ready.Checks)-1]
	if last.Code != "permissions" || last.Status != EliCheckBlocked || last.Message != "Update the GitHub App permission: contents write" {
		t.Fatalf("permissions check: %+v", last)
	}
	if got := blockerCodes(ready); len(got) != 1 || got[0] != "permissions" {
		t.Fatalf("blockers: %v", got)
	}
}

// A page that offers to start a repair has to know whether the repository would
// have started one itself. Readiness already reads the policy for its fifth
// check, so it says which mode that is, and says nothing before it gets there.
func TestReadinessNamesTheRepositorysMode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape func(*store.EliRepairPolicy)
		want  string
	}{
		{"automatic", func(p *store.EliRepairPolicy) { p.Automatic = true }, "automatic"},
		{"ask first", func(p *store.EliRepairPolicy) { p.AskFirst = true }, "ask_first"},
		{"manual", func(*store.EliRepairPolicy) {}, "manual"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := readinessSetup(t)
			policies, err := s.h.st.EliRepairPolicies(s.h.ctx)
			if err != nil || len(policies) != 1 {
				t.Fatalf("policies %+v %v", policies, err)
			}
			policy := policies[0]
			tc.shape(&policy)
			if err := s.h.st.SetEliRepairPolicy(s.h.ctx, policy); err != nil {
				t.Fatal(err)
			}
			if got := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0).PolicyMode; got != tc.want {
				t.Errorf("mode %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("before the policy is reached", func(t *testing.T) {
		s := newCommandSetup(t, 5)
		if got := s.h.c.EliReadiness(s.h.ctx, s.user.ID, s.repo, 1, 0).PolicyMode; got != "" {
			t.Errorf("mode %q for someone who is not linked, want none", got)
		}
	})
}

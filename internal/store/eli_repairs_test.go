package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestRepairQueueDeduplicatesBeforeChargingAndKeepsInterruptedWritesStopped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := &EliRepair{Repo: "acme/repo", DedupKey: "comment:1"}
	added, e := s.EnqueueEliRepair(ctx, r)
	if e != nil || !added {
		t.Fatalf("enqueue %v %v", added, e)
	}
	duplicate := &EliRepair{Repo: r.Repo, DedupKey: r.DedupKey}
	added, e = s.EnqueueEliRepair(ctx, duplicate)
	if e != nil || added || duplicate.ID != r.ID {
		t.Fatalf("duplicate %+v %v %v", duplicate, added, e)
	}
	r.State = "working"
	r.CommitSHA = "fix"
	if e = s.SaveEliRepair(ctx, r); e != nil {
		t.Fatal(e)
	}
	if e = s.InterruptEliRepairs(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = s.NextEliRepair(ctx)
	if !errors.Is(e, ErrNotFound) {
		t.Fatalf("repeated interrupted repair: %v", e)
	}
	for i := 0; i < 105; i++ {
		_, e = s.EnqueueEliRepair(ctx, &EliRepair{Repo: fmt.Sprintf("acme/%d", i), DedupKey: fmt.Sprint(i)})
		if e != nil {
			t.Fatal(e)
		}
	}
	own, e := s.IsEliRepairCommit(ctx, r.Repo, "fix")
	if e != nil || !own {
		t.Fatal("old repair commit lost loop protection")
	}
}
func TestPersonalProviderDefaultsAreIndependentAndNamesAreScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, owner := range []string{"", "usr_one", "usr_two"} {
		p := &AssistantProvider{OwnerID: owner, Name: "Model", Kind: "openai", Enabled: true}
		if e := s.CreateAssistantProvider(ctx, p); e != nil {
			t.Fatal(e)
		}
		if e := s.SetDefaultAssistantProvider(ctx, p.ID); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := s.ListAssistantProviders(ctx)
	if e != nil || len(rows) != 3 {
		t.Fatalf("%v %v", rows, e)
	}
	for _, r := range rows {
		if !r.IsDefault {
			t.Errorf("lost default for %q", r.OwnerID)
		}
	}
}
func TestCheckingRepairsRemainVisibleBeyondTheHistoryLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r := &EliRepair{Repo: "acme/check", DedupKey: "check"}
	s.EnqueueEliRepair(ctx, r)
	r.State = "checking"
	s.SaveEliRepair(ctx, r)
	for i := 0; i < 105; i++ {
		s.EnqueueEliRepair(ctx, &EliRepair{Repo: fmt.Sprint(i), DedupKey: fmt.Sprint(i)})
	}
	rows, e := s.CheckingEliRepairs(ctx)
	if e != nil || len(rows) != 1 || rows[0].ID != r.ID {
		t.Fatalf("checking rows %v %v", rows, e)
	}
}

func TestGitHubRepairConsentIsPinnedAndClearedWhenAnAdministratorChangesTheLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := &User{Username: "owner", Role: RoleViewer}
	if e := s.CreateUser(ctx, user); e != nil {
		t.Fatal(e)
	}
	link := EliIdentity{UserID: user.ID, GitHubUserID: 42, GitHubLogin: "octo"}
	s.SetEliIdentity(ctx, link)
	rows, _ := s.EliIdentities(ctx)
	if rows[0].Confirmed {
		t.Fatal("admin enabled personal provider use")
	}
	if e := s.ConfirmEliIdentity(ctx, user.ID, 43, true); !errors.Is(e, ErrNotFound) {
		t.Fatalf("stale identity: %v", e)
	}
	if e := s.ConfirmEliIdentity(ctx, user.ID, 42, true); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.EliIdentities(ctx)
	if !rows[0].Confirmed {
		t.Fatal("owner confirmation not kept")
	}
	s.SetEliIdentity(ctx, link)
	rows, _ = s.EliIdentities(ctx)
	if rows[0].Confirmed {
		t.Fatal("admin retained an earlier confirmation")
	}
	p := &AssistantProvider{OwnerID: user.ID, Name: "Personal", Kind: "openai"}
	s.CreateAssistantProvider(ctx, p)
	s.DeleteUser(ctx, user.ID)
	if _, e := s.GetAssistantProvider(ctx, p.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("deleted account retained provider: %v", e)
	}
}

func TestAnExplicitUIRepairCanRetryAFailureWithoutRepeatingAnActiveRepair(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := &EliRepair{Repo: "acme/repo", UserID: "owner", DedupKey: "ui:repo:1:head:owner"}
	if added, err := s.EnqueueEliRepair(ctx, first); err != nil || !added {
		t.Fatal(err)
	}
	duplicate := &EliRepair{Repo: first.Repo, UserID: first.UserID, DedupKey: first.DedupKey}
	if added, err := s.EnqueueEliRepair(ctx, duplicate); err != nil || added || duplicate.ID != first.ID {
		t.Fatalf("duplicate %+v %v", duplicate, err)
	}
	first.State = "failed"
	if err := s.SaveEliRepair(ctx, first); err != nil {
		t.Fatal(err)
	}
	retry := &EliRepair{Repo: first.Repo, UserID: first.UserID, DedupKey: first.DedupKey}
	if added, err := s.EnqueueEliRepair(ctx, retry); err != nil || !added || retry.ID == first.ID {
		t.Fatalf("retry %+v %v", retry, err)
	}
	duplicate = &EliRepair{Repo: first.Repo, UserID: first.UserID, DedupKey: first.DedupKey}
	if added, err := s.EnqueueEliRepair(ctx, duplicate); err != nil || added || duplicate.ID != retry.ID {
		t.Fatalf("active retry duplicated %+v %v", duplicate, err)
	}
}
func TestPersonalRepairHistoryIsFilteredBeforeThePageLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	own := &EliRepair{Repo: "own/repo", UserID: "owner", DedupKey: "own"}
	s.EnqueueEliRepair(ctx, own)
	for i := 0; i < 110; i++ {
		s.EnqueueEliRepair(ctx, &EliRepair{Repo: fmt.Sprint("repo/", i), UserID: fmt.Sprint("user", i), DedupKey: fmt.Sprint("other", i)})
	}
	rows, _, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Scoped: true, OwnerID: "owner", Limit: MaxEliRepairPage})
	if err != nil || len(rows) != 1 || rows[0].ID != own.ID {
		t.Fatalf("lost personal history: %v %v", rows, err)
	}
}
func TestEliMigrationPreservesExistingProviderOwnershipAndFleetConsent(t *testing.T) {
	ctx := context.Background()
	path := atSchemaBefore(t, "0093_eli_repairs.sql")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO assistant_providers(id,name,kind,model,enabled,is_default,fleet_access,owner_id,key_enc,created_at,updated_at) VALUES('shared','Model','openai','model',1,0,1,'',x'0102',1,2),('subscription','Plan','claude_code','sonnet',1,1,0,'owner',NULL,3,4)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	shared, err := s.GetAssistantProvider(ctx, "shared")
	if err != nil || shared.OwnerID != "" || !shared.FleetAccess || len(shared.KeyEnc) != 2 {
		t.Fatalf("lost shared provider: %+v %v", shared, err)
	}
	own, err := s.GetAssistantProvider(ctx, "subscription")
	if err != nil || own.OwnerID != "owner" || !own.IsDefault || own.Model != "sonnet" {
		t.Fatalf("lost subscription: %+v %v", own, err)
	}
}

func TestManualRepairPoliciesNeedNoInstallationPayerAndOffersSpendNoAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	inst, _, _ := seedPool(t, s)
	policy := EliRepairPolicy{Repo: "acme/repo", InstallationID: inst.ID, Enabled: true, AskFirst: true}
	if err := s.SetEliRepairPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	policies, err := s.EliRepairPolicies(ctx)
	if err != nil || len(policies) != 1 || !policies[0].AskFirst || policies[0].ProviderID != "" {
		t.Fatalf("policy: %+v %v", policies, err)
	}
	user := &User{Username: "owner", Role: RoleViewer}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	offer := EliRepairOpportunity{Repo: policy.Repo, InstallationID: inst.ID, PullNumber: 1, HeadSHA: "old", RunID: 1}
	for range 3 {
		if err := s.OfferEliRepair(ctx, offer); err != nil {
			t.Fatal(err)
		}
	}
	offers, err := s.EliRepairOpportunities(ctx, user.ID)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %+v %v", offers, err)
	}
	rows, _, _ := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: MaxEliRepairPage})
	if len(rows) != 0 {
		t.Fatal("offers created charged attempts")
	}
	if err := s.DismissEliOpportunity(ctx, offers[0].ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.OfferEliRepair(ctx, offer); err != nil {
		t.Fatal(err)
	}
	own, _ := s.EliRepairOpportunities(ctx, user.ID)
	others, _ := s.EliRepairOpportunities(ctx, "another")
	if len(own) != 0 || len(others) != 1 {
		t.Fatal("dismissal was not personal or was undone by redelivery")
	}
	offer.HeadSHA = "new"
	if err := s.OfferEliRepair(ctx, offer); err != nil {
		t.Fatal(err)
	}
	own, _ = s.EliRepairOpportunities(ctx, user.ID)
	if len(own) != 1 || own[0].HeadSHA != "new" {
		t.Fatal("new head suppressed by old dismissal")
	}
	if err := s.SetEliOpportunityState(ctx, own[0].ID, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := s.OfferEliRepair(ctx, offer); err != nil {
		t.Fatal(err)
	}
	own, _ = s.EliRepairOpportunities(ctx, user.ID)
	if len(own) != 0 {
		t.Fatal("accepted offer reopened")
	}
}
func TestRepairDetailsPreserveReportedAndUnknownUsage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	row := &EliRepair{Repo: "acme/repo", DedupKey: "details"}
	if _, err := s.EnqueueEliRepair(ctx, row); err != nil {
		t.Fatal(err)
	}
	row.Details = EliRepairDetails{Model: "test", Stage: "prepare_fix", SourceBytes: 123, Steps: []EliRepairStep{{InputTokens: 10, OutputTokens: 5, UsageReported: true}, {UsageReported: false}}}
	if err := s.SaveEliRepair(ctx, row); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: MaxEliRepairPage})
	if err != nil || len(rows) != 1 || len(rows[0].Details.Steps) != 2 || !rows[0].Details.Steps[0].UsageReported || rows[0].Details.Steps[1].UsageReported || rows[0].Details.Steps[0].InputTokens != 10 {
		t.Fatalf("usage %+v %v", rows, err)
	}
}

func TestConcurrentOfferAcceptancesCannotChargeDifferentPeople(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := &EliRepair{Repo: "acme/repo", DedupKey: "offer:one", UserID: "one"}
	if _, err := s.EnqueueEliRepair(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := &EliRepair{Repo: "acme/repo", DedupKey: "offer:one", UserID: "two"}
	if _, err := s.EnqueueEliRepair(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("another owner accepted: %v", err)
	}
	rows, _, _ := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: MaxEliRepairPage})
	if len(rows) != 1 {
		t.Fatal("duplicate attempt charged")
	}
}

// A GitHub account may serve one Zoomies user. The refusal that names the
// holding row, and the move that answers it, are what make the days after an
// instance move workable: the same person verifying their link against the
// account the migration re-created must be offered the press, not a driver
// error, and the moved row must not inherit a confirmation nobody re-made.
func TestALinkAnotherUserHoldsIsRefusedAndRelinkedByName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	makeUser := func(name string) string {
		u := &User{Username: name, Role: RoleAdmin}
		if e := s.CreateUser(ctx, u); e != nil {
			t.Fatal(e)
		}
		return u.ID
	}
	oldUser := makeUser("previous")
	newUser := makeUser("moved")
	// The migration's legacy: the old account holds the link the new account
	// is being given.
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: oldUser, GitHubUserID: 42, GitHubLogin: "octo"}); e != nil {
		t.Fatal(e)
	}
	link := EliIdentity{UserID: newUser, GitHubUserID: 42, GitHubLogin: "octo"}
	if e := s.SetEliIdentity(ctx, link); !errors.Is(e, ErrEliIdentityHeld) {
		t.Fatalf("held link: %v", e)
	}
	rows, _ := s.EliIdentities(ctx)
	if len(rows) != 1 || rows[0].UserID != oldUser || rows[0].GitHubUserID != 42 {
		t.Fatalf("a refused link wrote something: %+v", rows)
	}
	// The holding account's own link is an update: the login may have changed
	// case on GitHub, and the row is theirs to replace.
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: oldUser, GitHubUserID: 42, GitHubLogin: "Octo"}); e != nil {
		t.Fatalf("holder relink: %v", e)
	}
	rows, _ = s.EliIdentities(ctx)
	if rows[0].GitHubLogin != "Octo" || rows[0].Confirmed {
		t.Fatalf("holder update: %+v", rows)
	}
	// The move answers exactly the refusal above, and answers it changing the
	// confirmation: nobody's allow survives being dragged across accounts.
	holder, e := s.RelinkEliIdentity(ctx, link)
	if e != nil || holder != oldUser {
		t.Fatalf("relink: holder %q %v", holder, e)
	}
	rows, _ = s.EliIdentities(ctx)
	if len(rows) != 1 || rows[0].UserID != newUser || rows[0].Confirmed {
		t.Fatalf("moved link: %+v", rows)
	}
	// ConfirmEliIdentity is pinned to the numeric identity, so the moved row's
	// stale screen approves nothing: the user allows it again themselves.
	if e := s.ConfirmEliIdentity(ctx, newUser, 42, true); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RelinkEliIdentity(ctx, EliIdentity{UserID: oldUser, GitHubUserID: 42, GitHubLogin: "octo"}); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.EliIdentities(ctx)
	if len(rows) != 1 || rows[0].UserID != oldUser || rows[0].Confirmed {
		t.Fatalf("second move: %+v", rows)
	}
	// A link nobody holds goes straight onto the new user.
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: newUser, GitHubUserID: 99, GitHubLogin: "other"}); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.EliIdentities(ctx)
	if len(rows) != 2 {
		t.Fatalf("unheld row: %+v", rows)
	}
	// And moving a link when nobody else holds it is a first link.
	held, e := s.RelinkEliIdentity(ctx, EliIdentity{UserID: newUser, GitHubUserID: 99, GitHubLogin: "other"})
	if e != nil || held != "" {
		t.Fatalf("unheld move: %q %v", held, e)
	}
}

// Deleting the holding account releases the link, which is the third way a
// post-migration tie clears: the operator removes the duplicate account and
// the verification succeeds without a move.
func TestADeletedUserReleasesItsLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	oldUser := &User{Username: "previous", Role: RoleAdmin}
	if e := s.CreateUser(ctx, oldUser); e != nil {
		t.Fatal(e)
	}
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: oldUser.ID, GitHubUserID: 42, GitHubLogin: "octo"}); e != nil {
		t.Fatal(e)
	}
	newUser := &User{Username: "fresh", Role: RoleAdmin}
	if e := s.CreateUser(ctx, newUser); e != nil {
		t.Fatal(e)
	}
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: newUser.ID, GitHubUserID: 42, GitHubLogin: "octo"}); !errors.Is(e, ErrEliIdentityHeld) {
		t.Fatalf("held link: %v", e)
	}
	if e := s.DeleteUser(ctx, oldUser.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.SetEliIdentity(ctx, EliIdentity{UserID: newUser.ID, GitHubUserID: 42, GitHubLogin: "octo"}); e != nil {
		t.Fatal(e)
	}
}

// What a repair was about is written once, when it is queued or when the worker
// has resolved the pull request, and the worker then saves the row's details
// many times as it moves through its stages. The case has to ride along with
// every one of those saves, or the page that follows the repair would lose the
// failure it was started for the first time the stage changed.
func TestARepairKeepsItsCaseThroughEveryWrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	want := &EliRepairCase{
		PullTitle: "Retry transient 502s in the registry client",
		PullURL:   "https://github.com/acme/repo/pull/7",
		Author:    "dana",
		Branch:    "fix/registry-retry",
		RunURL:    "https://github.com/acme/repo/actions/runs/9",
		Workflow:  "CI",
		RunNumber: 482,
		Jobs: []EliRepairCaseJob{{
			Name:       "Go (api-2)",
			Step:       "Run go test -race ./internal/...",
			URL:        "https://github.com/acme/repo/actions/runs/9/job/3",
			DurationMS: 192000,
		}},
	}
	r := &EliRepair{Repo: "acme/repo", DedupKey: "ui:case", Details: EliRepairDetails{Case: want}}
	if added, e := s.EnqueueEliRepair(ctx, r); e != nil || !added {
		t.Fatalf("enqueue: added=%v, %v", added, e)
	}
	r.State = "working"
	r.Details.Stage = "prepare_fix"
	r.Details.Model = "a-model"
	if e := s.SaveEliRepair(ctx, r); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetEliRepair(ctx, r.ID, "", false)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got.Details.Case, want) {
		t.Fatalf("the case did not survive a save:\n got %+v\nwant %+v", got.Details.Case, want)
	}
	if got.Details.Stage != "prepare_fix" || got.State != "working" {
		t.Fatalf("the save itself was lost: %+v", got)
	}
}

// Every repair made before cases existed has details with no "case" in them,
// and some have details older still. They must open without an error.
func TestARepairWrittenBeforeCasesExistStillReads(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := insertRepair(t, s, EliRepair{Repo: "acme/repo", State: "failed", Details: EliRepairDetails{Model: "m", Stage: "source"}})
	got, e := s.GetEliRepair(ctx, id, "", false)
	if e != nil || got.Details.Case != nil || got.Details.Stage != "source" {
		t.Fatalf("a row with no case: %+v, %v", got, e)
	}
	if _, e := s.exec(ctx, `UPDATE eli_repairs SET details=? WHERE id=?`, `{"model":"m"}`, id); e != nil {
		t.Fatal(e)
	}
	got, e = s.GetEliRepair(ctx, id, "", false)
	if e != nil || got.Details.Case != nil || got.Details.Model != "m" {
		t.Fatalf("a row with the oldest details: %+v, %v", got, e)
	}
}

// One repair is read by the rule the history is read by: the owner and an
// administrator reach it and nobody else does, an automatic repair has no owner
// to match, and a repair someone cleared from the history is still there by its
// id, since clearing hides a row and does nothing else.
func TestGetEliRepairScopesLikeTheHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	own := insertRepair(t, s, EliRepair{Repo: "acme/repo", UserID: "usr_one", State: "failed"})
	automatic := insertRepair(t, s, EliRepair{Repo: "acme/repo", Trigger: "automatic", State: "failed"})
	for _, tc := range []struct {
		name    string
		id      string
		owner   string
		scoped  bool
		visible bool
	}{
		{"the owner reads their own", own, "usr_one", true, true},
		{"another user does not", own, "usr_two", true, false},
		{"an administrator reads any", own, "", false, true},
		{"an administrator reads an automatic one", automatic, "", false, true},
		{"a scoped caller with no identity reads nothing", own, "", true, false},
		{"nobody scoped reaches an automatic repair", automatic, "usr_one", true, false},
		{"a missing id is not found", "rep_missing", "", false, false},
	} {
		got, e := s.GetEliRepair(ctx, tc.id, tc.owner, tc.scoped)
		switch {
		case tc.visible && (e != nil || got == nil || got.ID != tc.id):
			t.Errorf("%s: %+v, %v", tc.name, got, e)
		case !tc.visible && !errors.Is(e, ErrNotFound):
			t.Errorf("%s: want ErrNotFound, got %+v, %v", tc.name, got, e)
		}
	}
	results, e := s.ClearEliRepairs(ctx, []string{own}, "usr_one", true)
	if e != nil || len(results) != 1 || !results[0].OK {
		t.Fatalf("clearing: %+v, %v", results, e)
	}
	if got, e := s.GetEliRepair(ctx, own, "usr_one", true); e != nil || got.ID != own {
		t.Fatalf("a cleared repair is still readable by id: %+v, %v", got, e)
	}
}

package controller

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// A repair that ended without a fix asks for a person, and it went on asking
// after there was nothing left to do: nothing looked at a finished repair
// again, so one whose pull request had been closed days before still headed the
// Needs you list. The three things that settle it are the three that retire an
// offer. What Eli did stays in the history; only the asking stops.
func TestAFinishedRepairStopsAskingOnceItsPullRequestNoLongerNeedsIt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		state         string
		head, commit  string
		checks        string
		checkErr      error
		closed        bool
		wantCode      string
		wantNeedsYou  bool
		wantMessageIn string
	}{
		{name: "the pull request closed", state: "failed", head: "old", checks: "failed", closed: true, wantCode: "closed", wantMessageIn: "closed or merged"},
		{name: "the pull request has a newer commit", state: "failed", head: "older", checks: "failed", wantCode: "superseded", wantMessageIn: "newer commit"},
		{name: "a newer commit followed Eli's", state: "checks_failed", head: "older", commit: "eli", checks: "failed", wantCode: "superseded", wantMessageIn: "newer commit"},
		{name: "the checks now pass", state: "failed", head: "old", checks: "passed", wantCode: "fixed", wantMessageIn: "now pass"},
		{name: "the checks on Eli's commit now pass", state: "checks_failed", head: "older", commit: "old", checks: "passed", wantCode: "fixed", wantMessageIn: "now pass"},
		{name: "nobody saw a result and the checks pass", state: "unverified", head: "older", commit: "old", checks: "passed", wantCode: "fixed", wantMessageIn: "now pass"},
		{name: "the same commit still fails", state: "failed", head: "old", checks: "failed", wantNeedsYou: true},
		{name: "Eli's commit still fails", state: "checks_failed", head: "older", commit: "old", checks: "failed", wantNeedsYou: true},
		{name: "the checks are still running", state: "failed", head: "old", checks: "waiting", wantNeedsYou: true},
		{name: "the checks cannot be read", state: "failed", head: "old", checkErr: errors.New("GitHub unavailable"), wantNeedsYou: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := readinessSetup(t)
			s.fake.check, s.fake.checkErr = tc.checks, tc.checkErr
			var client github.Client = s.fake
			if tc.closed {
				client = closedRepairPull{s.fake}
			}
			s.h.c.clients.entries[s.inst.ID] = &clientEntry{client: client, updatedAt: s.inst.UpdatedAt.Truncate(time.Millisecond)}

			// The fake's pull request is at "old".
			r := &store.EliRepair{DedupKey: "ui:" + tc.name, InstallationID: s.inst.ID, Repo: s.repo, PullNumber: 7, HeadSHA: tc.head, UserID: s.user.ID, Trigger: "mention"}
			if added, err := s.h.st.EnqueueEliRepair(s.h.ctx, r); err != nil || !added {
				t.Fatalf("EnqueueEliRepair = %v, %v", added, err)
			}
			r.State, r.CommitSHA, r.Message = tc.state, tc.commit, "What Eli said when it stopped."
			if err := s.h.st.SaveEliRepair(s.h.ctx, r); err != nil {
				t.Fatal(err)
			}

			s.h.c.retireSettledEliRepairs(s.h.ctx)

			_, needs, err := s.h.st.ListEliRepairsPage(s.h.ctx, store.EliRepairFilter{Group: "needs_action"})
			if err != nil || (needs == 1) != tc.wantNeedsYou {
				t.Fatalf("needs you = %d (err %v), want listed = %v", needs, err, tc.wantNeedsYou)
			}
			got, err := s.h.st.GetEliRepair(s.h.ctx, r.ID, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNeedsYou {
				if got.State != tc.state || got.Message != r.Message || got.Details.FailureCode != "" {
					t.Fatalf("a repair that still needs someone was rewritten: %q %q %q", got.State, got.Message, got.Details.FailureCode)
				}
				return
			}
			if got.State != "superseded" || got.Details.FailureCode != tc.wantCode {
				t.Fatalf("state %q, code %q; want superseded, %q", got.State, got.Details.FailureCode, tc.wantCode)
			}
			// The reason comes first, and what Eli found is not thrown away with it.
			if !strings.Contains(got.Message, tc.wantMessageIn) || !strings.Contains(got.Message, r.Message) {
				t.Fatalf("message %q does not say %q and keep what Eli said", got.Message, tc.wantMessageIn)
			}
			if got.CommitSHA != tc.commit || got.HeadSHA != tc.head {
				t.Fatalf("the record of what Eli worked on changed: head %q, commit %q", got.HeadSHA, got.CommitSHA)
			}
			if len(s.fake.comments) != 0 {
				t.Fatalf("settling a repair posted on the pull request: %q", s.fake.comments)
			}
		})
	}
}

// A repair that is still running, passed, or was already settled is not the
// Needs you list's, and GitHub is not asked about it.
func TestOnlyRepairsThatAskForSomeoneAreCheckedAgain(t *testing.T) {
	s := readinessSetup(t)
	s.h.c.clients.entries[s.inst.ID] = &clientEntry{client: closedRepairPull{s.fake}, updatedAt: s.inst.UpdatedAt.Truncate(time.Millisecond)}
	for _, state := range []string{"queued", "checking", "succeeded", "superseded"} {
		r := &store.EliRepair{DedupKey: "ui:" + state, InstallationID: s.inst.ID, Repo: s.repo, PullNumber: 7, HeadSHA: "old", UserID: s.user.ID, Trigger: "mention"}
		if _, err := s.h.st.EnqueueEliRepair(s.h.ctx, r); err != nil {
			t.Fatal(err)
		}
		r.State, r.Message = state, "as it was"
		if err := s.h.st.SaveEliRepair(s.h.ctx, r); err != nil {
			t.Fatal(err)
		}
		// Each is the newest attempt on its own pull request.
		r.PullNumber++
	}
	s.h.c.retireSettledEliRepairs(s.h.ctx)
	rows, _, err := s.h.st.ListEliRepairsPage(s.h.ctx, store.EliRepairFilter{})
	if err != nil || len(rows) != 4 {
		t.Fatalf("history = %d rows (err %v)", len(rows), err)
	}
	for _, row := range rows {
		if row.Message != "as it was" {
			t.Errorf("a %s repair was rewritten: %q", row.State, row.Message)
		}
	}
}

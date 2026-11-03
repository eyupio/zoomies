package controller

import (
	"context"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type closedRepairDuringChecks struct{ *repairFake }

func (f *closedRepairDuringChecks) RepairPull(context.Context, string, int) (github.RepairPull, error) {
	return github.RepairPull{Number: 1, Open: false}, &github.RepairTargetChanged{Code: "closed", Message: "The PR is closed or merged."}
}

func TestAClosedPRFinishesRepairMonitoringWithoutPermissionAdvice(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	fake := &closedRepairDuringChecks{&repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	r := &store.EliRepair{DedupKey: "closed", Repo: inst.Target + "/repo", InstallationID: inst.ID, PullNumber: 1}
	if _, err := h.st.EnqueueEliRepair(h.ctx, r); err != nil {
		t.Fatal(err)
	}
	published := h.c.Now()
	r.PublishedAt, r.CommitSHA, r.State = &published, "fixed", "checking"
	r.Details.Summary = "Preserve the retry deadline."
	if err := h.st.SaveEliRepair(h.ctx, r); err != nil {
		t.Fatal(err)
	}
	h.advance(31 * time.Second)
	h.c.checkEliRepairs(h.ctx)
	rows, _, err := h.st.ListEliRepairsPage(h.ctx, store.EliRepairFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v error=%v", rows, err)
	}
	got := rows[0]
	if got.State != "superseded" || got.Details.FailureCode != "closed" || got.Details.Summary != r.Details.Summary {
		t.Fatalf("closed PR was not settled: %+v", got)
	}
	if got.Message != "The pull request was closed or merged. Eli stopped watching its checks. The repair commit remains on record." {
		t.Fatalf("wrong advice: %s", got.Message)
	}
}

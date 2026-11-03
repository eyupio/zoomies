package store

import (
	"context"
	"testing"
	"time"
)

func TestRepairCommentsAndCheckOutcomesDoNotReplaceEachOther(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })
	ctx := context.Background()
	r := &EliRepair{Repo: "acme/repo", DedupKey: "comment-metadata"}
	if _, err := s.EnqueueEliRepair(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.State = "checking"
	if err := s.SaveEliRepair(ctx, r); err != nil {
		t.Fatal(err)
	}
	// The observer read this row before the worker's comment finished.
	if err := s.SetEliRepairComment(ctx, r.ID, 11); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	r.State, r.Message = "succeeded", "The checks passed."
	if err := s.SaveEliRepair(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEliRepair(ctx, r.ID, "", false)
	if err != nil || got.CommentID != 11 {
		t.Fatalf("check outcome replaced the comment ID: %+v, %v", got, err)
	}
	settled := got.UpdatedAt
	now = now.Add(time.Minute)
	if err := s.SetEliRepairComment(ctx, r.ID, 22); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetEliRepair(ctx, r.ID, "", false)
	if err != nil || got.CommentID != 22 || got.State != "succeeded" || got.Message != r.Message || !got.UpdatedAt.Equal(settled) {
		t.Fatalf("comment delivery changed the check outcome or polling clock: %+v, %v", got, err)
	}
}

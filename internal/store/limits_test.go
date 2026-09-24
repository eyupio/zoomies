package store

import (
	"context"
	"testing"
	"time"
)

// limits.join_tokens is about tokens that could still enrol a machine. A
// spent or lapsed one is history, and counting it would refuse an operator
// for tokens nobody can use.
func TestOnlyUnusedUnexpiredJoinTokensAreOutstanding(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newTestStoreAt(t, func() time.Time { return now })

	for i, tc := range []struct {
		hash    string
		expires time.Time
	}{
		{"h-live", now.Add(time.Hour)},
		{"h-lapsed", now.Add(-time.Minute)},
		{"h-spent", now.Add(time.Hour)},
	} {
		tok := &JoinToken{TokenHash: tc.hash, Prefix: "zmj_" + string(rune('a'+i)), ExpiresAt: tc.expires}
		if err := s.CreateJoinToken(ctx, tok); err != nil {
			t.Fatalf("CreateJoinToken: %v", err)
		}
	}
	if _, err := s.RedeemJoinToken(ctx, "h-spent", JoinClaim{HostID: "hst_1", Name: "one"}, now); err != nil {
		t.Fatalf("RedeemJoinToken: %v", err)
	}
	n, err := s.CountOutstandingJoinTokens(ctx)
	if err != nil {
		t.Fatalf("CountOutstandingJoinTokens: %v", err)
	}
	if n != 1 {
		t.Fatalf("outstanding = %d, want 1 (only the unused, unexpired token)", n)
	}
}

func TestHostsAndPoolsAreCounted(t *testing.T) {
	s := newTestStore(t)
	seedPool(t, s)
	ctx := context.Background()
	if n, err := s.CountPools(ctx); err != nil || n != 1 {
		t.Errorf("CountPools = %d, %v; want 1", n, err)
	}
	if n, err := s.CountHosts(ctx); err != nil || n != 1 {
		t.Errorf("CountHosts = %d, %v; want 1", n, err)
	}
}

package store

import (
	"context"
	"testing"
	"time"
)

// The fence is the safe side of its own question, so anything it cannot read
// as a clear "no" leaves the fleet fenced. A half-finished restore is exactly
// what writes a value nobody can parse, and reading that as "carry on" would
// let two controllers reconcile one fleet's runners from the same rows.
func TestAnUnreadableFenceIsAFence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	fence, err := s.RecoveryFenced(ctx)
	if err != nil {
		t.Fatalf("RecoveryFenced: %v", err)
	}
	if fence.Fenced {
		t.Error("a fresh database is fenced, so no instance could ever start")
	}

	if err := s.SetSetting(ctx, SettingRecoveryFenced, "sort of", false); err != nil {
		t.Fatal(err)
	}
	fence, err = s.RecoveryFenced(ctx)
	if err != nil {
		t.Fatalf("RecoveryFenced: %v", err)
	}
	if !fence.Fenced {
		t.Error("a value that is neither true nor false left the fleet unfenced")
	}
	if fence.Reason == "" {
		t.Error("the fence gives no reason, so nothing tells an operator what to fix")
	}
}

func TestTheFenceCarriesItsReasonAndCanBeLifted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetRecoveryFence(ctx, true, "restored from a backup taken on Tuesday"); err != nil {
		t.Fatalf("SetRecoveryFence: %v", err)
	}
	fence, err := s.RecoveryFenced(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !fence.Fenced || fence.Reason != "restored from a backup taken on Tuesday" {
		t.Errorf("fence = %+v", fence)
	}

	if err := s.SetRecoveryFence(ctx, false, ""); err != nil {
		t.Fatalf("lifting: %v", err)
	}
	fence, err = s.RecoveryFenced(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fence.Fenced {
		t.Error("the fence would not lift")
	}
	// The reason goes with it: a stale reason on a lifted fence is a sentence
	// waiting to be shown next to the wrong thing.
	if raw, _ := s.GetSetting(ctx, SettingRecoveryFencedReason); raw != "" {
		t.Errorf("the reason outlived the fence: %q", raw)
	}
}

// A restore invalidates the credentials the backup froze. What it must not
// invalidate is history: a redeemed join token cannot be used again and is the
// record of how a host got here.
func TestARestoreTakesTheLiveCredentialsAndKeepsTheHistory(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := s.Now()

	user := &User{Username: "ops", Role: RoleAdmin, PasswordHash: "x"}
	if err := s.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, &Session{UserID: user.ID, TokenHash: "sess", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	unused := &JoinToken{TokenHash: "unused", ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateJoinToken(ctx, unused); err != nil {
		t.Fatal(err)
	}
	redeemed := &JoinToken{TokenHash: "redeemed", ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateJoinToken(ctx, redeemed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemJoinToken(ctx, "redeemed", "hst_1", now); err != nil {
		t.Fatal(err)
	}

	sessions, err := s.DeleteAllSessions(ctx)
	if err != nil || sessions != 1 {
		t.Fatalf("DeleteAllSessions = %d, %v", sessions, err)
	}
	joins, err := s.DeleteUnusedJoinTokens(ctx)
	if err != nil || joins != 1 {
		t.Fatalf("DeleteUnusedJoinTokens = %d, %v", joins, err)
	}
	left, err := s.ListJoinTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].TokenHash != "redeemed" {
		t.Errorf("the redeemed token was deleted with the live one: %+v", left)
	}
}

// Revoking keeps the row, because the audit trail has to still say what each
// token was and when it stopped working.
func TestRevokingEveryTokenKeepsWhatEachOneWas(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for _, name := range []string{"ci", "prometheus"} {
		if err := s.CreateAPIToken(ctx, &APIToken{Name: name, Role: RoleViewer, TokenHash: name}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.RevokeAllAPITokens(ctx)
	if err != nil || n != 2 {
		t.Fatalf("RevokeAllAPITokens = %d, %v", n, err)
	}
	tokens, err := s.ListAPITokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("revoking deleted the rows: %d left", len(tokens))
	}
	for _, tok := range tokens {
		if !tok.Revoked {
			t.Errorf("%s is still valid", tok.Name)
		}
	}
	// And a second pass changes nothing, so a repeated restore is not a
	// growing count of "revoked" in somebody's report.
	again, err := s.RevokeAllAPITokens(ctx)
	if err != nil || again != 0 {
		t.Errorf("a second revoke reported %d, %v", again, err)
	}
}

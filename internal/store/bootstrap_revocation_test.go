package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBootstrapRevocationRacesRedemption(t *testing.T) {
	for range 30 {
		s := newTestStore(t)
		ctx := context.Background()
		tok := &JoinToken{TokenHash: "race", ExpiresAt: s.Now().Add(time.Hour)}
		if err := s.CreateJoinToken(ctx, tok); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		revoked, redeemed := make(chan error, 1), make(chan error, 1)
		go func() { <-start; revoked <- s.RevokeUnusedJoinToken(ctx, tok.ID) }()
		go func() {
			<-start
			_, err := s.RedeemJoinToken(ctx, "race", JoinClaim{HostID: "host"}, s.Now())
			redeemed <- err
		}()
		close(start)
		r, d := <-revoked, <-redeemed
		if r == nil {
			if !errors.Is(d, ErrNotFound) {
				t.Fatalf("revoked token redeemed: %v", d)
			}
		} else if !errors.Is(r, ErrJoinTokenUsed) || d != nil {
			t.Fatalf("outcomes: revoke=%v redeem=%v", r, d)
		}
	}
}

func TestBootstrapRevocationFailsClosedOnStoreError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok := &JoinToken{TokenHash: "retained", ExpiresAt: s.Now().Add(time.Hour)}
	if err := s.CreateJoinToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `CREATE TRIGGER deny_revoke BEFORE DELETE ON join_tokens BEGIN SELECT RAISE(ABORT, 'injected revocation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUnusedJoinToken(ctx, tok.ID); err == nil {
		t.Fatal("revocation failure was swallowed")
	}
	if _, err := s.GetJoinToken(ctx, tok.ID); err != nil {
		t.Fatal("failed revocation lost original credential")
	}
}

package store

import (
	"context"
	"testing"
)

// A pool that leaves out the default labels has to stay that way across a
// read, or the scheduler would start offering it jobs its runners never
// advertise.
func TestAPoolRemembersItLeavesOutTheDefaultLabels(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, pool, _ := seedPool(t, s)
	if pool.NoDefaultLabels {
		t.Fatal("a pool created without the setting leaves out the default labels")
	}

	pool.NoDefaultLabels = true
	pool.Ephemeral = false
	if err := s.UpdatePool(ctx, pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	got, err := s.GetPool(ctx, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoDefaultLabels {
		t.Fatal("no_default_labels did not survive a round trip")
	}
}

// 0052 must leave every existing pool advertising the labels it always has,
// so that a job asking for self-hosted keeps landing where it did before the
// upgrade.
func TestAPoolFromBeforeTheUpgradeKeepsItsDefaultLabels(t *testing.T) {
	ctx := context.Background()
	path := atSchemaBefore(t, "0052_pool_no_default_labels.sql")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("upgrading: %v", err)
	}
	defer s.Close()
	var v int
	if err := s.read.QueryRowContext(ctx,
		`SELECT dflt_value IS NOT NULL AND "notnull" FROM pragma_table_info('pools') WHERE name = 'no_default_labels'`).Scan(&v); err != nil {
		t.Fatalf("reading the new column: %v", err)
	}
	if v != 1 {
		t.Fatal("no_default_labels arrived without a NOT NULL default; existing pools would read as unset")
	}
	_, pool, _ := seedPool(t, s)
	if got, _ := s.GetPool(ctx, pool.ID); got.NoDefaultLabels {
		t.Fatal("a pool on an upgraded database leaves out the default labels")
	}
}

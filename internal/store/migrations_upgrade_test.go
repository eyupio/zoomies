package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// atSchema builds a database carrying only the first n migrations, which is
// what a release older than this one left behind.
//
// It applies the real embedded SQL rather than a hand-written schema dump: a
// fixture written by hand drifts from what the migrations actually produce,
// and then the upgrade it is testing is an upgrade from a database that never
// existed.
func atSchema(t *testing.T, n int) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if n > len(migs) {
		t.Fatalf("this build has %d migrations, so it cannot be rolled back to %d", len(migs), n)
	}

	// Applied straight onto the file rather than through Open, which would
	// migrate to head on the way past. Trimming the ledger afterwards was the
	// first attempt and is wrong: it leaves the columns the later migrations
	// added, so re-applying them fails on a duplicate and the fixture is a
	// database that never existed.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatalf("creating the ledger: %v", err)
	}
	for _, m := range migs[:n] {
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			t.Fatalf("applying %s: %v", m.name, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, 0)`, m.name); err != nil {
			t.Fatalf("recording %s: %v", m.name, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the fixture: %v", err)
	}
	return path
}

// A database from an older release has to open on this binary and come out at
// head. It is the upgrade every operator does, and until now nothing ran it.
//
// v0.1-alpha shipped one migration and v0.2-beta ten, so both are real points
// somebody's database is sitting at right now.
func TestADatabaseFromAnOlderReleaseMigratesToHead(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		at   int
	}{
		{"v0.1-alpha, which shipped 0001_init.sql alone", 1},
		{"v0.2-beta, which shipped through 0010", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := atSchema(t, tc.at)

			s, err := Open(ctx, Options{Path: path})
			if err != nil {
				t.Fatalf("opening a %s database: %v", tc.name, err)
			}
			defer s.Close()

			applied, err := s.AppliedMigrations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			migs, err := loadMigrations()
			if err != nil {
				t.Fatal(err)
			}
			if len(applied) != len(migs) {
				t.Fatalf("the ledger has %d migrations after the upgrade, want %d", len(applied), len(migs))
			}
			if err := s.IntegrityCheck(ctx); err != nil {
				t.Errorf("the upgraded database does not pass its integrity check: %v", err)
			}

			// And it is a working fleet database, not just a schema: the
			// columns the later migrations added are readable and writable.
			inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
			if err := s.CreateInstallation(ctx, inst); err != nil {
				t.Fatalf("the upgraded database will not take an installation: %v", err)
			}
			if _, err := s.GetInstallation(ctx, inst.ID); err != nil {
				t.Fatalf("reading it back: %v", err)
			}
		})
	}
}

// The providers migration is the first one to add a column to a table that
// already has rows in a real fleet's database, and the first to add tables that
// point at two of them. An upgrade that left an existing join token unreadable
// -- because a NOT NULL column arrived without a default -- would take every
// agent enrolment with it, and the failure would arrive at the worst moment:
// after the upgrade, on a fleet that was working.
func TestADatabaseWithRowsInItStillTakesTheProviderTables(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, len(shippedMigrations)-1)

	// A join token written by the older release, with none of the columns the
	// upgrade is about to add.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO join_tokens
		(id, token_hash, prefix, created_by, labels, capacity, created_at, expires_at, used_by_id)
		VALUES ('join_old', 'hash', 'zjt_', 'alice', '{}', 2, 1, 9999999999999, '')`); err != nil {
		t.Fatalf("planting a join token at the old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("opening a database from the release before providers: %v", err)
	}
	defer s.Close()

	tokens, err := s.ListJoinTokens(ctx)
	if err != nil {
		t.Fatalf("reading a join token written before the upgrade: %v", err)
	}
	if len(tokens) != 1 || tokens[0].MachineID != "" || tokens[0].ExpectedName != "" {
		t.Fatalf("the pre-upgrade join token came back as %+v, want it unscoped and readable", tokens)
	}

	// And the new tables work on an upgraded database, not only on one this
	// build created from nothing.
	p := &Provider{Kind: ProviderFake, Name: "lab", MaxMachines: 2}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("the upgraded database will not take a provider: %v", err)
	}
	m := &Machine{ProviderID: p.ID}
	if err := s.CreateMachine(ctx, m); err != nil {
		t.Fatalf("the upgraded database will not take a machine: %v", err)
	}
	if _, err := s.GetMachine(ctx, m.ID); err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if err := s.IntegrityCheck(ctx); err != nil {
		t.Errorf("the upgraded database does not pass its integrity check: %v", err)
	}
}

// A migration that fails has to stop startup and leave the ledger clean, so
// the next start tries it again. The failure mode this prevents is the worst
// one available: a half-applied migration recorded as done, which nothing will
// ever try again and which no later migration can assume the shape of.
func TestAFailingMigrationStopsStartupAndIsTriedAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	broken := []migration{
		{name: "9001_first_half_works.sql", sql: `CREATE TABLE half_applied (id TEXT PRIMARY KEY)`},
		{name: "9002_this_one_does_not.sql", sql: `CREATE TABLE nope (id TEXT PRIMARY KEY); THIS IS NOT SQL`},
	}
	err = s.applyMigrations(ctx, broken, map[string]bool{})
	if err == nil {
		t.Fatal("a migration full of nonsense was applied without complaint")
	}
	// Naming the file is the difference between "the database will not open"
	// and knowing which change to look at.
	if !strings.Contains(err.Error(), "9002_this_one_does_not.sql") {
		t.Errorf("the failure does not name the migration: %v", err)
	}

	applied, err := s.AppliedMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range applied {
		names = append(names, a.Name)
	}
	// The one that worked is recorded and the one that failed is not: each
	// migration is its own transaction, so a failure costs only itself.
	if !contains(names, "9001_first_half_works.sql") {
		t.Errorf("the migration that succeeded was rolled back too: %v", names)
	}
	if contains(names, "9002_this_one_does_not.sql") {
		t.Fatalf("the failed migration is in the ledger, so it will never be tried again: %v", names)
	}

	// Fixed and re-run: it applies, and the one before it is not applied twice.
	fixed := []migration{
		broken[0],
		{name: "9002_this_one_does_not.sql", sql: `CREATE TABLE now_it_works (id TEXT PRIMARY KEY)`},
	}
	seen := map[string]bool{"9001_first_half_works.sql": true}
	if err := s.applyMigrations(ctx, fixed, seen); err != nil {
		t.Fatalf("the fixed migration did not apply on the re-run: %v", err)
	}
	if _, err := s.exec(ctx, `INSERT INTO now_it_works (id) VALUES ('x')`); err != nil {
		t.Errorf("the re-run did not actually create the table: %v", err)
	}
}

// A store that could not be opened must not be left half-built: Open closes
// both pools itself, so a caller that got an error has nothing to clean up and
// no file handle leaks into the next attempt.
func TestAFailedOpenLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
		"9999_from_the_future.sql", ms(s.Now())); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	bad, err := Open(ctx, Options{Path: path})
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Open = %v, want ErrSchemaNewer", err)
	}
	if bad != nil {
		t.Error("Open returned a store alongside its error, which a caller will not close")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// preMigrationCopies lists the backups this package took before migrating.
func preMigrationCopies(t *testing.T, dbPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(dbPath), PreMigrationDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the pre-migration directory: %v", err)
	}
	var out []string
	for _, e := range entries {
		// Only the ones this package names: anything else in there is
		// somebody's, and counting it would make the retention assertion
		// below pass or fail on a directory retention never touches.
		if e.IsDir() && strings.HasPrefix(e.Name(), BackupDirPrefix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Migrations are one-way and the binary that wrote a database refuses to open
// it once a newer one has moved it on. So the moment before a migration is
// when a rollback is most likely to be wanted and least likely to have been
// prepared for -- an operator upgrading is not thinking about backups.
func TestUpgradingCopiesTheDatabaseFirst(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, 1)

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	copies := preMigrationCopies(t, path)
	if len(copies) != 1 {
		t.Fatalf("the upgrade left %v pre-migration copies, want one", copies)
	}

	// It is a real database, and the layout `zoomies restore` takes: a copy
	// nobody can put back without knowing it is special is not a rollback.
	dir := filepath.Join(filepath.Dir(path), PreMigrationDir, copies[0])
	old, err := Open(ctx, Options{Path: filepath.Join(dir, BackupDBName), ReadOnly: true})
	if err != nil {
		t.Fatalf("the copy does not open: %v", err)
	}
	defer old.Close()
	if err := old.IntegrityCheck(ctx); err != nil {
		t.Errorf("the copy does not pass its integrity check: %v", err)
	}
	// And it is the database as it was *before* the upgrade, which is the
	// whole point: one migration, not the whole ledger.
	applied, err := old.AppliedMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Errorf("the copy has %d migrations; it was taken after the upgrade, not before it", len(applied))
	}
}

// A fresh install has no rows to lose, and putting an empty copy beside every
// one of them would teach operators that the directory is noise.
func TestAFirstStartTakesNoCopy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("a first start left %v behind", copies)
	}
}

// A ledger that exists and is empty is what a crash between creating the table
// and applying the first migration leaves behind. There is still nothing to
// lose, and a copy of an empty database beside every such start would be noise
// -- so this is a separate case from the fresh file above, which never gets as
// far as reading the ledger at all.
func TestAnEmptyLedgerTakesNoCopyEither(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("a database with an empty ledger was copied: %v", copies)
	}
}

// Opening a database that is already at head is the ordinary case -- every
// restart -- and it must not copy the database each time.
func TestAnOpenWithNothingPendingTakesNoCopy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "zoomies.db")
	first, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if copies := preMigrationCopies(t, path); len(copies) != 0 {
		t.Errorf("restarting on an up-to-date database copied it: %v", copies)
	}
}

// Two is what is worth keeping: the upgrade that just happened, and the one
// before it -- which is the one an operator reaches for when the first went
// unnoticed. More would be a copy of the whole database per release, kept
// forever, on the disk the fleet also needs.
func TestOnlyTheLastTwoCopiesAreKept(t *testing.T) {
	ctx := context.Background()
	path := atSchema(t, 1)
	root := filepath.Join(filepath.Dir(path), PreMigrationDir)

	// Three upgrades' worth, planted with names a year apart so the order is
	// unambiguous, plus something that is not ours.
	for _, stamp := range []string{"20240101-000000", "20250101-000000", "20260101-000000"} {
		if err := os.MkdirAll(filepath.Join(root, BackupDirPrefix+stamp), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stranger := filepath.Join(root, "notes")
	if err := os.MkdirAll(stranger, 0o700); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	copies := preMigrationCopies(t, path)
	// The newest two: the one this open just took, and 20260101.
	if len(copies) != preMigrationKeep {
		t.Fatalf("kept %v, want %d", copies, preMigrationKeep)
	}
	if contains(copies, BackupDirPrefix+"20240101-000000") {
		t.Errorf("the oldest copy survived: %v", copies)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("retention removed a directory this package did not name: %v", err)
	}
}

// The platform role is the first change to reach the users table since the
// first schema, and the first rebuild of a table another one points at. Two
// things could go wrong quietly: a column dropped on the way through, and
// every session deleted by the cascade a DROP fires. Both would surface
// after the upgrade, on a fleet that was working.
func TestThePlatformRoleRebuildKeepsEveryRowItsIndexesAndTheSessions(t *testing.T) {
	ctx := context.Background()
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	path := atSchema(t, len(migs)-1)

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	// Two administrators, the second enabled and older than the first is
	// not: the promotion has to pick by creation time, not by row order.
	seed(`INSERT INTO users (id, username, email, display_name, role, password_hash,
		oidc_subject, disabled, must_change_password, created_at)
		VALUES ('usr_late','late','late@example.com','Late','admin','hash','sub-late',0,0,2000)`)
	seed(`INSERT INTO users (id, username, email, display_name, role, password_hash,
		oidc_subject, disabled, must_change_password, created_at)
		VALUES ('usr_first','first','first@example.com','First','admin','hash','sub-first',0,0,1000)`)
	// An older administrator who has since been disabled is not the one
	// still operating the instance.
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_gone','gone','admin','hash',1,500)`)
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_view','viewer','viewer','hash',0,3000)`)
	seed(`INSERT INTO sessions (id, user_id, token_hash, user_agent, ip, created_at, expires_at)
		VALUES ('ses_one','usr_first','hash-one','curl','127.0.0.1',1000,9999999)`)
	seed(`INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at)
		VALUES ('ses_two','usr_view','hash-two',1000,9999999)`)
	seed(`INSERT INTO api_tokens (id, name, role, user_id, scopes, token_hash, prefix, revoked, created_at)
		VALUES ('tok_one','scraper','admin','usr_first','["a"]','hash-tok','zoo_abc',0,1000)`)
	db.Close()

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("upgrading: %v", err)
	}
	defer s.Close()

	// Every row is still there, with its columns.
	u, err := s.GetUser(ctx, "usr_first")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RolePlatform {
		t.Errorf("the account that installed the instance is %q, want platform; an upgrade that promotes nobody leaves an instance whose timers nobody can change", u.Role)
	}
	if u.Email != "first@example.com" || u.DisplayName != "First" || u.OIDCSubject != "sub-first" {
		t.Errorf("columns were lost in the rebuild: %+v", u)
	}
	// Who else is promoted is 0045's business, and
	// TestAnUpgradeLeavesEveryAdministratorWithWhatTheyHad is where that
	// lives. What matters here is that a role nobody held is not invented:
	// the viewer is still a viewer.
	if got, err := s.GetUser(ctx, "usr_view"); err != nil || got.Role != RoleViewer {
		t.Errorf("the viewer came back as %+v (%v)", got, err)
	}

	// The sessions survived the cascade a DROP would otherwise have fired.
	var sessions int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 2 {
		t.Errorf("%d sessions after the upgrade, want 2: dropping a table a foreign key points at deletes them", sessions)
	}

	// And the token, with the role it had.
	var tokenRole string
	if err := s.read.QueryRowContext(ctx, `SELECT role FROM api_tokens WHERE id = 'tok_one'`).Scan(&tokenRole); err != nil {
		t.Fatalf("the api token did not survive: %v", err)
	}
	// Its role is 0045's business too; what this test is asking is whether
	// the row survived the rebuild at all.
	if tokenRole == "" {
		t.Error("the api token came back with no role")
	}

	// The indexes came back with the tables.
	if _, err := s.exec(ctx, `INSERT INTO users (id, username, role, password_hash, created_at)
		VALUES ('usr_dup','first','viewer','hash',4000)`); err == nil {
		t.Error("a duplicate username was accepted, so the unique index did not come back")
	}
	for _, name := range []string{"idx_users_username", "idx_users_oidc", "idx_api_tokens_hash"} {
		var found string
		err := s.read.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&found)
		if err != nil {
			t.Errorf("index %s is missing after the rebuild: %v", name, err)
		}
	}
	if err := s.IntegrityCheck(ctx); err != nil {
		t.Errorf("integrity check after the rebuild: %v", err)
	}

	// And the new role is now a value the column accepts.
	if _, err := s.exec(ctx, `INSERT INTO users (id, username, role, password_hash, created_at)
		VALUES ('usr_plat','second-platform','platform','hash',5000)`); err != nil {
		t.Errorf("the rebuilt CHECK still refuses the platform role: %v", err)
	}
}

// A self-hosted fleet whose operations are shared between several
// administrators is the ordinary case, and an upgrade that leaves one of them
// able to take a backup and the others not -- or that stops a nightly
// `zoomies backup` running on an administrator's token -- has taken something
// away without saying so. Everything that held administrator keeps what
// administrator meant.
func TestAnUpgradeLeavesEveryAdministratorWithWhatTheyHad(t *testing.T) {
	ctx := context.Background()
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	// From before the role existed at all, which is what a released build
	// left behind.
	path := atSchema(t, len(migs)-2)

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_first','first','admin','hash',0,1000)`)
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_second','second','admin','hash',0,2000)`)
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_third','third','admin','hash',0,3000)`)
	// Disabled today, but what it finds when somebody enables it is what it
	// had.
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_away','away','admin','hash',1,4000)`)
	seed(`INSERT INTO users (id, username, role, password_hash, disabled, created_at)
		VALUES ('usr_op','op','operator','hash',0,5000)`)
	// The nightly backup's credential, and one somebody already revoked.
	seed(`INSERT INTO api_tokens (id, name, role, user_id, scopes, token_hash, prefix, revoked, created_at)
		VALUES ('tok_nightly','nightly backup','admin','usr_first','["backups:write"]','hash-a','zoo_a',0,1000)`)
	seed(`INSERT INTO api_tokens (id, name, role, user_id, scopes, token_hash, prefix, revoked, created_at)
		VALUES ('tok_gone','retired','admin','usr_first','["*"]','hash-b','zoo_b',1,1000)`)
	seed(`INSERT INTO api_tokens (id, name, role, user_id, scopes, token_hash, prefix, revoked, created_at)
		VALUES ('tok_view','dashboard','viewer','usr_op','["*"]','hash-c','zoo_c',0,1000)`)
	db.Close()

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatalf("upgrading: %v", err)
	}
	defer s.Close()

	for _, id := range []string{"usr_first", "usr_second", "usr_third", "usr_away"} {
		u, err := s.GetUser(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.Role != RolePlatform {
			t.Errorf("%s came up from the upgrade as %q; an administrator who could take a backup yesterday must still be able to today", id, u.Role)
		}
	}
	if u, err := s.GetUser(ctx, "usr_op"); err != nil || u.Role != RoleOperator {
		t.Errorf("the operator was promoted: %+v (%v)", u, err)
	}

	roleOf := func(id string) string {
		t.Helper()
		var role string
		if err := s.read.QueryRowContext(ctx, `SELECT role FROM api_tokens WHERE id = ?`, id).Scan(&role); err != nil {
			t.Fatal(err)
		}
		return role
	}
	if got := roleOf("tok_nightly"); got != string(RolePlatform) {
		t.Errorf("the nightly backup's token is %q; it would start answering 403 with nothing said to anybody", got)
	}
	if got := roleOf("tok_gone"); got != string(RoleAdmin) {
		t.Errorf("a revoked token was rewritten to %q; it authorises nothing and the audit trail should not read as though somebody changed it", got)
	}
	if got := roleOf("tok_view"); got != string(RoleViewer) {
		t.Errorf("a viewer token became %q", got)
	}
}

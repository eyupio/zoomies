package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"
)

// insertRepair writes a repair row directly, so a test controls its state, its
// time and its owner. EnqueueEliRepair cannot do that: it forces the state to
// queued and refuses past the daily budget, which is exactly what a history of
// hundreds of rows needs to get round.
func insertRepair(t *testing.T, s *Store, r EliRepair) string {
	t.Helper()
	if r.ID == "" {
		r.ID = NewID(PrefixEliRepair)
	}
	if r.DedupKey == "" {
		r.DedupKey = r.ID
	}
	if r.Trigger == "" {
		r.Trigger = "mention"
	}
	if r.State == "" {
		r.State = "succeeded"
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.Now()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	_, err := s.exec(context.Background(), `INSERT INTO eli_repairs (`+eliRepairCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.DedupKey, r.InstallationID, r.Repo, r.PullNumber, r.JobID, r.RunID, r.HeadSHA, r.CommitSHA, r.UserID, r.GitHubUserID, r.GitHubLogin, r.ProviderID, r.Trigger, boolInt(r.RequesterAdmin), r.Instruction, r.State, r.Message, r.CommentID, msp(r.PublishedAt), ms(r.CreatedAt), ms(r.UpdatedAt), repairDetailsJSON(r.Details))
	if err != nil {
		t.Fatalf("insert repair %s: %v", r.ID, err)
	}
	return r.ID
}

func repairIDs(rows []*EliRepair) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestTheRepairHistoryIsPagedFilteredAndCounted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := s.Now().Add(-time.Hour)
	var ids []string
	for i, row := range []struct{ repo, state, trigger string }{
		{"acme/site", "failed", "mention"},
		{"acme/site", "failed", "mention"},
		{"acme/site", "failed", "mention"},
		{"acme/site", "succeeded", "mention"},
		{"acme/widgets", "succeeded", "mention"},
		{"acme/widgets", "working", "automatic"},
		{"acme/widgets", "queued", "automatic"},
		{"acme/gizmo", "checks_failed", "automatic"},
	} {
		ids = append(ids, insertRepair(t, s, EliRepair{Repo: row.repo, State: row.state, Trigger: row.trigger, CreatedAt: base.Add(time.Duration(i) * time.Minute)}))
	}

	for _, tc := range []struct {
		name   string
		filter EliRepairFilter
		rows   int
		total  int
	}{
		{"a first page", EliRepairFilter{Limit: 3}, 3, 8},
		{"a short last page", EliRepairFilter{Limit: 3, Offset: 6}, 2, 8},
		{"a page past the end", EliRepairFilter{Limit: 3, Offset: 60}, 0, 8},
		{"needs attention", EliRepairFilter{Group: "attention"}, 4, 4},
		{"still running", EliRepairFilter{Group: "active"}, 2, 2},
		{"passed", EliRepairFilter{Group: "passed"}, 2, 2},
		{"a repository in any case", EliRepairFilter{Repo: "WIDGETS"}, 3, 3},
		{"automatic", EliRepairFilter{Trigger: "automatic"}, 3, 3},
		{"requested", EliRepairFilter{Trigger: "requested"}, 5, 5},
		{"a group and a repository together", EliRepairFilter{Group: "attention", Repo: "site"}, 3, 3},
	} {
		rows, total, err := s.ListEliRepairsPage(ctx, tc.filter)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(rows) != tc.rows || total != tc.total {
			t.Errorf("%s: got %d rows and a total of %d, want %d and %d", tc.name, len(rows), total, tc.rows, tc.total)
		}
	}

	rows, _, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ids[7], ids[6], ids[5]}; !slices.Equal(repairIDs(rows), want) {
		t.Errorf("the first page is not the newest rows, newest first: got %v, want %v", repairIDs(rows), want)
	}
	rows, _, err = s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: 3, Offset: 6})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ids[1], ids[0]}; !slices.Equal(repairIDs(rows), want) {
		t.Errorf("the last page is not the oldest rows: got %v, want %v", repairIDs(rows), want)
	}
}

// A page that repeats a row or drops one is worse than a slow one: a person
// selecting rows across pages would clear the wrong set.
func TestRepairPagingIsStableWhenRowsShareATimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	when := s.Now().Add(-time.Minute)
	var inserted []string
	for range 5 {
		inserted = append(inserted, insertRepair(t, s, EliRepair{Repo: "acme/site", CreatedAt: when}))
	}
	var paged []string
	for _, offset := range []int{0, 2, 4} {
		rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: 2, Offset: offset})
		if err != nil || total != 5 {
			t.Fatalf("offset %d: total %d, %v", offset, total, err)
		}
		paged = append(paged, repairIDs(rows)...)
	}
	// Every id once is the property; the id as the tie-break, highest first, is
	// what guarantees it, so the order is pinned as well. The ids are random, so
	// insertion order is not accidentally the same order.
	want := slices.Clone(inserted)
	slices.Sort(want)
	slices.Reverse(want)
	if !slices.Equal(paged, want) {
		t.Fatalf("pages of two did not cover the five rows once each, in id order\n got  %v\n want %v", paged, want)
	}
}

func TestARepairSearchTreatsPercentUnderscoreAndBackslashAsText(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, repo := range []string{"acme/a_b", "acme/axb", "acme/100%", "acme/x"} {
		insertRepair(t, s, EliRepair{Repo: repo})
	}
	for _, tc := range []struct {
		search string
		want   []string
	}{
		{"a_b", []string{"acme/a_b"}},
		{"%", []string{"acme/100%"}},
		{`\`, nil},
		{"   ", []string{"acme/a_b", "acme/axb", "acme/100%", "acme/x"}},
	} {
		rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Repo: tc.search})
		if err != nil {
			t.Fatalf("%q: %v", tc.search, err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Repo)
		}
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) || total != len(want) {
			t.Errorf("searching for %q found %v (total %d), want %v", tc.search, got, total, want)
		}
	}
}

func TestAPersonSeesOnlyTheirOwnRepairsAndAnEmptyOwnerIsNobody(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mine := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner"})
	insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "other"})
	insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: ""})

	rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Scoped: true, OwnerID: "owner"})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != mine {
		t.Fatalf("a person's history: %v total %d, %v", repairIDs(rows), total, err)
	}
	rows, total, err = s.ListEliRepairsPage(ctx, EliRepairFilter{Scoped: true, OwnerID: ""})
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("an empty owner must match nobody, not every row with no owner: %v total %d, %v", repairIDs(rows), total, err)
	}
	if _, total, _ = s.ListEliRepairsPage(ctx, EliRepairFilter{}); total != 3 {
		t.Errorf("an unscoped listing is for administrators and sees all three, got %d", total)
	}
}

func TestClearingARepairHidesItFromTheHistoryAndNothingElse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	row := &EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: "comment:1", CommitSHA: "fixsha"}
	if _, err := s.EnqueueEliRepair(ctx, row, 1); err != nil {
		t.Fatal(err)
	}
	row.State = "succeeded"
	if err := s.SaveEliRepair(ctx, row); err != nil {
		t.Fatal(err)
	}
	var updated int64
	if err := s.read.QueryRowContext(ctx, `SELECT updated_at FROM eli_repairs WHERE id=?`, row.ID).Scan(&updated); err != nil {
		t.Fatal(err)
	}

	results, err := s.ClearEliRepairs(ctx, []string{row.ID}, "owner", true)
	if err != nil || len(results) != 1 || !results[0].OK {
		t.Fatalf("clear: %+v %v", results, err)
	}

	rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{})
	if err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("a cleared repair is still listed: %v total %d, %v", repairIDs(rows), total, err)
	}
	// Clearing is housekeeping on the history, not a change to the repair, so
	// the row's own clock must not move.
	var after int64
	if err := s.read.QueryRowContext(ctx, `SELECT updated_at FROM eli_repairs WHERE id=?`, row.ID).Scan(&after); err != nil || after != updated {
		t.Errorf("clearing changed updated_at from %d to %d (%v)", updated, after, err)
	}

	// The history is the only reader that forgets it. The budget, the loop
	// guard and the deduplication all go on counting the row.
	_, err = s.EnqueueEliRepair(ctx, &EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: "comment:2"}, 1)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("clearing reset the daily limit: %v", err)
	}
	if own, err := s.IsEliRepairCommit(ctx, "acme/site", "fixsha"); err != nil || !own {
		t.Errorf("clearing let Eli forget its own commit: %v %v", own, err)
	}
	repoUse, personUse, err := s.EliRepairBudget(ctx, "acme/site", "owner")
	if err != nil || repoUse.Used != 1 || personUse.Used != 1 {
		t.Errorf("the budget stopped counting a cleared repair: repo %d person %d, %v", repoUse.Used, personUse.Used, err)
	}
}

func TestClearReportsEachRowAndRepeatsAreHarmless(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ownRow := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner"})
	activeRow := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner", State: "working"})
	otherPersonsRow := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "other"})

	got, err := s.ClearEliRepairs(ctx, []string{ownRow, ownRow, activeRow, otherPersonsRow, "missing"}, "owner", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []EliRepairClear{
		{ID: ownRow, OK: true, Changed: true},
		{ID: activeRow, Error: "still running"},
		{ID: otherPersonsRow, Error: "not found"},
		{ID: "missing", Error: "not found"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("results\n got  %+v\n want %+v", got, want)
	}

	// Someone else's row and a row that does not exist read the same, so a
	// caller cannot use the answer to find out which ids are real.
	if got[2].Error != got[3].Error {
		t.Errorf("a stranger's row and a missing one must be indistinguishable")
	}

	// Still ok, so the request is harmless, but this call changed nothing, and an
	// audit row built from Changed must not say otherwise.
	if got, err = s.ClearEliRepairs(ctx, []string{ownRow}, "owner", true); err != nil || !slices.Equal(got, []EliRepairClear{{ID: ownRow, OK: true, Changed: false}}) {
		t.Errorf("clearing a cleared repair is not harmless, or claims to have changed it: %+v %v", got, err)
	}
	rows, _, _ := s.ListEliRepairsPage(ctx, EliRepairFilter{})
	if !slices.Contains(repairIDs(rows), activeRow) || slices.Contains(repairIDs(rows), ownRow) {
		t.Errorf("the history should keep the running repair and drop the cleared one, got %v", repairIDs(rows))
	}

	// An administrator reaches every row.
	got, err = s.ClearEliRepairs(ctx, []string{otherPersonsRow}, "", false)
	if err != nil || !slices.Equal(got, []EliRepairClear{{ID: otherPersonsRow, OK: true, Changed: true}}) {
		t.Errorf("an administrator could not clear another person's repair: %+v %v", got, err)
	}
	// An administrator clearing what is already cleared changes nothing either.
	got, err = s.ClearEliRepairs(ctx, []string{ownRow}, "", false)
	if err != nil || !slices.Equal(got, []EliRepairClear{{ID: ownRow, OK: true, Changed: false}}) {
		t.Errorf("an administrator clearing an already cleared repair: %+v %v", got, err)
	}
	got, err = s.ClearEliRepairs(ctx, nil, "owner", true)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("clearing nothing should answer with an empty list, got %#v %v", got, err)
	}
}

// A person who asks again for something already done should see the answer,
// not an empty list because they cleared it last week.
func TestARequestForAClearedRepairBringsItBackIntoView(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: "ui:acme/site:7"})
	if _, err := s.ClearEliRepairs(ctx, []string{id}, "owner", true); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.ListEliRepairsPage(ctx, EliRepairFilter{}); total != 0 {
		t.Fatalf("the repair should be hidden before it is asked for again, total %d", total)
	}

	again := &EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: "ui:acme/site:7"}
	added, err := s.EnqueueEliRepair(ctx, again, 5)
	if err != nil || added || again.ID != id {
		t.Fatalf("the same request must answer with the saved row: added %v id %s (want %s), %v", added, again.ID, id, err)
	}
	rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("the repair did not come back into view: %v total %d, %v", repairIDs(rows), total, err)
	}
}

// Only a person asking again is a reason to show a repair again. A webhook
// redelivers the same comment, and every later failing job on a head Eli has
// already repaired hits the same automatic key; neither is anyone asking for
// the repair, so neither may undo a clear. They still deduplicate: the row goes
// on counting, it just stays out of sight.
func TestOnlyAPersonAskingAgainBringsAClearedRepairBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		back bool
	}{
		{"a request from the page", "ui:acme/site:7", true},
		{"an offer a person accepts again", "offer:opp_7", true},
		{"a webhook redelivering the same comment", "comment:acme/site:7", false},
		{"a later failing job on a head already repaired", "automatic:acme/site:abc123", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			id := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: tc.key})
			if _, err := s.ClearEliRepairs(ctx, []string{id}, "owner", true); err != nil {
				t.Fatal(err)
			}

			again := &EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: tc.key}
			added, err := s.EnqueueEliRepair(ctx, again, 5)
			if err != nil || added || again.ID != id {
				t.Fatalf("a repeat of %q must deduplicate onto the saved row: added %v id %s (want %s), %v", tc.key, added, again.ID, id, err)
			}
			_, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if back := total == 1; back != tc.back {
				t.Errorf("after a repeat of %q the repair is back in the history: %v, want %v (total %d)", tc.key, back, tc.back, total)
			}
			// Hidden or not, the row is still the ledger entry for the budget.
			if repoUse, _, err := s.EliRepairBudget(ctx, "acme/site", "owner"); err != nil || repoUse.Used != 1 {
				t.Errorf("the budget stopped counting the repair: repo %d, %v", repoUse.Used, err)
			}
		})
	}
}

// An offer is one person's. Someone else asking with its key is refused before
// anything else happens, and must not be able to bring the owner's cleared
// repair back into view on the way.
func TestAnotherPersonsOfferCannotBringAClearedRepairBack(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner", DedupKey: "offer:opp_9"})
	if _, err := s.ClearEliRepairs(ctx, []string{id}, "owner", true); err != nil {
		t.Fatal(err)
	}

	_, err := s.EnqueueEliRepair(ctx, &EliRepair{Repo: "acme/site", UserID: "someone-else", DedupKey: "offer:opp_9"}, 5)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("another person using the offer's key: %v, want a conflict", err)
	}
	if _, total, _ := s.ListEliRepairsPage(ctx, EliRepairFilter{}); total != 0 {
		t.Errorf("a refused request un-cleared the owner's repair, total %d", total)
	}
}

// Select-all must offer exactly what the list shows, or a person clears rows
// they could not see.
func TestAClearedRepairIsLeftOutOfTheSelection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cleared := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner"})
	kept := insertRepair(t, s, EliRepair{Repo: "acme/site", UserID: "owner"})
	if ids, total, err := s.EliRepairSelection(ctx, EliRepairFilter{}); err != nil || total != 2 || len(ids) != 2 {
		t.Fatalf("before clearing: %v total %d, %v", ids, total, err)
	}
	if _, err := s.ClearEliRepairs(ctx, []string{cleared}, "owner", true); err != nil {
		t.Fatal(err)
	}

	ids, total, err := s.EliRepairSelection(ctx, EliRepairFilter{})
	if err != nil || total != 1 || !slices.Equal(ids, []string{kept}) {
		t.Errorf("the selection after a clear is %v with a total of %d, want just %s: %v", ids, total, kept, err)
	}
}

func TestSelectionIsTheClearableMatchesCappedWithTheTotal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := s.Now().Add(-24 * time.Hour)
	running := map[string]bool{}
	var newestFinished string
	for i := range MaxEliRepairSelection + 5 {
		newestFinished = insertRepair(t, s, EliRepair{Repo: "acme/site", CreatedAt: base.Add(time.Duration(i) * time.Second)})
	}
	for i := range 3 {
		id := insertRepair(t, s, EliRepair{Repo: "acme/site", State: "working", CreatedAt: base.Add(time.Duration(MaxEliRepairSelection+10+i) * time.Second)})
		running[id] = true
	}

	ids, total, err := s.EliRepairSelection(ctx, EliRepairFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != MaxEliRepairSelection || total != MaxEliRepairSelection+5 {
		t.Fatalf("got %d ids and a total of %d, want %d and %d", len(ids), total, MaxEliRepairSelection, MaxEliRepairSelection+5)
	}
	for _, id := range ids {
		if running[id] {
			t.Fatalf("the selection offered a running repair, which cannot be cleared: %s", id)
		}
	}
	if ids[0] != newestFinished {
		t.Errorf("the selection should take the newest rows first, got %s want %s", ids[0], newestFinished)
	}

	ids, total, err = s.EliRepairSelection(ctx, EliRepairFilter{Group: "active"})
	if err != nil || len(ids) != 0 || total != 0 {
		t.Errorf("every active row is unclearable, so the selection is empty: %v total %d, %v", ids, total, err)
	}
}

func TestThePageSizeDefaultsAndIsClamped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for range MaxEliRepairPage + 5 {
		insertRepair(t, s, EliRepair{Repo: "acme/site"})
	}
	for _, tc := range []struct {
		name  string
		limit int
		want  int
	}{
		{"no limit asked for", 0, DefaultEliRepairPage},
		{"a negative limit", -4, DefaultEliRepairPage},
		{"a limit within the cap", 7, 7},
		{"a limit past the cap", MaxEliRepairPage * 10, MaxEliRepairPage},
	} {
		rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: tc.limit})
		if err != nil || len(rows) != tc.want || total != MaxEliRepairPage+5 {
			t.Errorf("%s: got %d rows and a total of %d, want %d rows: %v", tc.name, len(rows), total, tc.want, err)
		}
	}
	// A negative offset must not reach SQL as one.
	if rows, _, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Limit: 2, Offset: -5}); err != nil || len(rows) != 2 {
		t.Errorf("a negative offset: %d rows, %v", len(rows), err)
	}
}

// The API rejects a group it does not know, so the store only has to be safe
// when asked anyway: a filter that is not understood widens rather than
// emptying the page or building SQL from it.
func TestAnUnknownGroupOrTriggerIsTreatedAsNoFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertRepair(t, s, EliRepair{Repo: "acme/site", State: "failed", Trigger: "automatic"})
	insertRepair(t, s, EliRepair{Repo: "acme/site", State: "succeeded"})
	_, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{Group: "state='x' OR 1=1", Trigger: "'; DROP TABLE eli_repairs;"})
	if err != nil || total != 2 {
		t.Fatalf("total %d, %v", total, err)
	}
}

func TestTheClearedColumnMigrationKeepsExistingRepairsInTheHistory(t *testing.T) {
	ctx := context.Background()
	path := atSchemaBefore(t, "0100_eli_repair_cleared.sql")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO eli_repairs(id,dedup_key,installation_id,repo,trigger,created_at,updated_at) VALUES('erf_old','comment:old','inst','acme/site','mention',1000,2000)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	rows, total, err := s.ListEliRepairsPage(ctx, EliRepairFilter{})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != "erf_old" {
		t.Fatalf("an existing repair left the history on upgrade: %v total %d, %v", repairIDs(rows), total, err)
	}
}

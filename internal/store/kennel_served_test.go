package store

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"
)

// servedNamesFrom runs one of the queries that find the served names and reads the
// array it returns as a set.
func servedNamesFrom(t *testing.T, s *Store, query string, since time.Time) map[string]bool {
	t.Helper()
	var array string
	if err := s.read.QueryRowContext(context.Background(), query, ms(since)).Scan(&array); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal([]byte(array), &names); err != nil {
		t.Fatalf("%q is not an array of names: %v", array, err)
	}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return set
}

// servedByAskingTheJobsTable is the question the served filter used to put to the
// jobs table, as a subquery of the list: every job in the window that is the
// fleet's, and the repository it names. It reads every one of them, which is why
// the filter no longer asks it that way, and it is kept here because it is the
// plainest statement of what "served" means, and so what the quicker way must
// agree with.
func servedByAskingTheJobsTable(t *testing.T, s *Store, since time.Time) map[string]bool {
	t.Helper()
	rows, err := s.read.QueryContext(context.Background(), `SELECT DISTINCT LOWER(repo) FROM jobs
		WHERE queued_at >= ? AND repo <> '' AND installation_id <> '' AND `+managedJobSQL("jobs"), ms(since))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names[n] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// The filter used to put the question to the jobs table once for the total and
// once for the page, reading every job in the window each time. Asking it the
// quick way is only a gain if it names the same repositories, in every window,
// for every kind of job a history holds, and it is the sort of change that is
// right for the five repositories of a hand-made test and wrong for the
// two hundred of a real one.
func TestTheServedFilterKeepsTheRepositoriesAskingTheJobsTableWouldName(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	s := newTestStoreAt(t, func() time.Time { return now })
	// A hundred and fifty repositories of forty jobs each, a quarter of which only
	// ever ran on somebody else's runners, listed by Kennel Club with capitals the
	// jobs do not have. It lists fifty more that have no job at all. There are more
	// jobs than a window can hold and still be read whole, so the widest window is
	// found by walking the repositories and the narrower ones by reading the jobs,
	// and both ways are held to the same answer.
	const named = 150
	written, _ := kennelBenchHistory(t, s, now, 6000, named)
	kennelBenchRepositories(t, s)
	choseByRepository, choseByWindow := false, false

	all, total, err := s.ListKennelRepositories(ctx, KennelFilter{}, Page{Limit: 500})
	if err != nil || total != kennelBenchRepos || len(all) != kennelBenchRepos {
		t.Fatalf("the list holds %d repositories (total %d), %v; want %d", len(all), total, err, kennelBenchRepos)
	}

	yes, no := true, false
	for _, c := range []struct {
		name   string
		window time.Duration
	}{
		{"the widest window", kennelBenchWidest},
		{"a day", 24 * time.Hour},
		{"an hour", time.Hour},
		{"a minute", time.Minute},
		// Ends in the future, so no job is in it.
		{"a window that has not begun", -time.Hour},
	} {
		since := now.Add(-c.window)
		direct := servedByAskingTheJobsTable(t, s, since)
		if c.window == kennelBenchWidest {
			// And the history says the same, so that the two ways of asking are not
			// both wrong in the same way.
			if counted := kennelBenchServedSince(written, since, named); !maps.Equal(counted, direct) {
				t.Fatalf("the jobs table names %d repositories as served, and the history says %d", len(direct), len(counted))
			}
		}
		// Each way of asking, on its own, whichever one the filter would choose.
		for way, query := range map[string]string{"by repository": kennelServedByRepositorySQL, "by window": kennelServedByWindowSQL} {
			if got := servedNamesFrom(t, s, query, since); !maps.Equal(got, direct) {
				t.Errorf("%s, %s: %d names, and asking the jobs table directly gives %d", c.name, way, len(got), len(direct))
			}
		}
		// And the choice between them is the one the comment promises: reading the
		// window if it holds fewer jobs than the threshold, walking otherwise.
		inWindow := 0
		for _, j := range written {
			if !j.QueuedAt.Before(since) {
				inWindow++
			}
		}
		wantQuery, wantWay := kennelServedByWindowSQL, "by window"
		if inWindow >= kennelServedScanBelow {
			wantQuery, wantWay = kennelServedByRepositorySQL, "by repository"
		}
		if chosen, err := s.kennelServedQuery(ctx, since); err != nil || chosen != wantQuery {
			t.Errorf("%s: %d jobs in the window, and the filter did not choose to find the names %s (%v)", c.name, inWindow, wantWay, err)
		}
		choseByRepository = choseByRepository || wantWay == "by repository"
		choseByWindow = choseByWindow || wantWay == "by window"

		listed := func(served *bool) map[string]bool {
			t.Helper()
			rs, total, err := s.ListKennelRepositories(ctx, KennelFilter{Served: served, ServedSince: since}, Page{Limit: 500})
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if total != len(rs) {
				t.Fatalf("%s: the total is %d and the page holds %d of them", c.name, total, len(rs))
			}
			names := map[string]bool{}
			for _, r := range rs {
				names[strings.ToLower(r.FullName)] = true
			}
			return names
		}
		got, rest := listed(&yes), listed(&no)

		wantServed := 0
		for _, r := range all {
			name := strings.ToLower(r.FullName)
			if direct[name] {
				wantServed++
			}
			if got[name] != direct[name] {
				t.Errorf("%s: %s served = %v in the list, and %v asking the jobs table", c.name, r.FullName, got[name], direct[name])
			}
			if rest[name] == direct[name] {
				t.Errorf("%s: %s is in the not-served list = %v, and in the served = %v; each repository is in exactly one", c.name, r.FullName, rest[name], direct[name])
			}
		}
		if len(got) != wantServed || len(rest) != kennelBenchRepos-wantServed {
			t.Errorf("%s: %d served and %d not served, want %d and %d", c.name, len(got), len(rest), wantServed, kennelBenchRepos-wantServed)
		}
		if c.window == kennelBenchWidest && (wantServed == 0 || wantServed >= kennelBenchKnown) {
			t.Fatalf("%s: %d of the listed repositories are served, which would not tell the two lists apart", c.name, wantServed)
		}
		if c.window < 0 && len(got) != 0 {
			t.Errorf("%s: %d repositories are served, want none", c.name, len(got))
		}
	}
	if !choseByRepository || !choseByWindow {
		t.Errorf("the windows went one way only (by repository %v, by window %v), so the other way was not tried", choseByRepository, choseByWindow)
	}
}

// The jobs table keeps a repository as the webhook spelt it, and a repository can
// arrive spelt two ways. The filter folds case to match them to Kennel Club's, so
// one spelling's job being the fleet's makes the repository served, whichever
// spelling it is, and the other's being somebody else's does not undo it. It asks
// each spelling on its own, in the order they sort, so both orders are tried.
func TestARepositoryIsServedIfAnySpellingOfItHadAJobOfTheFleets(t *testing.T) {
	ctx := context.Background()
	s, inst, clock := kennelStore(t)
	now := *clock
	touch(t, s, inst, 1, "acme/Lower", "public")
	touch(t, s, inst, 2, "acme/Upper", "public")
	touch(t, s, inst, 3, "acme/Neither", "public")
	ran := func(id int64, repo string) {
		job(t, s, id, Job{Repo: repo, State: JobCompleted, QueuedAt: now.Add(-time.Hour), RunnerID: "run_1", PoolID: "pool_1", Labels: StringSlice{"self-hosted"}})
	}
	somebodyElses := func(id int64, repo string) {
		job(t, s, id, Job{Repo: repo, State: JobCompleted, QueuedAt: now.Add(-time.Hour), Labels: StringSlice{"ubuntu-latest"}})
	}
	// Capitals sort before lower case, so the first spelling the filter meets is the
	// capitalised one: here the one that was somebody else's, there the fleet's.
	somebodyElses(1, "acme/LOWER")
	ran(2, "acme/lower")
	ran(3, "acme/UPPER")
	somebodyElses(4, "acme/upper")
	somebodyElses(5, "acme/NEITHER")
	somebodyElses(6, "acme/neither")

	yes, no := true, false
	list := func(served *bool) string {
		t.Helper()
		rs, _, err := s.ListKennelRepositories(ctx, KennelFilter{Served: served, ServedSince: now.Add(-30 * 24 * time.Hour)}, Page{Sort: "name"})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, r := range rs {
			names = append(names, r.FullName)
		}
		return strings.Join(names, ",")
	}
	if got := list(&yes); got != "acme/Lower,acme/Upper" {
		t.Errorf("served = %s, want acme/Lower,acme/Upper: a job of the fleet's under either spelling is enough", got)
	}
	if got := list(&no); got != "acme/Neither" {
		t.Errorf("not served = %s, want acme/Neither, whose every spelling was somebody else's", got)
	}
}

// With no job to name, the served repositories are none and the unserved are all,
// which the array that carries them to the list has to say as an empty array and
// not as nothing.
func TestAnEmptyJobsTableServesNoRepository(t *testing.T) {
	ctx := context.Background()
	s, inst, clock := kennelStore(t)
	touch(t, s, inst, 1, "acme/api", "public")
	touch(t, s, inst, 2, "acme/web", "public")
	since := (*clock).Add(-30 * 24 * time.Hour)

	if names, err := s.kennelServedNames(ctx, since); err != nil || names != "[]" {
		t.Fatalf("the served names of an empty jobs table are %q, %v; want []", names, err)
	}
	yes, no := true, false
	if rs, total, err := s.ListKennelRepositories(ctx, KennelFilter{Served: &yes, ServedSince: since}, Page{}); err != nil || total != 0 || len(rs) != 0 {
		t.Errorf("served = %d repositories (total %d), %v; want none", len(rs), total, err)
	}
	if rs, total, err := s.ListKennelRepositories(ctx, KennelFilter{Served: &no, ServedSince: since}, Page{}); err != nil || total != 2 || len(rs) != 2 {
		t.Errorf("not served = %d repositories (total %d), %v; want both", len(rs), total, err)
	}
}

// The quick way of finding the served repositories is quick because of how it
// reads the jobs table: through idx_jobs_repo, a repository's spelling at a time,
// and not by going through every job. A plan that fell back to that would name
// the same repositories, so no test of the answer would notice, and it would cost
// several times as much on a fleet that keeps its jobs for long.
func TestTheServedNamesAreFoundThroughTheRepositoryIndexAndNotByReadingEveryJob(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	s := newTestStoreAt(t, func() time.Time { return now })
	kennelBenchHistory(t, s, now, 2000, 100)

	plan := explain(t, s, kennelServedByRepositorySQL, ms(now.Add(-kennelBenchWidest)))
	if !strings.Contains(plan, "idx_jobs_repo (repo=?") {
		t.Errorf("the plan does not look up a repository's jobs through idx_jobs_repo: %s", plan)
	}
	for _, scan := range []string{"SCAN jobs", "idx_jobs_queued_id", "idx_jobs_queued_at"} {
		if strings.Contains(plan, scan) {
			t.Errorf("the plan contains %q, which reads the jobs by time and not by repository: %s", scan, plan)
		}
	}
}

// The other two statements are quick for the opposite reason: they read the jobs
// by when they were queued, so that a window holding few of them costs for those
// few and not for the table. A plan that read every job to answer either would be
// right about the answer and slow for a table that has kept its jobs for long.
func TestTheJobsInAWindowAreReadByWhenTheyWereQueuedAndTheCountStopsEarly(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	s := newTestStoreAt(t, func() time.Time { return now })
	kennelBenchHistory(t, s, now, 2000, 100)
	since := ms(now.Add(-time.Hour))

	for name, c := range map[string]struct {
		query string
		args  []any
	}{
		"the plain query":    {kennelServedByWindowSQL, []any{since}},
		"the count of a few": {kennelServedProbeSQL, []any{since, kennelServedScanBelow}},
	} {
		plan := explain(t, s, c.query, c.args...)
		if !strings.Contains(plan, "(queued_at>?") {
			t.Errorf("%s does not read the window through the index on when jobs were queued: %s", name, plan)
		}
		if strings.Contains(plan, "SCAN jobs") {
			t.Errorf("%s reads every job in the table: %s", name, plan)
		}
	}
}

package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	// kennelBenchRepos is how many repositories Kennel Club knows, and
	// kennelBenchNamed how many of them the job history names. Some of the ones it
	// names only ever ran on somebody else's runners, which is the position of a
	// repository that uses hosted runners for its work, so the filter has to tell
	// a repository the fleet served from one it merely saw a job for.
	kennelBenchRepos = 500
	kennelBenchNamed = 200

	// kennelBenchWidest is the widest window the controller asks for: thirty days,
	// or the job retention if that is shorter. A fleet that keeps its jobs that
	// long asks the jobs table about all of them.
	kennelBenchWidest = 30 * 24 * time.Hour
	kennelBenchNarrow = time.Hour
)

// kennelBenchJobRepo is the repository a job in this benchmark's history was run
// for, in lower case as a webhook delivery spells it.
func kennelBenchJobRepo(j *Job) string {
	return fmt.Sprintf("acme/svc-%d", j.GitHubJobID%kennelBenchNamed)
}

// kennelBenchHistory is pageHistory with its jobs spread over kennelBenchNamed
// repositories and stamped with an installation, which the served filter
// requires. pageHistory uses five repositories, which would leave the filter's
// list of names with almost nothing in it.
func kennelBenchHistory(tb testing.TB, s *Store, now time.Time, n int) ([]*Job, pageWant) {
	tb.Helper()
	written, want := pageHistory(tb, s, now, n)
	if _, err := s.exec(context.Background(),
		`UPDATE jobs SET repo = 'acme/svc-' || (github_job_id % ?), installation_id = 'inst_bench'`, kennelBenchNamed); err != nil {
		tb.Fatalf("spreading the jobs over repositories: %v", err)
	}
	return written, want
}

// kennelBenchRepositories writes the repositories Kennel Club knows. The served
// ones are spelt with capitals, as a listing from GitHub has them, while the jobs
// carry them in lower case, so the filter has to fold case to match them, which
// is what the loop does and the reason the query calls LOWER on both sides.
func kennelBenchRepositories(tb testing.TB, s *Store) {
	tb.Helper()
	inst := &Installation{AppID: 1, InstallationID: 2, Target: "acme", TargetType: TargetOrg}
	if err := s.CreateInstallation(context.Background(), inst); err != nil {
		tb.Fatalf("CreateInstallation: %v", err)
	}
	for i := 0; i < kennelBenchRepos; i++ {
		name := fmt.Sprintf("acme/other-%d", i)
		if i < kennelBenchNamed {
			name = fmt.Sprintf("Acme/Svc-%d", i)
		}
		if _, err := s.TouchKennelRepository(context.Background(), KennelRepositoryRef{
			GitHubHost: "github.com", RepositoryID: int64(i + 1), InstallationID: inst.ID, FullName: name,
		}); err != nil {
			tb.Fatalf("TouchKennelRepository(%s): %v", name, err)
		}
	}
}

// kennelBenchServedSince is the repositories the fleet had a hand in a job for
// since a moment, counted from the jobs as they were written and not asked of the
// store. A job is the fleet's if it asked for a self-hosted runner, which is how
// pageHistory marks the jobs it writes for a pool.
func kennelBenchServedSince(written []*Job, since time.Time) map[string]bool {
	names := map[string]bool{}
	for _, j := range written {
		if !j.QueuedAt.Before(since) && slices.Contains(j.Labels, "self-hosted") {
			names[kennelBenchJobRepo(j)] = true
		}
	}
	return names
}

// BenchmarkListKennelRepositoriesServed is the Kennel Club list with its served
// filter on: which of the repositories it knows the fleet has run a job for, or
// has not. The filter asks the jobs table, once and in full through the window,
// so what it costs follows the number of jobs the window holds and not the number
// of repositories listed. The controller's window is the lesser of thirty days and
// the job retention, which for most fleets is the whole of the table.
//
// The history is ten thousand jobs, the size the other benchmarks of the store
// use, and a hundred thousand, which is a fleet that keeps its jobs for long. Each
// view is timed over the wide window, and again over an hour, which shows how
// much of the cost is the window and how much is the filter. The unfiltered list
// is there to be compared against.
//
// What each view should find is counted from the jobs while they are written and
// checked before anything is timed, so that a filter that matched everything, or
// nothing, is not timed under the name of one that did not. The store never runs
// ANALYZE, so neither does this: a plan chosen from statistics is not the plan an
// operator gets.
func BenchmarkListKennelRepositoriesServed(b *testing.B) {
	ctx := context.Background()
	served, notServed := true, false
	for _, n := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("jobs=%d", n), func(b *testing.B) {
			now := time.Now().Truncate(time.Millisecond)
			s := newTestStoreAt(b, func() time.Time { return now })
			written, want := kennelBenchHistory(b, s, now, n)
			kennelBenchRepositories(b, s)

			fleet := 0
			for _, j := range written {
				if slices.Contains(j.Labels, "self-hosted") {
					fleet++
				}
			}
			if fleet != want.all {
				b.Fatalf("%d of the jobs asked for a self-hosted runner, and the history says the fleet had %d", fleet, want.all)
			}

			type view struct {
				name   string
				filter KennelFilter
				total  int
				served *bool           // the direction asked for, nil for none
				names  map[string]bool // the repositories that count as served
			}
			views := []view{{name: "unfiltered", total: kennelBenchRepos}}
			wideJobs := 0
			for _, w := range []struct {
				label  string
				window time.Duration
			}{{"30d", kennelBenchWidest}, {"1h", kennelBenchNarrow}} {
				since := now.Add(-w.window)
				names := kennelBenchServedSince(written, since)
				// Every repository the jobs name is one Kennel Club knows, so the
				// count of the ones it knows that were served is the count of names.
				in := len(names)
				// What the filter pays for is the jobs the window holds, so that is
				// what makes the narrow window narrow. It need not name fewer
				// repositories: an hour of a large history reaches all of them.
				jobs := 0
				for _, j := range written {
					if !j.QueuedAt.Before(since) {
						jobs++
					}
				}
				if in == 0 {
					b.Fatalf("the %s window names no served repository, so there is nothing for the filter to find", w.label)
				}
				if w.window == kennelBenchWidest {
					wideJobs = jobs
					// Some, but not all, of the repositories the history names: the rest
					// only had jobs on somebody else's runners, and the filter must not
					// count them as served.
					if in >= kennelBenchNamed {
						b.Fatalf("the wide window names all %d repositories the jobs name as served; some were expected to have run only on somebody else's runners", in)
					}
				} else if jobs*10 >= wideJobs {
					b.Fatalf("the narrow window holds %d jobs and the wide one %d; the narrow one should hold under a tenth", jobs, wideJobs)
				}
				views = append(views,
					view{"served_" + w.label, KennelFilter{Served: &served, ServedSince: since}, in, &served, names},
					view{"not_served_" + w.label, KennelFilter{Served: &notServed, ServedSince: since}, kennelBenchRepos - in, &notServed, names})
			}

			page := Page{Limit: 50}
			for _, v := range views {
				b.Run("view="+v.name, func(b *testing.B) {
					repos, total, err := s.ListKennelRepositories(ctx, v.filter, page)
					if err != nil {
						b.Fatalf("ListKennelRepositories: %v", err)
					}
					if total != v.total || len(repos) != min(v.total, page.Limit) {
						b.Fatalf("the %s view found %d repositories and returned %d; the history holds %d for it", v.name, total, len(repos), v.total)
					}
					for _, r := range repos {
						if v.served != nil && v.names[strings.ToLower(r.FullName)] != *v.served {
							b.Fatalf("the %s view returned %s, which the history says is not %s", v.name, r.FullName, map[bool]string{true: "served", false: "unserved"}[*v.served])
						}
					}

					b.ReportAllocs()
					for b.Loop() {
						if _, _, err := s.ListKennelRepositories(ctx, v.filter, page); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

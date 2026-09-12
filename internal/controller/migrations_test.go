package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// Paging through an organisation has to reach every repository in it, whatever
// case its name is written in.
//
// The cursor is a repository name, and it is compared in lowercase so that the
// one the browser sends back matches whatever GitHub returned. That only works
// if the listing is ordered the same way: sorted by raw bytes, every
// capitalised name sorts before every lowercase one, the binary search for the
// cursor runs over an order it does not share, and the repositories caught
// between the two are returned on no page at all. They are not merely
// misordered -- they are unreachable, and a repository the wizard never lists
// is one nobody can migrate.
func TestMigrationPagingReachesEveryRepositoryWhateverItsCase(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	// Names GitHub would really return: some capitalised, some not, mixed
	// together so that no single ordering happens to work by luck.
	var want []string
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("acme/repo%02d", i)
		if i%2 == 0 {
			name = fmt.Sprintf("acme/Repo%02d", i)
		}
		h.gh.AddRepo(name)
		want = append(want, strings.ToLower(name))
	}

	client, err := h.c.ClientFor(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	// A page size that does not divide the fleet evenly, so the last page is a
	// partial one and the cursor is exercised more than once.
	const perPage = 5
	seen := map[string]int{}
	cursor := ""
	for page := 0; ; page++ {
		if page > len(want) {
			t.Fatalf("paging did not terminate after %d pages", page)
		}
		repos, next, total, err := migrationRepos(h.ctx, client, nil, cursor, perPage)
		if err != nil {
			t.Fatalf("migrationRepos(cursor=%q): %v", cursor, err)
		}
		if total != len(want) {
			t.Errorf("total = %d on page %d, want %d: the wizard reports which slice of the organisation is on screen", total, page, len(want))
		}
		for _, r := range repos {
			seen[strings.ToLower(r.FullName)]++
		}
		if next == "" {
			break
		}
		cursor = next
	}

	for _, name := range want {
		switch seen[name] {
		case 1:
		case 0:
			t.Errorf("%s appeared on no page, so it can never be migrated", name)
		default:
			t.Errorf("%s appeared on %d pages, so the wizard offers it twice", name, seen[name])
		}
	}
}

// The cursor is matched without regard to case, so a client that lowercases the
// name it was handed still gets the page after it rather than a repeat of the
// one it has.
func TestMigrationPagingAcceptsACursorInAnyCase(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()

	for _, name := range []string{"acme/Alpha", "acme/Bravo", "acme/Charlie", "acme/Delta"} {
		h.gh.AddRepo(name)
	}

	client, err := h.c.ClientFor(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	first, next, _, err := migrationRepos(h.ctx, client, nil, "", 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if next == "" {
		t.Fatalf("first page of four repositories at two a page says it is the whole organisation")
	}

	for _, cursor := range []string{next, strings.ToLower(next), strings.ToUpper(next)} {
		rest, _, _, err := migrationRepos(h.ctx, client, nil, cursor, 2)
		if err != nil {
			t.Fatalf("second page (cursor %q): %v", cursor, err)
		}
		for _, r := range rest {
			for _, had := range first {
				if strings.EqualFold(r.FullName, had.FullName) {
					t.Errorf("cursor %q returned %s again, which the operator has already seen", cursor, r.FullName)
				}
			}
		}
	}
}

// A repository the operator named by hand is matched without regard to case:
// GitHub resolves a repository URL that way, and an operator who typed the name
// into the wizard should not be told their own repository does not exist.
func TestMigrationReposMatchesANamedRepositoryWhateverItsCase(t *testing.T) {
	h := newHarness(t)
	inst := h.installation()
	h.gh.AddRepo("acme/Widgets")

	client, err := h.c.ClientFor(h.ctx, inst.ID)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	for _, named := range []string{"acme/Widgets", "acme/widgets", "ACME/WIDGETS"} {
		repos, next, total, err := migrationRepos(h.ctx, client, []string{named}, "", MaxPlanRepos)
		if err != nil {
			t.Fatalf("migrationRepos(%q): %v", named, err)
		}
		if len(repos) != 1 || !strings.EqualFold(repos[0].FullName, "acme/Widgets") {
			t.Fatalf("migrationRepos(%q) = %v, want the one repository", named, repos)
		}
		// A named request is not a page of a listing: there is nothing after it.
		if next != "" || total != 1 {
			t.Errorf("migrationRepos(%q) = cursor %q total %d, want no cursor and a total of 1", named, next, total)
		}
	}
}

// A pool that belongs to another installation is not somewhere this one's jobs
// could be sent, so it is not offered as a migration target. Getting this wrong
// would point one organisation's workflows at another's runners.
func TestMigrationPoolsAreScopedToTheirInstallation(t *testing.T) {
	h := newHarness(t)
	mine := h.installation()
	theirs := h.installationOn("globex", store.TargetOrg)

	wanted := h.pool(mine, "mine-linux")
	h.pool(theirs, "theirs-linux")

	disabled := h.pool(mine, "mine-windows")
	disabled.Enabled = false
	if err := h.st.UpdatePool(h.ctx, disabled); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}

	pools, err := h.c.migrationPools(h.ctx, mine.ID)
	if err != nil {
		t.Fatalf("migrationPools: %v", err)
	}
	if len(pools) != 1 || pools[0].ID != wanted.ID {
		var got []string
		for _, p := range pools {
			got = append(got, p.Name)
		}
		t.Fatalf("migrationPools = %v, want only the enabled pool of this installation (%s)", got, wanted.Name)
	}
}

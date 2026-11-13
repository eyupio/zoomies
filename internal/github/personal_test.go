package github

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestProbeLearnsThatAnAccountIsPersonalAndReadsItsRepositories(t *testing.T) {
	// The first probe of an installation is how Zoomies finds out what kind of
	// account it is on. An operator who typed their own login as an
	// organisation used to get GitHub's bare "Not Found" for the organisation
	// endpoint, which reads like a missing permission and is not one: a person
	// has no runners of their own to list. The probe has to ask what the
	// account is first and then read the runners where a person keeps them.
	f := newFake(t)
	f.SetAccountType("User")
	f.SetPermissions(map[string]string{"administration": "write", "actions": "read", "metadata": "read"})
	f.AddRepo("acme/widgets")

	info, err := f.Client("acme", store.TargetOrg).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.AccountType != store.AccountUser {
		t.Fatalf("account type = %q, want %q", info.AccountType, store.AccountUser)
	}
	// A person cannot grant an organisation permission, so asking for it would
	// send them looking for a setting that does not exist.
	if missing := info.MissingRequirements(store.TargetOrg); len(missing) != 0 {
		t.Fatalf("a personal account with Administration write reported %v", missing)
	}
}

func TestProbeOfAPersonalAccountNamesAdministrationWhenItIsMissing(t *testing.T) {
	f := newFake(t)
	f.SetAccountType("User")
	f.SetPermissions(map[string]string{"organization_self_hosted_runners": "write", "actions": "read", "metadata": "read"})

	info, err := f.Client("acme", store.TargetOrg).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	missing := strings.Join(info.MissingRequirements(store.TargetOrg), "; ")
	if !strings.Contains(missing, "Administration") || strings.Contains(missing, "Self-hosted runners") {
		t.Fatalf("missing requirements = %q, want Administration and not the organisation permission", missing)
	}
}

func TestProbeSaysAnOrganisationIsOne(t *testing.T) {
	f := newFake(t)
	info, err := f.Client("acme", store.TargetOrg).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.AccountType != store.AccountOrganization {
		t.Fatalf("account type = %q, want %q", info.AccountType, store.AccountOrganization)
	}
}

func TestListRunnersOnAnOrganisationThatIsNotFoundPointsAtAPersonalAccount(t *testing.T) {
	// The reconcile loop meets the 404 before any probe has corrected the
	// installation, and its message is what the problems list shows.
	f := newFake(t)
	f.SetAccountType("User")

	_, err := f.Client("acme", store.TargetOrg).ListRunners(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "personal account") {
		t.Errorf("error %q does not mention a personal account", err)
	}
}

func TestAPersonalAccountRegistersARunnerOnTheRepositoryNamed(t *testing.T) {
	// A runner on a personal account can only take the jobs of the repository
	// it is registered on, so which repository is the whole of the request.
	f := newFake(t)
	f.AddRepo("acme/widgets")
	f.AddRepo("acme/gadgets")
	c := f.PersonalClient("acme")

	jit, err := c.CreateJITConfig(context.Background(), JITRequest{Name: "r1", Labels: []string{"linux"}, Repo: "acme/gadgets"})
	if err != nil {
		t.Fatalf("CreateJITConfig: %v", err)
	}
	runners, err := c.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if len(runners) != 1 || runners[0].ID != jit.RunnerID || runners[0].Repo != "acme/gadgets" {
		t.Fatalf("runners = %+v, want the one runner on acme/gadgets", runners)
	}
}

func TestAPersonalAccountRefusesARunnerWithNoRepository(t *testing.T) {
	// There is no account to fall back to. Registering on a guessed repository
	// would start a runner no waiting job can reach, so it is refused with the
	// reason instead.
	f := newFake(t)
	c := f.PersonalClient("acme")

	for name, repo := range map[string]string{"none": "", "another account's": "other/widgets"} {
		_, err := c.CreateJITConfig(context.Background(), JITRequest{Name: "r1", Labels: []string{"linux"}, Repo: repo})
		if err == nil {
			t.Errorf("%s repository: the runner was registered", name)
			continue
		}
		if !strings.Contains(err.Error(), "acme") {
			t.Errorf("%s repository: error %q does not name the account", name, err)
		}
	}
	if got := f.Runners(); len(got) != 0 {
		t.Fatalf("runners registered: %+v", got)
	}
}

func TestAPersonalAccountListsTheRunnersOfEveryRepository(t *testing.T) {
	f := newFake(t)
	f.AddRepo("acme/empty")
	a := f.AddRepoRunner("acme/widgets", "w1", []string{"linux"})
	b := f.AddRepoRunner("acme/gadgets", "g1", []string{"linux"})
	c := f.PersonalClient("acme")

	runners, err := c.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	got := map[int64]string{}
	for _, r := range runners {
		got[r.ID] = r.Repo
	}
	if len(got) != 2 || got[a.ID] != "acme/widgets" || got[b.ID] != "acme/gadgets" {
		t.Fatalf("runners = %+v", runners)
	}
}

func TestAPersonalAccountListingFailsWholeWhenARepositoryCannotBeRead(t *testing.T) {
	// The reap reads a listing as proof of which registrations are gone. One
	// that silently left a repository out would have its runners taken for
	// deregistered, so a repository that cannot be read fails the listing.
	f := newFake(t)
	f.AddRepoRunner("acme/widgets", "w1", []string{"linux"})
	f.AddRepoRunner("acme/gadgets", "g1", []string{"linux"})
	f.SetError("/repos/acme/gadgets/actions/runners", http.StatusForbidden, "Resource not accessible by integration")

	_, err := f.PersonalClient("acme").ListRunners(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if !strings.Contains(err.Error(), "Administration") {
		t.Errorf("error %q does not name the permission a personal account needs", err)
	}
}

func TestAPersonalAccountDeletesARunnerFromTheRepositoryItIsOn(t *testing.T) {
	f := newFake(t)
	f.AddRepo("acme/widgets")
	c := f.PersonalClient("acme")
	jit, err := c.CreateJITConfig(context.Background(), JITRequest{Name: "r1", Labels: []string{"linux"}, Repo: "acme/widgets"})
	if err != nil {
		t.Fatalf("CreateJITConfig: %v", err)
	}
	// One this client never registered: a controller restart forgets which
	// repository each runner is on, and the delete has to find it again.
	other := f.AddRepoRunner("acme/gadgets", "g1", []string{"linux"})

	for _, id := range []int64{jit.RunnerID, other.ID} {
		if err := c.DeleteRunner(context.Background(), id); err != nil {
			t.Fatalf("DeleteRunner(%d): %v", id, err)
		}
	}
	if got := f.Runners(); len(got) != 0 {
		t.Fatalf("runners left: %+v", got)
	}
	// Already gone is the end state asked for.
	if err := c.DeleteRunner(context.Background(), other.ID); err != nil {
		t.Fatalf("deleting a runner that is gone: %v", err)
	}
}

func TestAPersonalAccountHasNoRunnerGroupsOrRegistrationTokens(t *testing.T) {
	f := newFake(t)
	c := f.PersonalClient("acme")

	groups, err := c.ListRunnerGroups(context.Background())
	if err != nil || len(groups) != 0 {
		t.Fatalf("ListRunnerGroups = %v, %v; want none", groups, err)
	}
	if _, err := c.CreateRunnerGroup(context.Background(), RunnerGroupCreate{Name: "g"}); err == nil || !strings.Contains(err.Error(), "personal account") {
		t.Fatalf("CreateRunnerGroup: %v", err)
	}
	// A token-registered runner stays on one repository for good, where the
	// account's other repositories cannot reach it; the message has to send
	// the operator to the setting that avoids it.
	if _, err := c.CreateRegistrationToken(context.Background()); err == nil || !strings.Contains(err.Error(), "ephemeral") {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}
}

func TestTheFactoryBuildsAPersonalClientFromTheInstallation(t *testing.T) {
	f := newFake(t)
	f.SetAccountType("User")
	f.AddRepoRunner("acme/widgets", "w1", []string{"linux"})
	inst := installationFor(f, f.URL(), store.TargetOrg, "acme")
	inst.AccountType = store.AccountUser

	c, err := NewAppFactory(f.Server().Client()).For(context.Background(), inst, testKey())
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	runners, err := c.ListRunners(context.Background())
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if !slices.ContainsFunc(runners, func(r Runner) bool { return r.Name == "w1" && r.Repo == "acme/widgets" }) {
		t.Fatalf("runners = %+v", runners)
	}
}

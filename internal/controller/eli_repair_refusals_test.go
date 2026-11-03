package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// A command Eli recognises from someone who may write to the repository and then
// declines used to vanish: no reaction, no comment, no row, and a delivery the
// log called accepted. The person who typed it could not tell a refusal from a
// webhook that never arrived, so these hold that they are told why, and that
// someone who could not have asked is told nothing.
type commandSetup struct {
	h    *harness
	inst *store.Installation
	fake *repairFake
	repo string
	user *store.User
}

func newCommandSetup(t *testing.T) *commandSetup {
	t.Helper()
	return newCommandSetupAt(t, nil)
}

// newCommandSetupAt is newCommandSetup on a clock the test holds, for the
// readiness tests that need two allowance windows to differ.
func newCommandSetupAt(t *testing.T, clock func() time.Time) *commandSetup {
	t.Helper()
	h := newHarnessAt(t, clock)
	inst := h.installation()
	repo := inst.Target + "/repo"
	fake := &repairFake{Client: h.gh.Client(inst.Target, inst.TargetType)}
	h.c.clients.entries[inst.ID] = &clientEntry{client: fake, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	p := &store.AssistantProvider{Name: "Install", Kind: "openai", Enabled: true}
	if err := h.st.CreateAssistantProvider(h.ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetEliRepairPolicy(h.ctx, store.EliRepairPolicy{Repo: repo, InstallationID: inst.ID, ProviderID: p.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	user := &store.User{Username: "octo", Role: store.RoleViewer}
	if err := h.st.CreateUser(h.ctx, user); err != nil {
		t.Fatal(err)
	}
	return &commandSetup{h: h, inst: inst, fake: fake, repo: repo, user: user}
}

// link ties the commenter's GitHub account to the Zoomies user, and records the
// user's consent only when asked to.
func (s *commandSetup) link(t *testing.T, consent bool) {
	t.Helper()
	if err := s.h.st.SetEliIdentity(s.h.ctx, store.EliIdentity{UserID: s.user.ID, GitHubUserID: 42, GitHubLogin: "octo"}); err != nil {
		t.Fatal(err)
	}
	if consent {
		if err := s.h.st.ConfirmEliIdentity(s.h.ctx, s.user.ID, 42, true); err != nil {
			t.Fatal(err)
		}
	}
}

// comment delivers a "/eli fix" comment with the given id, as the webhook would.
func (s *commandSetup) comment(t *testing.T, id int64) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"action": "created", "repository": map[string]any{"full_name": s.repo}, "issue": map[string]any{"number": 1, "pull_request": map[string]any{}}, "comment": map[string]any{"id": id, "body": "/eli fix this PR", "user": map[string]any{"id": 42, "login": "octo", "type": "User"}}, "sender": map[string]any{"id": 42}})
	if err := s.h.c.enqueueRepairWebhook(s.h.ctx, s.inst, "issue_comment", body); err != nil {
		t.Fatal(err)
	}
}

func (s *commandSetup) rows(t *testing.T) []*store.EliRepair {
	t.Helper()
	rows, _, err := s.h.st.ListEliRepairsPage(s.h.ctx, store.EliRepairFilter{Limit: store.MaxEliRepairPage})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// refusedWith checks the one thing the person is told: a single comment on the PR
// that says each phrase, and a confused face on the comment they typed.
func (s *commandSetup) refusedWith(t *testing.T, commentID int64, phrases ...string) {
	t.Helper()
	if len(s.fake.comments) != 1 {
		t.Fatalf("Eli posted %d comments, want one: %q", len(s.fake.comments), s.fake.comments)
	}
	for _, want := range phrases {
		if !strings.Contains(s.fake.comments[0], want) {
			t.Errorf("the reply %q does not say %q", s.fake.comments[0], want)
		}
	}
	if len(s.fake.confused) != 1 || s.fake.confused[0] != commentID {
		t.Errorf("confused reactions = %v, want one on comment %d", s.fake.confused, commentID)
	}
	for _, id := range s.fake.reacted {
		if id == commentID {
			t.Errorf("Eli put eyes on a request it did not start: %v", s.fake.reacted)
		}
	}
}

func TestAWriterWhoIsNotLinkedIsToldHowToBeLinked(t *testing.T) {
	s := newCommandSetup(t)
	s.comment(t, 101)
	if rows := s.rows(t); len(rows) != 0 {
		t.Fatalf("an unlinked commenter started a repair: %+v", rows)
	}
	s.refusedWith(t, 101, "not linked", "@octo", "administrator", "Eli repairs")
}

// The link is the administrator's act and the consent is the person's own, and
// the page says administrators cannot give it for them, so a linked account
// whose owner has not allowed it is a different thing to tell someone.
func TestAWriterWhoHasNotAllowedTheLinkIsToldToAllowIt(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, false)
	s.comment(t, 102)
	if rows := s.rows(t); len(rows) != 0 {
		t.Fatalf("an account its owner had not allowed started a repair: %+v", rows)
	}
	s.refusedWith(t, 102, "linked", "Allow this GitHub account to request repairs using my provider")
}

func TestAWriterWhoseZoomiesAccountIsDisabledIsToldSo(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	s.user.Disabled = true
	if err := s.h.st.UpdateUser(s.h.ctx, s.user); err != nil {
		t.Fatal(err)
	}
	s.comment(t, 103)
	if rows := s.rows(t); len(rows) != 0 {
		t.Fatalf("a disabled account started a repair: %+v", rows)
	}
	s.refusedWith(t, 103, "disabled", "administrator")
}

// Somebody GitHub says cannot write to the repository gets no answer at all, on
// a public repository especially: a comment from anyone must not make the App
// post. They were never going to be admitted, linked or not.
func TestACommenterWhoCannotWriteIsNotAnswered(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprint("linked=", linked), func(t *testing.T) {
			s := newCommandSetup(t)
			s.fake.noWrite = true
			if linked {
				s.link(t, true)
			}
			s.comment(t, 107)
			if len(s.fake.comments) != 0 || len(s.fake.confused) != 0 || len(s.fake.reacted) != 0 {
				t.Errorf("Eli answered a commenter without write access: comments %q, confused %v, eyes %v", s.fake.comments, s.fake.confused, s.fake.reacted)
			}
			if rows := s.rows(t); len(rows) != 0 {
				t.Errorf("a commenter without write access started a repair: %+v", rows)
			}
		})
	}
}

// A request that is admitted is not also refused.
func TestAnAdmittedCommandIsNotAlsoRefused(t *testing.T) {
	s := newCommandSetup(t)
	s.link(t, true)
	s.comment(t, 108)
	if rows := s.rows(t); len(rows) != 1 {
		t.Fatalf("the request was not admitted: %+v", rows)
	}
	if len(s.fake.comments) != 0 || len(s.fake.confused) != 0 {
		t.Errorf("Eli refused a request it admitted: %q %v", s.fake.comments, s.fake.confused)
	}
	if len(s.fake.reacted) != 1 || s.fake.reacted[0] != 108 {
		t.Errorf("the queued request was not acknowledged: %v", s.fake.reacted)
	}
	s.comment(t, 108)
	if len(s.fake.reacted) != 1 || len(s.rows(t)) != 1 {
		t.Error("redelivery acknowledged or queued the same request again")
	}
}

func TestAWriterIsToldWhenRepositoryRepairsAreNotEnabled(t *testing.T) {
	for _, setup := range []string{"missing", "disabled", "different installation"} {
		t.Run(setup, func(t *testing.T) {
			s := newCommandSetup(t)
			s.link(t, true)
			policies, err := s.h.st.EliRepairPolicies(s.h.ctx)
			if err != nil || len(policies) != 1 {
				t.Fatalf("policies %v, %v", policies, err)
			}
			p := policies[0]
			switch setup {
			case "missing":
				s.repo = s.inst.Target + "/another-repo"
			case "disabled":
				p.Enabled = false
			case "different installation":
				p.InstallationID = s.h.installationOn("other", store.TargetOrg).ID
			}
			if err := s.h.st.SetEliRepairPolicy(s.h.ctx, p); err != nil {
				t.Fatal(err)
			}
			s.comment(t, 109)
			s.refusedWith(t, 109, "not enabled", "administrator", "Repair settings", "Manual only")
			if len(s.rows(t)) != 0 {
				t.Error("a repository without an enabled policy queued a repair")
			}
		})
	}
}

func TestMissingRepositoryPolicyDoesNotAnswerSomeoneWithoutWriteAccess(t *testing.T) {
	s := newCommandSetup(t)
	s.repo = s.inst.Target + "/another-repo"
	s.fake.noWrite = true
	s.comment(t, 110)
	if len(s.fake.comments) != 0 || len(s.fake.confused) != 0 || len(s.fake.reacted) != 0 || len(s.rows(t)) != 0 {
		t.Error("a commenter without write permission was answered or queued")
	}
}

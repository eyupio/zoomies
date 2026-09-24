package auth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// bootstrapRows returns every auth.bootstrap row, so a test can say both that
// one was written and that only one was.
func bootstrapRows(t *testing.T, st *store.Store) []*store.AuditEvent {
	t.Helper()
	rows, _, err := st.ListAudit(t.Context(), store.AuditFilter{Actions: []string{"auth.bootstrap"}}, store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	return rows
}

// An instance a compose file brought up has nobody to read the setup token,
// so the environment's account has to be the platform one and has to leave a
// row saying the system made it and how -- an unexplained platform account is
// what somebody inheriting the instance cannot account for.
func TestAnUnattendedPasswordCreatesThePlatformAccountAndAuditsIt(t *testing.T) {
	s, st, _ := newService(t)

	u, tok, err := s.CreateUnattendedIdentity(t.Context(), Unattended{Username: "Ops", Password: testPassword})
	if err != nil {
		t.Fatalf("CreateUnattendedIdentity: %v", err)
	}
	if tok != nil {
		t.Fatal("a password bootstrap must not register a token")
	}
	if u.Role != store.RolePlatform || u.Username != "ops" {
		t.Fatalf("created %q as %s; want ops as platform", u.Username, u.Role)
	}
	if _, _, err := s.Login(t.Context(), "ops", testPassword, "203.0.113.1", "test"); err != nil {
		t.Fatalf("the bootstrap password does not sign in: %v", err)
	}

	rows := bootstrapRows(t, st)
	if len(rows) != 1 {
		t.Fatalf("got %d auth.bootstrap rows; want exactly one", len(rows))
	}
	row := rows[0]
	if row.ActorKind != KindSystem || row.ActorID != "system" || row.TargetID != u.ID {
		t.Fatalf("the row names actor %s/%s and target %q; want the system, about %s", row.ActorKind, row.ActorID, row.TargetID, u.ID)
	}
	var after map[string]any
	if err := json.Unmarshal([]byte(row.After), &after); err != nil {
		t.Fatalf("decoding the row: %v", err)
	}
	if after["method"] != BootstrapEnvPassword || after["role"] != string(store.RolePlatform) {
		t.Fatalf("the row says %v; want the environment password method and the platform role", after)
	}
}

// The token variant exists so a provisioner can drive the API with no browser
// at all: the token it wrote into the file has to be the one that works, as
// platform, and the secret must never reach the audit row.
func TestAnUnattendedTokenAuthenticatesAsPlatform(t *testing.T) {
	s, st, _ := newService(t)
	const secret = "0123456789abcdef0123456789abcdef-provisioner"

	u, tok, err := s.CreateUnattendedIdentity(t.Context(), Unattended{Username: "terraform", Token: secret + "\n"})
	if err != nil {
		t.Fatalf("CreateUnattendedIdentity: %v", err)
	}
	if tok == nil || tok.UserID != u.ID {
		t.Fatalf("the token %+v does not belong to the account %s", tok, u.ID)
	}
	id, err := s.Authenticate(t.Context(), AuthInput{Authorization: "Bearer " + secret})
	if err != nil {
		t.Fatalf("the provisioner's token does not authenticate: %v", err)
	}
	if id.Role != store.RolePlatform || id.UserID != u.ID {
		t.Fatalf("the token authenticates as %s for %q; want platform for %s", id.Role, id.UserID, u.ID)
	}
	if _, _, err := s.Login(t.Context(), "terraform", "", "203.0.113.1", "test"); err == nil {
		t.Fatal("a token-only account must not sign in with an empty password")
	}

	rows := bootstrapRows(t, st)
	if len(rows) != 1 {
		t.Fatalf("got %d auth.bootstrap rows; want exactly one", len(rows))
	}
	if strings.Contains(rows[0].After, secret) || !strings.Contains(rows[0].After, BootstrapEnvToken) {
		t.Fatalf("the row is %s; want the token method and no secret", rows[0].After)
	}
}

// The environment must never be a way to add a platform account to an
// instance somebody has already claimed.
func TestAnUnattendedIdentityIsRefusedOnceAnyAccountExists(t *testing.T) {
	s, st, _ := newService(t)
	addUser(t, st, "someone", store.RoleViewer, nil)

	for name, in := range map[string]Unattended{
		"password": {Username: "ops", Password: testPassword},
		"token":    {Username: "ops", Token: strings.Repeat("x", MinBootstrapTokenLength)},
	} {
		if _, _, err := s.CreateUnattendedIdentity(t.Context(), in); !errors.Is(err, ErrAlreadyBootstrapped) {
			t.Errorf("%s: got %v; want ErrAlreadyBootstrapped", name, err)
		}
	}
	if rows := bootstrapRows(t, st); len(rows) != 0 {
		t.Fatalf("a refused bootstrap wrote %d audit rows", len(rows))
	}
}

// A token the API would refuse, or one easier to guess than a minted one, is
// turned away before anything is written, so the next start can try again.
func TestAnUnattendedTokenThatCouldNeverWorkIsRefused(t *testing.T) {
	cases := map[string]string{
		"short":      "too-short",
		"whitespace": strings.Repeat("a", 20) + " " + strings.Repeat("b", 20),
		"agent":      AgentTokenPrefix + strings.Repeat("a", 40),
		"join":       JoinTokenPrefix + strings.Repeat("a", 40),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newService(t)
			if _, _, err := s.CreateUnattendedIdentity(t.Context(), Unattended{Username: "ops", Token: token}); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v; want ErrInvalidInput", err)
			}
			if need, _ := s.NeedsBootstrap(t.Context()); !need {
				t.Fatal("a refused token left an account behind, which closes bootstrap for good")
			}
		})
	}
}

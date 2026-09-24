package controller

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

func writeSecret(t *testing.T, value string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(value+"\n"), mode); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

func (h *harness) bootstrapWith(b config.Bootstrap) {
	h.c.UpdateConfig(func(c *config.Config) { c.Bootstrap = b })
}

func (h *harness) problemsByCode() map[string]Problem {
	h.t.Helper()
	ps, err := h.c.Problems(h.ctx)
	if err != nil {
		h.t.Fatalf("Problems: %v", err)
	}
	out := map[string]Problem{}
	for _, p := range ps {
		out[p.Code] = p
	}
	return out
}

// A compose file or a Terraform module has nobody watching, so on an empty
// database the environment's account is the one that makes the instance
// usable, and it is the platform one.
func TestTheEnvironmentCreatesTheFirstPlatformAccountOnAnEmptyDatabase(t *testing.T) {
	h := newHarness(t)
	h.bootstrapWith(config.Bootstrap{
		Admin:        "ops",
		PasswordFile: writeSecret(t, "correct-horse-battery-staple", 0o600),
	})

	if err := h.c.BootstrapFromEnvironment(h.ctx); err != nil {
		t.Fatalf("BootstrapFromEnvironment: %v", err)
	}
	u, err := h.st.GetUserByUsername(h.ctx, "ops")
	if err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	if u.Role != store.RolePlatform {
		t.Fatalf("the account is %s; want platform", u.Role)
	}
	if _, ok := h.problemsByCode()["bootstrap.ignored"]; ok {
		t.Fatal("variables that did their job must not be reported as ignored")
	}
}

// Variables that silently do nothing are a credential sitting in a file for no
// reason, so a start that ignores them says which ones to remove.
func TestTheBootstrapVariablesAreIgnoredAndNamedOnceAnAccountExists(t *testing.T) {
	h := newHarness(t)
	if err := h.st.CreateUser(h.ctx, &store.User{Username: "someone", Role: store.RoleAdmin}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	h.bootstrapWith(config.Bootstrap{
		Admin:     "ops",
		TokenFile: writeSecret(t, strings.Repeat("t", 40), 0o600),
	})

	if err := h.c.BootstrapFromEnvironment(h.ctx); err != nil {
		t.Fatalf("BootstrapFromEnvironment: %v", err)
	}
	if n, _ := h.st.CountUsers(h.ctx); n != 1 {
		t.Fatalf("there are %d accounts; the environment must not add one to a claimed instance", n)
	}
	p, ok := h.problemsByCode()["bootstrap.ignored"]
	if !ok {
		t.Fatal("no bootstrap.ignored problem")
	}
	if p.Severity != config.SeverityWarning {
		t.Errorf("bootstrap.ignored is %s; want a warning", p.Severity)
	}
	for _, v := range []string{"ZOOMIES_BOOTSTRAP_ADMIN", "ZOOMIES_BOOTSTRAP_TOKEN_FILE"} {
		if !strings.Contains(p.Title+p.Fix, v) {
			t.Errorf("the problem does not name %s: %q / %q", v, p.Title, p.Fix)
		}
	}
	if strings.Contains(p.Title+p.Fix, "PASSWORD_FILE") {
		t.Errorf("the problem names a variable that is not set: %q", p.Title)
	}
}

// The rule the encryption key file follows: a platform credential readable by
// every account on the host is not one only the provisioner holds.
func TestABootstrapFileOthersCanReadIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits say nothing about access on Windows")
	}
	h := newHarness(t)
	h.bootstrapWith(config.Bootstrap{
		Admin:        "ops",
		PasswordFile: writeSecret(t, "correct-horse-battery-staple", 0o644),
	})

	err := h.c.BootstrapFromEnvironment(h.ctx)
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("got %v; want a refusal that says how to fix the mode", err)
	}
	if n, _ := h.st.CountUsers(h.ctx); n != 0 {
		t.Fatal("a refused file still created an account")
	}
}

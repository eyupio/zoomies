package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/installer"
)

// TestTheControllerOnlyAnswersTemplateIsAWholeUnattendedInstall holds the
// committed template to what it claims to be.
//
// A provisioner copies this file and changes the example values; it does not
// read the installer to find out which keys it forgot. So the template has to
// load with the installer's own strict reader, leave nothing the installer
// would stop to ask for, and keep the choices that make it controller-only --
// and it must never carry a password inline, because the file is exactly the
// thing that ends up in a repository.
func TestTheControllerOnlyAnswersTemplateIsAWholeUnattendedInstall(t *testing.T) {
	path := filepath.Join(marketplaceDir, "controller-answers.yaml")
	a, err := installer.Load(path)
	if err != nil {
		t.Fatalf("the installer cannot read the template: %v", err)
	}
	mode, err := installer.ParseMode(a.Mode)
	if err != nil {
		t.Fatalf("the template's mode: %v", err)
	}
	if mode != installer.ModeController {
		t.Errorf("the template is mode %q; a controller-only instance runs no embedded agent", a.Mode)
	}
	if err := a.Validate(mode); err != nil {
		t.Errorf("the template leaves questions unanswered:\n%v", err)
	}

	checks := []struct {
		ok   bool
		what string
	}{
		{a.GitHub.Skip, "github.skip, so an install needs no App credentials"},
		{a.Pool.Skip, "pool.skip, since a controller with no agent has no runners for a pool"},
		{a.TLS.Mode == "files" && a.TLS.CertFile != "" && a.TLS.KeyFile != "", "TLS from files"},
		{strings.HasPrefix(a.ExternalURL, "https://"), "an https external URL"},
		{a.Admin.Username != "" && a.Admin.PasswordFile != "", "a first account whose password comes from a file"},
		{a.Admin.Password == "", "no inline password"},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("the template does not set %s", c.what)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the template: %v", err)
	}
	if !strings.Contains(string(raw), "ZOOMIES_BOOTSTRAP_") {
		t.Error("the template does not point a containerised install at the bootstrap variables")
	}
}

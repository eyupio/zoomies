package installer

import (
	"bytes"
	"context"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func uninstallOpts(t *testing.T) UninstallOptions {
	t.Helper()
	return UninstallOptions{
		ConfigDir: t.TempDir(),
		StateDir:  t.TempDir(),
		Out:       &bytes.Buffer{},
		In:        strings.NewReader(""),
	}
}

func TestUninstallItemsListsWhatIsThere(t *testing.T) {
	opts := uninstallOpts(t)
	writeFile(t, opts.ConfigDir, "zoomies.yaml", "server: {}\n")
	writeFile(t, opts.ConfigDir, "encryption.key", "key\n")
	writeFile(t, opts.StateDir, "zoomies.db", "")
	opts.BinaryPath = writeFile(t, t.TempDir(), "zoomies", "#!/bin/sh\n")

	byPath := map[string]RemovalItem{}
	for _, it := range UninstallItems(opts) {
		byPath[it.Path] = it
	}

	for _, path := range []string{
		filepath.Join(opts.ConfigDir, "zoomies.yaml"),
		filepath.Join(opts.ConfigDir, "encryption.key"),
		filepath.Join(opts.StateDir, "zoomies.db"),
		opts.StateDir,
		opts.BinaryPath,
	} {
		it, ok := byPath[path]
		if !ok {
			t.Errorf("%s is not listed", path)
			continue
		}
		if !it.Present {
			t.Errorf("%s exists but was listed as already gone", path)
		}
	}

	// The two irreplaceable things must say what losing them costs.
	if note := byPath[filepath.Join(opts.ConfigDir, "encryption.key")].Note; !strings.Contains(note, "decrypt") {
		t.Errorf("the key's note should say what cannot be decrypted without it, got %q", note)
	}
	if note := byPath[filepath.Join(opts.StateDir, "zoomies.db")].Note; !strings.Contains(note, "audit") {
		t.Errorf("the database's note should say what is in it, got %q", note)
	}
	if note := byPath[opts.BinaryPath].Note; !strings.Contains(note, "last") {
		t.Errorf("the binary goes last so a failed run can be finished, got %q", note)
	}
}

func TestUninstallItemsMarksWhatIsAlreadyGone(t *testing.T) {
	opts := uninstallOpts(t)
	for _, it := range UninstallItems(opts) {
		if it.Path == opts.StateDir || it.Path == opts.ConfigDir {
			continue // t.TempDir creates these
		}
		if it.Present {
			t.Errorf("%s should not be present in an empty temp directory", it.Path)
		}
	}
}

func TestUninstallItemsRespectsKeepConfig(t *testing.T) {
	opts := uninstallOpts(t)
	opts.KeepConfig = true
	writeFile(t, opts.ConfigDir, "zoomies.yaml", "")

	for _, it := range UninstallItems(opts) {
		if it.What == "configuration" {
			if !strings.Contains(it.Note, "kept") {
				t.Fatalf("a kept config must say so: %+v", it)
			}
			return
		}
	}
	t.Fatal("the configuration was not listed at all")
}

func TestUninstallDoesNothingWhenNothingIsInstalled(t *testing.T) {
	opts := uninstallOpts(t)
	if err := os.RemoveAll(opts.ConfigDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(opts.StateDir); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts.Out = &out

	if err := Uninstall(context.Background(), opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "not installed here") {
		t.Fatalf("it should say so plainly:\n%s", out.String())
	}
}

func TestUninstallRefusesWithoutConfirmation(t *testing.T) {
	opts := uninstallOpts(t)
	writeFile(t, opts.ConfigDir, "zoomies.yaml", "")
	opts.NonInteractive = true

	err := Uninstall(context.Background(), opts)
	if err == nil {
		t.Fatal("an unattended uninstall must be asked for explicitly")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("the error should say how to confirm, got: %v", err)
	}
	if !exists(filepath.Join(opts.ConfigDir, "zoomies.yaml")) {
		t.Fatal("nothing may be removed before the confirmation")
	}
}

func TestUninstallRemovesAndReportsLineByLine(t *testing.T) {
	opts := uninstallOpts(t)
	opts.Yes = true
	opts.NonInteractive = true
	writeFile(t, opts.ConfigDir, "zoomies.yaml", "")
	writeFile(t, opts.ConfigDir, "encryption.key", "")
	writeFile(t, opts.StateDir, "zoomies.db", "")
	binary := writeFile(t, t.TempDir(), "zoomies", "#!/bin/sh\n")
	opts.BinaryPath = binary
	var out bytes.Buffer
	opts.Out = &out

	if err := Uninstall(context.Background(), opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	for _, path := range []string{
		filepath.Join(opts.ConfigDir, "zoomies.yaml"),
		filepath.Join(opts.ConfigDir, "encryption.key"),
		opts.StateDir,
		binary,
	} {
		if exists(path) {
			t.Errorf("%s should be gone", path)
		}
	}
	report := out.String()
	for _, want := range []string{"removed", "zoomies.yaml", "encryption.key", "Done"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report should mention %q:\n%s", want, report)
		}
	}

	// Safe to re-run: everything is already gone and that is not an error.
	if err := Uninstall(context.Background(), opts); err != nil {
		t.Fatalf("second Uninstall: %v", err)
	}
}

func TestUninstallLeavesWhatIsNotItsAndSaysSo(t *testing.T) {
	opts := uninstallOpts(t)
	opts.Yes = true
	opts.NonInteractive = true
	opts.KeepConfig = true
	writeFile(t, opts.ConfigDir, "zoomies.yaml", "# edited by hand\n")
	writeFile(t, opts.ConfigDir, "fullchain.pem", "a certificate the operator put there")
	var out bytes.Buffer
	opts.Out = &out

	if err := Uninstall(context.Background(), opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !exists(filepath.Join(opts.ConfigDir, "zoomies.yaml")) {
		t.Fatal("the config was to be kept")
	}
	if !exists(filepath.Join(opts.ConfigDir, "fullchain.pem")) {
		t.Fatal("a file Zoomies did not write must be left alone")
	}
	report := out.String()
	if !strings.Contains(report, "left in place") || !strings.Contains(report, "fullchain.pem") {
		t.Fatalf("the report must name what was left behind:\n%s", report)
	}
}

func TestServiceUserToRemoveOnlyEverRemovesOurOwn(t *testing.T) {
	// Never the account a human logs in with, whatever the flags say.
	if got := serviceUserToRemove(UninstallOptions{ServiceUser: "ada"}); got != "" {
		t.Fatalf("serviceUserToRemove = %q, want empty", got)
	}
	// Never for an install that lives somewhere else: those never created an
	// account, and this is what keeps a test with temporary directories from
	// deleting a real one.
	if got := serviceUserToRemove(UninstallOptions{ConfigDir: t.TempDir(), StateDir: t.TempDir()}); got != "" {
		t.Fatalf("serviceUserToRemove for a relocated install = %q, want empty", got)
	}
	if os.Geteuid() != 0 {
		if got := serviceUserToRemove(UninstallOptions{}); got != "" {
			t.Fatalf("a non-root uninstall cannot remove accounts, got %q", got)
		}
	}
}

func TestAskYesNo(t *testing.T) {
	cases := []struct {
		in     string
		prompt string
		want   bool
	}{
		{"yes\n", "Remove? ", true},
		{"y\n", "Remove? ", true},
		{"no\n", "Remove? ", false},
		{"\n", "Deregister? [Y/n]: ", true},
		{"\n", "Remove? ", false},
		{"", "Remove? ", false},
	}
	for _, tc := range cases {
		got, err := askYesNo(strings.NewReader(tc.in), &bytes.Buffer{}, tc.prompt)
		if err != nil {
			t.Fatalf("askYesNo(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("askYesNo(%q, %q) = %v, want %v", tc.in, tc.prompt, got, tc.want)
		}
	}
}

func TestRemoveServiceUserNeedsRoot(t *testing.T) {
	t.Skip("creating and removing system accounts changes the machine the tests run on; " +
		"exercised in the packaging tests that run in a throwaway container")
}

// Runner names carry no instance identity, so the "zoomies-" prefix used to
// be the whole test of ownership: uninstalling a staging controller deleted a
// production controller's idle runners on the same organisation.
func TestUninstallDeregistersOnlyTheRunnersThisDatabaseKnows(t *testing.T) {
	remote := []github.Runner{
		{ID: 1, Name: "zoomies-ours-by-id"},
		{ID: 2, Name: "zoomies-ours-by-name"},
		{ID: 3, Name: "zoomies-someone-elses-controller"},
		{ID: 4, Name: "hand-registered"},
	}
	local := []*store.Runner{
		{Name: "zoomies-renamed-on-github", GitHubRunnerID: 1},
		{Name: "zoomies-ours-by-name"},
	}
	got := fleetRunners(remote, local)
	ids := make([]int64, 0, len(got))
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []int64{1, 2}) {
		t.Fatalf("would deregister %v, want only the two this database has rows for", ids)
	}
}

// On most installations the config directory and the state directory are one
// directory, and --keep-config has to survive that.
//
// config.StateDir and config.ConfigDir both answer ~/Library/Application
// Support/zoomies on macOS, and os.UserConfigDir()/zoomies for a non-root
// Linux install; only a root install separates /var/lib/zoomies from
// /etc/zoomies. Uninstall used RemoveAll on the state directory, so on every
// one of those it deleted the configuration too -- the zoomies.yaml just
// promised to be kept, the encryption key, and anything else in there -- and
// then reported only that the state directory had gone.
//
// The encryption key is the part that does not come back. It is the only copy,
// and without it the stored App private keys and webhook secrets in any backup
// the operator still holds cannot be decrypted.
//
// Every other test here gives ConfigDir and StateDir separate temporary
// directories, which is exactly why none of them could see this.
func TestUninstallWithOneSharedDirectoryKeepsTheConfigItPromisedToKeep(t *testing.T) {
	shared := t.TempDir()
	opts := UninstallOptions{
		ConfigDir:      shared,
		StateDir:       shared,
		Out:            &bytes.Buffer{},
		In:             strings.NewReader(""),
		Yes:            true,
		NonInteractive: true,
		KeepConfig:     true,
	}
	var out bytes.Buffer
	opts.Out = &out

	writeFile(t, shared, "zoomies.yaml", "server: {}\n")
	writeFile(t, shared, "encryption.key", "the only copy")
	writeFile(t, shared, "zoomies.db", "")
	// Something of the operator's own, which uninstall has never claimed.
	writeFile(t, shared, "fullchain.pem", "a certificate they put here")

	if err := Uninstall(context.Background(), opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if !exists(filepath.Join(shared, "zoomies.yaml")) {
		t.Errorf("--keep-config was given and zoomies.yaml was deleted anyway:\n%s", out.String())
	}
	if !exists(filepath.Join(shared, "fullchain.pem")) {
		t.Errorf("a file that was never ours was deleted:\n%s", out.String())
	}
	// The database is still the thing being uninstalled.
	if exists(filepath.Join(shared, "zoomies.db")) {
		t.Errorf("the database survived, so nothing was uninstalled:\n%s", out.String())
	}
	// The key goes: it is not configuration, and leaving it behind is the
	// thing removing it at all is meant to prevent.
	if exists(filepath.Join(shared, "encryption.key")) {
		t.Errorf("the encryption key was left behind:\n%s", out.String())
	}
	// And the report has to name what survived, or the operator does not know
	// there is anything still to clean up.
	if report := out.String(); !strings.Contains(report, "fullchain.pem") {
		t.Errorf("the report did not name what was left behind:\n%s", report)
	}
}

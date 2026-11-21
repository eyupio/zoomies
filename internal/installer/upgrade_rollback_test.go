package installer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/store"
)

// rollbackFixture is a native controller that does not answer until its
// service is restarted, with the previous release kept beside the installed
// one. It returns the plan, the binary path and a pointer to whether the
// restart has happened.
func rollbackFixture(t *testing.T) (*upgradePlan, string, *bool, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "zoomies")
	if err := os.WriteFile(bin, []byte("new build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin+PreviousSuffix, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	answering := false
	var out bytes.Buffer
	kept, err := backup.FileSHA256(bin + PreviousSuffix)
	if err != nil {
		t.Fatal(err)
	}
	p := &upgradePlan{
		opts: UpgradeOptions{
			Out: &out, BinaryPath: bin, KeptPrevious: kept,
			serveTimeout: 20 * time.Millisecond, servePoll: time.Millisecond, progressEvery: time.Nanosecond,
			run: func(_ context.Context, name string, args ...string) (string, error) {
				if name == "systemctl" && slices.Contains(args, "restart") {
					answering = true
				}
				return "", nil
			},
		},
		record:      DeploymentRecord{Deployment: DeploymentNative, Mode: ModeController},
		nativeUnits: []string{"zoomies.service"}, unit: "zoomies.service",
	}
	return p, bin, &answering, &out
}

func failingCheck(answering *bool) *serveCheck {
	return &serveCheck{
		what: "the controller",
		probe: func(context.Context) error {
			if *answering {
				return nil
			}
			return errors.New("connection refused")
		},
		stopped: func(context.Context) string { return "" },
		latest:  func(context.Context) string { return "" },
	}
}

// The whole point of a rollback is that the host ends on what it ran: a
// controller that never answers, having migrated nothing, is put back.
func TestAControllerThatDoesNotAnswerIsPutBackWhenNothingMigrated(t *testing.T) {
	p, bin, answering, out := rollbackFixture(t)
	err := p.waitServing(context.Background(), failingCheck(answering))
	if err == nil || !strings.Contains(err.Error(), "previous release is running again") {
		t.Fatalf("error = %v", err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "old build" {
		t.Fatalf("binary = %q, want the previous release back", b)
	}
	if !strings.Contains(out.String(), "putting the previous release back") {
		t.Fatalf("the rollback was not said:\n%s", out.String())
	}
	// The helper reads that this failure was undone from a marked line of its own.
	if !strings.Contains(out.String(), engineResultMarker+"rolled_back=true") {
		t.Fatalf("the rollback was not reported to the helper:\n%s", out.String())
	}
}

// Migrations run one way: an older release cannot open a database a newer one
// has moved on, so the binary must not be put back over it. The copy taken
// before the migration is named instead.
func TestAMigratedDatabaseIsNotRolledBackOnto(t *testing.T) {
	p, bin, answering, _ := rollbackFixture(t)
	dbDir := filepath.Join(t.TempDir(), "data")
	copyDir := filepath.Join(dbDir, store.PreMigrationDir, "zoomies-20300101-000000")
	if err := os.MkdirAll(copyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p.dbPath = filepath.Join(dbDir, "zoomies.db")
	p.beforeMigrate = ""

	err := p.waitServing(context.Background(), failingCheck(answering))
	if err == nil || !strings.Contains(err.Error(), copyDir) || !strings.Contains(err.Error(), "not put back") {
		t.Fatalf("error = %v", err)
	}
	if *answering {
		t.Fatal("the service was restarted although the database had migrated")
	}
	if b, _ := os.ReadFile(bin); string(b) != "new build" {
		t.Fatalf("binary = %q, want it left as the new release", b)
	}
}

// The kept binary is the one this upgrade replaced, and nothing else. Upgrades
// that do not go through the self-update (install.sh --upgrade, --no-download,
// "already on the latest build") still restart the service and wait for it, and
// the file left from an earlier upgrade is a release the database may have moved
// past: putting it back is an outage, reported as the previous release having
// been put back.
func TestAKeptBinaryThatThisUpgradeDidNotWriteIsNotRolledBackTo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker func(string) string
	}{
		{"no marker, a run that replaced nothing", func(string) string { return "" }},
		{"a marker for a different file", func(string) string { return "0000000000000000000000000000000000000000000000000000000000000000" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, bin, answering, out := rollbackFixture(t)
			p.opts.KeptPrevious = tc.marker(bin)

			err := p.waitServing(context.Background(), failingCheck(answering))
			if err == nil {
				t.Fatal("a controller that never answered was reported as fine")
			}
			if strings.Contains(err.Error(), "previous release is running again") || strings.Contains(out.String(), "putting the previous release back") {
				t.Fatalf("a stale kept binary was put back:\n%v\n%s", err, out.String())
			}
			if *answering {
				t.Fatal("the service was restarted onto a binary this upgrade did not keep")
			}
			if b, _ := os.ReadFile(bin); string(b) != "new build" {
				t.Fatalf("binary = %q, want it left as the new release", b)
			}
		})
	}
}

// A native controller with no kept previous binary, and a container whose
// service is its image, are not rolled back: there is nothing to put back.
func TestAControllerWithNothingToRollBackToIsNotPutBack(t *testing.T) {
	p, bin, answering, _ := rollbackFixture(t)
	if err := os.Remove(bin + PreviousSuffix); err != nil {
		t.Fatal(err)
	}
	err := p.waitServing(context.Background(), failingCheck(answering))
	if err == nil || !strings.Contains(err.Error(), "do not stop it part way") {
		t.Fatalf("error = %v", err)
	}
	if *answering {
		t.Fatal("the service was restarted with nothing to roll back to")
	}
	if b, _ := os.ReadFile(bin); string(b) != "new build" {
		t.Fatalf("binary = %q, want it unchanged", b)
	}
}

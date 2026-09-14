package proxmox

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// The harness's waits must fit inside the timeout its Makefile target gives it.
//
// A qualification run that is killed by `go test` does not merely fail: it
// fails having left virtual machines on somebody's cluster, and it throws away
// the closing reconciliation, which is the step that would have found them.
// This test has no build tag, so it runs on every ordinary `go test ./...`
// rather than only where there is a cluster to run against.
func TestTheQualificationFitsInsideItsMakefileTimeout(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	// Only the targets that run this package: the e2e harness next door has its
	// own budget and its own, much shorter, timeout.
	re := regexp.MustCompile(`(?m)^\s+\$\(GO\) test .*-tags e2e.*-timeout (\S+) \./test/e2e/proxmox`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no `go test -tags e2e ... -timeout <d> ./test/e2e/proxmox` line found in the Makefile; " +
			"if the target was renamed, update this test rather than deleting it")
	}
	for _, m := range matches {
		limit, err := time.ParseDuration(m[1])
		if err != nil {
			t.Fatalf("the Makefile's proxmox timeout %q is not a duration: %v", m[1], err)
		}
		if limit <= qualificationBudget {
			t.Errorf("the Makefile allows %s but the qualification's waits and cleanup need %s; "+
				"the run would be killed with machines still on the cluster", limit, qualificationBudget)
		}
	}
}

// Every wait has to be inside a budget, or adding one moves the real worst case
// without moving the number the Makefile is checked against.
//
// The arithmetic is spelled out here rather than trusted: the cycle budget is
// the one that is multiplied by twenty, so a wait left out of it is twenty
// times as wrong as it looks.
func TestEveryWaitIsInsideTheBudgetItIsCheckedBy(t *testing.T) {
	sum := waitFirstMachine + waitMachineCreated + waitMachineReady +
		waitJobCompleted + waitMachineDrained + waitMachineDeleted
	if cycleBudget != sum {
		t.Errorf("cycleBudget is %s but its phases add up to %s; a phase was added without being budgeted for", cycleBudget, sum)
	}
	// The fault cases and the fixed costs, against the whole.
	whole := waitSetup + waitControllerHealthy + waitProviderChecked +
		qualificationCycles*cycleBudget +
		scaleFromZeroBudget + multiPoolBurstBudget + restartMidCreateBudget +
		bootstrapFailureBudget + deleteFailureBudget +
		waitReconciliation + waitCleanup
	if qualificationBudget != whole {
		t.Errorf("qualificationBudget is %s but its parts add up to %s", qualificationBudget, whole)
	}
	if qualificationCycles != 20 {
		t.Errorf("the procedure asks for 20 cycles and this build would run %d; "+
			"the evidence is written as p95 of 20 and a different denominator is a different claim", qualificationCycles)
	}
}

// The reserve must cover everything that happens after the last cycle, or a run
// that stops in time still has no time to tidy up and reconcile.
func TestTheReserveCoversTheStepsThatRunAfterTheLastCycle(t *testing.T) {
	if reserveForTheEnd < waitReconciliation+waitCleanup {
		t.Errorf("the reserve is %s but the reconciliation and cleanup need %s; "+
			"a run that stopped in time would still be killed during cleanup", reserveForTheEnd, waitReconciliation+waitCleanup)
	}
}

// repoRoot walks up to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root from the test's working directory")
	return ""
}

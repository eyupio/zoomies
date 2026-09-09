package drill

import "time"

// The drills' waits.
//
// These are much shorter than the end-to-end harness's, and for a good reason:
// nothing here crosses a network or pulls an image. GitHub is a fake in this
// process, the runner is a stub already on disk, and the two Zoomies binaries
// are starting locally. A drill that needs minutes is a drill that has hung.
//
// The file carries no build tag so budget_test.go can check these against the
// Makefile's timeout in ordinary CI, the same way the e2e harness does.
const (
	waitProcessUp     = 30 * time.Second
	waitRunnerCreated = 60 * time.Second
	waitWorkloadUp    = 60 * time.Second
	waitJobDone       = 60 * time.Second
	waitRunnerGone    = 60 * time.Second
	// waitPromptCleanup is how long the removal path itself gets to delete a
	// GitHub registration. It is deliberately far shorter than the rest: the
	// reaper's first sweep is a minute after the controller starts, and an
	// assertion looser than that would be satisfied by the backstop instead of
	// by the code under test.
	waitPromptCleanup = 15 * time.Second
	// waitRecovery is what a fault drill allows for the fleet to come back
	// after something is killed. It is the longest wait here because a
	// restarted controller has to re-derive its task queue from the rows.
	waitRecovery = 90 * time.Second
)

// drillBudget is the longest one drill may legitimately take.
const drillBudget = waitProcessUp + waitRunnerCreated + waitWorkloadUp +
	waitJobDone + waitRunnerGone + waitPromptCleanup + waitRecovery

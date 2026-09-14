package e2e

import "time"

// The waits the scenario is allowed, and the budget they add up to.
//
// These live in a file with no build tag on purpose. The roadmap found this
// harness with waits totalling twenty-seven minutes behind a Makefile timeout
// of twenty, which means the scenario could never have reached its own last
// assertion: `go test` would have killed it first, and the run would have been
// reported as a timeout rather than as whatever it was actually stuck on. A
// guard for that is worth nothing if it only compiles under the tag that needs
// real GitHub credentials, so budget_test.go runs in ordinary CI and reads the
// number out of the Makefile.
//
// Each wait is generous, because the thing being waited for crosses a real
// network to GitHub and a real image pull. What matters is that their sum
// stays under what the Makefile allows, with room for the cleanup that runs
// after the last one.
const (
	waitControllerHealthy = 30 * time.Second
	waitRunnerCreated     = 5 * time.Minute
	waitRunnerRegistered  = 4 * time.Minute
	waitJobCompleted      = 8 * time.Minute
	waitRunnerDestroyed   = 4 * time.Minute
	// waitCleanup is the ledger's, not the scenario's: it runs after the
	// scenario has finished or failed, and it is the part that must not be cut
	// short, because what it is doing is deleting a pool and an installation
	// from somebody's real organisation.
	waitCleanup = 2 * time.Minute
)

// The installer scenario's waits. It creates nothing on GitHub and pulls one
// small base image rather than a runner image, so it is much the cheaper of
// the two -- but it still crosses a registry, and a machine that has never
// pulled the image pays for that once.
const (
	waitInstallerImage = 3 * time.Minute
	// waitInstalledControllerHealthy is longer than the other scenario's
	// equivalent because this controller is starting for the first time on a
	// fresh database, so it migrates before it listens.
	waitInstalledControllerHealthy = 90 * time.Second
	// waitInstallerSteps covers `zoomies init` and `zoomies uninstall`: making
	// an account, writing the files, creating the administrator, and taking it
	// all off again.
	waitInstallerSteps = 3 * time.Minute
)

// installerBudget is the longest the installer scenario may legitimately take.
const installerBudget = waitInstallerImage + waitInstalledControllerHealthy + waitInstallerSteps

// scenarioBudget is the longest the GitHub scenario may legitimately take,
// cleanup included.
const scenarioBudget = waitControllerHealthy + waitRunnerCreated + waitRunnerRegistered +
	waitJobCompleted + waitRunnerDestroyed + waitCleanup

// totalBudget is what one `go test` invocation may need, because the
// scenarios run in the same binary under one -timeout. Adding a scenario and
// forgetting this is how a suite starts being killed partway through its
// second one and reporting a timeout rather than a result.
const totalBudget = scenarioBudget + installerBudget

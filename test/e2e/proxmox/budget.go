package proxmox

import "time"

// The waits this harness allows, and the budget they add up to.
//
// The end-to-end harness next door was once found with waits totalling
// twenty-seven minutes behind a Makefile timeout of twenty, which means it
// could never have reached its own last assertion: `go test` would have killed
// it first and reported a timeout rather than whatever it was stuck on. That is
// worse here than it is there. This harness creates virtual machines, and a run
// killed between the clone and the delete leaves them running on somebody's
// cluster; the closing reconciliation, which is the step that decides
// qualification, is the last thing to run and therefore the first thing a
// too-short timeout throws away.
//
// So the numbers live in a file with no build tag, budget_test.go checks them
// against the Makefile in ordinary CI, and the harness itself refuses to start
// when `go test`'s own deadline cannot fit them -- fast, and before anything
// has been created.
//
// Each wait is generous, because every one of them crosses a real network to a
// real hypervisor. None is so generous that it stops meaning anything: a
// prepared template that takes four minutes to clone and boot on a
// qualification cluster is a finding, not a slow day, and the run should say so
// rather than wait.
const (
	// waitControllerHealthy covers a start and, in the restart case, a restart
	// that has to re-derive its machine work from the rows.
	waitControllerHealthy = 60 * time.Second
	// waitProviderChecked is the live preflight: one round trip to the cluster
	// for the version, the privileges, the storage and the template.
	waitProviderChecked = 2 * time.Minute
	// waitFirstMachine is queued work to a machine row. Nothing has been built
	// yet -- this is the controller's own loop -- so it is short, and a long one
	// means the demand never reached the provider rather than that the
	// hypervisor is slow.
	waitFirstMachine = 90 * time.Second
	// waitMachineCreated is the hypervisor's half: clone the template, then
	// power the guest on.
	waitMachineCreated = 4 * time.Minute
	// waitMachineReady is the guest's: the agent answering, the bootstrap
	// running inside it, and the host joining this controller.
	waitMachineReady = 4 * time.Minute
	// waitJobCompleted is a real job on a real repository, including whatever
	// the runner has to pull before it can start.
	waitJobCompleted = 5 * time.Minute
	// waitMachineDrained is a cordoned machine waiting for its runners to
	// finish. The job it ran is already over by the time a drain is asked for,
	// so this covers the pass that notices rather than the work.
	waitMachineDrained = 2 * time.Minute
	// waitMachineDeleted is the delete and, more to the point, the inspect that
	// proves the resource is gone. A 200 from the delete call is not that.
	waitMachineDeleted = 3 * time.Minute
	// waitMachineFailed is how long an induced failure gets to be recorded as
	// one. It is the longest single wait because giving up is deliberately not
	// the first thing the reconciler does: it retries with backoff first.
	waitMachineFailed = 6 * time.Minute
	// waitReconciliation is the closing inventory listing, which is two calls
	// to the cluster and a comparison.
	waitReconciliation = 5 * time.Minute
	// waitCleanup is the harness's, not the scenario's. It runs after the
	// scenario has finished or failed and it must not be cut short: what it is
	// doing is deleting virtual machines, a pool and an installation from
	// somebody's real cluster and organisation.
	waitCleanup = 15 * time.Minute
	// waitSetup covers everything before the first cycle: the admin, the
	// installation, the provider and the pools.
	waitSetup = 5 * time.Minute
)

// qualificationCycles is the number of full create, enrol, run, drain and
// delete cycles the procedure asks for. It is a constant rather than a setting
// because the evidence is "p95 of 20": a run that chose its own denominator
// could report a p95 of three and still call itself a qualification.
const qualificationCycles = 20

// cycleBudget is one full cycle's worst case.
const cycleBudget = waitFirstMachine + waitMachineCreated + waitMachineReady +
	waitJobCompleted + waitMachineDrained + waitMachineDeleted

// The five fault cases, each budgeted as what it actually does rather than as a
// share of the whole: a restart adds a controller start to a cycle, an induced
// bootstrap failure never reaches a job, and an induced delete failure pays for
// the delete twice.
const (
	scaleFromZeroBudget    = cycleBudget
	multiPoolBurstBudget   = cycleBudget + waitMachineCreated
	restartMidCreateBudget = cycleBudget + waitControllerHealthy
	bootstrapFailureBudget = waitFirstMachine + waitMachineCreated + waitMachineFailed + waitMachineDeleted
	deleteFailureBudget    = cycleBudget + waitMachineDeleted
)

// qualificationBudget is the longest a complete run may legitimately take,
// cleanup included. The Makefile's -timeout must exceed it, and so must
// whatever deadline `go test` was actually given.
const qualificationBudget = waitSetup + waitControllerHealthy + waitProviderChecked +
	qualificationCycles*cycleBudget +
	scaleFromZeroBudget + multiPoolBurstBudget + restartMidCreateBudget +
	bootstrapFailureBudget + deleteFailureBudget +
	waitReconciliation + waitCleanup

// reserveForTheEnd is what must still be left when a cycle begins, so that a
// run which cannot fit another one stops and reconciles instead of being killed
// part-way through building a machine.
//
// This is the whole reason the harness looks at its own deadline. A cycle
// interrupted by `go test`'s timeout leaves a VM on the cluster and no
// reconciliation to notice it, which is precisely the failure this procedure
// exists to detect.
const reserveForTheEnd = waitReconciliation + waitCleanup

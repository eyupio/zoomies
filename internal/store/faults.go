package store

// FaultKind is why the fleet, rather than the workflow, is the reason a job
// went wrong -- in the terms the operator's next move is decided in.
//
// GitHub records a job whose runner died exactly as it records a job whose
// tests failed: "failure". An operator reading that list has no way to tell
// the afternoon their code was broken from the afternoon their fleet was, and
// the two need completely different people. This is the fleet's own half of
// that answer.
//
// Every kind exists because something different is done about it -- a memory
// limit is raised, a registry credential is fixed, a host is looked at. A kind
// nothing acts on would be prose, and prose belongs in the fault's message.
type FaultKind string

const (
	// FaultHostLost is a host that stopped answering while it was running the
	// job. The runner went with the machine, and nothing here could stop it.
	FaultHostLost FaultKind = "host_lost"
	// FaultOutOfMemory is a runner killed for exceeding its memory limit. It
	// is kept apart from FaultRunnerExited because it is the commonest fleet
	// fault by a distance and the only one an operator fixes by typing a
	// number.
	FaultOutOfMemory FaultKind = "out_of_memory"
	// FaultOutOfDisk is a host with no room left. The job is only ever the one
	// unlucky enough to be running when the disk filled.
	FaultOutOfDisk FaultKind = "out_of_disk"
	// FaultRemoved is an operator who took the runner away with force while it
	// was working. It is a fleet fault because the workflow did nothing wrong,
	// but it is the one kind that needs no fixing: somebody meant it.
	FaultRemoved FaultKind = "removed"
	// FaultImage is a runner image that could not be pulled or would not
	// start: a tag that is not there, a registry that refused the credential.
	FaultImage FaultKind = "image"
	// FaultRegistration is GitHub refusing to register the runner -- a JIT
	// config it would not mint, an App whose permissions have moved. The
	// container may be perfectly healthy; it has nothing to attach to.
	FaultRegistration FaultKind = "registration"
	// FaultBackend is the container backend refusing the work or not
	// answering: a Docker socket that is not there, a daemon that never became
	// ready. This is the "cannot start the runner container" case.
	FaultBackend FaultKind = "backend"
	// FaultContainerConflict needs ownership inspection, not socket repair.
	FaultContainerConflict FaultKind = "container_conflict"
	// FaultBackendBusy is the daemon that is there and did not answer in time.
	// It is kept apart from FaultBackend because it is the one backend failure
	// where nothing is wrong with the backend: the host is carrying more work
	// than dockerd can keep up with, and the fix is on the pool's limits or
	// the host's capacity rather than on the socket. Sent to check whether the
	// daemon is running, an operator finds it running and stops believing the
	// fault line.
	FaultBackendBusy FaultKind = "backend_busy"
	// FaultConfig is a setting the runner itself refused. Nothing but an edit
	// fixes it, and until it is edited every runner in the pool will do the
	// same thing.
	FaultConfig FaultKind = "config"
	// FaultRunnerExited is a runner that stopped and could not be narrowed
	// further, including a kind reported by an agent newer than this build. It
	// is the unclassified case on purpose: guessing a kind would send somebody
	// to fix the wrong thing, and "read the runner's log" is honest.
	FaultRunnerExited FaultKind = "runner_exited"
)

// The two domains a failure can belong to. They are the whole point of the
// taxonomy: everything else is detail within one of them.
const (
	// FaultDomainWorkflow is the workflow's own outcome -- code that did not
	// compile, a test that did not pass. The fleet did its part.
	FaultDomainWorkflow = "workflow"
	// FaultDomainFleet is Zoomies' own: the job never got a working runner, or
	// lost the one it had.
	FaultDomainFleet = "fleet"
)

// allFaultKinds is the closed set, in the order the UI and the docs list them:
// the ones that happen to a running job first, then the ones that stop a runner
// ever taking one.
var allFaultKinds = []FaultKind{
	FaultHostLost, FaultOutOfMemory, FaultOutOfDisk, FaultRemoved,
	FaultImage, FaultRegistration, FaultBackend, FaultBackendBusy, FaultContainerConflict, FaultConfig, FaultRunnerExited,
}

// FaultKinds returns the closed set. The docs test and the UI's label table
// both read it, so a kind added here and nowhere else fails a test rather than
// rendering as a raw identifier.
func FaultKinds() []FaultKind { return append([]FaultKind(nil), allFaultKinds...) }

// Valid reports whether k is a kind this build knows. It is the check made
// before trusting a kind that arrived from anywhere but this package's own
// constants -- an agent, or a row written by a newer build.
func (k FaultKind) Valid() bool {
	for _, known := range allFaultKinds {
		if k == known {
			return true
		}
	}
	return false
}

// Normalise answers a kind this build knows for any kind at all: itself when it
// is known, FaultRunnerExited for anything else that is set, and empty for
// empty. An unrecognised kind reading as "we could not narrow it" is the safe
// direction -- it tells the operator to go and look, which is true.
func (k FaultKind) Normalise() FaultKind {
	switch {
	case k == "":
		return ""
	case k.Valid():
		return k
	}
	return FaultRunnerExited
}

// Fix is what to do about a fault of this kind, written for the person who has
// to do it. Empty for a kind that needs nothing done.
func (k FaultKind) Fix() string {
	switch k {
	case FaultHostLost:
		return "check that the host is up and its zoomies agent can still reach this controller; a machine that reboots takes its runners with it."
	case FaultOutOfMemory:
		return "raise the pool's memory limit, or put the pool on a host with more memory. The runner's page names the limit it was given."
	case FaultOutOfDisk:
		return "free space on the host, or give it a larger disk. Runner images and workflow caches are the usual weight."
	case FaultRemoved:
		return "nothing, unless the removal was a mistake: somebody removed this runner with force while it was working."
	case FaultImage:
		return "check the pool's image tag and that the host can reach the registry it is on."
	case FaultRegistration:
		return "check that the GitHub App is still installed on the repository and still holds its runner permissions."
	case FaultContainerConflict:
		return "automatic name-conflict recovery could not safely replace this container. Inspect its Zoomies ownership labels and parent runner, and check for duplicate agents sharing the daemon. Do not remove an active runner or a container owned by another workload."
	case FaultBackend:
		return "check the container backend on the host: the socket the agent names on the host's page, and whether the daemon is running."
	case FaultBackendBusy:
		return "the daemon is there and did not answer in time, so it is the host that is overloaded rather than the backend that is broken: lower the host's capacity or the pool's maximum runners, or give the pool CPU and memory limits so the daemon keeps a share of the machine. The host's throttle steps it down on its own while the pressure lasts."
	case FaultConfig:
		return "read the runner's log for the setting it named, and correct it on the pool; every runner in this pool will do the same until it is."
	case FaultRunnerExited:
		return "read the runner's last output on its page; a runner that stops mid-job has usually run out of memory or disk."
	}
	return ""
}

// FaultDomain names who a completed job's failure belongs to. Empty for a job
// that did not fail, which is different from not knowing.
//
// It is deliberately a method on the job rather than on the kind: a job with no
// fault is the fleet saying nothing went wrong on its side, and whether that
// makes the failure the workflow's depends on whether the job failed at all.
func (j *Job) FaultDomain() string {
	switch {
	case j.FleetFailed():
		return FaultDomainFleet
	case IsFailedConclusion(j.Conclusion):
		return FaultDomainWorkflow
	}
	return ""
}

// FleetFailed reports whether this fleet is the reason the job went wrong. It
// is the Go spelling of the SQL predicate the store's counts use, so the
// Overview's split and the Jobs page's filter cannot disagree.
//
// Either half alone is enough on purpose. The two are written together, but a
// row from before the kind existed carries only the message, and a fleet fault
// whose message went missing is still a fleet fault -- keying on one of them
// would quietly hand a job back to the workflow it did not belong to.
func (j *Job) FleetFailed() bool { return j.RunnerFault != "" || j.FaultKind != "" }

// WorkflowFailed reports whether the job failed on its own merits: GitHub
// concluded it failed and this fleet has nothing to confess.
func (j *Job) WorkflowFailed() bool {
	return !j.FleetFailed() && IsFailedConclusion(j.Conclusion)
}

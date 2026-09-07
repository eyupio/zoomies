// Package agent implements the half of Zoomies that runs runners: the agent
// daemon, its transport to the controller, and the reconciliation loop that
// keeps a host's workloads matching what the controller asked for.
//
// Agents only ever connect outbound. The controller never dials an agent, so a
// host behind NAT or a restrictive firewall needs no inbound rule -- which is
// the usual reason "multi-host" support turns into a VPN project.
package agent

import (
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/store"
)

// ProtocolVersion is bumped when the agent/controller wire format changes in a
// way an older peer cannot tolerate. The controller refuses a mismatched agent
// with a message telling the operator to upgrade it.
const ProtocolVersion = 1

// JoinRequest redeems a short-lived join token and enrols a new host.
type JoinRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	JoinToken       string `json:"join_token"`
	Name            string `json:"name"`
	Address         string `json:"address,omitempty"`
	Capacity        int    `json:"capacity"`
	OS              string `json:"os"`
	// Distro and OSVersion say which Linux this is. The controller cannot
	// place a pool that asks for Ubuntu 24.04 without them, and an agent too
	// old to send them is simply a host that makes no platform promise.
	Distro    string `json:"distro,omitempty"`
	OSVersion string `json:"os_version,omitempty"`
	Arch      string `json:"arch"`
	// CPUs and MemoryMB are how much machine this agent may use, which is the
	// cgroup's share when it runs in a container rather than the host's total.
	CPUs     int   `json:"cpus,omitempty"`
	MemoryMB int64 `json:"memory_mb,omitempty"`
	// DiskTotalMB and DiskFreeMB measure the filesystem holding the work
	// directory, which is where a runner's checkout and its caches land. Free
	// is what is available to a runner rather than what is unused: the two
	// differ by the reserve the filesystem keeps for root, and placing work
	// into space the job cannot write to is the failure that distinction
	// exists to prevent. Zero means the agent could not measure it, which is
	// not the same as a full disk.
	DiskTotalMB int64             `json:"disk_total_mb,omitempty"`
	DiskFreeMB  int64             `json:"disk_free_mb,omitempty"`
	Version     string            `json:"version"`
	Labels      map[string]string `json:"labels,omitempty"`
	Backends    []backend.Info    `json:"backends"`
	// PreviousToken is the agent token this host was issued the last time it
	// joined, sent when the credentials file still holds one. It is what lets
	// a rebuilt machine reclaim its own row: without it the controller refuses
	// to replace an existing host of the same name, because a join token on its
	// own must not be enough to take over somebody else's machine.
	PreviousToken string `json:"previous_token,omitempty"`
}

// JoinResponse hands back the host's identity and its long-lived agent token.
// The token is shown exactly once; the controller stores only its hash.
type JoinResponse struct {
	HostID     string `json:"host_id"`
	AgentToken string `json:"agent_token"`
	// ControllerVersion lets the agent warn about a version skew.
	ControllerVersion string `json:"controller_version"`
	HeartbeatInterval string `json:"heartbeat_interval"`
}

// HeartbeatRequest is sent on every interval. It carries the agent's own view
// of its runners so the controller can detect drift without polling.
type HeartbeatRequest struct {
	ProtocolVersion int `json:"protocol_version"`
	// Capacity is the agent's configured value, sent for the log and for
	// older controllers. The controller does not write it: capacity is set
	// at join and belongs to the operator after that.
	Capacity int `json:"capacity"`
	// CPUs and MemoryMB are facts about the machine rather than the operator's
	// choice, so unlike Capacity the controller does record them: a host
	// resized in place must stop describing itself as the machine it used to be.
	CPUs     int   `json:"cpus,omitempty"`
	MemoryMB int64 `json:"memory_mb,omitempty"`
	// DiskTotalMB and DiskFreeMB are the work directory's filesystem, sent on
	// every beat because free space is the one of these that moves on its own.
	// The controller writes them under a tolerance rather than on every
	// change, or a fleet would take one row write per host per beat for a
	// figure that is never exactly the same twice.
	DiskTotalMB int64          `json:"disk_total_mb,omitempty"`
	DiskFreeMB  int64          `json:"disk_free_mb,omitempty"`
	Version     string         `json:"version"`
	Backends    []backend.Info `json:"backends,omitempty"`
	Runners     []RunnerReport `json:"runners,omitempty"`
}

// HeartbeatResponse tells the agent whether the controller still recognises it.
type HeartbeatResponse struct {
	OK bool `json:"ok"`
	// Cordoned mirrors the host's cordon flag so the agent can stop asking for
	// work without waiting for the next task poll.
	Cordoned bool `json:"cordoned"`
	// ControllerVersion is echoed for skew detection.
	ControllerVersion string `json:"controller_version"`
	// ResyncRequested asks the agent to send a full runner report next time,
	// which the controller sets after its own restart.
	ResyncRequested bool `json:"resync_requested"`
	// UnknownRunners names the runners this host reported that the controller
	// has no live row for. They are the ones whose workloads may be removed.
	//
	// An agent adopts what it finds running when it starts, so that a restart
	// does not destroy the jobs on its host. That adoption is also what stops
	// it recognising genuine litter -- a workload whose runner the controller
	// deleted while the agent was down -- so the controller answers with the
	// ones it does not know, and only those are reaped. Additive: an older
	// controller sends nothing here, and an agent that receives nothing simply
	// keeps what it adopted, which is the safe direction.
	UnknownRunners []string `json:"unknown_runners,omitempty"`
}

// RunnerReport is the agent's observation of one runner. The controller merges
// it into the runner's authoritative state.
type RunnerReport struct {
	RunnerID string            `json:"runner_id"`
	State    store.RunnerState `json:"state"`
	Handle   backend.Handle    `json:"handle,omitempty"`
	Phase    backend.Phase     `json:"phase,omitempty"`
	ExitCode int               `json:"exit_code,omitempty"`
	Message  string            `json:"message,omitempty"`
	Stats    backend.Stats     `json:"stats,omitempty"`
	// GitHubRunnerID is filled in once the runner has registered.
	GitHubRunnerID int64     `json:"github_runner_id,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
}

// TaskKind names one lifecycle command.
type TaskKind string

const (
	// TaskCreateRunner asks the agent to materialise a runner.
	TaskCreateRunner TaskKind = "create_runner"
	// TaskStopRunner asks the agent to let the runner finish its current job
	// and then exit. This is what a drain becomes on the host.
	TaskStopRunner TaskKind = "stop_runner"
	// TaskRemoveRunner tears the workload down immediately.
	TaskRemoveRunner TaskKind = "remove_runner"
	// TaskStreamLogs opens an outbound log relay for a UI viewer.
	TaskStreamLogs TaskKind = "stream_logs"
	// TaskCancelLogs closes one.
	TaskCancelLogs   TaskKind = "cancel_logs"
	TaskPrewarmImage TaskKind = "prewarm_image"
)

// Task is one unit of work handed to an agent. Tasks are idempotent: the
// controller may redeliver one after a restart, and applying it twice must
// leave the host in the same place.
type Task struct {
	ID   string   `json:"id"`
	Kind TaskKind `json:"kind"`
	// RunnerID is the runner this task concerns.
	RunnerID string `json:"runner_id,omitempty"`
	// Spec is set for TaskCreateRunner and carries the credentials the runner
	// needs. It is the only place a JIT config crosses the wire, which is why
	// the agent transport requires TLS in any non-loopback deployment.
	Spec *backend.Spec `json:"spec,omitempty"`
	// Backend selects which registered backend handles this task.
	Backend    store.BackendKind `json:"backend,omitempty"`
	PoolID     string            `json:"pool_id,omitempty"`
	Image      string            `json:"image,omitempty"`
	PullPolicy store.PullPolicy  `json:"pull_policy,omitempty"`
	// StopTimeout bounds a graceful stop.
	StopTimeout time.Duration `json:"stop_timeout,omitempty"`
	// StreamID identifies a log relay for TaskStreamLogs and TaskCancelLogs.
	StreamID string `json:"stream_id,omitempty"`
	// LogOptions configures a log relay.
	LogOptions *backend.LogOptions `json:"log_options,omitempty"`
	IssuedAt   time.Time           `json:"issued_at"`
}

// TaskResult reports the outcome of a task back to the controller.
type TaskResult struct {
	TaskID string `json:"task_id"`
	// Kind is the kind of the task this answers. The controller uses it to
	// tell a lifecycle task that failed -- which leaves the runner unusable --
	// from a log relay that could not be opened, which leaves it exactly as it
	// was. An agent from before this field is read from the controller's own
	// record of the task instead.
	Kind     TaskKind       `json:"kind,omitempty"`
	RunnerID string         `json:"runner_id,omitempty"`
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Handle   backend.Handle `json:"handle,omitempty"`
	// ImagePullDuration is nil when the backend cannot distinguish pulling
	// from creation. ContainerStartedAt is the end of workload creation.
	ImagePullDuration  *time.Duration `json:"image_pull_duration,omitempty"`
	CreateDuration     time.Duration  `json:"create_duration,omitempty"`
	ContainerStartedAt *time.Time     `json:"container_started_at,omitempty"`
	Digest             string         `json:"digest,omitempty"`
	// State is the runner state the agent believes the runner reached.
	State       store.RunnerState `json:"state,omitempty"`
	CompletedAt time.Time         `json:"completed_at"`
}

// TaskBatch is the response to a task poll.
type TaskBatch struct {
	Tasks []Task `json:"tasks"`
	// Backoff asks the agent to wait before polling again, used when the
	// controller wants to shed load.
	Backoff time.Duration `json:"backoff,omitempty"`
}

// LogChunk is one frame of a relayed log stream.
type LogChunk struct {
	StreamID string `json:"stream_id"`
	Data     string `json:"data"`
	// EOF marks the final frame; Error explains an abnormal end.
	EOF   bool   `json:"eof,omitempty"`
	Error string `json:"error,omitempty"`
}

// Endpoints are the controller paths the agent uses. They live here so that the
// agent client and the controller's router cannot drift apart.
const (
	PathJoin      = "/api/v1/agent/join"
	PathHeartbeat = "/api/v1/agent/heartbeat"
	PathTasks     = "/api/v1/agent/tasks"
	PathResults   = "/api/v1/agent/results"
	PathReport    = "/api/v1/agent/report"
	PathLogs      = "/api/v1/agent/logs"
)

// DefaultPollWait is how long a task poll blocks before returning empty. It is
// short enough to keep proxies from timing the connection out and long enough
// that an idle agent makes very few requests.
const DefaultPollWait = 25 * time.Second

// DefaultStopTimeout is how long a graceful stop waits for a runner to finish
// its current job before the workload is killed.
const DefaultStopTimeout = 5 * time.Minute

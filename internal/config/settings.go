package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// The settings registry: one row per configuration key, and the single answer
// to every question the rest of the product asks about a setting.
//
// There used to be five answers. The struct's yaml tags said what the keys
// were, applyEnv said what their environment variables were, the settings API
// kept a map of the fourteen it would write and a list of the forty-five it
// would refuse, the browser kept a third list of prefixes it would offer an
// editor for, and docs/configuration.md described the lot by hand. Every one
// of them was maintained separately, so every one of them drifted:
// scheduler.drain_timeout, scheduler.default_runner_limits,
// scheduler.host_throttling, server.allow_indexing, github.upload_base_url and
// all five capacity_demand keys existed in the struct and in the environment
// and in neither API list, which made a PATCH for any of them answer "not a
// setting this API knows about" for a setting this API plainly had.
//
// So there is one list now, and it is this one. applyEnv walks it, the database
// layer walks it, the settings API renders and stages from it, the browser
// draws its editors from what the API publishes out of it, and a test in
// internal/docs fails if a key in it has no row in docs/configuration.md. A key
// added to Config without a row here fails TestEverySettingIsRegistered on the
// day it is added rather than after a release.

// Kind classifies a setting's value for the code that has to parse, store and
// render it without knowing which struct field is behind it.
type Kind string

const (
	// KindString is free text: an address, a path, a URL.
	KindString Kind = "string"
	// KindEnum is a string from a fixed set, listed in Choices.
	KindEnum Kind = "enum"
	// KindBool is a plain on/off.
	KindBool Kind = "bool"
	// KindOptionalBool is on, off, or unset -- and unset is a real answer
	// rather than a missing one, because the value is derived from something
	// else. security.cookie_secure is the only one: unset means "follow the
	// external URL and the TLS mode", which is what an operator wants until
	// the day they do not.
	KindOptionalBool Kind = "optional_bool"
	// KindInt is a whole number.
	KindInt Kind = "int"
	// KindDuration is a Go duration, written as an operator writes it: 30s,
	// 5m, 168h. It is carried as that string rather than as a number of
	// nanoseconds, so what an operator typed is what a reader sees.
	KindDuration Kind = "duration"
	// KindStrings is a list, written comma-separated in the environment and as
	// a YAML or JSON list everywhere else.
	KindStrings Kind = "strings"
	// KindLabels is a string map, written key=value,key=value in the
	// environment. agent.labels is the only one.
	KindLabels Kind = "labels"
)

// Scope says where a setting is allowed to live, which is the whole of the
// design that moved configuration into the database.
type Scope string

const (
	// ScopeInstance belongs to the fleet, so it lives in the fleet's database
	// and an administrator changes it in the UI. This is almost everything.
	ScopeInstance Scope = "instance"

	// ScopeBootstrap is read before the database can be opened, so it cannot
	// live inside it. There are exactly three: the path to the database, and
	// the two ways of naming the key that unseals what is in it. A setting
	// that unlocks a store cannot be stored in that store, and pretending
	// otherwise is how a product ends up with a configuration file it swears
	// it does not have.
	ScopeBootstrap Scope = "bootstrap"

	// ScopeLocal means something only to a standalone `zoomies agent`, which
	// is a process on somebody else's host with no database of its own. Its
	// controller URL, its enrolment token and its client certificates are
	// facts about that host, and the controller has no business holding them.
	// They are shown in the UI so the picture is complete, and they are not
	// editable there, because the machine that would have to act on the change
	// is not this one.
	ScopeLocal Scope = "local"
)

// Setting is one configuration key: what it is, where it may live, and whether
// a change to it is in force before the operator has finished reading the
// confirmation.
type Setting struct {
	// Key is the dotted path, spelled as it is in zoomies.yaml.
	Key string `json:"key"`
	// Label is the setting's name in prose, for a person reading a form.
	// "agent.docker_build_cache_mb" is what it is called in a file and in a
	// bug report; "Docker build cache target" is what it is called out loud,
	// and a settings page that only offers the first makes an operator
	// translate eighty-eight of them in their head.
	Label string `json:"label"`
	// Env is the ZOOMIES_* variable that overrides it.
	Env string `json:"env"`
	// Kind says how to parse and render the value.
	Kind Kind `json:"kind"`
	// Choices lists the permitted values of a KindEnum.
	Choices []string `json:"choices,omitempty"`
	// Scope says whether this key can live in the database.
	Scope Scope `json:"scope"`
	// Secret marks a credential. Its value never leaves the process: the API
	// reports whether one is set, never what it is, and the row in the
	// database is sealed.
	Secret bool `json:"secret"`
	// Live says the running controller picks this up on its next pass, so a
	// change is in force by the time the response is written. False means the
	// change is stored and applies at the next restart -- which is a promise
	// worth keeping carefully, because a settings page that says "saved" about
	// a value nothing is using is worse than one that refuses the edit.
	Live bool `json:"live"`
	// Summary is one line saying what the setting does, shown beside the field
	// in the UI and quoted back in the refusal when a value will not parse.
	Summary string `json:"summary"`
	// RestartReason says why a change waits for a restart, for the keys where
	// the reason is not obvious. Empty for a Live setting.
	RestartReason string `json:"restart_reason,omitempty"`
	// Floor is the smallest useful positive value of a duration. Zero is
	// always still allowed, because switching a timer off is a real answer;
	// what the floor refuses is a value that is technically a duration and
	// practically an outage. A poll interval of one millisecond is a denial of
	// service against GitHub, not a configuration choice.
	Floor time.Duration `json:"floor_ms,omitempty"`
}

// Stored reports whether this setting's value belongs in the database.
func (s Setting) Stored() bool { return s.Scope == ScopeInstance }

// Section is the part before the first dot: the group the UI draws a heading
// for.
func (s Setting) Section() string {
	head, _, _ := strings.Cut(s.Key, ".")
	return head
}

// registry is every setting, keyed by its dotted path.
//
// The summaries are written for the operator reading the settings page, not
// for whoever maintains this file: they say what the setting does and what
// turning it off costs, because that is the sentence somebody needs at the
// moment they are about to change it.
var registry = buildRegistry([]Setting{
	// ---------------------------------------------------------------------
	// server -- the listener, and how the world reaches it.
	// ---------------------------------------------------------------------
	{
		Key: "server.bind", Label: "Listen address", Env: "ZOOMIES_BIND", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The address the controller listens on. 127.0.0.1:8080 is this machine only; 0.0.0.0:8080 is every interface.",
		RestartReason: "the listener is already bound, and rebinding it under live connections is how a reload becomes an outage",
	},
	{
		Key: "server.external_url", Label: "External URL", Env: "ZOOMIES_EXTERNAL_URL", Kind: KindString, Scope: ScopeInstance,
		Summary:       "How GitHub and browsers reach this controller. It forms the webhook URL, so webhooks need it.",
		RestartReason: "the shell's sharing metadata and the single sign-on redirect are built from it at startup",
	},
	{
		Key: "server.tls.mode", Label: "TLS mode", Env: "ZOOMIES_TLS_MODE", Kind: KindEnum, Scope: ScopeInstance,
		Choices:       []string{string(TLSOff), string(TLSSelfSigned), string(TLSFiles)},
		Summary:       "How the listener terminates TLS: off behind a reverse proxy, self-signed for a generated certificate, files for one of your own.",
		RestartReason: "the certificate is handed to the listener when it is created",
	},
	{
		Key: "server.tls.cert_file", Label: "Certificate file", Env: "ZOOMIES_TLS_CERT_FILE", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The certificate the listener serves, when the mode is files.",
		RestartReason: "the certificate is handed to the listener when it is created",
	},
	{
		Key: "server.tls.key_file", Label: "Private key file", Env: "ZOOMIES_TLS_KEY_FILE", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The private key for that certificate. The file stays on disk; only its path is stored here.",
		RestartReason: "the certificate is handed to the listener when it is created",
	},
	{
		Key: "server.tls.hosts", Label: "Certificate host names", Env: "ZOOMIES_TLS_HOSTS", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "The names baked into a generated self-signed certificate.",
		RestartReason: "the certificate is generated once, at startup",
	},
	{
		Key: "server.trusted_proxies", Label: "Trusted proxies", Env: "ZOOMIES_TRUSTED_PROXIES", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "CIDRs whose X-Forwarded-For header is believed, or the word cloudflare for Cloudflare's published ranges. Empty takes client addresses from the socket, which is the safe answer.",
		RestartReason: "the ranges are parsed once and consulted on every request",
	},
	{
		Key: "server.allowed_origins", Label: "Allowed browser origins", Env: "ZOOMIES_ALLOWED_ORIGINS", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "Extra browser origins allowed to make state-changing requests. Empty means same-origin only, which is what the built-in UI needs.",
		RestartReason: "the list is compiled into the request middleware at startup",
	},
	{
		Key: "server.allow_indexing", Label: "Allow search engine indexing", Env: "ZOOMIES_ALLOW_INDEXING", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Invite search engines into the UI. Off by default: a controller is somebody's infrastructure rather than somebody's website.",
		RestartReason: "robots.txt is rendered once, with the rest of the shell",
	},
	{
		Key: "server.tailcat_enabled", Label: "Private agent network", Env: "ZOOMIES_TAILCAT_ENABLED", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Permit private agent connections, started on first enrolment.",
		RestartReason: "the private network is joined at startup",
	},
	{
		Key: "server.read_timeout", Label: "Read timeout", Env: "ZOOMIES_READ_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a client may take to send its request.",
		RestartReason: "it is a field of the HTTP server, set when that server is built",
	},
	{
		Key: "server.write_timeout", Label: "Write timeout", Env: "ZOOMIES_WRITE_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a response may take. It is 0, and should stay 0: the event stream and a followed log are responses that never end.",
		RestartReason: "it is a field of the HTTP server, set when that server is built",
	},
	{
		Key: "server.idle_timeout", Label: "Idle timeout", Env: "ZOOMIES_IDLE_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long an idle keep-alive connection is held open.",
		RestartReason: "it is a field of the HTTP server, set when that server is built",
	},

	// ---------------------------------------------------------------------
	// database and security -- the three keys that cannot live in the
	// database, and the ones that guard the door.
	// ---------------------------------------------------------------------
	{
		Key: "database.path", Label: "Database file", Env: "ZOOMIES_DB_PATH", Kind: KindString, Scope: ScopeBootstrap,
		Summary:       "The SQLite file holding this fleet, including every setting below. It is named in the configuration file or the environment because nothing can read it from inside itself.",
		RestartReason: "the database is open",
	},
	{
		Key: "security.encryption_key", Label: "Encryption key", Env: "ZOOMIES_ENCRYPTION_KEY", Kind: KindString, Scope: ScopeBootstrap, Secret: true,
		Summary:       "The 32-byte key, base64 or hex, that seals GitHub App private keys, webhook secrets and the stored credentials below. Prefer the key file or the environment variable: a key written into zoomies.yaml is a key in your configuration management system.",
		RestartReason: "everything sealed in the database was sealed with the key this process started with",
	},
	{
		Key: "security.encryption_key_file", Label: "Encryption key file", Env: "ZOOMIES_ENCRYPTION_KEY_FILE", Kind: KindString, Scope: ScopeBootstrap,
		Summary:       "Where that key is read from, and written to on a first run. Back it up beside the database: without it the sealed rows cannot be read.",
		RestartReason: "the key is read once, before anything that needs it",
	},
	{
		Key: "security.session_ttl", Label: "Session lifetime", Env: "ZOOMIES_SESSION_TTL", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a browser login lasts before it has to be made again.",
		RestartReason: "the authentication service takes its security settings when it is built",
	},
	{
		Key: "security.cookie_secure", Label: "Secure session cookies", Env: "ZOOMIES_COOKIE_SECURE", Kind: KindOptionalBool, Scope: ScopeInstance,
		Summary:       "Force the Secure attribute on session cookies. Unset derives it from the external URL and the TLS mode, which is right unless a proxy in front makes it wrong.",
		RestartReason: "the authentication service takes its security settings when it is built",
	},
	{
		Key: "security.disable_auth", Label: "Disable authentication", Env: "ZOOMIES_DISABLE_AUTH", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Remove all authentication. It exists for local development, and it is refused wherever this controller looks reachable.",
		RestartReason: "the authentication service takes its security settings when it is built",
	},
	{
		Key: "security.rate_limit_logins", Label: "Login attempts per minute", Env: "ZOOMIES_RATE_LIMIT_LOGINS", Kind: KindInt, Scope: ScopeInstance,
		Summary:       "Password attempts allowed per source address per minute, and five times that per account.",
		RestartReason: "the limiters are built with their limit when the authentication service is",
	},

	// ---------------------------------------------------------------------
	// github
	// ---------------------------------------------------------------------
	{
		Key: "github.api_base_url", Label: "GitHub API base URL", Env: "ZOOMIES_GITHUB_API_BASE_URL", Kind: KindString, Scope: ScopeInstance, Live: true,
		Summary: "https://api.github.com for github.com, or your Enterprise Server's /api/v3. It is the default for a new installation; each existing one keeps the base it was added with.",
	},
	{
		Key: "github.upload_base_url", Label: "GitHub upload base URL", Env: "ZOOMIES_GITHUB_UPLOAD_BASE_URL", Kind: KindString, Scope: ScopeInstance, Live: true,
		Summary: "The upload endpoint, when your Enterprise Server puts it somewhere other than beside the API.",
	},
	{
		Key: "github.webhook_path", Label: "Webhook path", Env: "ZOOMIES_WEBHOOK_PATH", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The path GitHub posts deliveries to. Changing it means changing the App's webhook URL too.",
		RestartReason: "the route is mounted once, when the router is built",
	},
	{
		Key: "github.poll_interval", Label: "Poll interval", Env: "ZOOMIES_POLL_INTERVAL", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Floor:   time.Second,
		Summary: "How often the fallback poller looks for queued jobs.",
	},
	{
		Key: "github.poll_fallback", Label: "Poll for queued jobs", Env: "ZOOMIES_POLL_FALLBACK", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "List queued jobs on a timer as well as waiting for webhooks. On by default: a controller that silently stops scaling because a webhook was misconfigured is worse than a few extra API calls.",
		RestartReason: "the poll loop stops when it is turned off and is started again at startup",
	},
	{
		Key: "github.allow_workflow_cancellation", Label: "Allow cancelling workflow runs", Env: "ZOOMIES_ALLOW_WORKFLOW_CANCELLATION", Kind: KindBool, Scope: ScopeInstance, Live: true,
		Summary: "Let operators ask GitHub to cancel the workflow run that owns a job. Turning it off is what a read-only Actions grant wants.",
	},
	{
		Key: "github.runner_image", Label: "Default runner image", Env: "ZOOMIES_RUNNER_IMAGE", Kind: KindString, Scope: ScopeInstance, Live: true,
		Summary: "The container image a new pool runs when it names neither an image nor an operating system.",
	},
	{
		Key: "github.runner_version", Label: "Pinned runner release", Env: "ZOOMIES_RUNNER_VERSION", Kind: KindString, Scope: ScopeInstance, Live: true,
		Summary: "Pin the actions/runner release. Empty tracks whatever the image carries.",
	},

	// ---------------------------------------------------------------------
	// agent -- the runner-executing half.
	// ---------------------------------------------------------------------
	{
		Key: "agent.embedded", Label: "Run an agent in this controller", Env: "ZOOMIES_AGENT_EMBEDDED", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Run an agent inside this controller, so a single machine needs one process. Off makes a controller that schedules runners onto other hosts and starts none itself.",
		RestartReason: "the backends and the agent are built at startup, and runners are already running against them",
	},
	{
		Key: "agent.name", Label: "Host name", Env: "ZOOMIES_AGENT_NAME", Kind: KindString, Scope: ScopeInstance,
		Summary:       "What this host is called in the fleet. Empty names it after the machine it is on.",
		RestartReason: "the host row is claimed under this name when the agent enrols",
	},
	{
		Key: "agent.capacity", Label: "Runners per host", Env: "ZOOMIES_AGENT_CAPACITY", Kind: KindInt, Scope: ScopeInstance,
		Summary:       "How many runners this host will hold at once. It defaults to one per two cores, which leaves the machine room to breathe.",
		RestartReason: "the agent reports its capacity when it enrols",
	},
	{
		Key: "agent.backend", Label: "Runner backend", Env: "ZOOMIES_AGENT_BACKEND", Kind: KindEnum, Scope: ScopeInstance,
		Choices:       []string{"docker", "podman", "process"},
		Summary:       "What a runner runs in: a Docker container, a Podman container, or a bare process on this host.",
		RestartReason: "the backend is built at startup, and runners are already running against it",
	},
	{
		Key: "agent.docker_host", Label: "Docker socket", Env: "ZOOMIES_DOCKER_HOST", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The Docker or Podman socket. Empty finds one, preferring a rootless socket over the root one.",
		RestartReason: "the backend is built at startup, and runners are already running against it",
	},
	{
		Key: "agent.work_dir", Label: "Working directory", Env: "ZOOMIES_WORK_DIR", Kind: KindString, Scope: ScopeInstance,
		Summary:       "Where runner working directories and the agent's own credentials live.",
		RestartReason: "the agent's credentials are read from it at startup",
	},
	{
		Key: "agent.labels", Label: "Host labels", Env: "ZOOMIES_AGENT_LABELS", Kind: KindLabels, Scope: ScopeInstance,
		Summary:       "Key=value labels describing this host, which a pool can require of the hosts it runs on.",
		RestartReason: "the labels are reported when the agent enrols",
	},
	{
		Key: "agent.network", Label: "Container network", Env: "ZOOMIES_AGENT_NETWORK", Kind: KindString, Scope: ScopeInstance,
		Summary:       "An existing container network to attach runners to. Empty uses the daemon's default bridge.",
		RestartReason: "the backend is built with it, and runners are already attached",
	},
	{
		Key: "agent.heartbeat_interval", Label: "Heartbeat interval", Env: "ZOOMIES_HEARTBEAT_INTERVAL", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How often an agent reports in. A host that goes quiet for 90 seconds is counted lost, so this has to be comfortably under that.",
		RestartReason: "the agent's heartbeat timer is set when it starts",
	},
	{
		Key: "agent.finished_retention", Label: "Keep finished containers for", Env: "ZOOMIES_AGENT_FINISHED_RETENTION", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a finished runner's container stays on the host before the agent deletes it. It is the window for reading a finished runner's log, and it is host disk: 0 deletes on the next pass.",
		RestartReason: "the agent is told its retention when it starts",
	},
	{
		Key: "agent.docker_build_cache_mb", Label: "Docker build cache target", Env: "ZOOMIES_AGENT_DOCKER_BUILD_CACHE_MB", Kind: KindInt, Scope: ScopeInstance,
		Summary:       "The target size for unused Docker builder cache. 0 leaves a shared or externally managed daemon alone.",
		RestartReason: "the agent is told its cache target when it starts",
	},
	{
		Key: "agent.registry_auth", Label: "Registry credentials", Env: "ZOOMIES_REGISTRY_AUTH", Kind: KindString, Scope: ScopeInstance, Secret: true,
		Summary:       "A base64 X-Registry-Auth value the container backends send when they pull. Without it a pool on a private registry cannot use pinned-only pulls at all.",
		RestartReason: "the backend is built with it",
	},
	{
		Key: "agent.runner_sha256", Label: "Runner archive digest", Env: "ZOOMIES_AGENT_RUNNER_SHA256", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The expected digest of the actions/runner archive the process backend downloads. Zoomies ships the digest for the release it pins; supply one when you pin another.",
		RestartReason: "the process backend is built with it",
	},
	{
		Key: "agent.allow_unverified_runner_download", Label: "Allow unverified runner downloads", Env: "ZOOMIES_AGENT_ALLOW_UNVERIFIED_RUNNER_DOWNLOAD", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Let the process backend install a runner archive whose digest it cannot check. The alternative to checking is executing whatever the network handed over.",
		RestartReason: "the process backend is built with it",
	},
	{
		Key: "agent.runner_download_url", Label: "Runner download mirror", Env: "ZOOMIES_AGENT_RUNNER_DOWNLOAD_URL", Kind: KindString, Scope: ScopeInstance,
		Summary:       "Where the process backend fetches runner archives from, for hosts that mirror releases internally. The path below it is the same.",
		RestartReason: "the process backend is built with it",
	},
	// The transport a standalone agent uses to reach a controller. None of it
	// can come from the controller's database, because a host that cannot yet
	// reach the controller is exactly the host that needs these values.
	{
		Key: "agent.controller_url", Label: "Controller URL", Env: "ZOOMIES_CONTROLLER_URL", Kind: KindString, Scope: ScopeLocal,
		Summary: "The controller a standalone agent connects to. It is configured on that agent's own host.",
	},
	{
		Key: "agent.join_token", Label: "Join token", Env: "ZOOMIES_JOIN_TOKEN", Kind: KindString, Scope: ScopeLocal, Secret: true,
		Summary: "The single-use token a standalone agent redeems to enrol. It is configured on that agent's own host.",
	},
	{
		Key: "agent.agent_token", Label: "Agent token", Env: "ZOOMIES_AGENT_TOKEN", Kind: KindString, Scope: ScopeLocal, Secret: true,
		Summary: "The credential a standalone agent carries afterwards. It is configured on that agent's own host.",
	},
	{
		Key: "agent.ca_file", Label: "Controller certificate", Env: "ZOOMIES_AGENT_CA_FILE", Kind: KindString, Scope: ScopeLocal,
		Summary: "The certificate a standalone agent pins for its controller. It is configured on that agent's own host.",
	},
	{
		Key: "agent.client_cert_file", Label: "Client certificate", Env: "ZOOMIES_AGENT_CLIENT_CERT_FILE", Kind: KindString, Scope: ScopeLocal,
		Summary: "A standalone agent's client certificate, for mutual TLS. It is configured on that agent's own host.",
	},
	{
		Key: "agent.client_key_file", Label: "Client private key", Env: "ZOOMIES_AGENT_CLIENT_KEY_FILE", Kind: KindString, Scope: ScopeLocal,
		Summary: "The key for that client certificate. It is configured on that agent's own host.",
	},
	{
		Key: "agent.insecure_skip_verify", Label: "Skip certificate verification", Env: "ZOOMIES_AGENT_INSECURE_SKIP_VERIFY", Kind: KindBool, Scope: ScopeLocal,
		Summary: "Let a standalone agent skip verifying its controller's certificate. It is configured on that agent's own host.",
	},
	{
		Key: "agent.allow_insecure_http", Label: "Allow plain HTTP to the controller", Env: "ZOOMIES_AGENT_ALLOW_INSECURE_HTTP", Kind: KindBool, Scope: ScopeLocal,
		Summary: "Let a standalone agent use a plain http:// controller URL off loopback, which puts its token and every runner credential on the wire in the clear. It is configured on that agent's own host.",
	},

	// ---------------------------------------------------------------------
	// runners -- what every runner is started with. The controller writes
	// these into each create task, so a change is in the next runner's
	// environment without anything restarting.
	// ---------------------------------------------------------------------
	{
		Key: "runners.docker_wait", Label: "Docker daemon wait", Env: "ZOOMIES_DOCKER_WAIT", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Floor:   time.Second,
		Summary: "How long a runner on a pool that provides Docker waits for that daemon before refusing to take a job. Whole seconds, up to an hour; 0 leaves the runner image's own default. A pool's env can set ZOOMIES_DOCKER_WAIT to override it for that pool.",
	},
	{
		Key: "runners.env", Label: "Runner environment", Env: "ZOOMIES_RUNNER_ENV", Kind: KindLabels, Scope: ScopeInstance, Live: true,
		Summary: "Key=value variables every runner starts with, such as a proxy or a package mirror. A pool's own env wins where the two name the same variable. Every job can read these, so a credential does not belong here: give it to the pool, or to the workflow as a GitHub secret.",
	},

	// ---------------------------------------------------------------------
	// scheduler -- every one of these is read fresh on each pass.
	// ---------------------------------------------------------------------
	{
		Key: "scheduler.interval", Label: "Scheduler interval", Env: "ZOOMIES_SCHEDULER_INTERVAL", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Floor:   time.Second,
		Summary: "How often the scheduler runs a pass even with nothing to react to.",
	},
	{
		Key: "scheduler.scale_up_delay", Label: "Scale-up delay", Env: "ZOOMIES_SCALE_UP_DELAY", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long a job must have been queued before it counts as demand. It damps churn when jobs arrive in bursts; 0 reacts at once.",
	},
	{
		Key: "scheduler.max_runner_lifetime", Label: "Maximum runner lifetime", Env: "ZOOMIES_MAX_RUNNER_LIFETIME", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "Drain a runner that has lived this long, next time it is not busy. It bounds how long a runner's credentials live; it never ends a job.",
	},
	{
		Key: "scheduler.provision_timeout", Label: "Provision timeout", Env: "ZOOMIES_PROVISION_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "Fail a runner that never finishes registering, so a bad image does not hold a host slot for ever.",
	},
	{
		Key: "scheduler.drain_timeout", Label: "Drain timeout", Env: "ZOOMIES_DRAIN_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "Fail a runner that has been draining this long with no job left on it. A runner still finishing a job is never touched by it.",
	},
	{
		Key: "scheduler.max_creates_per_tick", Label: "Runners created per pass", Env: "ZOOMIES_MAX_CREATES_PER_TICK", Kind: KindInt, Scope: ScopeInstance, Live: true,
		Summary: "How many runners may be created in one pass, so a thundering herd of queued jobs cannot exhaust a host in one go.",
	},
	{
		Key: "scheduler.default_runner_limits", Label: "Default runner limits", Env: "ZOOMIES_DEFAULT_RUNNER_LIMITS", Kind: KindBool, Scope: ScopeInstance, Live: true,
		Summary: "Give a runner whose pool sets no CPU or memory limit one slot's share of its host as a real limit. Off, a host's worth of them can each take every core.",
	},
	{
		Key: "scheduler.host_throttling", Label: "Throttle hosts under pressure", Env: "ZOOMIES_HOST_THROTTLING", Kind: KindBool, Scope: ScopeInstance, Live: true,
		Summary: "Let the controller throttle a host its measurements say is overwhelmed, and step it back up after a stretch of calm.",
	},

	// ---------------------------------------------------------------------
	// log
	// ---------------------------------------------------------------------
	{
		Key: "log.level", Label: "Log level", Env: "ZOOMIES_LOG_LEVEL", Kind: KindEnum, Scope: ScopeInstance, Live: true,
		Choices: []string{"debug", "info", "warn", "error"},
		Summary: "How much detail the controller logs.",
	},
	{
		Key: "log.format", Label: "Log format", Env: "ZOOMIES_LOG_FORMAT", Kind: KindEnum, Scope: ScopeInstance,
		Choices:       []string{"json", "text"},
		Summary:       "json for a log collector, text for a person reading a terminal.",
		RestartReason: "the log handler is built before anything else, including the database this setting is read from",
	},

	// ---------------------------------------------------------------------
	// oidc -- single sign-on.
	// ---------------------------------------------------------------------
	{
		Key: "oidc.enabled", Label: "Single sign-on", Env: "ZOOMIES_OIDC_ENABLED", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Offer single sign-on as well as local accounts.",
		RestartReason: "the provider is discovered at startup, and discovery is a network call that must not happen inside a settings request",
	},
	{
		Key: "oidc.issuer", Label: "Issuer URL", Env: "ZOOMIES_OIDC_ISSUER", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The identity provider's issuer URL, from which its endpoints are discovered.",
		RestartReason: "the provider is discovered at startup",
	},
	{
		Key: "oidc.client_id", Label: "Client ID", Env: "ZOOMIES_OIDC_CLIENT_ID", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The client this controller identifies itself as.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.client_secret", Label: "Client secret", Env: "ZOOMIES_OIDC_CLIENT_SECRET", Kind: KindString, Scope: ScopeInstance, Secret: true,
		Summary:       "The client secret that goes with it.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.redirect_url", Label: "Redirect URL", Env: "ZOOMIES_OIDC_REDIRECT_URL", Kind: KindString, Scope: ScopeInstance,
		Summary:       "Where the provider sends the browser back to. Empty derives it from the external URL.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.scopes", Label: "Scopes", Env: "ZOOMIES_OIDC_SCOPES", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "The scopes asked for at sign-in.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.username_claim", Label: "Username claim", Env: "ZOOMIES_OIDC_USERNAME_CLAIM", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The token claim that becomes a Zoomies username.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.groups_claim", Label: "Groups claim", Env: "ZOOMIES_OIDC_GROUPS_CLAIM", Kind: KindString, Scope: ScopeInstance,
		Summary:       "The token claim listing the groups a user is in.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.admin_groups", Label: "Administrator groups", Env: "ZOOMIES_OIDC_ADMIN_GROUPS", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "Provider groups whose members get the administrator role.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.operator_groups", Label: "Operator groups", Env: "ZOOMIES_OIDC_OPERATOR_GROUPS", Kind: KindStrings, Scope: ScopeInstance,
		Summary:       "Provider groups whose members get the operator role. A user in no mapped group is a viewer.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.allow_signup", Label: "Create accounts on first sign-in", Env: "ZOOMIES_OIDC_ALLOW_SIGNUP", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Create an account on a first successful single sign-on, rather than refusing anyone not already here.",
		RestartReason: "the provider is configured at startup",
	},
	{
		Key: "oidc.link_by_username", Label: "Link sign-on to local accounts", Env: "ZOOMIES_OIDC_LINK_BY_USERNAME", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Let a first single sign-on take over an existing local account with the same username. Turn it on for the one migration where that is the intention, then turn it off again.",
		RestartReason: "the provider is configured at startup",
	},

	// ---------------------------------------------------------------------
	// metrics
	// ---------------------------------------------------------------------
	{
		Key: "metrics.enabled", Label: "Prometheus endpoint", Env: "ZOOMIES_METRICS_ENABLED", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Serve the Prometheus endpoint.",
		RestartReason: "the route is mounted once, when the router is built",
	},
	{
		Key: "metrics.path", Label: "Metrics path", Env: "ZOOMIES_METRICS_PATH", Kind: KindString, Scope: ScopeInstance,
		Summary:       "Where it is served.",
		RestartReason: "the route is mounted once, when the router is built",
	},
	{
		Key: "metrics.public", Label: "Serve metrics without authentication", Env: "ZOOMIES_METRICS_PUBLIC", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Serve it without authentication. Off by default, because job and repository names are visible in the label set.",
		RestartReason: "the authentication around the route is decided when the router is built",
	},

	// ---------------------------------------------------------------------
	// retention -- audit rows are deliberately absent; they are never pruned.
	// ---------------------------------------------------------------------
	{
		Key: "retention.jobs", Label: "Keep job history for", Env: "ZOOMIES_RETENTION_JOBS", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long job history is kept.",
	},
	{
		Key: "retention.runners", Label: "Keep finished runners for", Env: "ZOOMIES_RETENTION_RUNNERS", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long finished runners are kept.",
	},
	{
		Key: "retention.scaling_events", Label: "Keep scaling history for", Env: "ZOOMIES_RETENTION_SCALING_EVENTS", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long scaling decisions are kept. Audit rows are not covered by this, or by anything: they are never deleted.",
	},
	{
		Key: "retention.samples", Label: "Keep Overview samples for", Env: "ZOOMIES_RETENTION_SAMPLES", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long the Overview's samples are kept.",
	},
	{
		Key: "retention.webhooks", Label: "Keep webhook deliveries for", Env: "ZOOMIES_RETENTION_WEBHOOKS", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long webhook deliveries are kept.",
	},
	{
		Key: "retention.machines", Label: "Keep deleted machines for", Env: "ZOOMIES_RETENTION_MACHINES", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long a deleted machine's row is kept, so what the fleet rented and gave back is still answerable after the machine itself is gone.",
	},

	// ---------------------------------------------------------------------
	// images and updates
	// ---------------------------------------------------------------------
	{
		Key: "images.refresh_interval", Label: "Image refresh interval", Env: "ZOOMIES_IMAGE_REFRESH_INTERVAL", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How often every pool's image is prewarmed again, so a moving tag reaches the hosts. 0 switches it off, which is what an air-gapped fleet wants.",
	},
	{
		Key: "updates.check_interval", Label: "Update check interval", Env: "ZOOMIES_UPDATE_CHECK_INTERVAL", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How often github.com is asked which release of Zoomies is current. 0 never asks, and is the one request that is not about your fleet. Nothing is ever downloaded by it.",
	},

	// ---------------------------------------------------------------------
	// capacity_demand -- publishing a request for more hosts to a provisioner.
	// ---------------------------------------------------------------------
	{
		Key: "capacity_demand.destination_url", Label: "Destination URL", Env: "ZOOMIES_CAPACITY_DEMAND_URL", Kind: KindString, Scope: ScopeInstance, Live: true,
		Summary: "Where signed requests for host capacity are posted. Empty disables the integration.",
	},
	{
		Key: "capacity_demand.signing_secret", Label: "Signing secret", Env: "ZOOMIES_CAPACITY_DEMAND_SIGNING_SECRET", Kind: KindString, Scope: ScopeInstance, Secret: true, Live: true,
		Summary: "The secret those requests are signed with. Anyone holding it can forge one, so it is stored sealed.",
	},
	{
		Key: "capacity_demand.cooldown", Label: "Cooldown", Env: "ZOOMIES_CAPACITY_DEMAND_COOLDOWN", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long to wait before asking for capacity for the same pool again.",
	},
	{
		Key: "capacity_demand.timeout", Label: "Request timeout", Env: "ZOOMIES_CAPACITY_DEMAND_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long one of those requests may take.",
	},
	{
		Key: "capacity_demand.pools", Label: "Pools to publish for", Env: "ZOOMIES_CAPACITY_DEMAND_POOLS", Kind: KindStrings, Scope: ScopeInstance, Live: true,
		Summary: "Which pools to publish demand for. Empty publishes for all of them.",
	},

	// ---------------------------------------------------------------------
	// provider -- renting machines from a hypervisor.
	//
	// Only the kill switch and the two ceilings are live. The deadlines bound
	// operations that may already be in flight, so changing one under a clone
	// that is half-built would mean two passes disagreeing about when to give
	// up on it; they are stored and applied at the next start instead.
	// ---------------------------------------------------------------------
	{
		Key: "provider.enabled", Label: "Rent machines", Env: "ZOOMIES_PROVIDER_ENABLED", Kind: KindBool, Scope: ScopeInstance,
		Summary:       "Whether the machine loop runs at all. Off by default: renting a machine spends money, and nothing here should start doing that because a release added the ability to.",
		RestartReason: "the machine loop is started at startup, so turning it on is a thing a running process cannot do to itself",
	},
	{
		Key: "provider.paused", Label: "Pause new machines", Env: "ZOOMIES_PROVIDER_PAUSED", Kind: KindBool, Scope: ScopeInstance, Live: true,
		Summary: "Stop creating machines while leaving draining, deleting, recovering and verifying ownership running. A switch that stopped those too would strand running machines nobody is watching.",
	},
	{
		Key: "provider.interval", Label: "Machine loop interval", Env: "ZOOMIES_PROVIDER_INTERVAL", Kind: KindDuration, Scope: ScopeInstance,
		Floor:         time.Second,
		Summary:       "How often the machine loop runs. It is slower than the scheduler's on purpose: a clone takes minutes, and the pass that watches one gains nothing from a ten-second tick.",
		RestartReason: "the loop's timer is set when it starts",
	},
	{
		Key: "provider.sweep_interval", Label: "Ownership sweep interval", Env: "ZOOMIES_PROVIDER_SWEEP_INTERVAL", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How often each provider is asked for everything it believes it is running, which is how an orphaned machine and one that vanished underneath us are both found.",
		RestartReason: "the sweep is paced from the loop's own clock, set when it starts",
	},
	{
		Key: "provider.max_machines", Label: "Machines the fleet may rent", Env: "ZOOMIES_PROVIDER_MAX_MACHINES", Kind: KindInt, Scope: ScopeInstance, Live: true,
		Summary: "The ceiling across every provider. Zero rents nothing, exactly as a pool's max_runners of zero runs nothing: a maximum of none is none.",
	},
	{
		Key: "provider.max_creates_in_flight", Label: "Machines built at once", Env: "ZOOMIES_PROVIDER_MAX_CREATES_IN_FLIGHT", Kind: KindInt, Scope: ScopeInstance, Live: true,
		Summary: "How many machines may be being built at once across the fleet, so a burst of queued jobs cannot ask a hypervisor for fifty clones in one pass.",
	},
	{
		Key: "provider.scale_up_delay", Label: "Delay before renting", Env: "ZOOMIES_PROVIDER_SCALE_UP_DELAY", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a pool's demand must stand before a machine is bought for it. Zero, unlike the scheduler's: a machine that takes four minutes to arrive has already spent the delay by being slow.",
		RestartReason: "the loop reads its pacing when it starts",
	},
	{
		Key: "provider.call_timeout", Label: "Provider request timeout", Env: "ZOOMIES_PROVIDER_CALL_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long one API request to a provider may take.",
		RestartReason: "requests already in flight were given the old deadline",
	},
	{
		Key: "provider.create_timeout", Label: "Machine creation timeout", Env: "ZOOMIES_PROVIDER_CREATE_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long the whole asynchronous creation of a machine may take, rather than the request that starts it.",
		RestartReason: "a machine already being built was given the old deadline, and two passes disagreeing about when to give up on it is how one gets abandoned half-made",
	},
	{
		Key: "provider.bootstrap_timeout", Label: "Agent install timeout", Env: "ZOOMIES_PROVIDER_BOOTSTRAP_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long installing the agent inside a machine that is already up may take.",
		RestartReason: "a bootstrap already running was given the old deadline",
	},
	{
		Key: "provider.enrol_timeout", Label: "Enrolment timeout", Env: "ZOOMIES_PROVIDER_ENROL_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a bootstrapped machine has to appear as a host. It has to outlast a heartbeat timeout, or a machine that joined and went briefly quiet would be given up on.",
		RestartReason: "a machine already enrolling was given the old deadline",
	},
	{
		Key: "provider.delete_timeout", Label: "Deletion timeout", Env: "ZOOMIES_PROVIDER_DELETE_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long an asynchronous deletion may take.",
		RestartReason: "a deletion already running was given the old deadline",
	},
	{
		Key: "provider.ambiguity_timeout", Label: "Unknown-outcome timeout", Env: "ZOOMIES_PROVIDER_AMBIGUITY_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long an operation whose outcome is unknown is reconciled by looking before a person is asked instead. It must outlast the creation timeout: a create that is merely slow is not an unknown outcome.",
		RestartReason: "an operation already in doubt was given the old deadline",
	},
	{
		Key: "provider.idle_timeout", Label: "Idle before draining", Env: "ZOOMIES_PROVIDER_IDLE_TIMEOUT", Kind: KindDuration, Scope: ScopeInstance, Live: true,
		Summary: "How long a machine's host must have had no runner on it before the machine is drained.",
	},
	{
		Key: "provider.scale_down_cooldown", Label: "Cooldown before deleting", Env: "ZOOMIES_PROVIDER_SCALE_DOWN_COOLDOWN", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long that idleness must hold continuously before anything is deleted, so a quiet minute between two bursts does not destroy the machines the second burst is about to want.",
		RestartReason: "a cooldown already being counted was started against the old value",
	},
	{
		Key: "provider.delete_grace", Label: "Grace before a silent machine is lost", Env: "ZOOMIES_PROVIDER_DELETE_GRACE", Kind: KindDuration, Scope: ScopeInstance,
		Summary:       "How long a machine whose host has gone silent is left alone before it is treated as lost. It has to outlast the controller's own judgement that a host is gone, or a network blip would destroy a machine in the middle of a job.",
		RestartReason: "a grace period already being counted was started against the old value",
	},

	// ---------------------------------------------------------------------
	// ui -- what the web UI opens with. Each is a starting point an operator
	// moves away from on the page itself, which then remembers the move in
	// that browser; what is set here is what somebody who has never chosen
	// sees.
	// ---------------------------------------------------------------------
	{
		Key: "ui.capacity_map.overview_layout", Label: "Overview capacity map layout", Env: "ZOOMIES_UI_CAPACITY_MAP_OVERVIEW_LAYOUT", Kind: KindEnum, Scope: ScopeInstance, Live: true,
		Choices: CapacityLayouts,
		Summary: "How the host capacity map on the Overview opens: overlay draws every host on one chart, split draws a chart for each. An operator who picks the other one on the page keeps their pick in that browser.",
	},
	{
		Key: "ui.capacity_map.hosts_layout", Label: "Hosts capacity map layout", Env: "ZOOMIES_UI_CAPACITY_MAP_HOSTS_LAYOUT", Kind: KindEnum, Scope: ScopeInstance, Live: true,
		Choices: CapacityLayouts,
		Summary: "The same choice for the map on the Hosts page, which can open differently from the Overview's: split suits the page a machine is looked into on, overlay the page a fleet is glanced at.",
	},
})

// unregistered names the Config fields that deliberately have no row above, so
// that TestEverySettingIsRegistered can tell a decision from an omission.
//
// retention.audit is the only one. It is the old name for
// retention.scaling_events, kept because a file that still says it is honoured
// -- but it is not a setting in its own right, it is a spelling of another one,
// and offering it in the UI beside the key it sets would be offering the same
// value twice.
var unregistered = map[string]bool{
	"retention.audit": true,
}

func buildRegistry(list []Setting) map[string]Setting {
	out := make(map[string]Setting, len(list))
	for _, s := range list {
		if _, dup := out[s.Key]; dup {
			panic("config: duplicate setting " + s.Key)
		}
		if s.Live && s.RestartReason != "" {
			panic("config: " + s.Key + " is live and also gives a reason it needs a restart")
		}
		if s.Label == "" || s.Summary == "" {
			panic("config: " + s.Key + " has no label or no summary, and the settings page has nothing to call it")
		}
		out[s.Key] = s
	}
	return out
}

// Settings returns every registered setting, ordered by key.
func Settings() []Setting {
	out := make([]Setting, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// LookupSetting returns one setting by its dotted key.
func LookupSetting(key string) (Setting, bool) {
	s, ok := registry[key]
	return s, ok
}

// StoredSettings returns the settings that belong in the database, ordered by
// key. It is what the database layer iterates and what the settings API offers
// for editing.
func StoredSettings() []Setting {
	var out []Setting
	for _, s := range Settings() {
		if s.Stored() {
			out = append(out, s)
		}
	}
	return out
}

// SectionOrder is the order the UI and `zoomies config` put sections in: the
// order an operator reads them, which is roughly the order a request travels
// through the system, rather than alphabetical.
var SectionOrder = []string{
	"server", "database", "security", "github", "agent", "runners", "scheduler",
	"log", "oidc", "metrics", "retention", "images", "updates", "capacity_demand",
	"provider", "ui",
}

// CompareKeys orders two dotted keys by section first and then alphabetically,
// so a rendered list reads the way the documentation does.
func CompareKeys(a, b string) int {
	ai := slices.Index(SectionOrder, sectionOf(a))
	bi := slices.Index(SectionOrder, sectionOf(b))
	if ai < 0 {
		ai = len(SectionOrder)
	}
	if bi < 0 {
		bi = len(SectionOrder)
	}
	if ai != bi {
		return ai - bi
	}
	return strings.Compare(a, b)
}

func sectionOf(key string) string {
	head, _, _ := strings.Cut(key, ".")
	return head
}

// SettingError is a refusal an operator can act on: it names the key, says what
// the key is for, and says what was wrong with the value.
//
// The description is in there because "5 munutes is not a duration" is a
// sentence somebody has to open the documentation to act on, and "how long a
// runner that never registered is given (scheduler.provision_timeout): 5
// munutes is not a duration (try 30s, 5m, 2h)" is not.
type SettingError struct {
	Key    string
	Reason string
}

func (e *SettingError) Error() string {
	if s, ok := LookupSetting(e.Key); ok && s.Summary != "" {
		return fmt.Sprintf("%s (%s): %s", e.Key, firstClause(s.Summary), e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Key, e.Reason)
}

// firstClause is a summary trimmed to its first sentence and lowercased to sit
// inside a larger one, because a refusal wants the gist rather than the
// paragraph.
func firstClause(summary string) string {
	s := summary
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

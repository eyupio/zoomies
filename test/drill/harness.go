//go:build drill

package drill

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

// fleet is one drill's world: a fake GitHub, a controller process, and a
// remote agent process, all real except GitHub.
//
// The two Zoomies processes are the built binary, started the way an operator
// starts them. That is the point of this tier: every other test in the
// repository exercises the code inside the test process, where a restart is a
// struct being rebuilt and an agent is a goroutine. Here a restart is a
// process dying, the agent is on the other end of a socket having joined with
// a token, and the backend puts something on the host.
type fleet struct {
	t   *testing.T
	gh  *github.FakeGitHub
	api *apiClient

	controller *process
	agent      *process

	baseURL string
	// port and stateDir are kept so a drill can stop the controller and start
	// another on the same address -- against the same database, or against one
	// it has just restored.
	port     int
	stateDir string
	// agentWork is the directory inside the agent's state directory where the
	// backend lays out runners. The drill watches it to see a workload appear.
	agentWork string

	installationID string
}

// process is one child binary, with its output kept for the failure message.
type process struct {
	name string
	cmd  *exec.Cmd
	stop context.CancelFunc

	mu   sync.Mutex
	logs bytes.Buffer
	done chan struct{}
}

func (p *process) output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.logs.String()
}

// kill stops the process abruptly, which is what a drill wants when it is
// testing what a crash does. Waiting for it to exit is deliberate: a drill
// that carried on while the old process still held its port or its database
// lock would be testing the wrong thing.
func (p *process) kill() {
	p.stop()
	<-p.done
}

// newFleet stands up the whole thing and tears it down on cleanup.
func newFleet(t *testing.T) *fleet {
	t.Helper()
	requireBinary(t)

	gh := github.NewFake()
	t.Cleanup(gh.Close)
	gh.AddRepo("acme/api")

	f := &fleet{t: t, gh: gh}
	f.startController()
	f.connectInstallation()
	f.startAgent()
	return f
}

// startController runs the built binary as a controller pointed at the fake.
func (f *fleet) startController() {
	t := f.t
	t.Helper()
	f.port = freePort(t)
	f.stateDir = t.TempDir()
	f.startControllerOn(f.stateDir)
}

// startControllerOn runs the built binary as a controller against one state
// directory, on this fleet's address.
//
// The directory is a parameter because a restore drill's whole point is that
// the second controller comes up on a different one: the database it was given
// rather than the database it wrote.
func (f *fleet) startControllerOn(dir string) {
	t := f.t
	t.Helper()
	port := f.port
	f.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	f.controller = f.spawn("controller", []string{"controller"}, append(baseEnv(dir),
		fmt.Sprintf("ZOOMIES_BIND=127.0.0.1:%d", port),
		"ZOOMIES_DISABLE_AUTH=true", // loopback only; the validator allows it there
		"ZOOMIES_DB_PATH="+filepath.Join(dir, "zoomies.db"),
		// The whole reason this tier can run without credentials.
		"ZOOMIES_GITHUB_API_BASE_URL="+f.gh.URL(),
		// A remote agent is the point; the embedded one would hide the join.
		"ZOOMIES_AGENT_EMBEDDED=false",
		// Webhooks cannot reach a test process, so the poller is what notices
		// a queued job. Short, because a drill's patience is its runtime.
		"ZOOMIES_POLL_FALLBACK=true",
		"ZOOMIES_POLL_INTERVAL=2s",
	))
	f.api = &apiClient{t: t, base: f.baseURL + "/api/v1"}
	waitFor(t, waitProcessUp, "the controller to become healthy", func() bool {
		resp, err := http.Get(f.baseURL + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

// run executes the built binary as a one-shot command against a state
// directory, the way an operator runs `zoomies backup` on the machine holding
// the data. It returns what the command printed.
func (f *fleet) run(dir string, args ...string) string {
	t := f.t
	t.Helper()
	cmd := exec.Command(builtBinary(), args...)
	cmd.Env = append(baseEnv(dir), "ZOOMIES_DB_PATH="+filepath.Join(dir, "zoomies.db"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zoomies %s: %v\noutput: %q", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

// connectInstallation registers the fake's app with the controller.
func (f *fleet) connectInstallation() {
	t := f.t
	t.Helper()
	var inst struct {
		ID string `json:"id"`
	}
	f.api.post("/installations", map[string]any{
		"app_id":          f.gh.AppID(),
		"installation_id": f.gh.InstallationID(),
		"target":          "acme",
		"target_type":     "org",
		"private_key":     generateAppKey(t),
		"webhook_secret":  drillWebhookSecret,
		// Named on the installation rather than left to the global setting, so
		// the row itself says which GitHub this fleet is talking to.
		"api_base_url": f.gh.URL(),
	}, &inst)
	if inst.ID == "" {
		t.Fatal("the installation came back without an ID")
	}
	f.installationID = inst.ID
}

// startAgent joins a second binary as a remote agent, with a join token.
func (f *fleet) startAgent() {
	t := f.t
	t.Helper()
	var token struct {
		Token string `json:"token"`
	}
	f.api.post("/join-tokens", map[string]any{"capacity": 2}, &token)
	if token.Token == "" {
		t.Fatal("the join token came back empty")
	}

	dir := t.TempDir()
	f.agentWork = filepath.Join(dir, "work")
	if err := os.MkdirAll(f.agentWork, 0o750); err != nil {
		t.Fatalf("creating the agent work directory: %v", err)
	}
	// Before the agent starts: the backend looks for the runner tree the first
	// time it is asked to create anything.
	if err := stageStubRunner(f.agentWork); err != nil {
		t.Fatalf("staging the stub runner: %v", err)
	}

	f.agent = f.spawn("agent", []string{"agent"}, append(baseEnv(dir),
		"ZOOMIES_CONTROLLER_URL="+f.baseURL,
		"ZOOMIES_JOIN_TOKEN="+token.Token,
		"ZOOMIES_AGENT_BACKEND=process",
		"ZOOMIES_AGENT_CAPACITY=2",
		"ZOOMIES_WORK_DIR="+f.agentWork,
		// Plain HTTP to a loopback controller is what a drill has; the agent
		// refuses it otherwise, and rightly.
		"ZOOMIES_AGENT_ALLOW_INSECURE_HTTP=true",
		// Nothing should be downloaded: the stub tree is staged and the pool
		// pins its version. Saying so means a drill that somehow reaches for
		// the network fails here rather than hanging.
		"ZOOMIES_AGENT_RUNNER_DOWNLOAD_URL=http://127.0.0.1:1/never",
	))

	waitFor(t, waitProcessUp, "the agent to join and appear as a host", func() bool {
		var out struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		f.api.get("/hosts", &out)
		return len(out.Items) > 0
	})
}

// spawn starts the built binary and keeps its output for the failure message.
func (f *fleet) spawn(name string, args, env []string) *process {
	t := f.t
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, builtBinary(), args...)
	cmd.Env = env
	p := &process{name: name, cmd: cmd, stop: cancel, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = &syncWriter{p: p}, &syncWriter{p: p}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("starting the %s: %v", name, err)
	}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		cancel()
		<-p.done
		if t.Failed() {
			t.Logf("%s output:\n%s", name, p.output())
		}
	})
	return p
}

type syncWriter struct{ p *process }

func (w *syncWriter) Write(b []byte) (int, error) {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	return w.p.logs.Write(b)
}

// baseEnv is what both binaries need: a state and config directory of their
// own, and nothing inherited that could point them somewhere real.
func baseEnv(dir string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"ZOOMIES_STATE_DIR=" + dir,
		"ZOOMIES_CONFIG_DIR=" + dir,
		"ZOOMIES_LOG_FORMAT=text",
		"ZOOMIES_LOG_LEVEL=debug",
	}
}

// deliverJob posts a signed workflow_job webhook, the way GitHub drives a
// controller in production.
//
// The fallback poller only finds *queued* jobs -- it is the webhook's backstop,
// not a second channel -- so a drill that only added jobs to the fake would
// watch a runner sit idle until its timeout and never see the job it was made
// for. Delivering the webhook is what makes the drill exercise the path a real
// installation uses, and it is what puts the timing in the drill's hands: a
// fault drill needs to choose the moment a job starts.
func (f *fleet) deliverJob(action string, job github.QueuedJob, runnerName, conclusion string) {
	f.t.Helper()
	payload := map[string]any{
		"id":            job.ID,
		"run_id":        job.RunID,
		"name":          job.JobName,
		"workflow_name": job.WorkflowName,
		"labels":        job.Labels,
		"status":        action,
		"runner_name":   runnerName,
		"created_at":    job.QueuedAt.Format(time.RFC3339),
		"html_url":      job.HTMLURL,
	}
	if action == "in_progress" || action == "completed" {
		payload["started_at"] = job.QueuedAt.Add(time.Second).Format(time.RFC3339)
	}
	if action == "completed" {
		payload["completed_at"] = job.QueuedAt.Add(2 * time.Second).Format(time.RFC3339)
		payload["conclusion"] = conclusion
	}
	body, err := json.Marshal(map[string]any{
		"action":       action,
		"workflow_job": payload,
		"repository":   map[string]any{"full_name": job.Repo},
		"installation": map[string]any{"id": f.gh.InstallationID()},
	})
	if err != nil {
		f.t.Fatalf("encoding the webhook body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, f.baseURL+"/webhooks/github", bytes.NewReader(body))
	if err != nil {
		f.t.Fatalf("building the webhook request: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(drillWebhookSecret))
	mac.Write(body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(github.EventTypeHeader, "workflow_job")
	req.Header.Set(github.DeliveryIDHeader, fmt.Sprintf("drill-%d-%s", job.ID, action))
	req.Header.Set(github.SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatalf("delivering the %s webhook: %v", action, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		f.t.Fatalf("delivering the %s webhook: %s\n%s", action, resp.Status, raw)
	}
}

// drillWebhookSecret signs the drill's deliveries. It is a fixed string
// because both ends of the signature are this test.
const drillWebhookSecret = "drill-webhook-secret"

// createPool makes a pool on the process backend, pinned to the staged stub.
func (f *fleet) createPool(name string, labels ...string) string {
	f.t.Helper()
	var pool struct {
		ID string `json:"id"`
	}
	f.api.post("/pools", map[string]any{
		"installation_id": f.installationID,
		"name":            name,
		"labels":          labels,
		"backend":         "process",
		"min_runners":     0,
		"max_runners":     1,
		"idle_timeout":    "1m",
		"ephemeral":       true,
		"runner_version":  stubVersion,
	}, &pool)
	if pool.ID == "" {
		f.t.Fatal("the pool came back without an ID")
	}
	return pool.ID
}

// runners is the controller's view, including the ones it has finished with.
func (f *fleet) runners(poolID string) []runnerView {
	f.t.Helper()
	var out struct {
		Items []runnerView `json:"items"`
	}
	f.api.get("/runners?include_removed=true&pool_id="+poolID, &out)
	return out.Items
}

type runnerView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	JobsHandled int    `json:"jobs_handled"`
	ContainerID string `json:"container_id"`
}

// registeredWithGitHub reports whether the fake holds a registration under one
// name. Naming the runner matters: a drill that counted registrations would be
// racing whatever replacement the scheduler is entitled to create.
func (f *fleet) registeredWithGitHub(name string) bool {
	for _, r := range f.gh.Runners() {
		if r.Name == name {
			return true
		}
	}
	return false
}

// runnerDir is where the process backend laid a runner out. The backend's
// handle is the directory itself, so this is the same path it reports.
func (f *fleet) runnerDir(name string) string {
	return filepath.Join(f.agentWork, "runners", name)
}

// liveWorkloads is what is actually on the host: the process backend's handle
// is the runner's directory, so a directory with the stub's marker in it is a
// workload that really started.
func (f *fleet) liveWorkloads() []string {
	f.t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.agentWork, "runners"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatalf("reading the agent's runner directory: %v", err)
	}
	var live []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(f.agentWork, "runners", e.Name())
		if _, err := os.Stat(startedMarker(dir)); err == nil {
			live = append(live, e.Name())
		}
	}
	return live
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

type apiClient struct {
	t    *testing.T
	base string
}

func (c *apiClient) do(method, path string, body, out any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshalling the request body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		c.t.Fatalf("%s %s: %s\n%s", method, path, resp.Status, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decoding the response: %v\n%s", method, path, err, raw)
		}
	}
}

func (c *apiClient) get(path string, out any)        { c.do(http.MethodGet, path, nil, out) }
func (c *apiClient) post(path string, body, out any) { c.do(http.MethodPost, path, body, out) }

func builtBinary() string {
	dir, err := os.Getwd()
	if err != nil {
		return "zoomies"
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "zoomies")
		}
		dir = filepath.Dir(dir)
	}
	return "zoomies"
}

// requireBinary refuses to run rather than skipping: this tier exists to be
// the thing that cannot quietly not happen.
func requireBinary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(builtBinary()); err != nil {
		t.Fatalf("the zoomies binary is not at %s; run `make build-nogui` first", builtBinary())
	}
}

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// generateAppKey mints an RSA key for the fake's App.
//
// It is generated rather than checked in: the API parses the key to mint a JWT
// with it, so a placeholder string would be refused at the door, and a real
// key in the repository is a key somebody has to explain.
func generateAppKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating an app key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

var _ = store.RunnerIdle // the state names the drills assert on

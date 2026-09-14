//go:build e2e

package proxmox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The machinery the scenarios stand on: a real controller process, an API
// client that speaks to it as an administrator, and the polling that watches a
// machine become a host and go away again.
//
// It is a separate file from the scenarios for the reason test/drill's is: the
// scenarios should read as the procedure, and the procedure is not "start a
// process, scrape a setup token, mint an API token".

// ---------------------------------------------------------------------------
// The controller
// ---------------------------------------------------------------------------

// controllerProcess is one real zoomies controller, started from the built
// binary against a throwaway database.
//
// It binds a routable address rather than loopback, which every other harness in
// this repository can avoid and this one cannot: the machines it builds are on
// the cluster's network, and a controller they cannot reach is a controller they
// can never enrol with. That makes authentication mandatory -- the validator
// refuses to disable it on a public bind, and rightly -- so this also does the
// first-administrator dance and mints itself an API token.
type controllerProcess struct {
	t     *testing.T
	e     env
	dir   string
	base  string
	port  int
	token string

	mu      sync.Mutex
	logs    bytes.Buffer
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	cookies *cookieJar
}

func startController(t *testing.T, e env) *controllerProcess {
	t.Helper()
	u, err := url.Parse(e.controllerURL)
	if err != nil || u.Port() == "" {
		t.Fatalf("the controller URL %q has no port to bind", e.controllerURL)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("the controller URL %q has no numeric port", e.controllerURL)
	}
	c := &controllerProcess{t: t, e: e, dir: t.TempDir(), port: port,
		base: fmt.Sprintf("http://127.0.0.1:%d", port)}
	c.start()
	t.Cleanup(c.stop)
	c.authenticate()
	return c
}

// start launches the process and waits for it to answer. It is called again by
// restart, so it must be safe to run against a database that already has rows.
func (c *controllerProcess) start() {
	c.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, builtBinary(), "controller")
	cmd.Env = append(os.Environ(),
		// 0.0.0.0, because the guests are elsewhere. This is the whole reason
		// the rest of the authentication dance below exists.
		fmt.Sprintf("ZOOMIES_BIND=0.0.0.0:%d", c.port),
		"ZOOMIES_EXTERNAL_URL="+c.e.controllerURL,
		// Plain http on a lab network, so the session cookie must not be
		// Secure or it would be thrown away and every login would appear to do
		// nothing. The configuration warns about this; the warning is correct.
		"ZOOMIES_COOKIE_SECURE=false",
		"ZOOMIES_DB_PATH="+filepath.Join(c.dir, "zoomies.db"),
		"ZOOMIES_STATE_DIR="+c.dir,
		"ZOOMIES_CONFIG_DIR="+c.dir,
		"ZOOMIES_WORK_DIR="+filepath.Join(c.dir, "work"),
		// No agent here. A controller that could run the jobs itself would
		// never need a machine, and "scale from zero" would prove nothing.
		"ZOOMIES_AGENT_EMBEDDED=false",
		// This host is not reachable from GitHub, so the queue is discovered by
		// polling. The webhook path is covered by the integration tests.
		"ZOOMIES_POLL_FALLBACK=true",
		"ZOOMIES_POLL_INTERVAL=10s",
		"ZOOMIES_PROVIDER_ENABLED=true",
		"ZOOMIES_PROVIDER_INTERVAL=10s",
		// Nothing waits before buying: the procedure times the fleet's
		// response, and a delay would be measured as the hypervisor's.
		"ZOOMIES_PROVIDER_SCALE_UP_DELAY=0",
		// Long, because this harness decides when a machine is drained and
		// deleted. A fleet tidying up underneath the measurements would make
		// every timing somebody else's.
		"ZOOMIES_PROVIDER_IDLE_TIMEOUT=45m",
		"ZOOMIES_PROVIDER_SCALE_DOWN_COOLDOWN=45m",
		"ZOOMIES_LOG_FORMAT=text",
		"ZOOMIES_LOG_LEVEL=debug",
	)
	cmd.Stdout, cmd.Stderr = &lockedWriter{c: c}, &lockedWriter{c: c}
	if err := cmd.Start(); err != nil {
		cancel()
		c.t.Fatalf("starting the controller: %v", err)
	}
	c.mu.Lock()
	c.cmd, c.cancel = cmd, cancel
	c.mu.Unlock()

	waitFor(c.t, waitControllerHealthy, "the controller to become healthy", func() bool {
		resp, err := http.Get(c.base + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

// stop ends the process. It is the cleanup, so it must not fail the test on the
// way out; a controller that has already died is the normal case after a kill.
func (c *controllerProcess) stop() {
	c.mu.Lock()
	cancel, cmd := c.cancel, c.cmd
	c.cancel, c.cmd = nil, nil
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if cmd != nil {
		_ = cmd.Wait()
	}
}

// restart stops and starts the same controller against the same database. It is
// what the restart-mid-create case is: the rows are all the new process has, and
// what it does with them is the whole question.
func (c *controllerProcess) restart() {
	c.t.Helper()
	c.stop()
	c.start()
}

// output is everything the controller has printed, for a failure to quote.
func (c *controllerProcess) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logs.String()
}

type lockedWriter struct{ c *controllerProcess }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.logs.Write(p)
}

var setupTokenRE = regexp.MustCompile(`setup token: (\S+)`)

// authenticate creates the first administrator and mints an API token.
//
// The setup token is read out of the controller's own log, which is exactly the
// proof of ownership the design intends: "no account exists yet" is a condition
// an attacker can also satisfy, and holding the log is what distinguishes the
// operator from them.
func (c *controllerProcess) authenticate() {
	c.t.Helper()
	password := "qualification-" + randomHex(12)

	var setup string
	waitFor(c.t, waitControllerHealthy, "the setup token to be printed", func() bool {
		m := setupTokenRE.FindStringSubmatch(c.output())
		if len(m) == 2 {
			setup = m[1]
		}
		return setup != ""
	})

	// The bootstrap and the login carry an Origin of this controller's own
	// address: a client that sends none is treated as a plain one, but one that
	// then holds a session cookie is treated as a browser, and the cross-site
	// check would refuse it.
	post := func(path string, body any, out any) {
		status, raw, err := call(http.MethodPost, c.base+path, body, out, map[string]string{"Origin": c.base}, c.jar())
		if err != nil {
			c.t.Fatalf("POST %s: %v", path, err)
		}
		if status >= 300 {
			c.t.Fatalf("POST %s: %d\n%s", path, status, raw)
		}
	}
	post("/api/v1/auth/bootstrap", map[string]any{
		"username": "qualification", "password": password, "setup_token": setup,
	}, nil)
	post("/api/v1/auth/login", map[string]any{"username": "qualification", "password": password}, nil)

	var token struct {
		Token string `json:"token"`
	}
	post("/api/v1/tokens", map[string]any{
		"name": "proxmox-qualification", "role": "admin", "expires_in": "24h",
	}, &token)
	if token.Token == "" {
		c.t.Fatal("the API token came back empty, so nothing else in this run could be authorised")
	}
	c.token = token.Token
}

// jar is this process's cookie jar, held across the two cookie-authenticated
// calls above and used by nothing else: every other call in this harness is a
// bearer token, which the cross-site check exempts because a browser cannot send
// one.
func (c *controllerProcess) jar() *cookieJar {
	if c.cookies == nil {
		c.cookies = &cookieJar{}
	}
	return c.cookies
}

// ---------------------------------------------------------------------------
// The API client
// ---------------------------------------------------------------------------

type client struct {
	t    *testing.T
	base string
	tok  string
}

func newClient(t *testing.T, c *controllerProcess) *client {
	return &client{t: t, base: c.base + "/api/v1", tok: c.token}
}

func (c *client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.tok}
}

func (c *client) do(method, path string, body, out any) {
	c.t.Helper()
	status, raw, err := call(method, c.base+path, body, out, c.headers(), nil)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	if status >= 300 {
		c.t.Fatalf("%s %s: %d\n%s", method, path, status, raw)
	}
}

func (c *client) get(path string, out any)         { c.do(http.MethodGet, path, nil, out) }
func (c *client) post(path string, body, out any)  { c.do(http.MethodPost, path, body, out) }
func (c *client) patch(path string, body, out any) { c.do(http.MethodPatch, path, body, out) }
func (c *client) del(path string)                  { c.do(http.MethodDelete, path, nil, nil) }

// try is the cleanup's client: a failure is reported and moved past rather than
// fatal, because the next step may still remove something and a t.Fatal inside a
// cleanup would skip the rest of it.
func (c *client) try(method, path string, body, out any) bool {
	c.t.Helper()
	status, raw, err := call(method, c.base+path, body, out, c.headers(), nil)
	if err != nil {
		c.t.Errorf("cleanup: %s %s: %v", method, path, err)
		return false
	}
	if status >= 300 && status != http.StatusNotFound {
		c.t.Errorf("cleanup: %s %s: %d\n%s", method, path, status, raw)
		return false
	}
	return true
}

// cookieJar is the two cookies the bootstrap and login exchange. net/http/cookiejar
// would do, and this is four lines and carries nothing across hosts.
type cookieJar struct{ cookies []*http.Cookie }

func call(method, rawURL string, body, out any, headers map[string]string, jar *cookieJar) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshalling the request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		return 0, nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if jar != nil {
		for _, c := range jar.cookies {
			req.AddCookie(c)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if jar != nil {
		jar.cookies = append(jar.cookies, resp.Cookies()...)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 300 && out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, raw, fmt.Errorf("decoding the response: %w", err)
		}
	}
	return resp.StatusCode, raw, nil
}

// ---------------------------------------------------------------------------
// What the API says about a machine
// ---------------------------------------------------------------------------

// machineView is the part of GET /machines this procedure reads. It is written
// out rather than imported from internal/controller so that a field renamed in
// the API breaks this harness loudly, here, rather than silently becoming a zero
// value that every assertion then passes on.
type machineView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Message        string `json:"message"`
	ProviderID     string `json:"provider_id"`
	PoolID         string `json:"pool_id"`
	ResourceZone   string `json:"resource_zone"`
	ResourceID     string `json:"resource_id"`
	HostID         string `json:"host_id"`
	HostName       string `json:"host_name"`
	Address        string `json:"address"`
	ProviderError  string `json:"provider_error"`
	BootstrapError string `json:"bootstrap_error"`
	Attempts       int    `json:"attempts"`
	SafeToDelete   bool   `json:"safe_to_delete"`
	Timeline       []struct {
		Phase string    `json:"phase"`
		At    time.Time `json:"at"`
	} `json:"timeline"`
	DeletedAt *time.Time `json:"deleted_at"`
}

// vmid is the identifier the provider gave this machine, or zero before it has
// one.
func (m machineView) vmid() int {
	n, err := strconv.Atoi(m.ResourceID)
	if err != nil {
		return 0
	}
	return n
}

// at is when the machine reached one phase, from the timeline the API renders.
// The timeline is the controller's own timestamps rather than this harness's
// observations, so a timing taken from it is not limited by how often this polls.
func (m machineView) at(phase string) (time.Time, bool) {
	for _, e := range m.Timeline {
		if e.Phase == phase {
			return e.At, true
		}
	}
	return time.Time{}, false
}

// complaint is what a machine says is wrong with it, naming which of the two
// independent systems complained. They are separate columns for a reason: the
// hypervisor and the guest are different things to go and look at.
func (m machineView) complaint() string {
	var parts []string
	if m.ProviderError != "" {
		parts = append(parts, "provider: "+m.ProviderError)
	}
	if m.BootstrapError != "" {
		parts = append(parts, "bootstrap: "+m.BootstrapError)
	}
	if m.Message != "" {
		parts = append(parts, m.Message)
	}
	if len(parts) == 0 {
		return "no complaint recorded"
	}
	return strings.Join(parts, "; ")
}

type machineList struct {
	Items []machineView `json:"items"`
}

// machines is every machine this fleet has, deleted ones included. Deleted rows
// are what prove a delete was confirmed rather than merely issued.
func (c *client) machines() []machineView {
	c.t.Helper()
	var out machineList
	c.get("/machines?include_deleted=true&limit=200", &out)
	return out.Items
}

func (c *client) machine(id string) machineView {
	c.t.Helper()
	var m machineView
	c.get("/machines/"+id, &m)
	return m
}

// watch polls one machine until a condition holds, recording when each state was
// first seen on the way.
//
// The observations are how the two phases with no server-side timestamp are
// timed -- a guest that has begun answering, a machine that has begun draining
// -- and they are bounded by the poll interval, which the evidence says out
// loud. Everything else is read from the timeline, which is exact.
type watch struct {
	firstSeen map[string]time.Time
	last      machineView
}

func newWatch() *watch { return &watch{firstSeen: map[string]time.Time{}} }

func (w *watch) seen(state string) (time.Time, bool) {
	at, ok := w.firstSeen[state]
	return at, ok
}

// watchInterval is how often a machine is asked about. It is the resolution of
// every observed timing in the evidence, so it is recorded there.
const watchInterval = 2 * time.Second

// until polls until cond holds or the limit passes. It returns whether the
// condition held, rather than failing: several cases here are about a machine
// that is expected not to get somewhere, and those want to say so in their own
// words.
func (w *watch) until(c *client, id string, limit time.Duration, cond func(machineView) bool) bool {
	c.t.Helper()
	deadline := time.Now().Add(limit)
	for {
		m := c.machine(id)
		w.last = m
		if _, ok := w.firstSeen[m.State]; !ok {
			w.firstSeen[m.State] = time.Now()
		}
		if cond(m) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(watchInterval)
	}
}

// ---------------------------------------------------------------------------
// Waiting, and the deadline this harness is under
// ---------------------------------------------------------------------------

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", limit, what)
		}
		time.Sleep(3 * time.Second)
	}
}

// remaining is how much of `go test`'s own deadline is left, and whether there
// is one at all.
func remaining(t *testing.T) (time.Duration, bool) {
	d, ok := t.Deadline()
	if !ok {
		return 0, false
	}
	return time.Until(d), true
}

// assertTimeToFinish refuses to go on unless there is time for what is left plus
// the reconciliation and the cleanup.
//
// This is the guard budget.go exists for, made to bite at run time as well as in
// CI. A run killed by `go test` mid-cycle leaves a virtual machine on the
// cluster and skips the reconciliation that would have found it, so a harness
// that can see it will not finish should stop while it can still tidy up and say
// why.
func assertTimeToFinish(t *testing.T, need time.Duration, what string) {
	t.Helper()
	left, ok := remaining(t)
	if !ok {
		return
	}
	if left < need+reserveForTheEnd {
		t.Fatalf("stopping before %s: it needs %s and the reconciliation and cleanup need %s, "+
			"but this run has %s left. Give `go test` a longer -timeout (make test-e2e-proxmox uses one "+
			"that fits the whole procedure); carrying on would leave machines on the cluster and never reconcile them",
			what, need, reserveForTheEnd, left.Round(time.Second))
	}
}

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

// dispatchWorkflow triggers the fixture workflow and returns the marker it was
// dispatched with. The label goes with the marker so the workflow asks for this
// run's pool rather than a shared one.
func dispatchWorkflow(t *testing.T, e env, label string) string {
	t.Helper()
	marker := fmt.Sprintf("zoomies-pve-%s-%d", label, time.Now().UnixNano())
	cmd := exec.Command("gh", "workflow", "run", e.workflow,
		"--repo", e.repo, "-f", "marker="+marker, "-f", "label="+label)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("triggering %s in %s: %v\n%s", e.workflow, e.repo, err, out)
	}
	return marker
}

// githubRunnersWithLabel lists the registrations GitHub currently holds that
// carry one label.
//
// It asks GitHub rather than Zoomies, and that is the point: the failure this
// looks for is a registration left behind after a machine is destroyed, and the
// row says "removed" exactly when Zoomies believes it removed it -- which is the
// belief under test.
func githubRunnersWithLabel(e env, label string) ([]string, error) {
	path := "/orgs/" + e.target + "/actions/runners"
	if e.targetType == "repo" {
		path = "/repos/" + e.target + "/actions/runners"
	}
	out, err := exec.Command("gh", "api", "--paginate", path).Output()
	if err != nil {
		return nil, fmt.Errorf("asking GitHub for %s's runners: %w", e.target, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var found []string
	for {
		var page struct {
			Runners []struct {
				ID     int64  `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
				Labels []struct {
					Name string `json:"name"`
				} `json:"labels"`
			} `json:"runners"`
		}
		if err := dec.Decode(&page); err != nil {
			break
		}
		for _, r := range page.Runners {
			for _, l := range r.Labels {
				if l.Name == label {
					found = append(found, fmt.Sprintf("%s (id %d, %s)", r.Name, r.ID, r.Status))
					break
				}
			}
		}
	}
	return found, nil
}

// ---------------------------------------------------------------------------
// The one call in this harness that changes the cluster
// ---------------------------------------------------------------------------

// setProtection turns Proxmox's own protection flag on or off, which is how the
// induced delete failure is induced: a protected guest refuses to be destroyed,
// and the fleet has to notice, record the refusal and try again once it is
// cleared.
//
// It lives in the tagged half deliberately. inventory.go is read-only, because
// the closing reconciliation must not be able to change what it is measuring;
// this is the scenario's hand on the cluster, not the check's.
func (c *Cluster) setProtection(ctx context.Context, node string, vmid int, on bool) error {
	form := url.Values{"protection": {map[bool]string{true: "1", false: "0"}[on]}}
	path := fmt.Sprintf("%s/nodes/%s/qemu/%d/config", c.base, url.PathEscape(node), vmid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("setting protection on VM %d: %w", vmid, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("setting protection on VM %d: %s: %s", vmid, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Odds and ends
// ---------------------------------------------------------------------------

// newRunID is this run's identity. It is short, lower case and alphanumeric
// because it becomes part of a GitHub runner label and a pool name.
func newRunID() string { return randomHex(4) }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// A run that cannot read the system's randomness has bigger problems,
		// and a fixed identity would make two runs collide on one organisation.
		panic("cannot read randomness for this run's identity: " + err.Error())
	}
	return hex.EncodeToString(b)[:n]
}

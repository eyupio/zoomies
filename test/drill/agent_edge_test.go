//go:build drill

package drill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
)

// hammerWorkers is how many goroutines each hammer a noisy host's heartbeat
// and its task poll. It is far past anything one agent does -- an agent sends
// one heartbeat an interval and holds one poll -- and enough that, before the
// per-host limits, the noisy host's polls alone would have crossed the
// fleet-wide shed threshold on a controller holding only them.
const hammerWorkers = 32

// The agent-edge drill: one host hammering heartbeats and polls does not slow
// another host's work.
//
// A host is the untrusted edge. Its agent's token reaches only its own rows,
// but every call it makes still costs the controller something every other
// host shares: a write behind the single writer, a held connection counted
// against the fleet-wide poll threshold. The limits are per host so that a
// broken retry loop or a stolen token is that host's problem. The claim is the
// pair: the noisy host is refused, visibly, and the quiet host's task arrives
// inside one poll interval regardless -- asserted against the clock, not
// merely recorded.
func TestAHostHammeringTheAgentRoutesDoesNotDelayAnotherHostsTasks(t *testing.T) {
	f := newFleet(t)
	rec := newRecord(t, "agent-edge")
	defer rec.write()

	quiet := f.hostViews()
	if len(quiet) != 1 {
		t.Fatalf("the fleet has %d hosts before the noisy one joins, want the drill's agent alone", len(quiet))
	}
	quietID := quiet[0].ID

	noisyID, noisyToken := f.joinBareHost("drill-noisy")
	rec.note("noisy host joined", noisyID)

	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() { stop(); wg.Wait() }()
	var beats, refused, polls, emptyAtOnce atomic.Int64
	for range hammerWorkers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				status := f.agentCall(ctx, noisyID, noisyToken, http.MethodPost, agent.PathHeartbeat,
					agent.HeartbeatRequest{ProtocolVersion: agent.ProtocolVersion, Version: "drill"})
				beats.Add(1)
				if status == http.StatusTooManyRequests {
					refused.Add(1)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				started := time.Now()
				status := f.agentCall(ctx, noisyID, noisyToken, http.MethodGet, agent.PathTasks+"?wait=25", nil)
				polls.Add(1)
				if status == http.StatusOK && time.Since(started) < time.Second {
					emptyAtOnce.Add(1)
				}
			}
		}()
	}
	rec.faultInjected(fmt.Sprintf("%d goroutines hammering heartbeat and %d hammering the task poll as one host", hammerWorkers, hammerWorkers))

	// Let the hammer reach its stride before any work is queued, so the
	// delivery below is measured under it rather than beside it.
	waitFor(t, waitProcessUp, "the noisy host to be refused on its heartbeats and its polls", func() bool {
		return refused.Load() > 0 && emptyAtOnce.Load() > 0
	})

	label := "drill-agent-edge"
	poolID := f.createPool("drill-edge", label)
	// From the job being queued to a workload on the quiet host is the
	// poller finding it, the pass that creates the runner and queues its task,
	// and the task's delivery -- the whole path the hammer could slow down.
	queued := time.Now()
	f.gh.AddQueuedJob("acme/api", "CI", "build", []string{"self-hosted", label})

	var runner runnerView
	waitFor(t, waitRunnerCreated, "a runner to be created for the pool", func() bool {
		for _, r := range f.runners(poolID) {
			runner = r
			return true
		}
		return false
	})
	waitFor(t, waitWorkloadUp, "a workload to appear on the quiet host", func() bool {
		return len(f.liveWorkloads()) == 1
	})
	delivered := time.Since(queued)
	rec.note("runner", runner.Name)
	rec.note("task delivered to the quiet host", delivered.Round(100*time.Millisecond).String())
	if delivered > agent.DefaultPollWait {
		t.Errorf("the quiet host's job took %s to become a workload under the hammer; it must arrive within one poll interval (%s)",
			delivered, agent.DefaultPollWait)
	}

	// The quiet host kept its heartbeats: its budget is its own.
	if !f.hostHealthy(quietID) {
		t.Error("the quiet host went unhealthy while another host was being refused")
	}

	stop()
	wg.Wait()

	// The refusals are on the scrape, which is where an operator watching a
	// fleet would see one host misbehaving.
	rate := f.counter(`zoomies_agent_requests_limited_total{limit="rate"}`)
	poll := f.counter(`zoomies_agent_requests_limited_total{limit="poll"}`)
	rec.note("noisy host", fmt.Sprintf("%d heartbeats (%d refused), %d polls (%d answered at once)",
		beats.Load(), refused.Load(), polls.Load(), emptyAtOnce.Load()))
	rec.note("scrape", fmt.Sprintf("limited rate=%v poll=%v", rate, poll))
	if rate < 1 || poll < 1 {
		t.Errorf("zoomies_agent_requests_limited_total shows rate=%v poll=%v; both limits should have been reached and counted", rate, poll)
	}
	rec.recovered()
	rec.pass("one host hammering heartbeats and polls was refused and counted, and another host's task still arrived inside one poll interval")
}

// joinBareHost enrols a host from the test process with a join token, the
// way an agent does, and returns its identity. It offers no backend, so
// nothing is ever placed on it and it can only make noise.
func (f *fleet) joinBareHost(name string) (string, string) {
	f.t.Helper()
	var token struct {
		Token string `json:"token"`
	}
	f.api.post("/join-tokens", map[string]any{"capacity": 2}, &token)
	body, _ := json.Marshal(agent.JoinRequest{
		ProtocolVersion: agent.ProtocolVersion, JoinToken: token.Token, Name: name,
		OS: "linux", Arch: "amd64", Capacity: 2,
	})
	resp, err := http.Post(f.baseURL+agent.PathJoin, "application/json", bytes.NewReader(body))
	if err != nil {
		f.t.Fatalf("joining %s: %v", name, err)
	}
	defer resp.Body.Close()
	var out agent.JoinResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil || out.AgentToken == "" {
		f.t.Fatalf("joining %s answered %d", name, resp.StatusCode)
	}
	return out.HostID, out.AgentToken
}

// agentCall makes one agent request and returns its status, or zero when the
// request did not complete.
func (f *fleet) agentCall(ctx context.Context, hostID, token, method, path string, body any) int {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.baseURL+path, rdr)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(agent.HeaderHostID, hostID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// hostHealthy asks the API whether a host is healthy, which is the heartbeat's
// whole purpose seen from the operator's side.
func (f *fleet) hostHealthy(id string) bool {
	f.t.Helper()
	var h struct {
		Healthy bool `json:"healthy"`
	}
	f.api.get("/hosts/"+id, &h)
	return h.Healthy
}

// counter reads one series off the controller's scrape, or zero if it is not
// there.
func (f *fleet) counter(series string) float64 {
	f.t.Helper()
	resp, err := http.Get(f.baseURL + "/metrics")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(line, series+" "); ok {
			v, _ := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			return v
		}
	}
	return 0
}

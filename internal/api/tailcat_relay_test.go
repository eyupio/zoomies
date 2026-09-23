package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
)

// sealRelay stores an identity sealed with region, as a controller that had
// enrolled a private host before would have it.
func sealRelay(t *testing.T, h *harness, region *tailcfg.DERPRegion) {
	t.Helper()
	identity := tailcat.NewPrivateKey()
	identity.Public.Region = []*tailcfg.DERPRegion{region}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := h.key.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetSetting(h.ctx, tailcatIdentitySetting, base64.StdEncoding.EncodeToString(sealed), true); err != nil {
		t.Fatal(err)
	}
}

// privateProblem is tailcat.unavailable from the problems list, or nil.
func privateProblem(t *testing.T, h *harness) *controller.Problem {
	t.Helper()
	problems, err := h.ctrl.Problems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range problems {
		if problems[i].Code == "tailcat.unavailable" {
			return &problems[i]
		}
	}
	return nil
}

// relayDoor is a port that refuses connections until it is opened, and then
// forwards them to a real relay -- a relay that was unreachable and came back
// at the same address, which is what the sealed identity names.
type relayDoor struct {
	port   int
	target string
}

func newRelayDoor(t *testing.T, target string) *relayDoor {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return &relayDoor{port: port, target: target}
}

func (d *relayDoor) open(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(d.port)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				up, err := net.Dial("tcp", d.target)
				if err != nil {
					return
				}
				defer up.Close()
				go func() { _, _ = io.Copy(up, c) }()
				_, _ = io.Copy(c, up)
			}()
		}
	}()
}

// A controller whose sealed relay has gone used to say so only in a log line,
// and before that refused to start at all -- taking the direct hosts, the UI
// and the webhooks down with the private ones, and needing a restart to come
// back. It must start, say so in the problems list within a minute, and clear
// it by itself once the relay answers, with a private host able to join
// through it again without anybody restarting anything.
func TestAControllerWhoseSealedRelayIsDownSaysSoAndRecoversWithoutARestart(t *testing.T) {
	t.Setenv("IN_TS_TEST", "true")
	h := newHarness(t)
	dm := integration.RunDERPAndSTUN(t, func(string, ...any) {}, "127.0.0.1")
	live := dm.Regions[1]
	door := newRelayDoor(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(live.Nodes[0].DERPPort)))
	sealed := live.Clone()
	sealed.Nodes[0].DERPPort = door.port
	sealRelay(t, h, sealed)
	// No relay map in a test: the expansion finds nothing else, as it would
	// for a controller whose whole egress to the relays is down.
	h.api.private.expand = func(context.Context) (*tailcfg.DERPRegion, error) {
		return nil, errors.New("the relay map is unreachable in this test")
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	t.Cleanup(h.api.closeTailcat)
	if err := h.api.resumeTailcat(ctx, 100*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("an unreachable relay stopped the controller starting: %v", err)
	}

	deadline := time.Now().Add(time.Minute)
	var p *controller.Problem
	for p == nil && time.Now().Before(deadline) {
		if p = privateProblem(t, h); p == nil {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if p == nil {
		t.Fatal("a controller whose sealed relay is unreachable raised no tailcat.unavailable within a minute")
	}
	if p.Severity != config.SeverityWarning || p.Audience != controller.AudiencePlatform || p.Since == nil {
		t.Fatalf("tailcat.unavailable = %+v, want a platform warning that says since when", p)
	}

	door.open(t)
	deadline = time.Now().Add(time.Minute)
	for privateProblem(t, h) != nil {
		if time.Now().After(deadline) {
			t.Fatal("tailcat.unavailable did not clear within a minute of the relay answering")
		}
		time.Sleep(50 * time.Millisecond)
	}

	address, err := h.api.ensureTailcat(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := h.ctrl.Auth().CreateJoinToken(h.ctx, time.Minute, nil, 1, "test")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := agent.NewHTTPTransport(agent.HTTPOptions{ControllerURL: "tailcat://" + address})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	jctx, jcancel := context.WithTimeout(h.ctx, time.Minute)
	defer jcancel()
	if _, err := tr.Join(jctx, agent.JoinRequest{ProtocolVersion: agent.ProtocolVersion, JoinToken: token, Name: "back-again", Capacity: 1, OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("a private host could not join through the relay once it answered, without a restart: %v", err)
	}
}

// The sealed relay is the first choice every time, because it is the one each
// enrolled host's address names; another is used only while it does not
// answer, so new enrolments are not refused for the length of an outage, and
// the node goes back to it as soon as it does.
func TestTheListenerPrefersItsSealedRelayAndFallsBackOnlyWhileItIsDown(t *testing.T) {
	t.Setenv("IN_TS_TEST", "true")
	h := newHarness(t)
	sealed := &tailcfg.DERPRegion{RegionID: 1, RegionCode: "sealed", Nodes: []*tailcfg.DERPNode{{Name: "1a", RegionID: 1, HostName: "sealed.invalid"}}}
	other := &tailcfg.DERPRegion{RegionID: 2, RegionCode: "other", Nodes: []*tailcfg.DERPNode{{Name: "2a", RegionID: 2, HostName: "other.invalid"}}}
	sealRelay(t, h, sealed)
	var mu sync.Mutex
	up := map[tailcfg.DERPRegionID]bool{2: true}
	h.api.private.probe = func(_ context.Context, r *tailcfg.DERPRegion) error {
		mu.Lock()
		defer mu.Unlock()
		if !up[r.RegionID] {
			return errors.New("relay " + r.RegionCode + " did not answer")
		}
		return nil
	}
	h.api.private.expand = func(context.Context) (*tailcfg.DERPRegion, error) { return other, nil }
	t.Cleanup(h.api.closeTailcat)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	if err := h.api.resumeTailcat(ctx, time.Hour, time.Hour); err != nil {
		t.Fatal(err)
	}
	regionOf := func() tailcfg.DERPRegionID {
		h.api.private.mu.Lock()
		defer h.api.private.mu.Unlock()
		if h.api.private.region == nil {
			return 0
		}
		return h.api.private.region.RegionID
	}
	if got := regionOf(); got != 2 {
		t.Fatalf("with the sealed relay down and another answering, the node runs on region %d, want the one that answers", got)
	}
	if p := privateProblem(t, h); p != nil {
		t.Fatalf("a listener serving through a relay that answers raised %q", p.Code)
	}

	mu.Lock()
	up[1] = true
	mu.Unlock()
	h.api.checkTailcat(h.ctx)
	if got := regionOf(); got != 1 {
		t.Fatalf("with the sealed relay back, the node stayed on region %d; the sealed relay is the first choice", got)
	}

	mu.Lock()
	up = map[tailcfg.DERPRegionID]bool{}
	mu.Unlock()
	h.api.checkTailcat(h.ctx)
	if got := regionOf(); got != 1 {
		t.Fatalf("with no relay answering, the node moved to region %d; it should stay on the sealed one", got)
	}
	if privateProblem(t, h) == nil {
		t.Fatal("no relay answers and tailcat.unavailable was not raised")
	}
}

// The public relays are rate-limited, so a listener that is healthy on its
// sealed relay must not probe it at the outage rate; one that has a fault must.
func TestAHealthyListenerProbesItsRelayAtTheSlowRate(t *testing.T) {
	t.Setenv("IN_TS_TEST", "true")
	h := newHarness(t)
	sealed := &tailcfg.DERPRegion{RegionID: 1, RegionCode: "sealed", Nodes: []*tailcfg.DERPNode{{Name: "1a", RegionID: 1, HostName: "sealed.invalid"}}}
	sealRelay(t, h, sealed)
	var mu sync.Mutex
	probes, up := 0, true
	h.api.private.probe = func(context.Context, *tailcfg.DERPRegion) error {
		mu.Lock()
		defer mu.Unlock()
		probes++
		if !up {
			return errors.New("relay did not answer")
		}
		return nil
	}
	h.api.private.expand = func(context.Context) (*tailcfg.DERPRegion, error) { return nil, errors.New("no relay map") }
	t.Cleanup(h.api.closeTailcat)
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	if err := h.api.resumeTailcat(ctx, 10*time.Millisecond, time.Hour); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	healthy := probes
	up = false
	mu.Unlock()
	if healthy > 1 {
		t.Fatalf("a healthy listener probed its relay %d times in 300ms; it should wait the slow interval", healthy)
	}
	// A fault found some other way -- a failed enrolment -- must switch the
	// loop to the fast rate at its next check; here the slow timer is already
	// running, so drive one check by hand and then watch the fast rate take over.
	h.api.checkTailcat(h.ctx)
	go h.api.watchTailcat(ctx, 10*time.Millisecond, time.Hour)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if probes-healthy < 5 {
		t.Fatalf("a listener with a fault probed only %d times in 300ms; it should check at the fast rate", probes-healthy)
	}
}

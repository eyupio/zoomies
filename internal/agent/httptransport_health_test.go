package agent

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// silenceableProxy forwards TCP connections to a target until silence is
// called. From then on the connections it already holds stay open and swallow
// everything in both directions, while a connection opened afterwards is
// forwarded as before: one path dead without a reset or a FIN, a fresh one
// fine. That is what a controller connection looks like when something on the
// path starts dropping its packets, such as a NAT or firewall that has
// forgotten the flow.
type silenceableProxy struct {
	ln       net.Listener
	target   string
	accepted atomic.Int32

	mu    sync.Mutex
	pairs []*proxiedPair
}

type proxiedPair struct {
	client, server net.Conn
	silent         atomic.Bool
}

func newSilenceableProxy(t *testing.T, target string) *silenceableProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &silenceableProxy{ln: ln, target: target}
	go p.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, pair := range p.pairs {
			_ = pair.client.Close()
			_ = pair.server.Close()
		}
	})
	return p
}

func (p *silenceableProxy) serve() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.accepted.Add(1)
		server, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		pair := &proxiedPair{client: client, server: server}
		p.mu.Lock()
		p.pairs = append(p.pairs, pair)
		p.mu.Unlock()
		go pair.forward(server, client)
		go pair.forward(client, server)
	}
}

// forward keeps reading after the pair falls silent, so the sender sees its
// bytes accepted and never learns they went nowhere.
func (pair *proxiedPair) forward(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 && !pair.silent.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// silence kills every connection open now, without telling either end.
func (p *silenceableProxy) silence() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pair := range p.pairs {
		pair.silent.Store(true)
	}
}

func (p *silenceableProxy) url() string { return "https://" + p.ln.Addr().String() }

// writeServerCA pins the test server's certificate the way an operator pins a
// self-signed controller's, so the agent's real TLS path is the one exercised.
func writeServerCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "controller.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatalf("writing the CA file: %v", err)
	}
	return path
}

// A connection to the controller can die without a word: nothing reaches the
// agent, so the kernel keeps the socket for the quarter of an hour it
// retransmits for. HTTP/2 puts every call on that one connection, and a call's
// deadline resets only its own stream, so before the health check every
// heartbeat, poll and report queued onto the dead connection and timed out,
// until somebody restarted the agent. On one fleet that took twelve minutes,
// long enough for the controller to give the host's runners up as lost.
// Pinging a silent connection and closing it when the ping goes unanswered is
// what sends the next call to a fresh connection.
func TestTheAgentLeavesAControllerConnectionThatStopsAnswering(t *testing.T) {
	t.Parallel()
	var sawHTTP2 atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc(PathHeartbeat, func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			sawHTTP2.Store(true)
		}
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{OK: true})
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	proxy := newSilenceableProxy(t, srv.Listener.Addr().String())
	tr, err := NewHTTPTransport(HTTPOptions{
		ControllerURL: proxy.url(),
		CAFile:        writeServerCA(t, srv),
		Logger:        testLogger(),
		pingAfter:     200 * time.Millisecond,
		pingTimeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	tr.SetCredentials("host-1", "agent-token")

	heartbeat := func(timeout time.Duration) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_, err := tr.Heartbeat(ctx, HeartbeatRequest{})
		return err
	}
	// The first TLS handshake and HTTP/2 setup get their own generous bound,
	// so a loaded race runner does not fail before the dead path is exercised.
	if err := heartbeat(10 * time.Second); err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}
	// Over HTTP/1.1 a timed-out call closes its own connection, and this test
	// would pass for that reason alone. Multiplexing is where the hazard is.
	if !sawHTTP2.Load() {
		t.Fatal("the heartbeat did not use HTTP/2, so this test is not exercising the multiplexed connection")
	}

	proxy.silence()

	// A dead connection may cost the call already on it, never the agent's
	// link to the controller. The bound is generous against the health check
	// above and short against the hundred or more timed-out calls Go needs
	// before it gives up on a connection without one.
	deadline := time.Now().Add(8 * time.Second)
	for {
		err := heartbeat(time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("every heartbeat still fails on the dead connection: %v", err)
		}
	}
	if got := proxy.accepted.Load(); got < 2 {
		t.Fatalf("connections opened = %d, want a second one after the first went silent", got)
	}
}

// The test above shortens the health check to keep the suite quick; this pins
// what production gets. Waiting for a beat, finding a dead connection and
// starting the retry must all fit inside the controller's patience: a host
// silent for store.HeartbeatTimeout is called
// unhealthy, and one silent for five minutes has its runners given up as lost.
func TestTheControllerConnectionIsHealthCheckedInsideTheHeartbeatTimeout(t *testing.T) {
	tr, err := NewHTTPTransport(HTTPOptions{ControllerURL: "https://controller.example:8080", Logger: testLogger()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	ht, ok := tr.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", tr.http.Transport)
	}
	if ht.HTTP2 == nil || ht.HTTP2.SendPingTimeout <= 0 {
		t.Fatal("the controller connection has no HTTP/2 health check; a connection that dies silently is used until the kernel gives up on it")
	}
	if ht.HTTP2.PingTimeout <= 0 {
		t.Fatal("the health check has no ping timeout of its own; Go's default would apply without anyone having chosen it")
	}
	if found := config.MaxQuietHeartbeatInterval + ht.HTTP2.SendPingTimeout + ht.HTTP2.PingTimeout + heartbeatRetryInitial; found >= store.HeartbeatTimeout {
		t.Fatalf("the heartbeat interval, dead connection check and first retry take %s, not inside the controller's %s heartbeat timeout", found, store.HeartbeatTimeout)
	}
}

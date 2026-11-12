package controller

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eyupio/zoomies/internal/provider"
	"github.com/tailscale/tailcat"
)

// dialBudget is what one provider connection is allowed to take to open. The
// dialer is what a provider's HTTP transport calls per request, so this is the
// budget behind the reconciler's own deadlines, not an addition to them: the
// smaller of the two is what a caller waits for.
const dialBudget = 30 * time.Second

// dialIntroTimeout bounds the first attempt on a client that has worked
// before. Opening a connection through a working tunnel is a TCP handshake
// over WireGuard, normally well under a second, while the one failure a
// long-lived client can hit -- its gateway restarted and forgot it, which
// nothing about a dropped WireGuard peer reports as an error -- is a dial
// that hangs to the deadline. A short first attempt is what lets the dialer
// notice that, and still finish inside the call's own budget.
//
// It is a variable so the tests that make a dial hang on purpose need not sit
// out the whole five seconds; nothing outside a test changes it.
var dialIntroTimeout = 5 * time.Second

// privateDialer opens connections to a provider through a `zoomies gateway`
// rather than the network: a hypervisor on a home network has no address this
// controller can route to, and the gateway's Tailcat address is what stands
// in for one. It is the provider-side counterpart of the agent's private
// transport, with the direction reversed -- the controller is the one dialling.
//
// The address is a capability, kept sealed on the provider row and never put
// in an error: what a message names is the provider, and what an operator
// checks is whether the gateway beside the hypervisor is running.
type privateDialer struct {
	// address is immutable: the capability every client is built from.
	address string

	mu     sync.Mutex  // guards the fields below
	client *dialClient // the client in use, nil once the dialer is closed
	closed bool
}

// dialClient is one Tailcat client in the dialer, with whether a connection
// has ever been carried through it. A client that has never carried one and
// fails is telling us about the network it sits in -- a relay that does not
// answer, or a gateway that is not up yet -- and a fresh client would start
// from exactly the same place, so it is kept. One that has carried traffic
// and then fails is telling us about itself: the only thing that turns a
// working tunnel into a dead one without an error is the gateway's memory of
// the introduction, and only a fresh client re-announces from scratch. The
// flag is a one-writer fact seen by concurrent dials of this dialer, so it is
// atomic rather than under the dialer's lock.
type dialClient struct {
	client *tailcat.Client
	worked atomic.Bool
}

// newPrivateDialer validates the address without touching the network. A
// malformed one is refused here, when the row is being built, rather than
// discovered at the first clone.
func newPrivateDialer(address string) (*privateDialer, error) {
	info, err := tailcat.ParseAddr(tailcat.Addr(address))
	if err != nil || info.PresharedKey.IsZero() {
		return nil, errors.New("the private connection address is not a complete Tailcat address; copy it again from the gateway's output")
	}
	return &privateDialer{address: address, client: newDialClient(address)}, nil
}

// newDialClient builds one client for the dialer's address. Upstream
// diagnostics can quote the address; only our own messages, which never do,
// reach the log.
func newDialClient(address string) *dialClient {
	client := tailcat.NewClient(tailcat.Addr(address))
	client.Logf = func(string, ...any) {}
	return &dialClient{client: client}
}

// DialContext is what a provider's HTTP transport calls. The port is the one
// from the endpoint the operator typed, so the gateway forwards to the same
// service whether or not the host part means anything from here.
func (d *privateDialer) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("private connection: the endpoint has no port; write it as https://pve.example.com:8006")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return nil, errors.New("private connection: the endpoint's port is not a number")
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialBudget)
	defer cancel()
	conn, err := d.dial(dialCtx, uint16(port))
	if err != nil {
		if ctx.Err() != nil {
			// The caller stopped, not the tunnel: never dress a shutdown up
			// as a provider problem.
			return nil, ctx.Err()
		}
		// The sentinel's own sentence is the message: it names what to look
		// at, and quoting the dial's underlying error would risk quoting the
		// upstream library's diagnostics, which may contain the capability.
		return nil, provider.ErrPrivateConnection
	}
	return conn, nil
}

// dial opens one connection through the dialer, starting over from a fresh
// introduction when the current client's tunnel shows dead.
func (d *privateDialer) dial(ctx context.Context, port uint16) (net.Conn, error) {
	cur := d.get()
	if cur == nil {
		return nil, errors.New("private connection: the dialer is closed")
	}
	introCtx, cancel := context.WithTimeout(ctx, dialIntroTimeout)
	conn, err := dialGateway(introCtx, cur.client, port)
	cancel()
	if err == nil {
		cur.worked.Store(true)
		return conn, nil
	}
	if ctx.Err() != nil || d.isClosed() {
		return nil, err
	}
	if !cur.worked.Load() {
		// Nothing this client ever dialled worked, so its failure says what
		// any fresh client would start out saying: there is no tunnel to
		// re-announce. The next call will say it again, and the first one
		// after the gateway answers goes through untouched.
		return nil, err
	}
	// This client carried traffic once, so its capability is real and what
	// died is its own state. A fresh client re-announces from scratch on its
	// first use, which is what lets a provider come back on its own after its
	// gateway has restarted -- within this one call's budget, and without the
	// controller restart that a dead client otherwise waits for.
	if fresh := d.replace(cur); fresh != nil && fresh != cur {
		if conn, err = dialGateway(ctx, fresh.client, port); err == nil {
			fresh.worked.Store(true)
			return conn, nil
		}
	}
	return nil, err
}

// dialGateway re-introduces this controller to the gateway and opens one
// connection to it.
//
// A client's first dial introduces it to the server, and that introduction is
// a fact the server holds only in its own memory: a gateway restart on its
// host -- a crash, a reboot, an update -- forgets it while keeping the same
// persisted address, and a client that never repeats the introduction can
// dial forever against a server that silently drops its packets. Ping repeats
// it unconditionally, and it is a no-op on the server's side when the peer is
// already known, so a first introduction waits for the real acknowledgment,
// with the resends packet loss there needs, rather than betting the tunnel on
// one packet.
func dialGateway(ctx context.Context, client *tailcat.Client, port uint16) (net.Conn, error) {
	if _, err := client.Ping(ctx); err != nil {
		return nil, err
	}
	return client.DialTCPPort(ctx, port)
}

// replace retires one client and builds another, unless another dial already
// did, in which case theirs is handed back: two dials failing together must
// not compete to rebuild, and the loser simply dials through the winner's.
func (d *privateDialer) replace(cur *dialClient) *dialClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.client != cur {
		return d.client
	}
	_ = cur.client.Close()
	d.client = newDialClient(d.address)
	return d.client
}

// get hands back the current client, or nil once the dialer is closed.
func (d *privateDialer) get() *dialClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.client
}

// isClosed says whether Close has happened.
func (d *privateDialer) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// Close releases the current client's userspace network stack, and stops any
// dial being prepared after it. Idempotent, because the cache drops a dialer
// both when its row changes and when the controller stops.
func (d *privateDialer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	if d.client != nil {
		_ = d.client.client.Close()
		d.client = nil
	}
}

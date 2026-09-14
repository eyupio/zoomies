package controller

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
)

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
	client *tailcat.Client
	once   sync.Once
}

// newPrivateDialer validates the address without touching the network. A
// malformed one is refused here, when the row is being built, rather than
// discovered at the first clone.
func newPrivateDialer(address string) (*privateDialer, error) {
	info, err := tailcat.ParseAddr(tailcat.Addr(address))
	if err != nil || info.PresharedKey.IsZero() {
		return nil, errors.New("the private connection address is not a complete Tailcat address; copy it again from the gateway's output")
	}
	client := tailcat.NewClient(tailcat.Addr(address))
	// Upstream diagnostics can quote the address. Only our own messages,
	// which never do, reach the log.
	client.Logf = func(string, ...any) {}
	return &privateDialer{client: client}, nil
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
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := d.client.DialTCPPort(dialCtx, uint16(port))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("private connection: cannot reach the gateway; check that `zoomies gateway` is running beside the provider and that both sides have outbound internet access")
	}
	return conn, nil
}

// Close releases the userspace network stack. Idempotent, because the cache
// drops a client both when its row changes and when the controller stops.
func (d *privateDialer) Close() {
	d.once.Do(func() { _ = d.client.Close() })
}

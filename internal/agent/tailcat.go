package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/tailscale/tailcat"
)

// TailcatController is deliberately not an address: the actual capability is
// kept alongside the agent token in the mode-0600 credentials file.
const TailcatController = "tailcat://controller"

// DisplayController is safe for banners and errors, including malformed input.
func DisplayController(raw string) string {
	if strings.HasPrefix(strings.ToLower(raw), "tailcat:") {
		return TailcatController
	}
	return raw
}

func newTailcatTransport(opts HTTPOptions) (*HTTPTransport, error) {
	_, address, ok := strings.Cut(opts.ControllerURL, "://")
	if !ok {
		return nil, errors.New("agent: invalid private connection URL; copy the complete enrolment command again")
	}
	if address == "controller" {
		address = opts.TailcatAddress
	}
	if !strings.HasPrefix(address, "tc") {
		return nil, errors.New("agent: no private connection address; re-join using a fresh command from Add a host")
	}
	info, err := tailcat.ParseAddr(tailcat.Addr(address))
	if err != nil || info.PresharedKey.IsZero() {
		return nil, errors.New("agent: invalid private connection address; copy the complete Tailcat enrolment command again")
	}
	client := tailcat.NewClient(tailcat.Addr(address))
	// Upstream diagnostics may contain capability addresses. Only our own
	// errors, which never include those addresses, go to operator logs.
	client.Logf = func(string, ...any) {}
	ht := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := dialTailcat(ctx, client)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrRetryable
			}
			return conn, nil
		},
		// A connection through the tunnel is a WireGuard peer-to-peer flow, not
		// a real TCP socket: when the controller restarts and forgets its
		// peers, nothing here ever sends a FIN, so a pooled idle connection
		// looks perfectly healthy to net/http and gets reused into a silent
		// hang instead of a dial. Dialling fresh every call is what makes
		// dialTailcat's re-announcement below run often enough to matter.
		DisableKeepAlives: true,
	}
	// This synthetic origin never resolves or leaves the process. Every socket
	// is opened by Tailcat; proxies and redirects cannot reroute credentials.
	transport, err := NewHTTPTransport(HTTPOptions{
		ControllerURL: "http://127.0.0.1",
		HTTPClient:    &http.Client{Transport: ht, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Logger:        opts.Logger,
	})
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	transport.tailcatAddress = address
	var once sync.Once
	transport.closeTunnel = func() { once.Do(func() { ht.CloseIdleConnections(); _ = client.Close() }) }
	return transport, nil
}

// dialTailcat re-announces this client to the server before every dial.
//
// Client.DialTCPPort only does that once per Client, ever: it is right for a
// single short-lived connection, and wrong for an agent that reuses the same
// Client for as long as it runs. The server's record of which clients have
// introduced themselves lives only in that process's memory, so a controller
// restart forgets every one of them while keeping the same persisted
// address -- and a client that never repeats the introduction can dial
// forever against a server that silently drops its packets, because nothing
// about a dropped WireGuard peer looks like an error to wait out. Ping
// repeats it unconditionally, and it is a no-op on the server's side when
// the peer is already known, so paying for it on every dial is what lets a
// private host recover from a controller restart on its own.
func dialTailcat(ctx context.Context, client *tailcat.Client) (net.Conn, error) {
	if _, err := client.Ping(ctx); err != nil {
		return nil, err
	}
	return client.DialTCPPort(ctx, 80)
}

// Close releases the userspace network stack after daemon or enrolment shutdown.
func (t *HTTPTransport) Close() {
	if t.closeTunnel != nil {
		t.closeTunnel()
	}
}

func (t *HTTPTransport) TailcatAddress() string { return t.tailcatAddress }

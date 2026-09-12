package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

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
			conn, err := client.DialTCPPort(ctx, 80)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, ErrRetryable
			}
			return conn, nil
		},
		MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second,
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

// Close releases the userspace network stack after daemon or enrolment shutdown.
func (t *HTTPTransport) Close() {
	if t.closeTunnel != nil {
		t.closeTunnel()
	}
}

func (t *HTTPTransport) TailcatAddress() string { return t.tailcatAddress }

// Package gateway publishes one private service -- a hypervisor's API on a
// home network, typically -- to a controller over Tailcat.
//
// It is the provider-side half of what internal/agent's private transport is
// for hosts, with the direction reversed. A host connects outbound to the
// controller, so the controller listens inside the tunnel. A provider is
// something the controller connects to, so the gateway listens instead: it
// runs beside the hypervisor, on any machine that can reach its API, and
// forwards every TCP connection that arrives through the tunnel to that one
// address. The controller keeps the gateway's address sealed beside the
// provider's credential and dials through it.
//
// The gateway exposes exactly the target and nothing else. There is no port
// on the operating system, no route to the LAN and no way to ask it for a
// second destination: a client that holds the address can open connections to
// the target, which is why the address is treated as a credential everywhere
// it is handled. TLS is not terminated here -- the controller still verifies
// the hypervisor's certificate end to end, so the gateway learns nothing about
// the API token that passes through it.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
)

// StateFile is the identity, kept in the state directory. Losing it changes
// the address, and every provider row pointing at this gateway with it.
const StateFile = "gateway.json"

// dialTimeout bounds one forwarded connection's dial of the target. The
// controller has its own budget for the request; this is only so that a target
// that has gone away frees the tunnel's side promptly.
const dialTimeout = 10 * time.Second

// relayTimeout bounds relay discovery at startup, which the upstream library
// otherwise does with a background context.
const relayTimeout = 20 * time.Second

// Options is what a gateway needs.
type Options struct {
	// Target is the host:port every forwarded connection goes to, as the
	// machine running the gateway reaches it: "192.168.1.10:8006".
	Target string
	// StateDir is where the identity is kept. The address printed at startup
	// is derived from it, so a gateway restarted from the same directory
	// keeps its address.
	StateDir string
	// Region, when set, is the relay to use instead of discovering one. Tests
	// point it at a local relay; production leaves it nil.
	Region *tailcfg.DERPRegion
	Logger *slog.Logger
}

// Gateway is one running tunnel end.
type Gateway struct {
	node    *tailcat.Server
	address string
	target  string
	log     *slog.Logger
	closed  chan struct{}
	once    sync.Once
	active  sync.WaitGroup
}

// StatePath is where a gateway in the given directory keeps its identity.
func StatePath(dir string) string { return filepath.Join(dir, StateFile) }

// ParseTarget checks a target the way Start will use it, so a command can
// refuse a bad one before touching the network or the state directory.
func ParseTarget(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", errors.New("gateway: no target; give the address the hypervisor's API answers on, as this machine reaches it, e.g. --target 192.168.1.10:8006")
	}
	if strings.Contains(target, "://") {
		return "", fmt.Errorf("gateway: the target is a host and port, not a URL: write %q without the scheme and path", target)
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("gateway: %q is not host:port; a Proxmox API is usually on port 8006, e.g. 192.168.1.10:8006", target)
	}
	return net.JoinHostPort(host, port), nil
}

// Start loads or creates the identity, joins the relay and begins forwarding.
//
// The address it returns is a capability: whoever holds it can open
// connections to the target. It goes to the operator once, to paste into the
// provider's form, and nowhere else -- not into the log, which is why the
// upstream library's own logging is discarded.
func Start(ctx context.Context, opts Options) (*Gateway, error) {
	target, err := ParseTarget(opts.Target)
	if err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "gateway")
	if opts.StateDir == "" {
		return nil, errors.New("gateway: no state directory; the identity that keeps the address stable across restarts has to live somewhere")
	}

	identity, fresh, err := loadIdentity(StatePath(opts.StateDir))
	if err != nil {
		return nil, err
	}
	if opts.Region != nil {
		identity.Public.Region = []*tailcfg.DERPRegion{opts.Region}
	}
	if len(identity.Public.Region) == 0 {
		// Resolve the relay with a bounded context before Start, which
		// otherwise performs discovery with a background context inside the
		// upstream library. The region is saved with the identity so that
		// the address, which embeds it, is the same after a restart.
		identity.Public.RegionID = -1
		resolveCtx, cancel := context.WithTimeout(ctx, relayTimeout)
		defer cancel()
		if err := identity.Public.Expand(resolveCtx, tailcat.ExpandForServer); err != nil {
			return nil, errors.New("gateway: cannot reach a Tailcat relay; check this machine's outbound internet access and try again")
		}
		fresh = true
	}

	g := &Gateway{target: target, log: log, closed: make(chan struct{})}
	g.node = &tailcat.Server{
		Key:          identity.Private,
		PresharedKey: identity.Public.PresharedKey,
		Region:       identity.Public.Region[0],
		Logf:         func(string, ...any) {},
		// Every port forwards to the one target. Refusing all but one would
		// only add a way for the endpoint an operator typed on the controller
		// and the target they typed here to disagree, with a reset and no
		// message as the symptom; there is one destination either way.
		OnTCP: func(uint16) func(net.Conn) { return g.forward },
	}
	if err := g.node.Start(); err != nil {
		return nil, errors.New("gateway: cannot start the private connection; check network-interface permissions and outbound internet access, then try again")
	}
	if fresh {
		if err := saveIdentity(StatePath(opts.StateDir), identity); err != nil {
			_ = g.node.Close()
			return nil, err
		}
	}
	g.address = string(identity.Public.Addr())
	return g, nil
}

// Address is the capability a controller needs to reach the target.
func (g *Gateway) Address() string { return g.address }

// Target is what forwarded connections go to.
func (g *Gateway) Target() string { return g.target }

// Close stops accepting connections and cuts the ones in flight. A controller
// mid-request sees a closed connection and retries on its own schedule, as it
// does for any hypervisor that went away.
func (g *Gateway) Close() error {
	g.once.Do(func() {
		close(g.closed)
		_ = g.node.Close()
	})
	g.active.Wait()
	return nil
}

// forward carries one tunnelled connection to the target and back.
func (g *Gateway) forward(tunnel net.Conn) {
	g.active.Add(1)
	defer g.active.Done()
	defer tunnel.Close()
	select {
	case <-g.closed:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	local, err := (&net.Dialer{}).DialContext(ctx, "tcp", g.target)
	if err != nil {
		// The one failure worth a log line: the tunnel is up and the thing
		// behind it is not, which is what an operator staring at "provider
		// unreachable" on the controller needs to hear from this side.
		g.log.Warn("cannot reach the target behind the gateway; the controller will see the provider as unreachable",
			"target", g.target, "error", err.Error())
		return
	}
	defer local.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(local, tunnel); closeWrite(local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(tunnel, local); closeWrite(tunnel); done <- struct{}{} }()
	select {
	case <-done:
	case <-g.closed:
		return
	}
	select {
	case <-done:
	case <-g.closed:
	}
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// loadIdentity reads the saved identity, or makes a new one when there is
// none. fresh says the caller has to save it.
//
// A world-readable identity file is refused rather than used, as the agent's
// credentials file is: it is the address, and the address is the capability.
func loadIdentity(path string) (identity *tailcat.PrivateKey, fresh bool, err error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return tailcat.NewPrivateKey(), true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("gateway: reading the identity at %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("gateway: the identity at %s is readable by other users, and it is the address a controller uses to reach the target; run chmod 600 on it", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("gateway: reading the identity at %s: %w", path, err)
	}
	identity = &tailcat.PrivateKey{}
	if json.Unmarshal(raw, identity) != nil || identity.Private.IsZero() || identity.Public.PresharedKey.IsZero() {
		return nil, false, fmt.Errorf("gateway: the identity at %s is incomplete; move it aside to start with a new address, then update the provider on the controller with it", path)
	}
	return identity, false, nil
}

// saveIdentity writes the identity with mode 0600, atomically, so a crash
// between two writes cannot leave a half-written address behind.
func saveIdentity(path string, identity *tailcat.PrivateKey) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("gateway: creating the state directory %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("gateway: encoding the identity: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".gateway-*.json")
	if err != nil {
		return fmt.Errorf("gateway: creating a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has succeeded
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("gateway: setting permissions on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("gateway: writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("gateway: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("gateway: moving the identity into place at %s: %w", path, err)
	}
	return nil
}

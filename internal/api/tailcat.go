package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/tailscale/tailcat"
)

const tailcatIdentitySetting = "tailcat.identity.v1"

type tailcatContextKey struct{}

// The listener exists only in memory. It cannot expose a port on the OS, route
// the home LAN, or give a tunnel peer access to the operator API.
type tunnelListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *tunnelListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case c := <-l.connections:
		return c, nil
	}
}
func (l *tunnelListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *tunnelListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80} }
func (l *tunnelListener) offer(c net.Conn) {
	select {
	case <-l.done:
		_ = c.Close()
	case l.connections <- c:
	}
}

type privateConnections struct {
	mu       sync.Mutex
	closed   bool
	node     *tailcat.Server
	http     *http.Server
	listener *tunnelListener
	address  string
}

// Only transport-observed facts mark hosts as Tailcat hosts. A JSON field or
// proxy header from an agent cannot grant it that badge.
func connectionKind(ctx context.Context) string {
	if ctx.Value(tailcatContextKey{}) == true {
		return "tailcat"
	}
	return "direct"
}

func (s *Server) privateAgentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := r.URL.Path == agent.PathJoin || r.URL.Path == agent.PathHeartbeat ||
			r.URL.Path == agent.PathTasks || r.URL.Path == agent.PathResults || r.URL.Path == agent.PathReport ||
			strings.HasPrefix(r.URL.Path, agent.PathLogs+"/")
		if !allowed {
			http.NotFound(w, r)
			return
		}
		for _, key := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "X-Forwarded-Proto"} {
			r.Header.Del(key)
		}
		s.handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tailcatContextKey{}, true)))
	})
}

// ensureTailcat starts lazily when an operator asks for a private enrolment.
// The sealed identity lives in the normal database backup, so restarting the
// controller does not strand every lab that enrolled through it.
func (s *Server) ensureTailcat(ctx context.Context) (string, error) {
	p := &s.private
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !s.cfg().Server.TailcatEnabled || s.cfg().Security.DisableAuth {
		return "", errors.New("private connections are disabled; enable authentication and server.tailcat_enabled, then restart the controller")
	}
	if p.node != nil {
		return p.address, nil
	}
	if s.key == nil {
		return "", errors.New("private connections need the controller encryption key; check security.encryption_key_file")
	}
	stored, err := s.ctrl.Store().GetSetting(ctx, tailcatIdentitySetting)
	if err != nil {
		return "", errors.New("cannot read the private connection identity")
	}
	identity := tailcat.NewPrivateKey()
	if stored != "" {
		sealed, err := base64.StdEncoding.DecodeString(stored)
		if err != nil {
			return "", errors.New("cannot decode the private connection identity")
		}
		raw, err := s.key.Open(sealed)
		if err != nil {
			return "", errors.New("cannot decrypt the private connection identity; restore the matching encryption key")
		}
		if json.Unmarshal(raw, identity) != nil || identity.Private.IsZero() || identity.Public.PresharedKey.IsZero() {
			return "", errors.New("the saved private connection identity is incomplete")
		}
	}
	// Resolve the relay with a bounded context before Start, which otherwise
	// performs discovery with a background context inside the upstream library.
	if len(identity.Public.Region) == 0 {
		identity.Public.RegionID = -1
		resolveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := identity.Public.Expand(resolveCtx, tailcat.ExpandForServer); err != nil {
			return "", errors.New("cannot reach a Tailcat relay; check outbound internet access and try again")
		}
	}
	listener := &tunnelListener{connections: make(chan net.Conn), done: make(chan struct{})}
	node := &tailcat.Server{
		Key: identity.Private, PresharedKey: identity.Public.PresharedKey,
		Region: identity.Public.Region[0], Logf: func(string, ...any) {},
		OnTCP: func(port uint16) func(net.Conn) {
			if port != 80 {
				return nil
			}
			return listener.offer
		},
	}
	if err := node.Start(); err != nil {
		_ = listener.Close()
		_ = node.Close()
		return "", errors.New("cannot start the private connection; check network-interface permissions and outbound internet access, then try again")
	}
	if stored == "" {
		raw, err := json.Marshal(identity)
		if err == nil {
			var sealed []byte
			sealed, err = s.key.Seal(raw)
			if err == nil {
				err = s.ctrl.Store().SetSetting(ctx, tailcatIdentitySetting, base64.StdEncoding.EncodeToString(sealed), true)
			}
		}
		if err != nil {
			_ = listener.Close()
			_ = node.Close()
			return "", errors.New("cannot save the private connection identity")
		}
	}
	server := s.httpServer(context.Background())
	server.Handler = s.privateAgentHandler()
	p.node, p.listener, p.http, p.address = node, listener, server, string(identity.Public.Addr())
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("private connection listener stopped; restart the controller")
		}
	}()
	return p.address, nil
}

func (s *Server) closeTailcat() {
	p := &s.private
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.listener != nil {
		_ = p.listener.Close()
	}
	if p.http != nil {
		_ = p.http.Close()
	}
	if p.node != nil {
		_ = p.node.Close()
	}
}

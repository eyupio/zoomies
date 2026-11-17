package registrymirror

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
)

// A registry answers a blob request with a redirect to wherever the bytes are,
// and the mirror makes the request that follows for whoever asked. Where it
// goes is chosen by the registry, so it is held to what a content network is: a
// public name over HTTPS. It is never the host's own network, which would let a
// registry, or anyone who could make one answer a certain way, reach a loopback
// service, a neighbour on the LAN or a cloud metadata address from this host.
//
// The check is made on the name and on every address it resolves to, before the
// request. It is not made at connect time, which would catch a name that
// resolves differently the second time, because a proxy between the mirror and
// the network makes the address the mirror connects to the proxy's. The names
// come from the registry and not from the job, which is what keeps that
// acceptable.

// blockedPrefixes are the ranges the standard library's own predicates do not
// cover: shared address space, the benchmarking and reserved blocks, "this
// network", and the NAT64 prefix, which can be made to name any IPv4 address.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
}

func addrAllowed(a netip.Addr) bool {
	a = a.Unmap() // ::ffff:127.0.0.1 is 127.0.0.1
	switch {
	case a.IsLoopback(), a.IsPrivate(), a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast(), a.IsMulticast(), a.IsUnspecified():
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// checkRedirectTarget says whether the mirror may fetch a blob from the place a
// registry redirected to.
func checkRedirectTarget(ctx context.Context, u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("a redirect to %q is refused: a blob is fetched over https", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("a redirect with no host is refused")
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if !addrAllowed(a) {
			return fmt.Errorf("a redirect to %s is refused: it is not a public address", a)
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("a redirect to %s is refused: it does not resolve: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("a redirect to %s is refused: it resolves to nothing", host)
	}
	for _, a := range addrs {
		if !addrAllowed(a) {
			return fmt.Errorf("a redirect to %s is refused: it resolves to %s, which is not a public address", host, a)
		}
	}
	return nil
}

// guardTransport checks every request a client makes before it is made, which
// for a client that follows redirects includes each hop.
type guardTransport struct{ next http.RoundTripper }

func (g guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := checkRedirectTarget(req.Context(), req.URL); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(req)
}

// newGuardedClient is the client the mirror fetches the far side of a redirect
// with unless it is given another. Every request it makes passes the check,
// including each hop of a redirect chain, and the standard client ends a chain
// at ten.
func newGuardedClient() *http.Client {
	return &http.Client{Transport: guardTransport{next: newTransport()}}
}

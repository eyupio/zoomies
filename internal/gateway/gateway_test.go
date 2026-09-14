package gateway

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/tstest/integration"
)

func TestTargetsAreHostAndPortAndNothingElse(t *testing.T) {
	for _, bad := range []string{"", "https://pve:8006", "pve", ":8006", "pve:"} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	got, err := ParseTarget(" 192.168.1.10:8006 ")
	if err != nil || got != "192.168.1.10:8006" {
		t.Fatalf("ParseTarget = %q, %v", got, err)
	}
}

// The identity is the address, and the address is a capability to open
// connections to the target, so it is kept the way the agent keeps its token.
func TestIdentityIsPrivateAndRefusedWhenItIsNot(t *testing.T) {
	dir := t.TempDir()
	path := StatePath(dir)
	identity := tailcat.NewPrivateKey()
	if err := saveIdentity(path, identity); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity saved with mode %o", info.Mode().Perm())
	}
	loaded, fresh, err := loadIdentity(path)
	if err != nil || fresh {
		t.Fatalf("loadIdentity = fresh %v, %v", fresh, err)
	}
	if !loaded.Private.Equal(identity.Private) || !loaded.Public.PresharedKey.Equal(identity.Public.PresharedKey) {
		t.Fatal("the identity did not survive a round trip")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadIdentity(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("a world-readable identity was used: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadIdentity(filepath.Join(dir, "broken.json")); err == nil {
		t.Fatal("an empty identity was used")
	}
	if _, fresh, err := loadIdentity(filepath.Join(dir, "absent.json")); err != nil || !fresh {
		t.Fatalf("a missing identity is a fresh start: fresh %v, %v", fresh, err)
	}
}

// A local DERP relay exercises the real data path: a TLS server that only the
// gateway can reach, a client that holds nothing but the address, and the
// hypervisor's certificate verified end to end through the tunnel.
func TestGatewayForwardsTheTunnelToItsTargetAndKeepsItsAddressAcrossRestarts(t *testing.T) {
	t.Setenv("IN_TS_TEST", "true")
	dm := integration.RunDERPAndSTUN(t, func(string, ...any) {}, "127.0.0.1")
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.Host)
	}))
	defer target.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	g, err := Start(ctx, Options{Target: target.Listener.Addr().String(), StateDir: dir, Region: dm.Regions[1]})
	if err != nil {
		t.Fatal(err)
	}
	address := g.Address()
	if !strings.HasPrefix(address, "tc") {
		t.Fatalf("address %q is not a Tailcat address", address)
	}

	client := tailcat.NewClient(tailcat.Addr(address))
	client.Logf = func(string, ...any) {}
	defer client.Close()
	pool := target.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	httpClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return client.DialTCPPort(ctx, 8006)
		},
	}}
	// example.com is the name httptest's certificate is issued for, and it
	// does not resolve to the target: only the tunnel gets there.
	res, err := httpClient.Get("https://example.com:8006/version")
	if err != nil {
		t.Fatalf("through the gateway: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "hello from example.com:8006" {
		t.Fatalf("got %q", body)
	}

	// The same directory is the same address, which is what lets a provider
	// row outlive a reboot of the machine the gateway runs on.
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Start(ctx, Options{Target: target.Listener.Addr().String(), StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Address() != address {
		t.Fatal("restarting the gateway changed its address")
	}
}

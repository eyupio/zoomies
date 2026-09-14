package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/provider"
	"github.com/tailscale/tailcat"
)

// capturingFactory keeps the last Config it was asked to build from, which is
// how a test sees what the controller hands a provider without a hypervisor.
type capturingFactory struct {
	sharedFakeFactory
	last provider.Config
}

func (f *capturingFactory) New(_ context.Context, cfg provider.Config) (provider.Provider, error) {
	f.last = cfg
	return f.fake, nil
}

func privateAddress(t *testing.T) string {
	t.Helper()
	identity := tailcat.NewPrivateKey()
	identity.Public.RegionID = 1
	return string(identity.Public.Addr())
}

// A provider on a home network is reached through its gateway, so the client
// built for it has to be given the tunnel to dial through -- and not the
// controller's own HTTP client, which would take the row's certificate
// settings and its private connection out of the picture together.
func TestAProviderWithAPrivateConnectionIsBuiltWithTheTunnelToDialThrough(t *testing.T) {
	h := newHarness(t)
	factory := &capturingFactory{sharedFakeFactory: sharedFakeFactory{fake: h.fake}}
	registry, err := provider.NewRegistry(factory)
	if err != nil {
		t.Fatal(err)
	}
	h.c.providers = registry
	_, row := h.machineFleet(t)

	pr, err := h.c.providerFor(h.ctx, row)
	if err != nil {
		t.Fatalf("direct provider: %v", err)
	}
	if factory.last.DialContext != nil || factory.last.HTTPClient != nil {
		t.Fatal("a direct provider was given a tunnel, or a ready-made client that ignores the row's certificate settings")
	}
	if h.c.ProviderView(row, nil).Connection != "direct" {
		t.Fatal("the view does not say the provider is reached directly")
	}

	sealed, err := h.key.SealString(privateAddress(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetProviderTailcatAddress(h.ctx, row.ID, sealed); err != nil {
		t.Fatal(err)
	}
	row, err = h.st.GetProvider(h.ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.c.ProviderView(row, nil).Connection != "tailcat" {
		t.Fatal("the view does not say the provider is reached privately")
	}
	again, err := h.c.providerFor(h.ctx, row)
	if err != nil {
		t.Fatalf("private provider: %v", err)
	}
	if again == pr {
		t.Fatal("the row changed and the cached client was reused")
	}
	if factory.last.DialContext == nil {
		t.Fatal("a private provider was built without the tunnel to dial through")
	}
	if factory.last.HTTPClient != nil {
		t.Fatal("a ready-made client would bypass the tunnel")
	}
	// An endpoint with no port cannot say which service the gateway should
	// forward to, and the message says so rather than naming the address.
	if _, err := factory.last.DialContext(h.ctx, "tcp", "pve.lan"); err == nil || !strings.Contains(err.Error(), "8006") {
		t.Fatalf("dialling without a port: %v", err)
	}
	h.c.closeProviderTunnels()
}

func TestAPrivateProviderIsRefusedWhenPrivateConnectionsAreOffOrTheAddressIsNotOne(t *testing.T) {
	h := newHarness(t)
	_, row := h.machineFleet(t)
	garbage, err := h.key.SealString("not-an-address")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetProviderTailcatAddress(h.ctx, row.ID, garbage); err != nil {
		t.Fatal(err)
	}
	row, _ = h.st.GetProvider(h.ctx, row.ID)
	if _, err := h.c.providerFor(h.ctx, row); err == nil || !strings.Contains(err.Error(), "Tailcat address") || strings.Contains(err.Error(), "not-an-address") {
		t.Fatalf("a malformed address was accepted, or quoted: %v", err)
	}

	h.c.UpdateConfig(func(c *config.Config) { c.Server.TailcatEnabled = false })
	if _, err := h.c.providerFor(h.ctx, row); err == nil || !strings.Contains(err.Error(), "server.tailcat_enabled") {
		t.Fatalf("private connections off, got: %v", err)
	}
}

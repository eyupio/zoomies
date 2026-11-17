package registrymirror

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// The mirror makes the request for whoever asks, and the place a registry
// redirects to is chosen by the registry. Whatever that is, it is not allowed to
// be the host's own network: a loopback service, a neighbour on the LAN, a cloud
// metadata address. A real content network is a public name over HTTPS.
func TestARedirectTargetOnAnAddressAJobShouldNotReachIsRefused(t *testing.T) {
	refused := []string{
		"http://93.184.216.34/blob",
		"ftp://93.184.216.34/blob",
		"https://127.0.0.1/blob",
		"https://127.1.2.3/blob",
		"https://[::1]/blob",
		"https://localhost/blob",
		"https://10.0.0.5/blob",
		"https://172.16.0.1/blob",
		"https://192.168.1.1/blob",
		"https://[fd00::1]/blob",
		"https://169.254.169.254/latest/meta-data/",
		"https://[fe80::1]/blob",
		"https://100.64.0.1/blob",
		"https://0.0.0.0/blob",
		"https://0.1.2.3/blob",
		"https://224.0.0.1/blob",
		"https://[ff02::1]/blob",
		"https://240.0.0.1/blob",
		"https://198.18.0.1/blob",
		"https://[::ffff:127.0.0.1]/blob",
		"https://[::ffff:10.0.0.1]/blob",
		"https://[64:ff9b::7f00:1]/blob",
	}
	for _, raw := range refused {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkRedirectTarget(context.Background(), u); err == nil {
				t.Errorf("%s was allowed", raw)
			}
		})
	}
	for _, raw := range []string{"https://93.184.216.34/blob", "https://[2606:4700:4700::1111]/blob"} {
		t.Run("allows "+raw, func(t *testing.T) {
			u, _ := url.Parse(raw)
			if err := checkRedirectTarget(context.Background(), u); err != nil {
				t.Errorf("%s was refused: %v", raw, err)
			}
		})
	}
}

func TestTheDefaultRedirectClientRefusesAContentNetworkOnALoopbackAddress(t *testing.T) {
	f := newFakeRegistry(t)
	digest := f.addBlob(blobOf(500, 'g'))
	// No RedirectClient: the default one, which is the one that guards.
	m, srv := newTestMirror(t, f, func(c *Config) { c.RedirectClient = nil })

	resp, body := do(t, http.MethodGet, srv.URL+"/v2/library/alpine/blobs/"+digest, nil)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
	if got := registryErrorCode(t, body); got != "UNAVAILABLE" {
		t.Errorf("code %s, want UNAVAILABLE", got)
	}
	if n := f.count("cdn"); n != 0 {
		t.Errorf("the content network on a loopback address was reached %d times", n)
	}
	if got := m.Stats().BytesStored; got != 0 {
		t.Errorf("%d bytes kept", got)
	}
}

package agent

import (
	"net/http"
	"strings"
	"testing"

	"github.com/tailscale/tailcat"
)

func TestPrivateConnectionErrorsNeverContainTheAddress(t *testing.T) {
	for _, address := range []string{"tc-secret-not-valid", "controller", "tc%zz-secret"} {
		_, err := NewHTTPTransport(HTTPOptions{ControllerURL: "TAILCAT://" + address})
		if err == nil {
			t.Fatal("invalid address accepted")
		}
		if strings.Contains(err.Error(), address) {
			t.Fatalf("address leaked: %s", err)
		}
	}
}

func TestPrivateTransportUsesNoProxyAndKeepsCapabilityOutOfDescription(t *testing.T) {
	key := tailcat.NewPrivateKey()
	key.Public.RegionID = 1
	address := string(key.Public.Addr())
	tr, err := NewHTTPTransport(HTTPOptions{ControllerURL: "tailcat://" + address})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if tr.Describe() != TailcatController || tr.TailcatAddress() != address {
		t.Fatal("capability and display address were not separated")
	}
	httpTransport := tr.http.Transport.(*http.Transport)
	if httpTransport.Proxy != nil {
		t.Fatal("proxy can intercept private agent requests")
	}
	if err := tr.http.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatal("private requests may follow redirects")
	}
	path := StatePath(t.TempDir())
	if err := Save(path, Credentials{HostID: "host_test", AgentToken: "token", Controller: tr.Describe(), TailcatAddress: address}); err != nil {
		t.Fatal(err)
	}
	saved, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewHTTPTransport(HTTPOptions{ControllerURL: saved.Controller, TailcatAddress: saved.TailcatAddress})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.TailcatAddress() != address {
		t.Fatal("restart lost the private connection")
	}
}

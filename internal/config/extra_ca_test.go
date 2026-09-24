package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Acme proxy CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "proxy-ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The file is mounted into every runner. A bad one has to stop the agent at
// startup, with the setting named, rather than fail every create or leave
// every job failing TLS behind the proxy it was meant to get them through.
func TestAnExtraCAFileMustBeAnAbsolutePathToAPEMCertificate(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "ca.der")
	if err := os.WriteFile(notPEM, []byte{0x30, 0x82, 0x01}, 0o600); err != nil {
		t.Fatal(err)
	}
	garbled := filepath.Join(dir, "garbled.pem")
	if err := os.WriteFile(garbled, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("nope")}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
	}{
		{"relative", "proxy-ca.pem"},
		{"missing", filepath.Join(dir, "absent.pem")},
		{"not PEM", notPEM},
		{"unparseable certificate", garbled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Agent.Embedded = true
			c.Agent.ExtraCAFile = tc.path
			fs := c.Validate()
			if !hasCode(fs.Errors(), "agent.extra_ca_invalid") {
				t.Fatalf("agent.extra_ca_file %q was accepted: %+v", tc.path, fs)
			}
			if hasCode(fs, "agent.extra_ca") {
				t.Fatal("a refused CA was also reported as trusted")
			}
		})
	}
}

// Trusting another CA widens what every job trusts. That is the point, and
// not a risk to warn about, but it is surprising enough to say once.
func TestAGoodExtraCAIsReportedAsInformationOnly(t *testing.T) {
	c := Default()
	c.Agent.Embedded = true
	if fs := c.Validate(); hasCode(fs, "agent.extra_ca") || hasCode(fs, "agent.extra_ca_invalid") {
		t.Fatal("an unset agent.extra_ca_file drew a finding")
	}
	c.Agent.ExtraCAFile = writeTestCA(t)
	fs := c.Validate()
	if hasCode(fs, "agent.extra_ca_invalid") {
		t.Fatalf("a good CA was refused: %+v", fs)
	}
	for _, f := range fs {
		if f.Code == "agent.extra_ca" && f.Severity == SeverityInfo {
			return
		}
	}
	t.Fatalf("no agent.extra_ca info finding: %+v", fs)
}

func TestTheExtraCAFileHasAnEnvironmentOverride(t *testing.T) {
	t.Setenv("ZOOMIES_AGENT_EXTRA_CA_FILE", "/etc/acme/proxy-ca.pem")
	c := Default()
	if err := c.applyEnv(); err != nil {
		t.Fatal(err)
	}
	if c.Agent.ExtraCAFile != "/etc/acme/proxy-ca.pem" {
		t.Fatalf("agent.extra_ca_file = %q", c.Agent.ExtraCAFile)
	}
}

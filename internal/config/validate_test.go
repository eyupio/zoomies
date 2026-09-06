package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// find returns the finding with this code, or a zero Finding.
func find(fs Findings, code string) Finding {
	for _, f := range fs {
		if f.Code == code {
			return f
		}
	}
	return Finding{}
}

// baseConfig is a configuration that validates cleanly, so each test can change
// exactly the one setting it is about.
func baseConfig(t *testing.T) *Config {
	t.Helper()
	c := Default()
	c.Database.Path = filepath.Join(t.TempDir(), "zoomies.db")
	return c
}

// security.disable_auth turns every request into an administrator, so the guard
// on it has to ask whether anything can reach the listener -- not merely what
// address it is bound to. A loopback bind behind a reverse proxy is the
// deployment this project recommends, and it is reachable by the whole world.
func TestDisableAuthIsRefusedWhereverTheControllerIsReachable(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:   "loopback with nothing in front",
			mutate: func(c *Config) { c.Server.Bind = "127.0.0.1:8080" },
		},
		{
			name:    "bound to every interface",
			mutate:  func(c *Config) { c.Server.Bind = "0.0.0.0:8080" },
			wantErr: true,
		},
		{
			name: "loopback, but an external URL says something forwards to it",
			mutate: func(c *Config) {
				c.Server.Bind = "127.0.0.1:8080"
				c.Server.ExternalURL = "https://zoomies.example.com"
			},
			wantErr: true,
		},
		{
			name: "loopback, but a trusted proxy says something forwards to it",
			mutate: func(c *Config) {
				c.Server.Bind = "127.0.0.1:8080"
				c.Server.TrustedProxies = []string{"10.0.0.0/8"}
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := baseConfig(t)
			c.Security.DisableAuth = true
			tc.mutate(c)

			f := find(c.Validate(), "auth.disabled")
			if f.Code == "" {
				t.Fatal("disable_auth produced no finding at all")
			}
			if got := f.Severity == SeverityError; got != tc.wantErr {
				t.Fatalf("severity = %q (fatal=%v); want fatal=%v", f.Severity, got, tc.wantErr)
			}
			if err := c.Validate().Err(); (err != nil) != tc.wantErr {
				t.Fatalf("Err() = %v; want startup to be stopped=%v", err, tc.wantErr)
			}
		})
	}
}

// A wildcard origin switches the cross-origin check off entirely. Every other
// setting that trades safety for convenience is named at startup and in the
// problems panel; this one was silent.
func TestWildcardAllowedOriginIsNamed(t *testing.T) {
	c := baseConfig(t)
	c.Server.AllowedOrigins = []string{"*"}

	f := find(c.Validate(), "origins.wildcard")
	if f.Severity != SeverityWarning {
		t.Fatalf(`allowed_origins "*" produced %+v; want a warning`, f)
	}
	if !strings.Contains(f.Detail, "session") {
		t.Errorf("the detail should say what it costs: %q", f.Detail)
	}

	// A real origin is not a finding.
	c.Server.AllowedOrigins = []string{"https://zoomies.example.com"}
	if f := find(c.Validate(), "origins.wildcard"); f.Code != "" {
		t.Errorf("a named https origin was flagged as a wildcard: %+v", f)
	}
	if f := find(c.Validate(), "origins.insecure"); f.Code != "" {
		t.Errorf("an https origin was flagged as insecure: %+v", f)
	}

	// A plaintext one is worth a note, because anyone on the path can be it.
	c.Server.AllowedOrigins = []string{"http://zoomies.example.com"}
	if f := find(c.Validate(), "origins.insecure"); f.Code == "" {
		t.Error("a plaintext allowed origin produced no finding")
	}
}

// Zoomies cannot see a proxy terminating TLS in front of it, so a publicly
// bound controller whose cookies lack Secure has to be told about it: one
// plain-HTTP request to that host otherwise hands over a live session.
func TestInsecureCookieOnAPublicBindIsNamed(t *testing.T) {
	c := baseConfig(t)
	c.Server.Bind = "0.0.0.0:8080"
	c.Server.ExternalURL = ""
	c.normalize()

	if f := find(c.Validate(), "auth.cookie_insecure"); f.Severity != SeverityWarning {
		t.Fatalf("public bind with insecure cookies produced %+v; want a warning", f)
	}

	// An https external URL derives Secure by itself, so the warning goes away.
	c.Security.CookieSecure = nil
	c.Server.ExternalURL = "https://zoomies.example.com"
	c.normalize()
	if f := find(c.Validate(), "auth.cookie_insecure"); f.Code != "" {
		t.Errorf("an https external URL should have turned Secure on: %+v", f)
	}

	// So does saying so outright, which is the reverse-proxy case.
	secure := true
	c.Security.CookieSecure = &secure
	c.Server.ExternalURL = ""
	if f := find(c.Validate(), "auth.cookie_insecure"); f.Code != "" {
		t.Errorf("cookie_secure was set explicitly and still warned: %+v", f)
	}

	// A loopback-only instance is not warned about: there is no network hop.
	c.Security.CookieSecure = nil
	c.Server.Bind = "127.0.0.1:8080"
	c.normalize()
	if f := find(c.Validate(), "auth.cookie_insecure"); f.Code != "" {
		t.Errorf("a loopback bind should not warn about cookie security: %+v", f)
	}
}

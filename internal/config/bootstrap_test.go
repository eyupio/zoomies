package config

import "testing"

// Half a bootstrap is a provisioner waiting on an instance that will never
// become usable, so it stops startup with a message saying what is missing.
func TestHalfABootstrapStopsStartup(t *testing.T) {
	cases := []struct {
		name string
		b    Bootstrap
		want bool
	}{
		{"nothing set", Bootstrap{}, false},
		{"a password", Bootstrap{Admin: "ops", PasswordFile: "/run/secrets/pw"}, false},
		{"a token", Bootstrap{Admin: "ops", TokenFile: "/run/secrets/tok"}, false},
		{"no username", Bootstrap{PasswordFile: "/run/secrets/pw"}, true},
		{"no credential", Bootstrap{Admin: "ops"}, true},
		{"both credentials", Bootstrap{Admin: "ops", PasswordFile: "/a", TokenFile: "/b"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Bootstrap = tc.b
			var got bool
			for _, f := range c.Validate().Errors() {
				if f.Code == "bootstrap.incomplete" {
					got = true
				}
			}
			if got != tc.want {
				t.Fatalf("bootstrap.incomplete raised = %v; want %v", got, tc.want)
			}
		})
	}
}

// The variables are read from the environment and nowhere else.
func TestTheBootstrapVariablesAreReadFromTheEnvironment(t *testing.T) {
	t.Setenv("ZOOMIES_BOOTSTRAP_ADMIN", " ops ")
	t.Setenv("ZOOMIES_BOOTSTRAP_TOKEN_FILE", "/run/secrets/tok")
	c := Default()
	if err := c.applyEnv(); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}
	if c.Bootstrap.Admin != "ops" || c.Bootstrap.TokenFile != "/run/secrets/tok" || c.Bootstrap.PasswordFile != "" {
		t.Fatalf("read %+v", c.Bootstrap)
	}
}

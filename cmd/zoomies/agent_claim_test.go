package main

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
)

func TestAnAgentThatClaimsItsCredentialIsNamedAfterItsHostname(t *testing.T) {
	for name, tc := range map[string]struct{ token, configured, want string }{
		"a claim takes the hostname":                     {agent.ClaimToken, "zoomies-2vcpu-4gb-ubuntu-2404-build", "zoomies-mach-abc"},
		"a claim takes it over a configured name":        {" " + agent.ClaimToken + "\n", "builder-07", "zoomies-mach-abc"},
		"a real token keeps the name it was given":       {"zoojoin_abc", "builder-07", "builder-07"},
		"no token keeps the name, as a joined host does": {"", "builder-07", "builder-07"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := claimedName(tc.token, tc.configured, "zoomies-mach-abc"); got != tc.want {
				t.Errorf("claimedName = %q, want %q", got, tc.want)
			}
		})
	}
}

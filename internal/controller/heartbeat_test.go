package controller

import (
	"testing"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// The validator judges an operator's deadlines against constants of its own,
// because config sits below the store and the agent and cannot import either.
// Every one of those restatements is a number that can drift away from the
// thing it describes without anything failing, so this is what holds them.
//
// The pair that matters most is the two silences. A host is unhealthy after
// ninety seconds, which only stops new runners being placed on it, and lost
// after five minutes, which fails the runners already there. A deadline judged
// against the wrong one of those is not off by a little: provider.delete_grace
// measured against the ninety seconds accepted a grace that destroys a machine
// while the fleet still believes a job is running on it.
func TestTheValidatorAgreesWithWhatItIsJudgingAgainst(t *testing.T) {
	for _, c := range []struct {
		what        string
		restated    any
		source      any
		consequence string
	}{
		{"config.HostUnhealthyAfter", config.HostUnhealthyAfter, store.HeartbeatTimeout,
			"the heartbeat warning would name the wrong silence"},
		{"config.RunnersLostAfter", config.RunnersLostAfter, hostLostAfter,
			"provider.delete_grace and provider.idle_timeout would be judged against a silence that frees nothing"},
		{"config.RunnerCreateBudget", config.RunnerCreateBudget, agent.CreateTimeout,
			"the provision timeout would be judged against a create budget the agent does not keep"},
	} {
		if c.restated != c.source {
			t.Errorf("%s = %s but its source is %s; %s", c.what, c.restated, c.source, c.consequence)
		}
	}
}

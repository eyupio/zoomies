package config

import (
	"strconv"
	"testing"
)

// limitFields is every ceiling, with the environment variable that overrides
// it and a way to set it, so each test below covers all five rather than the
// one somebody remembered.
var limitFields = []struct {
	key, env string
	set      func(c *Config, n int)
	get      func(c *Config) int
}{
	{"limits.hosts", "ZOOMIES_LIMITS_HOSTS", func(c *Config, n int) { c.Limits.Hosts = n }, func(c *Config) int { return c.Limits.Hosts }},
	{"limits.pools", "ZOOMIES_LIMITS_POOLS", func(c *Config, n int) { c.Limits.Pools = n }, func(c *Config) int { return c.Limits.Pools }},
	{"limits.runners", "ZOOMIES_LIMITS_RUNNERS", func(c *Config, n int) { c.Limits.Runners = n }, func(c *Config) int { return c.Limits.Runners }},
	{"limits.join_tokens", "ZOOMIES_LIMITS_JOIN_TOKENS", func(c *Config, n int) { c.Limits.JoinTokens = n }, func(c *Config) int { return c.Limits.JoinTokens }},
	{"limits.event_subscribers", "ZOOMIES_LIMITS_EVENT_SUBSCRIBERS", func(c *Config, n int) { c.Limits.EventSubscribers = n }, func(c *Config) int { return c.Limits.EventSubscribers }},
}

func findingFor(fs []Finding, code, setting string) *Finding {
	for i := range fs {
		if fs[i].Code == code && fs[i].Setting == setting {
			return &fs[i]
		}
	}
	return nil
}

// Zero is unlimited, and unlimited is the default: a ceiling nobody chose
// must never be the reason a fleet stops growing.
func TestEveryLimitIsUnlimitedByDefault(t *testing.T) {
	c := Default()
	for _, l := range limitFields {
		if got := l.get(c); got != 0 {
			t.Errorf("%s defaults to %d, want 0", l.key, got)
		}
	}
}

// A ceiling on a controller nobody else can reach protects nothing and can
// only refuse the operator, so it is a warning there -- and silence on a
// reachable one, where it is doing its job.
func TestALimitOnALoopbackBindIsAWarningAndOnAReachableOneIsNot(t *testing.T) {
	for _, l := range limitFields {
		t.Run(l.key, func(t *testing.T) {
			c := Default()
			c.Server.Bind = "127.0.0.1:8080"
			l.set(c, 1)
			f := findingFor(c.Validate(), "limits.loopback", l.key)
			if f == nil {
				t.Fatalf("%s set on a loopback bind drew no limits.loopback finding", l.key)
			}
			if f.Severity != SeverityWarning {
				t.Errorf("severity = %s, want warning", f.Severity)
			}

			c.Server.ExternalURL = "https://zoomies.example.com"
			if findingFor(c.Validate(), "limits.loopback", l.key) != nil {
				t.Errorf("%s drew limits.loopback on a controller with an external URL", l.key)
			}
		})
	}
}

func TestANegativeLimitIsRefused(t *testing.T) {
	for _, l := range limitFields {
		t.Run(l.key, func(t *testing.T) {
			c := Default()
			l.set(c, -1)
			f := findingFor(c.Validate(), "limits.negative", l.key)
			if f == nil || f.Severity != SeverityError {
				t.Fatalf("a negative %s was not refused: %+v", l.key, f)
			}
		})
	}
}

func TestEveryLimitHasAnEnvironmentOverride(t *testing.T) {
	for i, l := range limitFields {
		t.Run(l.key, func(t *testing.T) {
			want := 10 + i
			t.Setenv(l.env, strconv.Itoa(want))
			c := Default()
			if err := c.applyEnv(); err != nil {
				t.Fatalf("applyEnv: %v", err)
			}
			if got := l.get(c); got != want {
				t.Errorf("%s=%d left %s at %d", l.env, want, l.key, got)
			}
			s, ok := LookupSetting(l.key)
			if !ok || s.Scope != ScopePlatform {
				t.Errorf("%s is not a platform-scoped setting: %+v", l.key, s)
			}
		})
	}
}

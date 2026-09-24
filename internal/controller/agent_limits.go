package controller

import (
	"fmt"
	"sync"
	"time"

	"github.com/eyupio/zoomies/internal/agent"
	"github.com/eyupio/zoomies/internal/auth"
)

// agentCallsPerSecond sizes the per-host budget for heartbeats, results and
// reports: this many calls per second of agent.heartbeat_interval, and never
// fewer than minAgentCallsPerInterval in one interval.
//
// A well-behaved agent sends one heartbeat an interval, a result per task and
// a report when a runner changes, so even a host that starts and removes a
// full complement of runners in one interval spends a small fraction of it.
// What the budget is for is the agent that is not well behaved -- a bug in a
// retry loop, or a stolen token -- whose every call is a store write that
// queues behind the single writer every other host is also waiting for.
const (
	agentCallsPerSecond      = 20
	minAgentCallsPerInterval = 200
)

// Limit labels on zoomies_agent_requests_limited_total.
const (
	limitRate    = "rate"
	limitPoll    = "poll"
	limitRunners = "runners"
)

// ErrReportTooLarge is a heartbeat or runner report carrying more runners than
// agent.MaxRunnersPerReport. The API answers it with a 413.
var ErrReportTooLarge = fmt.Errorf("controller: a report carried more than %d runners", agent.MaxRunnersPerReport)

// agentLimiter is the per-host budget. It is rebuilt when the heartbeat
// interval changes, because the interval is a live setting and a budget sized
// for thirty seconds would be the wrong shape at five.
type agentLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	limiter  *auth.RateLimiter
}

// agentCallBudget returns how many calls one host may make in one interval.
func agentCallBudget(interval time.Duration) int {
	return max(minAgentCallsPerInterval, int(interval/time.Second)*agentCallsPerSecond)
}

// AllowAgentCall spends one call from a host's budget for heartbeats, results
// and reports. When it is refused, the duration says how long until the next
// call would be accepted, for a Retry-After header.
func (c *Controller) AllowAgentCall(hostID string) (bool, time.Duration) {
	interval := c.heartbeatInterval()
	c.agentCalls.mu.Lock()
	if c.agentCalls.limiter == nil || c.agentCalls.interval != interval {
		c.agentCalls.interval = interval
		c.agentCalls.limiter = auth.NewRateLimiter(agentCallBudget(interval), interval, c.Now)
	}
	l := c.agentCalls.limiter
	c.agentCalls.mu.Unlock()

	if l.Allow(hostID) {
		return true, 0
	}
	c.metrics.agentLimited.WithLabelValues(limitRate).Inc()
	return false, l.RetryAfter(hostID)
}

// checkReportSize refuses a report that carries more runners than any host
// could be running.
func (c *Controller) checkReportSize(n int) error {
	if n <= agent.MaxRunnersPerReport {
		return nil
	}
	c.metrics.agentLimited.WithLabelValues(limitRunners).Inc()
	return fmt.Errorf("%w (it carried %d); a host cannot be running that many, so the agent is misbehaving -- check its log and version", ErrReportTooLarge, n)
}

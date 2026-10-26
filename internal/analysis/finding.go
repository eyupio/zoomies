package analysis

import (
	"fmt"
	"time"
)

// Confidence says how far a reader may lean on a finding. Anything short of
// high carries its reason, so a reader is never left to guess what was thin.
type Confidence string

const (
	High   Confidence = "high"
	Medium Confidence = "medium"
	Low    Confidence = "low"
)

// NextStep is never a command, and names a setting or a catalog code where
// one applies: the catalog's own rule, because a pasted command skips the
// reading that says whether it applies. Some steps rightly name neither, such
// as GitHub's own delay in handing a job over, or the step that says job names
// were withheld, and are said in their text alone.
type NextStep struct {
	Text string `json:"text"`
	// Setting is a settings key, or a pool or host field as "pool.max_runners".
	Setting string `json:"setting,omitempty"`
	// Code is a catalog code.
	Code string `json:"code,omitempty"`
	// Link is the code's catalog page. The controller fills it after the
	// analysis returns: where the docs are published is not the analysis's
	// to know, and the UI has no other way to turn a code into an address.
	Link string `json:"link,omitempty"`
}

// Finding is the one shape every analysis answers in, so the route, the MCP
// tool, Eli, the CLI and the UI card render the same answer and cannot drift
// on a field name.
type Finding[E any] struct {
	// Analysis is queue_wait, compare, host_pressure or startup_failures.
	Analysis string `json:"analysis"`
	// Version is queue-wait-v1, compare-v1, host-pressure-v1 or
	// startup-failures-v1; a reader refuses a version it does not know rather
	// than misreading its evidence.
	Version string    `json:"version"`
	Since   time.Time `json:"since"`
	Until   time.Time `json:"until"`
	Summary string    `json:"summary"`
	// Dominant is what the summary leads with; "" when nothing does.
	Dominant   string     `json:"dominant"`
	Confidence Confidence `json:"confidence"`
	// ConfidenceReason is set exactly when Confidence is not High.
	ConfidenceReason string `json:"confidence_reason,omitempty"`
	// NextSteps and Codes are never nil, so they marshal as [] and a reader
	// need not tell "none" from "missing".
	NextSteps []NextStep `json:"next_steps"`
	Codes     []string   `json:"codes"`
	Evidence  E          `json:"evidence"`
}

// Interval is a half-open span of time, [From, To).
type Interval struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// MaxReadJobs is the most jobs or runners the store reads for one finding.
// A busy period holds more; the finding then says the read was cut rather
// than ranking a part as if it were the whole.
const MaxReadJobs = 5000

// Words says a duration the way a person would: "3h 10m", "12m 5s", "45s".
// Two units are enough to compare causes by, and a negative duration (two
// clocks disagreeing) reads as none rather than as a minus sign.
func Words(d time.Duration) string {
	s := int64(d.Round(time.Second) / time.Second)
	if s <= 0 {
		return "0s"
	}
	h, m, sec := s/3600, s%3600/60, s%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && sec > 0:
		return fmt.Sprintf("%dm %ds", m, sec)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", sec)
}

// CatalogCodes is every code any analysis can name, so one test can prove
// each has a catalog page. Each analysis adds its own table here.
func CatalogCodes() []string {
	var out []string
	for _, table := range [][]string{queueWaitCodes, compareCodes, hostPressureCodes, startupFailuresCodes, recurringFailuresCodes} {
		out = append(out, table...)
	}
	return out
}

// nonNil keeps an empty list an empty list on the wire.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

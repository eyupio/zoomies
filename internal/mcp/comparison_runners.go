package mcp

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
)

// These are independent REST whitelists, not controller/store types. New
// upstream fields must not become model input merely because a server adds them.
type comparisonRunnerEvidence struct {
	Version   string               `json:"version"`
	Baseline  *comparisonRunnerArm `json:"baseline"`
	Candidate *comparisonRunnerArm `json:"candidate"`
}
type comparisonRunnerArm struct {
	SelectedJobs       *int                  `json:"selected_jobs"`
	MatchedJobs        *int                  `json:"matched_jobs"`
	RecordedGrants     *int                  `json:"recorded_grants"`
	RunnerObservations *int                  `json:"runner_observations"`
	RowsOmitted        *bool                 `json:"rows_omitted"`
	Shown              int                   `json:"shown"`
	Jobs               []comparisonRunnerJob `json:"jobs"`
}
type comparisonRunnerJob struct {
	RunID         string                       `json:"run_id"`
	Attempt       int                          `json:"attempt"`
	GitHubJobID   string                       `json:"github_job_id"`
	FleetMatched  *bool                        `json:"fleet_matched"`
	RecordedGrant *comparisonRunnerGrant       `json:"recorded_grant"`
	Observation   *comparisonRunnerObservation `json:"observation"`
}
type comparisonRunnerGrant struct {
	Basis    string   `json:"basis"`
	Source   string   `json:"source"`
	CPUs     *float64 `json:"cpus"`
	MemoryMB *int64   `json:"memory_mb"`
}
type comparisonRunnerObservation struct {
	SHA256                  string                   `json:"sha256"`
	IssuedAt                time.Time                `json:"issued_at"`
	ReceivedAt              time.Time                `json:"received_at"`
	Requested               *comparisonRunnerRequest `json:"requested"`
	ImageDigest             *string                  `json:"image_digest"`
	DigestCoverage          string                   `json:"digest_coverage"`
	AgentCompletedAt        *time.Time               `json:"agent_completed_at"`
	AgentContainerStartedAt *time.Time               `json:"agent_container_started_at"`
}
type comparisonRunnerRequest struct {
	Backend              string   `json:"backend"`
	ImageReferenceSHA256 string   `json:"image_reference_sha256"`
	CPUs                 *float64 `json:"cpus"`
	MemoryMB             *int64   `json:"memory_mb"`
	ResourceSource       string   `json:"resource_source"`
	DockerMode           string   `json:"docker_mode"`
	CPUTyped             *bool    `json:"cpu_typed"`
	MemoryTyped          *bool    `json:"memory_typed"`
}

func validateComparisonRunners(e *comparisonRunnerEvidence, a, b analysis.ExperimentArm) bool {
	if e == nil {
		return true
	} // Older controllers provide no runner facts.
	if e.Version != analysis.RunnerEvidenceVersion || e.Baseline == nil || e.Candidate == nil || len(a.Attempts) > analysis.MaxEvidenceRows || len(b.Attempts) > analysis.MaxEvidenceRows {
		return false
	}
	seen := map[string]bool{}
	for i, side := range []*comparisonRunnerArm{e.Baseline, e.Candidate} {
		arm := a
		if i == 1 {
			arm = b
		}
		if side.SelectedJobs == nil || side.MatchedJobs == nil || side.RecordedGrants == nil || side.RunnerObservations == nil || side.RowsOmitted == nil || side.Jobs == nil || len(side.Jobs) > analysis.MaxEvidenceRows {
			return false
		}
		n, matched, grants, observations := *side.SelectedJobs, *side.MatchedJobs, *side.RecordedGrants, *side.RunnerObservations
		if n < len(side.Jobs) || n > len(arm.Attempts)*analysis.MaxJobsPerAttempt || matched < 0 || matched > n || grants < 0 || grants > matched || observations < 0 || observations > matched || *side.RowsOmitted != (n > len(side.Jobs)) {
			return false
		}
		attempts := map[string]bool{}
		for _, attempt := range arm.Attempts {
			attempts[strconv.FormatInt(attempt.RunID, 10)+":"+strconv.Itoa(attempt.Attempt)] = true
		}
		shownMatches, shownGrants, shownObservations := 0, 0, 0
		for _, job := range side.Jobs {
			if !analysis.DecimalID(job.RunID) || !analysis.DecimalID(job.GitHubJobID) || job.Attempt <= 0 || !attempts[job.RunID+":"+strconv.Itoa(job.Attempt)] || seen[job.GitHubJobID] || job.FleetMatched == nil {
				return false
			}
			seen[job.GitHubJobID] = true
			if *job.FleetMatched {
				shownMatches++
			}
			if job.RecordedGrant != nil {
				g := job.RecordedGrant
				if !*job.FleetMatched || g.Basis != "retained_job_granted_size_v1" || !comparisonGrantSource(g.Source) || !comparisonDimensions(g.CPUs, g.MemoryMB) || g.CPUs == nil && g.MemoryMB == nil {
					return false
				}
				shownGrants++
			}
			if job.Observation != nil {
				if !*job.FleetMatched || !validComparisonRunnerObservation(job.Observation) {
					return false
				}
				shownObservations++
			}
		}
		missing := n - len(side.Jobs)
		for _, pair := range [][2]int{{matched, shownMatches}, {grants, shownGrants}, {observations, shownObservations}} {
			if pair[0] < pair[1] || pair[0]-pair[1] > missing {
				return false
			}
		}
		// Ignore any upstream shown field: only this projection chooses its rows.
		side.Shown = len(side.Jobs)
	}
	return true
}
func comparisonGrantSource(s string) bool {
	switch s {
	case "pool", "host", "profile", "reduced", "history":
		return true
	}
	return false
}
func comparisonDimensions(cpu *float64, mem *int64) bool {
	return (cpu == nil || *cpu > 0 && !math.IsInf(*cpu, 0) && !math.IsNaN(*cpu)) && (mem == nil || *mem > 0)
}
func validComparisonRunnerObservation(o *comparisonRunnerObservation) bool {
	r := o.Requested
	if !analysis.LowerHex(o.SHA256, 64) || o.IssuedAt.IsZero() || o.ReceivedAt.IsZero() || r == nil || r.CPUTyped == nil || r.MemoryTyped == nil || !comparisonDimensions(r.CPUs, r.MemoryMB) || r.ImageReferenceSHA256 != "" && !analysis.LowerHex(r.ImageReferenceSHA256, 64) || r.ResourceSource != "" && !comparisonGrantSource(r.ResourceSource) {
		return false
	}
	switch r.Backend {
	case "docker", "podman", "process":
	default:
		return false
	}
	switch r.DockerMode {
	case "", "none", "dind", "host-socket":
	default:
		return false
	}
	for _, at := range []*time.Time{o.AgentCompletedAt, o.AgentContainerStartedAt} {
		if at != nil && at.IsZero() {
			return false
		}
	}
	switch o.DigestCoverage {
	case "reported":
		return o.ImageDigest != nil && strings.HasPrefix(*o.ImageDigest, "sha256:") && analysis.LowerHex(strings.TrimPrefix(*o.ImageDigest, "sha256:"), 64)
	case "not_reported", "unsupported":
		return o.ImageDigest == nil
	default:
		return false
	}
}

// Retain at least one row of each available kind per arm when the byte budget
// permits. Whole-row removal cannot turn a truncated cohort into a smaller one.
func trimComparisonRow(p *comparisonProjection) bool {
	type group struct {
		n    int
		drop func()
	}
	groups := []group{}
	if p.RunnerEvidence != nil {
		for _, side := range []*comparisonRunnerArm{p.RunnerEvidence.Baseline, p.RunnerEvidence.Candidate} {
			groups = append(groups, group{len(side.Jobs), func() {
				side.Jobs = side.Jobs[:len(side.Jobs)-1]
				side.Shown = len(side.Jobs)
				*side.RowsOmitted = true
			}})
		}
	}
	if p.WorkflowEvidence != nil {
		for _, side := range []*comparisonWorkflowArm{p.WorkflowEvidence.Baseline, p.WorkflowEvidence.Candidate} {
			groups = append(groups, group{len(side.Attempts), func() {
				side.Attempts = side.Attempts[:len(side.Attempts)-1]
				side.Shown = len(side.Attempts)
				*side.RowsOmitted = true
			}})
		}
	}
	groups = append(groups, group{len(p.Baseline.Attempts), func() { p.Baseline.Attempts = p.Baseline.Attempts[:len(p.Baseline.Attempts)-1] }}, group{len(p.Candidate.Attempts), func() { p.Candidate.Attempts = p.Candidate.Attempts[:len(p.Candidate.Attempts)-1] }})
	chosen := -1
	for i, g := range groups {
		if g.n > 1 && (chosen < 0 || g.n > groups[chosen].n) {
			chosen = i
		}
	}
	if chosen < 0 {
		for i, g := range groups {
			if g.n > 0 {
				chosen = i
				break
			}
		}
	}
	if chosen < 0 {
		return false
	}
	groups[chosen].drop()
	p.RowsOmitted = true
	p.BaselineShown = len(p.Baseline.Attempts)
	p.CandidateShown = len(p.Candidate.Attempts)
	return true
}

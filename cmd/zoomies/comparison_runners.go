package main

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/analysis"
)

// Human output has an explicit REST whitelist. Raw structured output is handled
// before this decoder, so future server fields and exact numbers survive intact.
type cliRunnerEvidence struct {
	Version   string        `json:"version"`
	Baseline  *cliRunnerArm `json:"baseline"`
	Candidate *cliRunnerArm `json:"candidate"`
}
type cliRunnerArm struct {
	SelectedJobs       *int           `json:"selected_jobs"`
	MatchedJobs        *int           `json:"matched_jobs"`
	RecordedGrants     *int           `json:"recorded_grants"`
	RunnerObservations *int           `json:"runner_observations"`
	RowsOmitted        *bool          `json:"rows_omitted"`
	Jobs               []cliRunnerJob `json:"jobs"`
}
type cliRunnerJob struct {
	RunID         string                `json:"run_id"`
	Attempt       int                   `json:"attempt"`
	GitHubJobID   string                `json:"github_job_id"`
	FleetMatched  *bool                 `json:"fleet_matched"`
	RecordedGrant *cliRunnerGrant       `json:"recorded_grant"`
	Observation   *cliRunnerObservation `json:"observation"`
}
type cliRunnerGrant struct {
	Basis    string   `json:"basis"`
	Source   string   `json:"source"`
	CPUs     *float64 `json:"cpus"`
	MemoryMB *int64   `json:"memory_mb"`
}
type cliRunnerObservation struct {
	SHA256                  string            `json:"sha256"`
	IssuedAt                time.Time         `json:"issued_at"`
	ReceivedAt              time.Time         `json:"received_at"`
	Requested               *cliRunnerRequest `json:"requested"`
	ImageDigest             *string           `json:"image_digest"`
	DigestCoverage          string            `json:"digest_coverage"`
	AgentCompletedAt        *time.Time        `json:"agent_completed_at"`
	AgentContainerStartedAt *time.Time        `json:"agent_container_started_at"`
}
type cliRunnerRequest struct {
	Backend              string   `json:"backend"`
	ImageReferenceSHA256 string   `json:"image_reference_sha256"`
	CPUs                 *float64 `json:"cpus"`
	MemoryMB             *int64   `json:"memory_mb"`
	ResourceSource       string   `json:"resource_source"`
	DockerMode           string   `json:"docker_mode"`
	CPUTyped             *bool    `json:"cpu_typed"`
	MemoryTyped          *bool    `json:"memory_typed"`
}

var errCLIRunnerEvidence = errors.New("this controller returned unsupported or inconsistent runner evidence; inspect --output json and check the controller version")

func parseCLIRunnerEvidence(raw json.RawMessage, a, b analysis.ExperimentArm) (*cliRunnerEvidence, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var e cliRunnerEvidence
	if json.Unmarshal(raw, &e) != nil || e.Version != analysis.RunnerEvidenceVersion || e.Baseline == nil || e.Candidate == nil || len(a.Attempts) > analysis.MaxEvidenceRows || len(b.Attempts) > analysis.MaxEvidenceRows {
		return nil, errCLIRunnerEvidence
	}
	seen := map[string]bool{}
	for i, side := range []*cliRunnerArm{e.Baseline, e.Candidate} {
		arm := a
		if i == 1 {
			arm = b
		}
		if side.SelectedJobs == nil || side.MatchedJobs == nil || side.RecordedGrants == nil || side.RunnerObservations == nil || side.RowsOmitted == nil || side.Jobs == nil || len(side.Jobs) > analysis.MaxEvidenceRows {
			return nil, errCLIRunnerEvidence
		}
		n, m, g, o := *side.SelectedJobs, *side.MatchedJobs, *side.RecordedGrants, *side.RunnerObservations
		if n < len(side.Jobs) || n > len(arm.Attempts)*analysis.MaxJobsPerAttempt || m < 0 || m > n || g < 0 || g > m || o < 0 || o > m || *side.RowsOmitted != (n > len(side.Jobs)) {
			return nil, errCLIRunnerEvidence
		}
		attempts := map[string]bool{}
		for _, v := range arm.Attempts {
			attempts[strconv.FormatInt(v.RunID, 10)+":"+strconv.Itoa(v.Attempt)] = true
		}
		matches, grants, observations := 0, 0, 0
		for _, j := range side.Jobs {
			id, err := strconv.ParseInt(j.GitHubJobID, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != j.GitHubJobID || !attempts[j.RunID+":"+strconv.Itoa(j.Attempt)] || seen[j.GitHubJobID] || j.FleetMatched == nil {
				return nil, errCLIRunnerEvidence
			}
			seen[j.GitHubJobID] = true
			if *j.FleetMatched {
				matches++
			}
			if v := j.RecordedGrant; v != nil {
				if !*j.FleetMatched || v.Basis != "retained_job_granted_size_v1" || !cliRunnerSource(v.Source) || !cliRunnerSizeValid(v.CPUs, v.MemoryMB) || v.CPUs == nil && v.MemoryMB == nil {
					return nil, errCLIRunnerEvidence
				}
				grants++
			}
			if j.Observation != nil {
				if !*j.FleetMatched || !cliRunnerObservationValid(j.Observation) {
					return nil, errCLIRunnerEvidence
				}
				observations++
			}
		}
		for _, pair := range [][2]int{{m, matches}, {g, grants}, {o, observations}} {
			if pair[0] < pair[1] || pair[0]-pair[1] > n-len(side.Jobs) {
				return nil, errCLIRunnerEvidence
			}
		}
	}
	return &e, nil
}
func cliRunnerSource(s string) bool {
	switch s {
	case "pool", "host", "profile", "reduced", "history":
		return true
	}
	return false
}
func cliRunnerSizeValid(c *float64, m *int64) bool {
	return (c == nil || *c > 0 && !math.IsNaN(*c) && !math.IsInf(*c, 0)) && (m == nil || *m > 0)
}
func cliRunnerObservationValid(o *cliRunnerObservation) bool {
	r := o.Requested
	if r == nil || r.CPUTyped == nil || r.MemoryTyped == nil || o.IssuedAt.IsZero() || o.ReceivedAt.IsZero() || !analysis.LowerHex(o.SHA256, 64) || !cliRunnerSizeValid(r.CPUs, r.MemoryMB) || r.ImageReferenceSHA256 != "" && !analysis.LowerHex(r.ImageReferenceSHA256, 64) || r.ResourceSource != "" && !cliRunnerSource(r.ResourceSource) {
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
	case "unsupported", "not_reported":
		return o.ImageDigest == nil
	}
	return false
}

func printCLIRunnerEvidence(p *printer, e *cliRunnerEvidence) {
	if e == nil {
		p.note("Runner evidence is unavailable from this controller; missing facts remain unknown.")
		return
	}
	p.note("Runner evidence: grants are controller assignment records; requested resources describe dispatch; agent digest/times are unverified reports. These facts do not attest applied limits or image/configuration equivalence.")
	for i, a := range []*cliRunnerArm{e.Baseline, e.Candidate} {
		name := []string{"baseline", "candidate"}[i]
		p.note("%s runner coverage: %d enumerated jobs, %d fleet matches, %d recorded grants, %d observations; showing %d jobs. Counts include omitted rows; enumeration may be partial.", name, *a.SelectedJobs, *a.MatchedJobs, *a.RecordedGrants, *a.RunnerObservations, len(a.Jobs))
		if *a.RowsOmitted {
			p.note("%s: additional job rows were omitted by the controller; no replacement evidence was collected.", name)
		}
		rows := [][]string{}
		for _, j := range a.Jobs {
			grant, request, claim := "not recorded", "no retained observation", "unknown"
			if g := j.RecordedGrant; g != nil {
				grant = cliRunnerSize(g.CPUs, g.MemoryMB) + " (" + g.Source + ")"
			}
			if o := j.Observation; o != nil {
				request = cliRunnerSize(o.Requested.CPUs, o.Requested.MemoryMB) + " (" + o.Requested.Backend + ")"
				claim = o.DigestCoverage
			}
			rows = append(rows, []string{j.RunID, strconv.Itoa(j.Attempt), j.GitHubJobID, strconv.FormatBool(*j.FleetMatched), grant, request, claim})
		}
		p.table([]string{"run", "attempt", "GitHub job", "fleet matched", "recorded grant", "requested resources", "agent digest coverage"}, rows)
		for _, j := range a.Jobs {
			if o := j.Observation; o != nil {
				p.note("%s job %s observation details:", name, j.GitHubJobID)
				digest := "unknown"
				if o.ImageDigest != nil {
					digest = *o.ImageDigest
				}
				p.keyValues([][2]string{{"requested resource source", cliRunnerUnknown(o.Requested.ResourceSource)}, {"requested Docker mode", cliRunnerUnknown(o.Requested.DockerMode)}, {"CPU explicitly typed in request", strconv.FormatBool(*o.Requested.CPUTyped)}, {"memory explicitly typed in request", strconv.FormatBool(*o.Requested.MemoryTyped)}, {"requested image reference fingerprint", cliRunnerUnknown(o.Requested.ImageReferenceSHA256)}, {"agent-reported image digest", digest}, {"dispatched at (controller)", o.IssuedAt.Format(time.RFC3339Nano)}, {"received at (controller)", o.ReceivedAt.Format(time.RFC3339Nano)}, {"completed at (agent)", cliRunnerTime(o.AgentCompletedAt)}, {"container started at (agent)", cliRunnerTime(o.AgentContainerStartedAt)}, {"observation digest", o.SHA256}})
			}
		}
	}
}
func cliRunnerUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return plain(s)
}
func cliRunnerTime(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.Format(time.RFC3339Nano)
}
func cliRunnerSize(c *float64, m *int64) string {
	cpu, mem := "unknown", "unknown"
	if c != nil {
		cpu = strconv.FormatFloat(*c, 'g', -1, 64)
	}
	if m != nil {
		mem = strconv.FormatInt(*m, 10)
	}
	return cpu + " CPUs / " + mem + " MB"
}

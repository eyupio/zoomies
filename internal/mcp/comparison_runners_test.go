package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func comparisonRunnerPointer[T any](v T) *T { return &v }
func comparisonRunnerFixture(n int, cut bool) *comparisonRunnerEvidence {
	arm := func(offset int) *comparisonRunnerArm {
		total := n
		if cut {
			total += 7
		}
		a := &comparisonRunnerArm{SelectedJobs: comparisonRunnerPointer(total), MatchedJobs: comparisonRunnerPointer(total), RecordedGrants: comparisonRunnerPointer(total), RunnerObservations: comparisonRunnerPointer(total), RowsOmitted: comparisonRunnerPointer(cut), Shown: 999, Jobs: []comparisonRunnerJob{}}
		for i := range n {
			a.Jobs = append(a.Jobs, comparisonRunnerJob{RunID: strconv.Itoa(offset + i + 1), Attempt: 1, GitHubJobID: strconv.Itoa(10000 + offset + i), FleetMatched: comparisonRunnerPointer(true), RecordedGrant: &comparisonRunnerGrant{Basis: "retained_job_granted_size_v1", Source: "pool", CPUs: comparisonRunnerPointer(1.0)}, Observation: &comparisonRunnerObservation{SHA256: strings.Repeat("a", 64), IssuedAt: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), ReceivedAt: time.Date(2026, 10, 13, 0, 2, 0, 0, time.UTC), Requested: &comparisonRunnerRequest{Backend: "docker", CPUs: comparisonRunnerPointer(2.0), MemoryMB: comparisonRunnerPointer(int64(4096)), DockerMode: "dind", ImageReferenceSHA256: strings.Repeat("b", 64), CPUTyped: comparisonRunnerPointer(false), MemoryTyped: comparisonRunnerPointer(false)}, DigestCoverage: "reported", ImageDigest: comparisonRunnerPointer("sha256:" + strings.Repeat("c", 64))}})
		}
		return a
	}
	return &comparisonRunnerEvidence{Version: "runner-evidence-v1", Baseline: arm(0), Candidate: arm(n)}
}
func comparisonRunnerBody(t *testing.T, n int, e *comparisonRunnerEvidence) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(comparisonFixture(t, n)), &doc); err != nil {
		t.Fatal(err)
	}
	doc["runner_evidence"] = e
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRetainedRunnerFactsKeepRequestsAndClaimsSeparateAndExcludeFutureFields(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(comparisonRunnerBody(t, 1, comparisonRunnerFixture(1, false))), &doc); err != nil {
		t.Fatal(err)
	}
	e := doc["runner_evidence"].(map[string]any)
	b := e["baseline"].(map[string]any)
	j := b["jobs"].([]any)[0].(map[string]any)
	o := j["observation"].(map[string]any)
	r := o["requested"].(map[string]any)
	for _, object := range []map[string]any{e, b, j, o, r, j["recorded_grant"].(map[string]any)} {
		object["future_secret"] = "withheld-runner-fact"
		object["host_id"] = "private-host"
	}
	j["github_job_id"] = "9223372036854775807"
	j["run_id"] = "9007199254740993"
	doc["report"].(map[string]any)["snapshot"].(map[string]any)["baseline"].(map[string]any)["attempts"].([]any)[0].(map[string]any)["run_id"] = json.Number("9007199254740993")
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	api := &kennelAPI{body: string(body)}
	content, value, err := kennelTool(t, "get_retained_comparison").run(t.Context(), api, json.RawMessage(comparisonArgs))
	if err != nil {
		t.Fatal(err)
	}
	p := value.(comparisonProjection)
	row := p.RunnerEvidence.Baseline.Jobs[0]
	if api.calls != 1 || p.RunnerEvidence.Baseline.Shown != 1 || row.RunID != "9007199254740993" || row.GitHubJobID != "9223372036854775807" || *row.RecordedGrant.CPUs != 1 || *row.Observation.Requested.CPUs != 2 || row.RecordedGrant.MemoryMB != nil || row.Observation.AgentCompletedAt != nil || p.Measurement.Eligibility.State != "inconclusive" {
		t.Fatal("changed descriptive facts or eligibility")
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != content[1].Text || !strings.Contains(content[0].Text, "unverified reports") || !strings.Contains(content[1].Text, `"github_job_id":"9223372036854775807"`) {
		t.Fatal("different or unlabelled model evidence")
	}
	for _, private := range []string{"future_secret", "withheld-runner-fact", "private-host", "host_id", "private-actor", "future_private"} {
		if strings.Contains(string(encoded), private) {
			t.Errorf("forwarded %s", private)
		}
	}
}

func TestRetainedRunnerFactsFitBothModelBudgetsWithoutChangingCoverageCounts(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{12 << 10, 64 << 10} {
		api := &kennelAPI{body: comparisonRunnerBody(t, 100, comparisonRunnerFixture(100, true))}
		ctx := context.WithValue(t.Context(), windowKey{}, windowLimits{block: limit})
		content, value, err := kennelTool(t, "get_retained_comparison").run(ctx, api, json.RawMessage(comparisonArgs))
		if err != nil {
			t.Fatal(err)
		}
		p := value.(comparisonProjection)
		for _, arm := range []*comparisonRunnerArm{p.RunnerEvidence.Baseline, p.RunnerEvidence.Candidate} {
			if *arm.SelectedJobs != 107 || *arm.RecordedGrants != 107 || *arm.RunnerObservations != 107 || !*arm.RowsOmitted || arm.Shown != len(arm.Jobs) || arm.Shown < 1 {
				t.Fatal("trimmed coverage counts or lost both kinds of evidence")
			}
		}
		if !p.RowsOmitted || p.Measurement.Eligibility.Baseline.Selected != 100 || p.BaselineShown != len(p.Baseline.Attempts) || p.BaselineShown < 1 || p.CandidateShown < 1 {
			t.Fatal("trimmed cohort aggregates or identity rows")
		}
		encoded, _ := json.Marshal(value)
		if len(content[0].Text)+len(content[1].Text) > limit || string(encoded) != content[1].Text || api.calls != 1 {
			t.Fatal("exceeded budget, changed structured rows or repeated the read")
		}
	}
}

func TestRetainedRunnerFactsRejectUnsupportedContradictoryAndUnboundEvidence(t *testing.T) {
	for _, mode := range []string{"version", "missing_arm", "missing_count", "negative_count", "inflated_count", "under_count", "omission", "missing_jobs", "job_bound", "job_id", "overflow_id", "unbound_run", "wrong_attempt", "duplicate", "unmatched_fact", "grant_basis", "grant_source", "grant_zero", "empty_grant", "request_backend", "request_source", "request_mode", "request_zero", "typed_unknown", "digest_missing", "digest_unsupported", "digest_invalid", "hash", "fingerprint", "issued_clock", "agent_clock"} {
		t.Run(mode, func(t *testing.T) {
			e := comparisonRunnerFixture(1, false)
			a := e.Baseline
			j := &a.Jobs[0]
			o := j.Observation
			switch mode {
			case "version":
				e.Version = "future"
			case "missing_arm":
				e.Candidate = nil
			case "missing_count":
				a.SelectedJobs = nil
			case "negative_count":
				*a.MatchedJobs = -1
			case "inflated_count":
				*a.SelectedJobs = 501
				*a.RowsOmitted = true
			case "under_count":
				*a.RecordedGrants = 0
			case "omission":
				*a.RowsOmitted = true
			case "missing_jobs":
				a.Jobs = nil
			case "job_bound":
				for range 100 {
					a.Jobs = append(a.Jobs, *j)
				}
			case "job_id":
				j.GitHubJobID = "001"
			case "overflow_id":
				j.GitHubJobID = "9223372036854775808"
			case "unbound_run":
				j.RunID = "999"
			case "wrong_attempt":
				j.Attempt = 2
			case "duplicate":
				e.Candidate.Jobs[0].GitHubJobID = j.GitHubJobID
			case "unmatched_fact":
				*j.FleetMatched = false
			case "grant_basis":
				j.RecordedGrant.Basis = "future"
			case "grant_source":
				j.RecordedGrant.Source = hostile
			case "grant_zero":
				*j.RecordedGrant.CPUs = 0
			case "empty_grant":
				j.RecordedGrant.CPUs = nil
			case "request_backend":
				o.Requested.Backend = "future"
			case "request_source":
				o.Requested.ResourceSource = hostile
			case "request_mode":
				o.Requested.DockerMode = "future"
			case "request_zero":
				*o.Requested.MemoryMB = 0
			case "typed_unknown":
				o.Requested.CPUTyped = nil
			case "digest_missing":
				o.ImageDigest = nil
			case "digest_unsupported":
				o.DigestCoverage = "unsupported"
			case "digest_invalid":
				*o.ImageDigest = "private-legacy-digest"
			case "hash":
				o.SHA256 = hostile
			case "fingerprint":
				o.Requested.ImageReferenceSHA256 = "raw-registry-reference"
			case "issued_clock":
				o.IssuedAt = time.Time{}
			case "agent_clock":
				o.AgentCompletedAt = &time.Time{}
			}
			api := &kennelAPI{body: comparisonRunnerBody(t, 1, e)}
			if _, _, err := kennelTool(t, "get_retained_comparison").run(t.Context(), api, json.RawMessage(comparisonArgs)); err == nil || api.calls != 1 {
				t.Fatal("accepted malformed evidence or retried", err)
			}
		})
	}
}

func TestRetainedRunnerFactsPreserveUnsupportedAndAbsentClaimsAsUnknown(t *testing.T) {
	for _, coverage := range []string{"unsupported", "not_reported"} {
		e := comparisonRunnerFixture(1, false)
		for _, a := range []*comparisonRunnerArm{e.Baseline, e.Candidate} {
			a.Jobs[0].Observation.DigestCoverage = coverage
			a.Jobs[0].Observation.ImageDigest = nil
			a.Jobs[0].Observation.Requested.CPUs = nil
			a.Jobs[0].Observation.Requested.MemoryMB = nil
		}
		api := &kennelAPI{body: comparisonRunnerBody(t, 1, e)}
		_, value, err := kennelTool(t, "get_retained_comparison").run(t.Context(), api, json.RawMessage(comparisonArgs))
		if err != nil {
			t.Fatal(err)
		}
		o := value.(comparisonProjection).RunnerEvidence.Baseline.Jobs[0].Observation
		if o.ImageDigest != nil || o.DigestCoverage != coverage || o.Requested.CPUs != nil || o.Requested.MemoryMB != nil {
			t.Fatal("invented missing claims or dimensions")
		}
	}
}

func TestRetainedRunnerFactsRejectLargerAttemptSetsUntilTheirContractIsReviewed(t *testing.T) {
	e := comparisonRunnerFixture(100, false)
	for i := range e.Candidate.Jobs {
		e.Candidate.Jobs[i].RunID = strconv.Itoa(102 + i)
	}
	api := &kennelAPI{body: comparisonRunnerBody(t, 101, e)}
	if _, _, err := kennelTool(t, "get_retained_comparison").run(t.Context(), api, json.RawMessage(comparisonArgs)); err == nil || api.calls != 1 {
		t.Fatal("accepted a larger v1 source attempt set", err)
	}
}

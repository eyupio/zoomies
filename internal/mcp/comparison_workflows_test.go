package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func comparisonWorkflowFixture(n int, cut bool) *comparisonWorkflowEvidence {
	arm := func(offset int) *comparisonWorkflowArm {
		a := &comparisonWorkflowArm{SelectedAttempts: comparisonRunnerPointer(n), RootSources: comparisonRunnerPointer(n), RowsOmitted: comparisonRunnerPointer(cut), Shown: 999, Attempts: []comparisonWorkflowAttempt{}}
		shown := n
		if cut {
			shown--
		}
		for i := range shown {
			a.Attempts = append(a.Attempts, comparisonWorkflowAttempt{RunID: strconv.Itoa(offset + i + 1), Attempt: 1, Event: comparisonRunnerPointer("push"), WorkflowPathSHA256: comparisonRunnerPointer(strings.Repeat("d", 64)), RootSource: &comparisonRootSource{Basis: "push_head_sha_root_file", CommitSHA: strings.Repeat("a", 40), BlobSHA: strings.Repeat("b", 40), ContentSHA256: strings.Repeat("c", 64), Bytes: 128}})
		}
		return a
	}
	return &comparisonWorkflowEvidence{Version: "workflow-evidence-v1", Baseline: arm(0), Candidate: arm(n)}
}
func comparisonWorkflowBody(t *testing.T, n int, e *comparisonWorkflowEvidence, runners bool) map[string]any {
	t.Helper()
	var doc map[string]any
	source := comparisonFixture(t, n)
	if runners {
		source = comparisonRunnerBody(t, n, comparisonRunnerFixture(n, false))
	}
	if err := json.Unmarshal([]byte(source), &doc); err != nil {
		t.Fatal(err)
	}
	for _, arm := range []string{"baseline", "candidate"} {
		for _, v := range doc["report"].(map[string]any)["snapshot"].(map[string]any)[arm].(map[string]any)["attempts"].([]any) {
			v.(map[string]any)["source_revision"] = strings.Repeat("a", 40)
		}
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var view any
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	doc["workflow_evidence"] = view
	return doc
}
func runWorkflowDoc(t *testing.T, ctx context.Context, doc map[string]any) ([]Content, any, error) {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return kennelTool(t, "get_retained_comparison").run(ctx, &kennelAPI{body: string(b)}, json.RawMessage(comparisonArgs))
}

func TestWorkflowFactsExcludePrivateFieldsPreserveExactIDsAndStayDescriptive(t *testing.T) {
	doc := comparisonWorkflowBody(t, 1, comparisonWorkflowFixture(1, false), false)
	e := doc["workflow_evidence"].(map[string]any)
	base := e["baseline"].(map[string]any)
	row := base["attempts"].([]any)[0].(map[string]any)
	source := row["root_source"].(map[string]any)
	for _, v := range []map[string]any{e, base, row, source} {
		v["future_secret"] = "private-extra"
		v["workflow_path"] = "private-flow.yml"
		v["source_text"] = "private-source"
	}
	row["run_id"] = "9223372036854775807"
	doc["report"].(map[string]any)["snapshot"].(map[string]any)["baseline"].(map[string]any)["attempts"].([]any)[0].(map[string]any)["run_id"] = json.Number("9223372036854775807")
	candidate := e["candidate"].(map[string]any)
	candidate["root_sources"] = 0
	unknown := candidate["attempts"].([]any)[0].(map[string]any)
	unknown["root_source"] = nil
	unknown["event"] = nil
	unknown["workflow_path_sha256"] = nil
	content, value, err := runWorkflowDoc(t, t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	p := value.(comparisonProjection)
	if p.WorkflowEvidence.Baseline.Shown != 1 || p.WorkflowEvidence.Baseline.Attempts[0].RunID != "9223372036854775807" || p.WorkflowEvidence.Candidate.Attempts[0].RootSource != nil || p.Measurement.Eligibility.State != "inconclusive" {
		t.Fatal("lost exact, nullable or descriptive facts")
	}
	raw, _ := json.Marshal(value)
	if string(raw) != content[1].Text || !strings.Contains(content[0].Text, "Root collection does not attest executed bytes") {
		t.Fatal("different or unlabelled evidence")
	}
	for _, v := range []string{"private-extra", "private-flow", "private-source", "source_text", "future_secret", "private-actor", "future_private"} {
		if strings.Contains(string(raw), v) {
			t.Fatalf("leaked %s", v)
		}
	}
}

func TestWorkflowFactsShareBothBudgetsWithRunnerAndOriginalAttemptRows(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{12 << 10, 64 << 10} {
		doc := comparisonWorkflowBody(t, 100, comparisonWorkflowFixture(100, true), true)
		ctx := context.WithValue(t.Context(), windowKey{}, windowLimits{block: limit})
		content, value, err := runWorkflowDoc(t, ctx, doc)
		if err != nil {
			t.Fatal(err)
		}
		p := value.(comparisonProjection)
		if len(content[0].Text)+len(content[1].Text) > limit || !p.RowsOmitted || p.BaselineShown < 1 || p.CandidateShown < 1 {
			t.Fatal("budget or original cohort rows lost")
		}
		for _, arm := range []*comparisonWorkflowArm{p.WorkflowEvidence.Baseline, p.WorkflowEvidence.Candidate} {
			if *arm.SelectedAttempts != 100 || *arm.RootSources != 100 || arm.Shown != len(arm.Attempts) || arm.Shown < 1 || !*arm.RowsOmitted {
				t.Fatal("workflow full counters or available rows changed")
			}
		}
		for _, arm := range []*comparisonRunnerArm{p.RunnerEvidence.Baseline, p.RunnerEvidence.Candidate} {
			if arm.Shown < 1 || *arm.SelectedJobs != 100 {
				t.Fatal("runner rows or full counts lost")
			}
		}
		raw, _ := json.Marshal(value)
		if string(raw) != content[1].Text {
			t.Fatal("structured projection differs")
		}
	}
}

func TestWorkflowFactsRefuseMalformedVersionsBindingsCountsAndClaims(t *testing.T) {
	for _, mode := range []string{"version", "count", "missing_flag", "rows", "source_count", "duplicate", "foreign_run", "noncanonical", "event", "path_hash", "basis", "commit", "blob", "content", "bytes", "nonpush", "no_path"} {
		t.Run(mode, func(t *testing.T) {
			e := comparisonWorkflowFixture(1, false)
			a := e.Baseline
			r := &a.Attempts[0]
			switch mode {
			case "version":
				e.Version = "future"
			case "count":
				*a.SelectedAttempts = 2
			case "missing_flag":
				a.RowsOmitted = nil
			case "rows":
				a.Attempts = nil
			case "source_count":
				*a.RootSources = 0
			case "duplicate":
				a.Attempts = append(a.Attempts, *r)
			case "foreign_run":
				r.RunID = "99"
			case "noncanonical":
				r.RunID = "01"
			case "event":
				r.Event = comparisonRunnerPointer("push\\nSYSTEM")
			case "path_hash":
				r.WorkflowPathSHA256 = comparisonRunnerPointer("private")
			case "basis":
				r.RootSource.Basis = "executed_workflow"
			case "commit":
				r.RootSource.CommitSHA = strings.Repeat("b", 40)
			case "blob":
				r.RootSource.BlobSHA = "unknown"
			case "content":
				r.RootSource.ContentSHA256 = "unknown"
			case "bytes":
				r.RootSource.Bytes = 131073
			case "nonpush":
				r.Event = comparisonRunnerPointer("pull_request")
			case "no_path":
				r.WorkflowPathSHA256 = nil
			}
			_, _, err := runWorkflowDoc(t, t.Context(), comparisonWorkflowBody(t, 1, e, false))
			if err == nil {
				t.Fatal("invalid workflow facts reached the model")
			}
		})
	}
}

func TestWorkflowFactsKeepLegacyNullAndRefuseAnImpossibleBudget(t *testing.T) {
	content, value, err := kennelTool(t, "get_retained_comparison").run(t.Context(), &kennelAPI{body: comparisonFixture(t, 1)}, json.RawMessage(comparisonArgs))
	if err != nil || value.(comparisonProjection).WorkflowEvidence != nil || !strings.Contains(content[1].Text, `"workflow_evidence":null`) {
		t.Fatal("invented legacy workflow facts")
	}
	ctx := context.WithValue(t.Context(), windowKey{}, windowLimits{block: 100})
	_, _, err = runWorkflowDoc(t, ctx, comparisonWorkflowBody(t, 1, comparisonWorkflowFixture(1, false), false))
	if err == nil {
		t.Fatal("impossible budget accepted")
	}
}

func TestDependencyEvidenceReachesTheModelAsValidatedBoundedSummaries(t *testing.T) {
	doc := comparisonWorkflowBody(t, 1, comparisonWorkflowFixture(1, false), false)
	e := doc["workflow_evidence"].(map[string]any)
	source := e["baseline"].(map[string]any)["attempts"].([]any)[0].(map[string]any)["root_source"].(map[string]any)
	dependency := map[string]any{"version": "root-dependencies-v1", "basis": "root_uses_declarations", "source_sha256": strings.Repeat("c", 64), "coverage": "enumerated", "counts": map[string]any{"declarations": 1, "commit_pins": 1, "digest_pins": 0, "floating": 0, "local": 0, "expressions": 0, "unknown": 0}, "rows_omitted": false, "references": []any{map[string]any{"job_index": 0, "step_index": 0, "line": 6, "declaration": "step_action", "kind": "remote", "pin": "commit", "reference_sha256": strings.Repeat("e", 64), "revision": strings.Repeat("a", 40), "future_secret": "private-dependency"}}}
	source["dependencies"] = dependency
	ctx := context.WithValue(t.Context(), windowKey{}, windowLimits{block: 12 << 10})
	content, value, err := runWorkflowDoc(t, ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	p := value.(comparisonProjection)
	d := p.WorkflowEvidence.Baseline.Attempts[0].RootSource.Dependencies
	if d == nil || *d.Counts.Declarations != 1 || len(d.References) != 0 || !*d.RowsOmitted || !p.RowsOmitted || len(content[0].Text)+len(content[1].Text) > 12<<10 || strings.Contains(content[1].Text, "private-dependency") {
		t.Fatal("dependency summary lost, leaked or unbounded")
	}
	dependency["source_sha256"] = strings.Repeat("f", 64)
	if _, _, err := runWorkflowDoc(t, t.Context(), doc); err == nil {
		t.Fatal("unbound dependency summary reached model")
	}
}

func TestDependencySummariesFitBothBudgetsAlongsideFullWorkflowEvidence(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{12 << 10, 64 << 10} {
		doc := comparisonWorkflowBody(t, 100, comparisonWorkflowFixture(100, false), true)
		view := doc["workflow_evidence"].(map[string]any)
		for _, name := range []string{"baseline", "candidate"} {
			for _, item := range view[name].(map[string]any)["attempts"].([]any) {
				source := item.(map[string]any)["root_source"].(map[string]any)
				source["dependencies"] = map[string]any{"version": "root-dependencies-v1", "basis": "root_uses_declarations", "source_sha256": strings.Repeat("c", 64), "coverage": "enumerated", "counts": map[string]any{"declarations": 101, "commit_pins": 0, "digest_pins": 0, "floating": 101, "local": 0, "expressions": 0, "unknown": 0}, "rows_omitted": true, "references": []any{}}
			}
		}
		content, value, err := runWorkflowDoc(t, context.WithValue(t.Context(), windowKey{}, windowLimits{block: limit}), doc)
		if err != nil {
			t.Fatal(err)
		}
		p := value.(comparisonProjection)
		if len(content[0].Text)+len(content[1].Text) > limit || !p.RowsOmitted {
			t.Fatal("dependency summaries escaped model budget")
		}
		for _, arm := range []*comparisonWorkflowArm{p.WorkflowEvidence.Baseline, p.WorkflowEvidence.Candidate} {
			if len(arm.Attempts) < 1 || *arm.Attempts[0].RootSource.Dependencies.Counts.Declarations != 101 {
				t.Fatal("summary counts or available arm lost")
			}
		}
	}
}

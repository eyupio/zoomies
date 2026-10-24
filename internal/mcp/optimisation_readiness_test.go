package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const readinessPreviewArgs = `{"preview_id":"opr_1","installation_id":"ins_1","repo":"acme/repo"}`

func readinessPreviewFixture() string {
	return `{
	  "id":"opr_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"private-reviewer",
	  "sha256":"` + strings.Repeat("a", 64) + `","created_at":"2026-10-24T10:00:00Z","currentness":"not_checked",
	  "selection":{"run_id":"10","attempt":2,"github_job_id":"100"},
	  "condition":"curl -fsS http://127.0.0.1:8080/health","head_sha":"` + strings.Repeat("c", 40) + `",
	  "source_sha256":"` + strings.Repeat("e", 64) + `","workflow_path":".github/workflows/ci.yml",
	  "job_name":"Build","step_name":"Wait for the API","line":12,"wait_seconds":30,"runs":4,"wait_ms":31000,
	  "before":"        run: sleep 30\n","after":"        run: |\n          timeout 30 sh -c 'until curl -fsS http://127.0.0.1:8080/health; do sleep 1; done'\n",
	  "verification":"inconclusive","explanation":"Replace a fixed 30s sleep with a bounded poll of the operator command.",
	  "content":"private workflow source that must not leave","future_private":"hidden"
	}`
}

func TestReadinessPreviewToolReadsOnceAndQuotesTheChangedBlock(t *testing.T) {
	api := &kennelAPI{body: readinessPreviewFixture()}
	tl := kennelTool(t, "get_readiness_preview")
	content, value, err := tl.run(t.Context(), api, json.RawMessage(readinessPreviewArgs))
	if err != nil {
		t.Fatal(err)
	}
	if api.calls != 1 || api.got != "GET /optimisations/readiness/previews/opr_1" || api.query.Get("installation_id") != "ins_1" || api.query.Get("repo") != "acme/repo" || len(api.query) != 2 {
		t.Fatalf("route %s %v", api.got, api.query)
	}
	if !tl.Annotations.ReadOnly || tl.action || tl.live || tl.OutputSchema == nil {
		t.Fatal("incorrect tool classification")
	}
	if len(content) != 2 || !strings.Contains(content[0].Text, "untrusted") || strings.Contains(content[0].Text, "timeout 30") || !strings.Contains(content[1].Text, "timeout 30 sh -c") || !strings.Contains(content[1].Text, "curl -fsS") {
		t.Fatalf("unsafe blocks: %+v", content)
	}
	if strings.Contains(content[1].Text, "private workflow source") || strings.Contains(content[1].Text, "private-reviewer") || strings.Contains(content[1].Text, "future_private") || strings.Contains(content[1].Text, `"run_id"`) {
		t.Fatal("passed source, actor identity, selection or an unreviewed field")
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != content[1].Text {
		t.Fatal("structured/text evidence differs")
	}
	if p := value.(readinessPreviewProjection); p.Condition != "curl -fsS http://127.0.0.1:8080/health" || p.WaitSeconds != 30 || p.Runs != 4 || p.WaitMS != 31000 || p.JobName != "Build" || p.StepName != "Wait for the API" {
		t.Fatalf("projection %+v", p)
	}
}

func TestReadinessPreviewToolRefusesInvalidArgumentsBeforeRead(t *testing.T) {
	for _, args := range []string{`{}`, `{"preview_id":"opr_../config","installation_id":"ins_1","repo":"acme/repo"}`, `{"preview_id":"opr_1","installation_id":"ins_1","repo":"acme/repo?x=y"}`, strings.TrimSuffix(readinessPreviewArgs, "}") + `,"capture":true}`} {
		api := &kennelAPI{}
		if _, _, err := kennelTool(t, "get_readiness_preview").run(t.Context(), api, json.RawMessage(args)); err == nil || api.calls != 0 {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestReadinessPreviewToolRefusesUnavailableOrInconsistentEvidence(t *testing.T) {
	for name, api := range map[string]*kennelAPI{
		"unavailable":  {err: errors.New("404 unavailable")},
		"forbidden":    {err: errors.New("403 forbidden")},
		"null":         {body: "null"},
		"wrong scope":  {body: strings.ReplaceAll(readinessPreviewFixture(), "acme/repo", "other/repo")},
		"currentness":  {body: strings.ReplaceAll(readinessPreviewFixture(), `"currentness":"not_checked"`, `"currentness":"checked"`)},
		"condition":    {body: strings.ReplaceAll(readinessPreviewFixture(), `"condition":"curl -fsS http://127.0.0.1:8080/health"`, `"condition":""`)},
		"before empty": {body: strings.ReplaceAll(readinessPreviewFixture(), `"before":"        run: sleep 30\n"`, `"before":""`)},
		"oversized":    {body: strings.Repeat("x", (64<<10)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := kennelTool(t, "get_readiness_preview").run(context.Background(), api, json.RawMessage(readinessPreviewArgs)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

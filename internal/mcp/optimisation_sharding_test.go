package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const shardingPreviewArgs = `{"preview_id":"ops_1","installation_id":"ins_1","repo":"acme/repo"}`

func shardingPreviewFixture() string {
	return `{
	  "id":"ops_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"private-reviewer",
	  "sha256":"` + strings.Repeat("a", 64) + `","created_at":"2026-10-24T10:00:00Z","currentness":"not_checked",
	  "selection":{"run_id":"10","attempt":2,"github_job_id":"100"},
	  "head_sha":"` + strings.Repeat("c", 40) + `","source_sha256":"` + strings.Repeat("e", 64) + `","workflow_path":".github/workflows/ci.yml",
	  "job_name":"End to end","step_name":"Browser tests","line":3,"shards":4,"runs":12,"test_ms":360000,"setup_ms":42000,"headroom":4,
	  "runner_ms_before":402000,"runner_ms_after":528000,"wall_ms_before":402000,"wall_ms_after":132000,
	  "before":"  e2e:\n    runs-on: ubuntu-latest\n","after":"  e2e-shards:\n    strategy:\n      matrix:\n        shard: [1, 2, 3, 4]\n        run: npx playwright test --shard=${{ matrix.shard }}/4\n",
	  "verification":"inconclusive","explanation":"Split the Browser tests step into 4 Playwright shards.",
	  "approval":{"approved_by":"private-approver","approved_at":"2026-10-24T10:02:00Z","head_sha":"` + strings.Repeat("c", 40) + `","source_sha256":"` + strings.Repeat("e", 64) + `","preview_sha256":"` + strings.Repeat("a", 64) + `"},
	  "content":"private workflow source that must not leave","future_private":"hidden"
	}`
}

func TestShardingPreviewToolReadsOnceAndQuotesTheChangedBlock(t *testing.T) {
	api := &kennelAPI{body: shardingPreviewFixture()}
	tl := kennelTool(t, "get_sharding_preview")
	content, value, err := tl.run(t.Context(), api, json.RawMessage(shardingPreviewArgs))
	if err != nil {
		t.Fatal(err)
	}
	if api.calls != 1 || api.got != "GET /optimisations/sharding/previews/ops_1" || api.query.Get("installation_id") != "ins_1" || api.query.Get("repo") != "acme/repo" || len(api.query) != 2 {
		t.Fatalf("route %s %v", api.got, api.query)
	}
	if !tl.Annotations.ReadOnly || tl.action || tl.live || tl.OutputSchema == nil {
		t.Fatal("incorrect tool classification")
	}
	if len(content) != 2 || !strings.Contains(content[0].Text, "untrusted") || strings.Contains(content[0].Text, "--shard=") || !strings.Contains(content[1].Text, "--shard=") || !strings.Contains(content[1].Text, "shard: [1, 2, 3, 4]") {
		t.Fatalf("unsafe blocks: %+v", content)
	}
	if strings.Contains(content[1].Text, "private workflow source") || strings.Contains(content[1].Text, "private-reviewer") || strings.Contains(content[1].Text, "private-approver") || strings.Contains(content[1].Text, "future_private") || strings.Contains(content[1].Text, `"run_id"`) {
		t.Fatal("passed source, actor identity, selection or an unreviewed field")
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != content[1].Text {
		t.Fatal("structured/text evidence differs")
	}
	if p := value.(shardingPreviewProjection); p.Shards != 4 || p.Runs != 12 || p.Headroom != 4 || p.RunnerMSAfter <= p.RunnerMSBefore || p.WallMSAfter >= p.WallMSBefore || p.JobName != "End to end" || p.StepName != "Browser tests" || p.Approval == nil || p.Approval.ApprovedAt.IsZero() || p.Approval.HeadSHA != p.HeadSHA || p.Approval.SourceSHA256 != p.SourceSHA256 || p.Approval.PreviewSHA256 == "" {
		t.Fatalf("projection %+v", p)
	}
}

func TestShardingPreviewToolRefusesInvalidArgumentsBeforeRead(t *testing.T) {
	for _, args := range []string{`{}`, `{"preview_id":"ops_../config","installation_id":"ins_1","repo":"acme/repo"}`, `{"preview_id":"opr_1","installation_id":"ins_1","repo":"acme/repo"}`, `{"preview_id":"ops_1","installation_id":"ins_1","repo":"acme/repo?x=y"}`, strings.TrimSuffix(shardingPreviewArgs, "}") + `,"capture":true}`} {
		api := &kennelAPI{}
		if _, _, err := kennelTool(t, "get_sharding_preview").run(t.Context(), api, json.RawMessage(args)); err == nil || api.calls != 0 {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestShardingPreviewToolRefusesAnInconsistentPreview(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"wrong ID":            func(s string) string { return strings.Replace(s, `"id":"ops_1"`, `"id":"ops_2"`, 1) },
		"currentness claimed": func(s string) string { return strings.Replace(s, `"not_checked"`, `"current"`, 1) },
		"a saving claimed": func(s string) string {
			return strings.Replace(s, `"verification":"inconclusive"`, `"verification":"verified"`, 1)
		},
		"one shard":             func(s string) string { return strings.Replace(s, `"shards":4`, `"shards":1`, 1) },
		"nine shards":           func(s string) string { return strings.Replace(s, `"shards":4`, `"shards":9`, 1) },
		"fewer than ten runs":   func(s string) string { return strings.Replace(s, `"runs":12`, `"runs":9`, 1) },
		"more shards than room": func(s string) string { return strings.Replace(s, `"headroom":4`, `"headroom":2`, 1) },
		"less runner time": func(s string) string {
			return strings.Replace(s, `"runner_ms_after":528000`, `"runner_ms_after":100000`, 1)
		},
		"slower wall clock": func(s string) string {
			return strings.Replace(s, `"wall_ms_after":132000`, `"wall_ms_after":900000`, 1)
		},
		"no shard flag":     func(s string) string { return strings.Replace(s, `--shard=`, `--noshard=`, 1) },
		"outside workflows": func(s string) string { return strings.Replace(s, `.github/workflows/ci.yml`, `scripts/ci.yml`, 1) },
	} {
		api := &kennelAPI{body: mutate(shardingPreviewFixture())}
		if _, _, err := kennelTool(t, "get_sharding_preview").run(t.Context(), api, json.RawMessage(shardingPreviewArgs)); !errors.Is(err, errShardingPreviewShape) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestShardingPreviewToolRefusesAnApprovalForADifferentHead(t *testing.T) {
	body := strings.Replace(shardingPreviewFixture(), `"approved_at":"2026-10-24T10:02:00Z","head_sha":"`+strings.Repeat("c", 40), `"approved_at":"2026-10-24T10:02:00Z","head_sha":"`+strings.Repeat("d", 40), 1)
	api := &kennelAPI{body: body}
	if _, _, err := kennelTool(t, "get_sharding_preview").run(t.Context(), api, json.RawMessage(shardingPreviewArgs)); !errors.Is(err, errShardingPreviewShape) {
		t.Fatalf("accepted a mismatched approval: %v", err)
	}
}

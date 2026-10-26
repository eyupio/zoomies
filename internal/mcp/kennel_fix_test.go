package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const kennelFixArgs = `{"fix_id":"kfp_1","installation_id":"ins_1","repo":"acme/repo"}`

func kennelFixFixture() string {
	head, source, digest := strings.Repeat("c", 40), strings.Repeat("e", 64), strings.Repeat("a", 64)
	return `{
	  "id":"kfp_1","installation_id":"ins_1","repository":"acme/repo","kennel_repository_id":"kcr_1","reviewed_by":"private-reviewer",
	  "sha256":"` + digest + `","created_at":"2026-10-25T10:00:00Z","currentness":"not_checked","check_code":"ci.no_timeout",
	  "blob_sha":"` + strings.Repeat("b", 40) + `","job_index":0,"job_name":"Build","minutes":30,"head_sha":"` + head + `","branch":"main",
	  "source_sha256":"` + source + `","workflow_path":".github/workflows/ci.yml","line":5,
	  "before":"  build:\n    runs-on: ubuntu-latest\n","after":"  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 30\n",
	  "verification":"recheck","explanation":"Give Build a timeout of 30 minutes.",
	  "approval":{"approved_by":"private-approver","approved_at":"2026-10-25T10:02:00Z","head_sha":"` + head + `","source_sha256":"` + source + `","preview_sha256":"` + digest + `"},
	  "publication":{"published_by":"private-publisher","published_at":"2026-10-25T10:03:00Z","branch":"zoomies/kennel-fix-kfp_1","base_branch":"main","base_sha":"` + head + `","head_sha":"` + head + `","source_sha256":"` + source + `","preview_sha256":"` + digest + `","pull_number":31,"pull_url":"https://github.com/acme/repo/pull/31"},
	  "content":"private workflow source that must not leave","future_private":"hidden"
	}`
}

func TestKennelFixToolReadsOnceAndQuotesTheChangedLines(t *testing.T) {
	api := &kennelAPI{body: kennelFixFixture()}
	tl := kennelTool(t, "get_kennel_fix")
	content, value, err := tl.run(t.Context(), api, json.RawMessage(kennelFixArgs))
	if err != nil {
		t.Fatal(err)
	}
	if api.calls != 1 || api.got != "GET /kennel/fixes/timeout/previews/kfp_1" || api.query.Get("installation_id") != "ins_1" || api.query.Get("repo") != "acme/repo" || len(api.query) != 2 {
		t.Fatalf("route %s %v", api.got, api.query)
	}
	if !tl.Annotations.ReadOnly || tl.action || tl.live || tl.OutputSchema == nil {
		t.Fatal("incorrect tool classification")
	}
	if len(content) != 2 || !strings.Contains(content[0].Text, "untrusted") || strings.Contains(content[0].Text, "timeout-minutes: 30") || !strings.Contains(content[1].Text, "timeout-minutes: 30") {
		t.Fatalf("unsafe blocks: %+v", content)
	}
	for _, leak := range []string{"private workflow source", "private-reviewer", "private-approver", "private-publisher", "future_private", `"blob_sha"`} {
		if strings.Contains(content[1].Text, leak) {
			t.Fatalf("passed %q", leak)
		}
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != content[1].Text {
		t.Fatal("structured/text evidence differs")
	}
	if p := value.(kennelFixProjection); p.CheckCode != "ci.no_timeout" || p.Minutes != 30 || p.JobName != "Build" || p.Branch != "main" || p.Approval == nil || p.Approval.PreviewSHA256 == "" || p.Publication == nil || p.Publication.PullNumber != 31 || p.Publication.PullURL != "https://github.com/acme/repo/pull/31" {
		t.Fatalf("projection %+v", p)
	}
}

func TestKennelFixToolRefusesInvalidArgumentsBeforeRead(t *testing.T) {
	for _, args := range []string{`{}`, `{"fix_id":"kfp_../config","installation_id":"ins_1","repo":"acme/repo"}`, `{"fix_id":"opr_1","installation_id":"ins_1","repo":"acme/repo"}`, `{"fix_id":"kfp_1","installation_id":"ins_1","repo":"acme/repo?x=y"}`, strings.TrimSuffix(kennelFixArgs, "}") + `,"capture":true}`} {
		api := &kennelAPI{}
		if _, _, err := kennelTool(t, "get_kennel_fix").run(t.Context(), api, json.RawMessage(args)); err == nil || api.calls != 0 {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestKennelFixToolRefusesUnavailableOrInconsistentEvidence(t *testing.T) {
	for name, api := range map[string]*kennelAPI{
		"unavailable":     {err: errors.New("404 unavailable")},
		"forbidden":       {err: errors.New("403 forbidden")},
		"null":            {body: "null"},
		"wrong scope":     {body: strings.ReplaceAll(kennelFixFixture(), "acme/repo", "other/repo")},
		"currentness":     {body: strings.ReplaceAll(kennelFixFixture(), `"currentness":"not_checked"`, `"currentness":"checked"`)},
		"another check":   {body: strings.ReplaceAll(kennelFixFixture(), `"check_code":"ci.no_timeout"`, `"check_code":"ci.no_concurrency"`)},
		"no minutes":      {body: strings.ReplaceAll(kennelFixFixture(), `"minutes":30`, `"minutes":0`)},
		"too many":        {body: strings.ReplaceAll(kennelFixFixture(), `"minutes":30`, `"minutes":1441`)},
		"no timeout line": {body: strings.ReplaceAll(kennelFixFixture(), "    timeout-minutes: 30\\n", "")},
		"oversized":       {body: strings.Repeat("x", (64<<10)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := kennelTool(t, "get_kennel_fix").run(context.Background(), api, json.RawMessage(kennelFixArgs)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestKennelFixToolRefusesAnApprovalForADifferentHeadAndAPublicationWithoutOne(t *testing.T) {
	head := strings.Repeat("c", 40)
	bad := strings.Replace(kennelFixFixture(), `"head_sha":"`+head+`","source_sha256"`, `"head_sha":"`+strings.Repeat("d", 40)+`","source_sha256"`, 1)
	if _, _, err := kennelTool(t, "get_kennel_fix").run(t.Context(), &kennelAPI{body: bad}, json.RawMessage(kennelFixArgs)); !errors.Is(err, errKennelFixShape) {
		t.Fatalf("accepted a mismatched head: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(kennelFixFixture()), &m); err != nil {
		t.Fatal(err)
	}
	m["approval"] = nil
	noApproval, _ := json.Marshal(m)
	if _, _, err := kennelTool(t, "get_kennel_fix").run(t.Context(), &kennelAPI{body: string(noApproval)}, json.RawMessage(kennelFixArgs)); !errors.Is(err, errKennelFixShape) {
		t.Fatalf("accepted a publication of a fix nobody approved: %v", err)
	}
}

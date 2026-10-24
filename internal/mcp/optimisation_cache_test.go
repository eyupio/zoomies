package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const cachePreviewArgs = `{"preview_id":"opc_1","installation_id":"ins_1","repo":"acme/repo"}`

func cachePreviewFixture() string {
	return `{
	  "id":"opc_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"private-reviewer",
	  "sha256":"` + strings.Repeat("a", 64) + `","created_at":"2026-10-23T10:00:00Z","currentness":"not_checked",
	  "head_sha":"` + strings.Repeat("c", 40) + `","workflow_path":".github/workflows/ci.yml",
	  "lockfile_path":"package-lock.json","lockfile_sha256":"` + strings.Repeat("d", 64) + `",
	  "source_sha256":"` + strings.Repeat("e", 64) + `","after_sha256":"` + strings.Repeat("f", 64) + `",
	  "package_manager":"npm","job_name":"Build","step_name":"Node","line":12,"runs":4,"setup_ms":4200,
	  "before":"        with:\n          node-version: 20\n","after":"        with:\n          cache: npm\n          node-version: 20\n",
	  "verification":"inconclusive","explanation":"Enable the setup action npm cache. The saving is unverified.",
	  "content":"private workflow source that must not leave","future_private":"hidden"
	}`
}

func TestCachePreviewToolReadsOnceAndQuotesTheChangedBlock(t *testing.T) {
	api := &kennelAPI{body: cachePreviewFixture()}
	tl := kennelTool(t, "get_cache_preview")
	content, value, err := tl.run(t.Context(), api, json.RawMessage(cachePreviewArgs))
	if err != nil {
		t.Fatal(err)
	}
	if api.calls != 1 || api.got != "GET /optimisations/cache/previews/opc_1" || api.query.Get("installation_id") != "ins_1" || api.query.Get("repo") != "acme/repo" || len(api.query) != 2 {
		t.Fatalf("route %s %v", api.got, api.query)
	}
	if !tl.Annotations.ReadOnly || tl.action || tl.live || tl.OutputSchema == nil {
		t.Fatal("incorrect tool classification")
	}
	if len(content) != 2 || !strings.Contains(content[0].Text, "untrusted") || strings.Contains(content[0].Text, "cache: npm") || !strings.Contains(content[1].Text, "cache: npm") || !strings.Contains(content[1].Text, "node-version: 20") {
		t.Fatalf("unsafe blocks: %+v", content)
	}
	if strings.Contains(content[1].Text, "private workflow source") || strings.Contains(content[1].Text, "private-reviewer") || strings.Contains(content[1].Text, "future_private") {
		t.Fatal("passed source, actor identity or an unreviewed field")
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != content[1].Text {
		t.Fatal("structured/text evidence differs")
	}
	if p := value.(cachePreviewProjection); p.PackageManager != "npm" || p.Runs != 4 || p.SetupMS != 4200 || p.Before != "        with:\n          node-version: 20\n" || p.After != "        with:\n          cache: npm\n          node-version: 20\n" || p.JobName != "Build" || p.StepName != "Node" {
		t.Fatalf("projection %+v", p)
	}
}

func TestCachePreviewToolRefusesInvalidArgumentsBeforeRead(t *testing.T) {
	for _, args := range []string{`{}`, `{"preview_id":"opc_../config","installation_id":"ins_1","repo":"acme/repo"}`, `{"preview_id":"opc_1","installation_id":"ins_1","repo":"acme/repo?x=y"}`, strings.TrimSuffix(cachePreviewArgs, "}") + `,"capture":true}`} {
		api := &kennelAPI{}
		if _, _, err := kennelTool(t, "get_cache_preview").run(t.Context(), api, json.RawMessage(args)); err == nil || api.calls != 0 {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestCachePreviewToolRefusesUnavailableOrInconsistentEvidence(t *testing.T) {
	for name, api := range map[string]*kennelAPI{
		"unavailable":  {err: errors.New("404 unavailable")},
		"forbidden":    {err: errors.New("403 forbidden")},
		"null":         {body: "null"},
		"wrong scope":  {body: strings.ReplaceAll(cachePreviewFixture(), "acme/repo", "other/repo")},
		"currentness":  {body: strings.ReplaceAll(cachePreviewFixture(), `"currentness":"not_checked"`, `"currentness":"checked"`)},
		"before empty": {body: strings.ReplaceAll(cachePreviewFixture(), `"before":"        with:\n          node-version: 20\n"`, `"before":""`)},
		"oversized":    {body: strings.Repeat("x", (64<<10)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := kennelTool(t, "get_cache_preview").run(context.Background(), api, json.RawMessage(cachePreviewArgs)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

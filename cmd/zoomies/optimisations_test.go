package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const cliCheckoutPreview = `{
  "id":"opv_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"usr_reviewer",
  "sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `",
  "created_at":"2026-10-23T10:00:00Z","currentness":"not_checked",
  "collection":{"version":"checkout-evidence-v1","basis":"push_source_unique_literal_names","step_number":42,"blob_sha":"` + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + `","source_funding":"installation_github_read"},
  "approval":{"approved_by":"usr_approver","approved_at":"2026-10-23T10:01:00Z","head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","source_sha256":"` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `","preview_sha256":"` + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" + `"},
  "publication":{"published_by":"usr_publisher","published_at":"2026-10-23T10:02:00Z","branch":"zoomies/checkout-opv_1","base_branch":"main","base_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","source_sha256":"` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `","preview_sha256":"` + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" + `","pull_number":21,"pull_url":"https://github.com/acme/repo/pull/21"},
  "verification":{"comparison_id":"cmp_1","linked_by":"usr_verifier","linked_at":"2026-10-23T10:03:00Z"},
  "preview":{"version":"checkout-preview-v1","kind":"checkout_depth","state":"preview_ready","head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","source_sha256":"` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `","after_sha256":"` + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" + `","evidence_id":"checkout:10:2:100:42","run_id":"10","attempt":2,"job_index":0,"step_index":0,"line":8,"checkout_ms":4200,"path":".github/workflows/ci.yml","before":"          fetch-depth: 0\n","after":"          fetch-depth: 1\n","verification":"inconclusive","explanation":"Reduce this checkout to one commit. Savings are unverified."}
}`

func TestOptimisationsGetPrintsMetadataAndTheOneChangedLine(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/checkout/previews/opv_1": cliCheckoutPreview})
	out, _ := runCLI(t, "optimisations", "get", "opv_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	for _, want := range []string{"opv_1", "acme/repo", ".github/workflows/ci.yml", "fetch-depth: 0", "fetch-depth: 1", "draft #21", "cmp_1", "untrusted repository text", "not a saving"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on: push\njobs:") {
		t.Error("the full workflow source was printed")
	}
}

func TestOptimisationsGetStructuredOutputPreservesTheControllerAnswer(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/checkout/previews/opv_1": cliCheckoutPreview})
	out, _ := runCLI(t, "optimisations", "get", "opv_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL, "--output", "json")
	var got, want bytes.Buffer
	if err := json.Compact(&got, []byte(out)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&want, []byte(cliCheckoutPreview)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Fatalf("structured output changed the answer:\n%s\nwant\n%s", got.String(), want.String())
	}
}

func TestOptimisationsGetRefusesInvalidScopeBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"optimisations", "get", "preview", "--installation", "ins_1", "--repo", "acme/repo"},
		{"optimisations", "get", "opv_1", "--installation", "installation", "--repo", "acme/repo"},
		{"optimisations", "get", "opv_1", "--installation", "ins_1", "--repo", "acme"},
	} {
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, args); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted %v: %s", args, errOut.String())
		}
	}
}

const cliCachePreview = `{
  "id":"opc_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"usr_reviewer",
  "sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","created_at":"2026-10-23T10:00:00Z","currentness":"not_checked",
  "head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","workflow_path":".github/workflows/ci.yml",
  "lockfile_path":"package-lock.json","lockfile_sha256":"` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `",
  "source_sha256":"` + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" + `","after_sha256":"` + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" + `",
  "package_manager":"npm","job_name":"Build","step_name":"Node","line":12,"runs":4,"setup_ms":4200,
  "before":"        with:\n          node-version: 20\n","after":"        with:\n          cache: npm\n          node-version: 20\n",
  "verification":"inconclusive","explanation":"Enable the setup action npm cache. The saving is unverified."
}`

func TestOptimisationsCachePrintsMetadataAndTheChangedBlock(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/cache/previews/opc_1": cliCachePreview})
	out, _ := runCLI(t, "optimisations", "cache", "opc_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	for _, want := range []string{"opc_1", "acme/repo", ".github/workflows/ci.yml", "package-lock.json", "cache: npm", "4 runs", "not approved", "not published", "untrusted repository text", "not a saving"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on: push") {
		t.Error("the full workflow source was printed")
	}
}

func TestOptimisationsCacheStructuredOutputPreservesTheControllerAnswer(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/cache/previews/opc_1": cliCachePreview})
	out, _ := runCLI(t, "optimisations", "cache", "opc_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL, "--output", "json")
	var got, want bytes.Buffer
	if err := json.Compact(&got, []byte(out)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&want, []byte(cliCachePreview)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Fatalf("structured output changed the answer:\n%s\nwant\n%s", got.String(), want.String())
	}
}

func TestOptimisationsCacheRefusesInvalidScopeBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"optimisations", "cache", "preview", "--installation", "ins_1", "--repo", "acme/repo"},
		{"optimisations", "cache", "opc_1", "--installation", "installation", "--repo", "acme/repo"},
		{"optimisations", "cache", "opc_1", "--installation", "ins_1", "--repo", "acme"},
	} {
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, args); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted %v: %s", args, errOut.String())
		}
	}
}

const cliReadinessPreview = `{
  "id":"opr_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"usr_reviewer",
  "sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","created_at":"2026-10-24T10:00:00Z","currentness":"not_checked",
  "condition":"curl -fsS http://127.0.0.1:8080/health","head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `",
  "source_sha256":"` + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" + `","workflow_path":".github/workflows/ci.yml",
  "job_name":"Build","step_name":"Wait for the API","line":12,"wait_seconds":30,"runs":4,"wait_ms":31000,
  "before":"        run: sleep 30\n","after":"        run: |\n          timeout 30 sh -c 'until curl -fsS http://127.0.0.1:8080/health; do sleep 1; done'\n",
  "verification":"inconclusive","explanation":"Replace a fixed 30s sleep with a bounded poll of the operator command."
}`

func TestOptimisationsReadinessPrintsTheConditionAndChangedBlock(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/readiness/previews/opr_1": cliReadinessPreview})
	out, _ := runCLI(t, "optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	for _, want := range []string{"opr_1", "acme/repo", "Wait for the API", "curl -fsS", "timeout 30 sh -c", "4 runs", "not approved", "not a saving", "operator-supplied"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on: push") {
		t.Error("the full workflow source was printed")
	}
}

func TestOptimisationsReadinessStructuredOutputPreservesTheControllerAnswer(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/readiness/previews/opr_1": cliReadinessPreview})
	out, _ := runCLI(t, "optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL, "--output", "json")
	var got, want bytes.Buffer
	if err := json.Compact(&got, []byte(out)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&want, []byte(cliReadinessPreview)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Fatalf("structured output changed the answer:\n%s\nwant\n%s", got.String(), want.String())
	}
}

func TestOptimisationsReadinessRefusesInvalidScopeBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"optimisations", "readiness", "preview", "--installation", "ins_1", "--repo", "acme/repo"},
		{"optimisations", "readiness", "opr_1", "--installation", "installation", "--repo", "acme/repo"},
		{"optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme"},
	} {
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, args); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted %v: %s", args, errOut.String())
		}
	}
}

func TestOptimisationsReadinessShowsWhoApprovedIt(t *testing.T) {
	approved := strings.Replace(cliReadinessPreview, `"explanation":`, `"approval":{"approved_by":"usr_approver","approved_at":"2026-10-24T10:02:00Z","head_sha":"`+strings.Repeat("c", 40)+`","source_sha256":"`+strings.Repeat("e", 64)+`","preview_sha256":"`+strings.Repeat("a", 64)+`"},"explanation":`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/readiness/previews/opr_1": approved})
	out, _ := runCLI(t, "optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	if !strings.Contains(out, "approved by usr_approver") || strings.Contains(out, "not approved") {
		t.Errorf("approval not shown:\n%s", out)
	}
}

func TestOptimisationsReadinessShowsTheDraftPullRequest(t *testing.T) {
	head, source, preview := strings.Repeat("c", 40), strings.Repeat("e", 64), strings.Repeat("a", 64)
	body := strings.Replace(cliReadinessPreview, `"explanation":`, `"approval":{"approved_by":"usr_approver","approved_at":"2026-10-24T10:02:00Z","head_sha":"`+head+`","source_sha256":"`+source+`","preview_sha256":"`+preview+`"},"publication":{"published_by":"usr_publisher","published_at":"2026-10-24T10:03:00Z","branch":"zoomies/readiness-opr_1","base_branch":"main","base_sha":"`+head+`","head_sha":"`+head+`","source_sha256":"`+source+`","preview_sha256":"`+preview+`","pull_number":12,"pull_url":"https://github.com/acme/repo/pull/12"},"explanation":`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/readiness/previews/opr_1": body})
	out, _ := runCLI(t, "optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	if !strings.Contains(out, "draft pull request #12") || !strings.Contains(out, "zoomies/readiness-opr_1") || !strings.Contains(out, "usr_publisher") {
		t.Errorf("publication not shown:\n%s", out)
	}
	unpublished := jsonRoutes(t, map[string]string{"/api/v1/optimisations/readiness/previews/opr_1": cliReadinessPreview})
	out, _ = runCLI(t, "optimisations", "readiness", "opr_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", unpublished.URL)
	if !strings.Contains(out, "not published") {
		t.Errorf("an unpublished preview did not say so:\n%s", out)
	}
}

const cliShardingPreview = `{
  "id":"ops_1","installation_id":"ins_1","repository":"acme/repo","reviewed_by":"usr_reviewer",
  "sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","created_at":"2026-10-24T10:00:00Z","currentness":"not_checked",
  "head_sha":"` + "cccccccccccccccccccccccccccccccccccccccc" + `","source_sha256":"` + "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" + `",
  "workflow_path":".github/workflows/ci.yml","job_name":"End to end","step_name":"Browser tests","line":3,"shards":4,"runs":12,
  "test_ms":360000,"setup_ms":42000,"headroom":4,"runner_ms_before":402000,"runner_ms_after":528000,"wall_ms_before":402000,"wall_ms_after":132000,
  "before":"  e2e:\n    runs-on: ubuntu-latest\n","after":"  e2e-shards:\n    strategy:\n      matrix:\n        shard: [1, 2, 3, 4]\n",
  "verification":"inconclusive","explanation":"Split the Browser tests step into 4 Playwright shards."
}`

func TestOptimisationsShardingPrintsTheDisclosureAndTheChangedBlock(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/sharding/previews/ops_1": cliShardingPreview})
	out, _ := runCLI(t, "optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	for _, want := range []string{"ops_1", "acme/repo", "Browser tests", "4, which together run the whole Playwright suite", "every shard repeats the setup", "4 of 4", "upper bound", "estimate from medians", "not a saving", "not approved", "not published", "shard: [1, 2, 3, 4]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on: push") {
		t.Error("the full workflow source was printed")
	}
}

func TestOptimisationsShardingStructuredOutputPreservesTheControllerAnswer(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/sharding/previews/ops_1": cliShardingPreview})
	out, _ := runCLI(t, "optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL, "--output", "json")
	var got, want bytes.Buffer
	if err := json.Compact(&got, []byte(out)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&want, []byte(cliShardingPreview)); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Fatalf("structured output changed the answer:\n%s\nwant\n%s", got.String(), want.String())
	}
}

func TestOptimisationsShardingRefusesInvalidScopeAndInconsistentAnswers(t *testing.T) {
	for _, args := range [][]string{
		{"optimisations", "sharding", "preview", "--installation", "ins_1", "--repo", "acme/repo"},
		{"optimisations", "sharding", "opr_1", "--installation", "ins_1", "--repo", "acme/repo"},
		{"optimisations", "sharding", "ops_1", "--installation", "installation", "--repo", "acme/repo"},
		{"optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme"},
	} {
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, args); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted %v: %s", args, errOut.String())
		}
	}
	bad := strings.Replace(cliShardingPreview, `"shards":4`, `"shards":1`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/sharding/previews/ops_1": bad})
	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL}); code == 0 || out.Len() != 0 || !strings.Contains(errOut.String(), "inconsistent") {
		t.Fatalf("an inconsistent answer was printed: %s %s", out.String(), errOut.String())
	}
}

func TestOptimisationsShardingShowsWhoApprovedIt(t *testing.T) {
	approved := strings.Replace(cliShardingPreview, `"explanation":`, `"approval":{"approved_by":"usr_approver","approved_at":"2026-10-24T10:02:00Z","head_sha":"`+strings.Repeat("c", 40)+`","source_sha256":"`+strings.Repeat("e", 64)+`","preview_sha256":"`+strings.Repeat("a", 64)+`"},"explanation":`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/sharding/previews/ops_1": approved})
	out, _ := runCLI(t, "optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	if !strings.Contains(out, "approved by usr_approver") || strings.Contains(out, "not approved") {
		t.Errorf("approval not shown:\n%s", out)
	}
}

func TestOptimisationsShardingShowsTheDraftPullRequest(t *testing.T) {
	head, source, preview := strings.Repeat("c", 40), strings.Repeat("e", 64), strings.Repeat("a", 64)
	body := strings.Replace(cliShardingPreview, `"explanation":`, `"approval":{"approved_by":"usr_approver","approved_at":"2026-10-24T10:02:00Z","head_sha":"`+head+`","source_sha256":"`+source+`","preview_sha256":"`+preview+`"},"publication":{"published_by":"usr_publisher","published_at":"2026-10-24T10:03:00Z","branch":"zoomies/sharding-ops_1","base_branch":"main","base_sha":"`+head+`","head_sha":"`+head+`","source_sha256":"`+source+`","preview_sha256":"`+preview+`","pull_number":12,"pull_url":"https://github.com/acme/repo/pull/12"},"explanation":`, 1)
	srv := jsonRoutes(t, map[string]string{"/api/v1/optimisations/sharding/previews/ops_1": body})
	out, _ := runCLI(t, "optimisations", "sharding", "ops_1", "--installation", "ins_1", "--repo", "acme/repo", "--url", srv.URL)
	if !strings.Contains(out, "draft pull request #12") || !strings.Contains(out, "zoomies/sharding-ops_1") || !strings.Contains(out, "usr_publisher") {
		t.Errorf("publication not shown:\n%s", out)
	}
}

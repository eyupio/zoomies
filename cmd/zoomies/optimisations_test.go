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

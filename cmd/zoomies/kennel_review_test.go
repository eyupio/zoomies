package main

import (
	"context"
	"strings"
	"testing"
)

const cliKennelReview = `{
  "version":1,"kennel_repository_id":"kcr_1","repository":"acme/repo","pull_number":9,"open":true,"base_branch":"main",
  "base_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "read_at":"2026-10-26T07:00:00Z","files_cut":false,"complete":false,
  "introduced":[{"code":"ci.no_timeout","severity":"warning","path":".github/workflows/ci.yml","job":0,"line":5,"title":"Jobs have no explicit timeout","detail":"d","fix":"Add timeout-minutes to the job.","head_sha":"cccccccccccccccccccccccccccccccccccccccc"}],
  "resolved":1,"reviewed":[".github/workflows/ci.yml"],"rule_changes":["AGENTS.md"],
  "not_reviewed":[{"path":"scripts/\u001b[2Jbuild.sh","reason":"not a workflow file, which is all this review covers"}]
}`

func TestKennelReviewPrintsOnlyWhatWasIntroducedAndWhatWasNotCovered(t *testing.T) {
	srv := jsonRoutes(t, map[string]string{"/api/v1/kennel/repositories/kcr_1/pull-reviews/9": cliKennelReview})
	out, _ := runCLI(t, "kennel", "review", "kcr_1", "--pull", "9", "--url", srv.URL)
	for _, want := range []string{"acme/repo", "#9", "head bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "merge base aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"incomplete", "ci.no_timeout", ".github/workflows/ci.yml:5", "Changed instruction file, reported and not applied: AGENTS.md", "Not reviewed: scripts/"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	// A path is a stranger's text; no escape sequence of it reaches the terminal.
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("a control character reached the terminal: %q", out)
	}
}

func TestKennelReviewRefusesAnIncompleteAnswerAndABadScopeBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"kennel", "review", "kcr_1"},
		{"kennel", "review", "kcr_1", "--pull", "0"},
		{"kennel", "review", "repo_1", "--pull", "9"},
	} {
		e, out, errOut := newTestEnv(t)
		if code := dispatch(context.Background(), e, args); code == 0 || out.Len() != 0 {
			t.Fatalf("accepted %v: %s", args, errOut.String())
		}
	}
	srv := jsonRoutes(t, map[string]string{"/api/v1/kennel/repositories/kcr_1/pull-reviews/9": `{"version":2}`})
	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"kennel", "review", "kcr_1", "--pull", "9", "--url", srv.URL}); code == 0 {
		t.Fatalf("accepted an unsupported version: %s", errOut.String())
	}
}

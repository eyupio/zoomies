package optimisation

import (
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/prrepair"
)

const timeoutSource = `on: push
jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - run: make
  lint:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - run: make lint
`

func timeoutFixture() (TimeoutSource, TimeoutRequest) {
	file := prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: timeoutSource}
	return TimeoutSource{HeadSHA: strings.Repeat("b", 40), Workflow: file}, TimeoutRequest{BlobSHA: GitBlobSHA(file.Content), JobIndex: 0, Minutes: 30}
}

var timeoutPolicy = Policy{SourceAllowed: true, WorkflowEditsAllowed: true}

func TestGitBlobSHAIsTheNameGitGivesTheContent(t *testing.T) {
	// `printf 'hello\n' | git hash-object --stdin`
	if got := GitBlobSHA("hello\n"); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("got %s", got)
	}
}

func TestPreviewTimeoutAddsTheMinutesTheOperatorSuppliedToTheNamedJob(t *testing.T) {
	source, req := timeoutFixture()
	p, err := PreviewTimeout(source, req, timeoutPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "timeout-preview-v1" || p.Kind != "kennel_fix_timeout" || p.State != "preview_ready" || p.CheckCode != "ci.no_timeout" || p.JobName != "Build" || p.Minutes != 30 || p.Verification != "recheck" || p.BlobSHA != req.BlobSHA {
		t.Fatalf("preview %+v", p)
	}
	if !strings.Contains(p.File.Content, "    runs-on: ubuntu-latest\n    timeout-minutes: 30\n    steps:") || p.AfterSHA256 != hash(p.File.Content) || p.SourceSHA256 != hash(timeoutSource) {
		t.Fatalf("file %q", p.File.Content)
	}
	if strings.Count(p.File.Content, "timeout-minutes") != 2 {
		t.Fatal("the edit touched a job it was not asked about")
	}
	for _, want := range []string{"not inferred", "closes when Kennel Club next reads"} {
		if !strings.Contains(p.Explanation, want) {
			t.Errorf("explanation %q lacks %q", p.Explanation, want)
		}
	}
	if err := CheckTimeoutCurrent(p, source, req, timeoutPolicy); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewTimeoutRefusesWhatItCannotStandBehind(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TimeoutSource, *TimeoutRequest, *Policy)
	}{
		{"source reads off", func(_ *TimeoutSource, _ *TimeoutRequest, p *Policy) { p.SourceAllowed = false }},
		{"workflow edits off", func(_ *TimeoutSource, _ *TimeoutRequest, p *Policy) { p.WorkflowEditsAllowed = false }},
		{"a blob the finding did not name", func(_ *TimeoutSource, r *TimeoutRequest, _ *Policy) { r.BlobSHA = strings.Repeat("0", 40) }},
		{"minutes below one", func(_ *TimeoutSource, r *TimeoutRequest, _ *Policy) { r.Minutes = 0 }},
		{"minutes above the bound", func(_ *TimeoutSource, r *TimeoutRequest, _ *Policy) { r.Minutes = 1441 }},
		{"a job that already has a timeout", func(_ *TimeoutSource, r *TimeoutRequest, _ *Policy) { r.JobIndex = 1 }},
		{"a job that is not there", func(_ *TimeoutSource, r *TimeoutRequest, _ *Policy) { r.JobIndex = 7 }},
		{"a head that is not a commit", func(s *TimeoutSource, _ *TimeoutRequest, _ *Policy) { s.HeadSHA = "main" }},
		{"a file outside the root workflows", func(s *TimeoutSource, r *TimeoutRequest, _ *Policy) {
			s.Workflow.Path = "docs/ci.yml"
		}},
		{"a partial file", func(s *TimeoutSource, _ *TimeoutRequest, _ *Policy) { s.Workflow.Partial = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, req := timeoutFixture()
			policy := timeoutPolicy
			tc.mutate(&source, &req, &policy)
			if _, err := PreviewTimeout(source, req, policy); err != ErrTimeoutUnsupported {
				t.Fatalf("err = %v, want the one refusal", err)
			}
		})
	}
}

// A file that changed after the preview was kept has a new name, so the old
// preview is stale and must be made again rather than applied to other bytes.
func TestCheckTimeoutCurrentRefusesAChangedFileAndAChangedValue(t *testing.T) {
	source, req := timeoutFixture()
	p, err := PreviewTimeout(source, req, timeoutPolicy)
	if err != nil {
		t.Fatal(err)
	}
	moved := source
	moved.Workflow.Content = strings.Replace(timeoutSource, "make\n", "make all\n", 1)
	movedReq := req
	movedReq.BlobSHA = GitBlobSHA(moved.Workflow.Content)
	if err := CheckTimeoutCurrent(p, moved, movedReq, timeoutPolicy); err == nil {
		t.Error("a changed file was accepted")
	}
	other := req
	other.Minutes = 45
	if err := CheckTimeoutCurrent(p, source, other, timeoutPolicy); err == nil {
		t.Error("a different value was accepted")
	}
	p.State = "stale"
	if err := CheckTimeoutCurrent(p, source, req, timeoutPolicy); err == nil {
		t.Error("a preview that is not ready was accepted")
	}
}

package optimisation

import (
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/prrepair"
)

const readinessSource = `on: push
jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - name: Wait for the API
        run: sleep 30
      - name: Test
        run: echo ok
`

func readinessFixture() (ReadinessSource, ReadinessEvidence) {
	file := prrepair.File{Path: ".github/workflows/ci.yml", Mode: "100644", Content: readinessSource}
	source := ReadinessSource{HeadSHA: strings.Repeat("a", 40), Workflow: file}
	wait := int64(31000)
	return source, ReadinessEvidence{EvidenceID: "readiness:1", HeadSHA: source.HeadSHA, SourceSHA256: hash(file.Content), Runs: 4, WaitMS: &wait, Condition: "curl -fsS http://127.0.0.1:8080/health"}
}

func TestPreviewReadinessReplacesOneFixedSleepWithABoundedPoll(t *testing.T) {
	source, evidence := readinessFixture()
	p, err := PreviewReadiness(source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "readiness-preview-v1" || p.Kind != "bounded_readiness_poll" || p.State != "preview_ready" || p.WaitSeconds != 30 || p.Runs != 4 || p.WaitMS != 31000 || p.Condition != evidence.Condition || p.Verification != "inconclusive" {
		t.Fatalf("preview %+v", p)
	}
	if !strings.Contains(p.Edit.Before, "run: sleep 30") || !strings.Contains(p.Edit.After, "run: |") || !strings.Contains(p.Edit.After, "timeout 30 sh -c 'until curl -fsS http://127.0.0.1:8080/health; do sleep 1; done'") {
		t.Fatalf("edit %+v", p.Edit)
	}
	if strings.Contains(p.File.Content, "run: sleep 30") || !strings.Contains(p.File.Content, "timeout 30 sh -c") || p.AfterSHA256 != hash(p.File.Content) {
		t.Fatalf("file %+v", p.File)
	}
	if !strings.Contains(p.Explanation, "operator supplied") || !strings.Contains(p.Explanation, "saving is unverified") {
		t.Fatalf("explanation %q", p.Explanation)
	}
	if err := CheckReadinessCurrent(p, source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}); err != nil {
		t.Fatal(err)
	}
	changed := evidence
	changed.Condition = "curl -fsS http://127.0.0.1:9090/ready"
	if err := CheckReadinessCurrent(p, source, changed, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}); !errors.Is(err, ErrReadinessUnsupported) {
		t.Fatalf("changed condition accepted: %v", err)
	}
}

func TestPreviewReadinessQuotesAnOperatorCommandWithApostrophes(t *testing.T) {
	source, evidence := readinessFixture()
	evidence.Condition = "curl -fsS 'http://127.0.0.1/health'"
	p, err := PreviewReadiness(source, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Edit.After, `until curl -fsS '\''http://127.0.0.1/health'\''; do sleep 1; done`) {
		t.Fatalf("quoting: %q", p.Edit.After)
	}
}

func TestPreviewReadinessRefusesUnsupportedSourceEvidenceAndPolicy(t *testing.T) {
	source, evidence := readinessFixture()
	for name, run := range map[string]func() (ReadinessSource, ReadinessEvidence, Policy){
		"policy": func() (ReadinessSource, ReadinessEvidence, Policy) {
			return source, evidence, Policy{SourceAllowed: true}
		},
		"head": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.HeadSHA = strings.Repeat("b", 40)
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"digest": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.SourceSHA256 = strings.Repeat("b", 64)
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"empty condition": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.Condition = " "
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"multiline condition": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.Condition = "true\ntrue"
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"thin samples": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.Runs = 1
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"no wait": func() (ReadinessSource, ReadinessEvidence, Policy) {
			e := evidence
			e.WaitMS = nil
			return source, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"not a root workflow": func() (ReadinessSource, ReadinessEvidence, Policy) {
			s := source
			s.Workflow.Path = ".github/actions/x/action.yml"
			return s, evidence, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
		"no fixed sleep": func() (ReadinessSource, ReadinessEvidence, Policy) {
			s := source
			s.Workflow.Content = strings.ReplaceAll(s.Workflow.Content, "sleep 30", "echo waiting")
			e := evidence
			e.SourceSHA256 = hash(s.Workflow.Content)
			return s, e, Policy{SourceAllowed: true, WorkflowEditsAllowed: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, e, p := run()
			if _, err := PreviewReadiness(s, e, p); !errors.Is(err, ErrReadinessUnsupported) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

package docs

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/kennel"
)

func scoreSection(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../docs/kennel-club.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "\n## The score\n")
	if !ok {
		t.Fatal("docs/kennel-club.md has no 'The score' section")
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	return section
}

// The published weights and ceilings are the code's. A number changed in one
// place and not the other would make the formula on the page a different one
// from the formula that scores, which is the one thing publishing it is for.
func TestTheScoreDocsStateTheWeightsAndCeilingsTheCodeUses(t *testing.T) {
	s := scoreSection(t)
	for _, want := range []string{
		fmt.Sprintf("Start at %d", kennel.ScoreStart),
		fmt.Sprintf("| Error | %d |", kennel.ScoreErrorWeight),
		fmt.Sprintf("| Warning | %d |", kennel.ScoreWarningWeight),
		fmt.Sprintf("| Note | %d |", kennel.ScoreInfoWeight),
		fmt.Sprintf("at most **%d**", kennel.ScoreErrorCap),
		fmt.Sprintf("at most **%d**", kennel.ScoreWarningCap),
		fmt.Sprintf("This is version %d.", kennel.ScoreVersion),
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the score section does not say %q", want)
		}
	}
}

// The worked examples are run through Score, so an example that stops being
// true is caught.
func TestTheScoreDocsWorkedExamplesAreWhatScoreReturns(t *testing.T) {
	s := scoreSection(t)
	f := func(sev kennel.Severity, n int) []kennel.Finding {
		var out []kennel.Finding
		for i := 0; i < n; i++ {
			out = append(out, kennel.Finding{Code: kennel.Code(fmt.Sprintf("%s.%d", sev, i)), Severity: sev})
		}
		return out
	}
	join := func(parts ...[]kennel.Finding) []kennel.Finding {
		var out []kennel.Finding
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	examples := []struct {
		row      string
		findings []kennel.Finding
	}{
		{"One warning", f(kennel.SeverityWarning, 1)},
		{"One error and one note", join(f(kennel.SeverityError, 1), f(kennel.SeverityInfo, 1))},
		{"Three warnings and two notes", join(f(kennel.SeverityWarning, 3), f(kennel.SeverityInfo, 2))},
	}
	for _, ex := range examples {
		want := fmt.Sprintf("| %s | %d |", ex.row, kennel.Score(kennel.Evaluation{Findings: ex.findings}).Value)
		if !strings.Contains(s, want) {
			t.Errorf("the worked examples do not contain %q", want)
		}
	}
}

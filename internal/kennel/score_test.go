package kennel

import (
	"math/rand"
	"slices"
	"testing"
)

func scoreOf(fs ...Finding) ScoreResult { return Score(Evaluation{Findings: fs}) }

func fnd(code string, sev Severity, subject string) Finding {
	return Finding{Code: Code(code), Severity: sev, Subject: subject}
}

func TestAnEmptyEvaluationScoresOneHundred(t *testing.T) {
	if r := scoreOf(); r.Value != 100 || r.Cap != 0 || r.Version != ScoreVersion {
		t.Fatalf("got %+v", r)
	}
}

func TestEachSeverityTakesItsWeightAndTheCapsApply(t *testing.T) {
	tests := []struct {
		name    string
		in      []Finding
		value   int
		wantCap int
	}{
		{"one info", []Finding{fnd("a", SeverityInfo, "")}, 99, 0},
		{"one warning is capped below ninety", []Finding{fnd("a", SeverityWarning, "")}, 89, 89},
		{"a lone error would be 80 uncapped and is 59", []Finding{fnd("a", SeverityError, "")}, 59, 59},
		{"two warnings fall under the cap on their own", []Finding{fnd("a", SeverityWarning, ""), fnd("b", SeverityWarning, "")}, 88, 0},
		{"an error and an info stays under the error cap", []Finding{fnd("a", SeverityError, ""), fnd("b", SeverityInfo, "")}, 59, 59},
		{"enough errors floor at zero", []Finding{
			fnd("a", SeverityError, ""), fnd("b", SeverityError, ""), fnd("c", SeverityError, ""),
			fnd("d", SeverityError, ""), fnd("e", SeverityError, ""), fnd("f", SeverityError, ""),
		}, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := scoreOf(tc.in...)
			if r.Value != tc.value || r.Cap != tc.wantCap {
				t.Fatalf("got value %d cap %d, want %d cap %d", r.Value, r.Cap, tc.value, tc.wantCap)
			}
		})
	}
}

// A finding repeated for one subject is one problem, not two, so reporting it
// twice cannot make a repository look worse than it is.
func TestTheSameCodeAndSubjectCountsOnce(t *testing.T) {
	r := scoreOf(fnd("a", SeverityWarning, "x"), fnd("a", SeverityWarning, "x"))
	if r.Open.Warning != 1 || r.Value != 89 {
		t.Fatalf("got %+v", r)
	}
	if r := scoreOf(fnd("a", SeverityWarning, "x"), fnd("a", SeverityWarning, "y")); r.Open.Warning != 2 {
		t.Fatalf("different subjects must both count, got %+v", r)
	}
}

func TestWaivedAndUnreadAndDisabledAreCarriedNotScored(t *testing.T) {
	r := Score(Evaluation{
		Ran:        []Code{"a", "b", "c"},
		Skipped:    []Skipped{{Code: "d"}},
		Incomplete: []Code{"e"},
		Disabled:   []Code{"f"},
		Waived:     []WaivedFinding{{}, {}},
	})
	if r.Value != 100 || r.Checked != 3 || r.Unread != 2 || r.Disabled != 1 || r.Waived != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestAddingAnOpenFindingNeverRaisesTheScoreAndOrderDoesNotMatter(t *testing.T) {
	sevs := []Severity{SeverityError, SeverityWarning, SeverityInfo}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		var fs []Finding
		for n := rng.Intn(8); n > 0; n-- {
			fs = append(fs, fnd(string(rune('a'+rng.Intn(5))), sevs[rng.Intn(3)], string(rune('x'+rng.Intn(2)))))
		}
		before := scoreOf(fs...)
		more := append(slices.Clone(fs), fnd(string(rune('a'+rng.Intn(5))), sevs[rng.Intn(3)], "z"))
		if after := scoreOf(more...); after.Value > before.Value {
			t.Fatalf("adding a finding raised %d to %d: %+v", before.Value, after.Value, fs)
		}
		shuffled := slices.Clone(fs)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if scoreOf(shuffled...) != before {
			t.Fatalf("order changed the result for %+v", fs)
		}
	}
}

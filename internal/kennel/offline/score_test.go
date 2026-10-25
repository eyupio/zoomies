package offline

import (
	"testing"

	"github.com/eyupio/zoomies/internal/kennel"
)

// The same findings give the same number here as on a controller, because both
// call kennel.Score; what differs is how much could be read, which the result
// carries so the number is never taken for a repository's whole score.
func TestTheOfflineScoreCountsWhatItFoundAndSaysWhatItCouldNotRead(t *testing.T) {
	r, err := Check("testdata/findings", Options{Public: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) == 0 {
		t.Fatal("the fixture found nothing, so there is no score to take anything from")
	}
	if r.Score.Version != kennel.ScoreVersion || r.Score.Checked == 0 || r.Score.Value >= kennel.ScoreStart {
		t.Errorf("score = %+v", r.Score)
	}
	if r.Score.Unread+r.Score.Disabled != len(r.NotChecked) {
		t.Errorf("unread %d and disabled %d, want the %d checks listed as not checked", r.Score.Unread, r.Score.Disabled, len(r.NotChecked))
	}
	if clean, err := Check("testdata/clean", Options{}); err != nil || clean.Score.Value != kennel.ScoreStart {
		t.Errorf("a clean checkout scored %+v (%v), want %d", clean.Score, err, kennel.ScoreStart)
	}
}

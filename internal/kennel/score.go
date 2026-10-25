package kennel

// ScoreVersion changes whenever a weight, a cap or the rule for counting
// changes, so a number from one formula is never compared with a number from
// another without saying so.
const ScoreVersion = 1

// What an open finding takes off the starting 100, and the ceilings the worst
// open severity imposes. They are exported because docs/kennel-club.md states
// them and a test holds the two together.
const (
	ScoreStart         = 100
	ScoreErrorWeight   = 20
	ScoreWarningWeight = 6
	ScoreInfoWeight    = 1
	// ScoreErrorCap and ScoreWarningCap keep a lone exposed repository from
	// reading like a good grade: without them one error and nothing else wrong
	// scores 80. It is the rule that an error is never hidden, in numbers.
	ScoreErrorCap   = 59
	ScoreWarningCap = 89
)

// ScoreResult is a repository's number and everything needed to recompute it.
type ScoreResult struct {
	Version int `json:"version"`
	Value   int `json:"value"`
	// Checked is how many checks were judged. Unread is how many could not be:
	// skipped for want of a permission or a read, or run against a partial one.
	// The score is of the checks that could be read, and is never a total while
	// Unread is above zero.
	Checked  int `json:"checked"`
	Unread   int `json:"unread"`
	Disabled int `json:"disabled"`
	// Waived findings take nothing away and are counted beside the number, so a
	// waiver cannot make a score look better without saying how many there are.
	Waived int `json:"waived"`
	// Open are the counts the number was built from, after two findings with
	// one code and one subject are counted once.
	Open Counts `json:"open"`
	// Cap is the ceiling that applied, or 0 when none did.
	Cap int `json:"cap"`
}

// Score works the number out of an evaluation. It is pure and reads nothing the
// evaluation does not carry, so the controller and the offline check agree.
func Score(e Evaluation) ScoreResult {
	r := ScoreResult{
		Version:  ScoreVersion,
		Checked:  len(e.Ran),
		Unread:   len(e.Skipped) + len(e.Incomplete),
		Disabled: len(e.Disabled),
		Waived:   len(e.Waived),
	}
	type key struct {
		code    Code
		subject string
	}
	worst := map[key]Severity{}
	for _, f := range e.Findings {
		k := key{f.Code, f.Subject}
		if cur, seen := worst[k]; !seen || f.Severity.rank() > cur.rank() {
			worst[k] = f.Severity
		}
	}
	taken := 0
	for _, s := range worst {
		switch s {
		case SeverityError:
			r.Open.Error++
			taken += ScoreErrorWeight
		case SeverityWarning:
			r.Open.Warning++
			taken += ScoreWarningWeight
		case SeverityInfo:
			r.Open.Info++
			taken += ScoreInfoWeight
		}
	}
	r.Open.Waived = r.Waived
	v := max(ScoreStart-taken, 0)
	switch {
	case r.Open.Error > 0 && v > ScoreErrorCap:
		v, r.Cap = ScoreErrorCap, ScoreErrorCap
	case r.Open.Error == 0 && r.Open.Warning > 0 && v > ScoreWarningCap:
		v, r.Cap = ScoreWarningCap, ScoreWarningCap
	}
	r.Value = v
	return r
}

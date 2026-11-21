package analysis

import (
	"strings"
	"testing"
)

// These predicates decide whether evidence a controller returned is well formed,
// on the CLI and in the MCP tools alike, so a case that one of them used to
// accept and another to refuse is exactly what has to be pinned here.
func TestLowerHexIsExactlyLowerCaseHexOfTheLengthAsked(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		n    int
		want bool
	}{
		"a full digest":           {strings.Repeat("a1", 32), 64, true},
		"a full commit":           {strings.Repeat("0f", 20), 40, true},
		"one short":               {strings.Repeat("a", 63), 64, false},
		"one long":                {strings.Repeat("a", 65), 64, false},
		"upper case":              {strings.Repeat("A", 40), 40, false},
		"mixed case":              {strings.Repeat("aB", 20), 40, false},
		"not hex":                 {strings.Repeat("g", 40), 40, false},
		"a digest where a commit": {strings.Repeat("a", 64), 40, false},
		"empty":                   {"", 40, false},
		"empty of no length":      {"", 0, true},
		"a space":                 {strings.Repeat("a", 39) + " ", 40, false},
		"non-ASCII digits":        {strings.Repeat("a", 38) + "é", 40, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := LowerHex(tc.in, tc.n); got != tc.want {
				t.Fatalf("LowerHex(%q, %d) = %v, want %v", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestDecimalIDIsAPositiveNumberWrittenTheOnlyWayItCanBe(t *testing.T) {
	for in, want := range map[string]bool{
		"1": true, "123456789012": true, "9223372036854775807": true,
		"0": false, "-1": false, "01": false, "+1": false, "1.0": false, " 1": false, "1 ": false, "": false,
		"9223372036854775808": false, "0x10": false, "１": false,
	} {
		if got := DecimalID(in); got != want {
			t.Errorf("DecimalID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEventNameIsALowerCaseWordOfBoundedLength(t *testing.T) {
	for in, want := range map[string]bool{
		"push": true, "pull_request": true, "workflow_dispatch": true, "a1_": true, strings.Repeat("a", 64): true,
		"": false, strings.Repeat("a", 65): false, "Push": false, "pull-request": false, "push ": false, "push\n": false, "é": false,
	} {
		if got := EventName(in); got != want {
			t.Errorf("EventName(%q) = %v, want %v", in, got, want)
		}
	}
}

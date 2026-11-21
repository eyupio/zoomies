package analysis

import (
	"strconv"
	"strings"
)

// RootDependencies is a reviewed public whitelist independent of private parser
// and store types. Null counts mean unsupported inspection, never known zero.
type RootDependencies struct {
	Version      string                    `json:"version"`
	Basis        string                    `json:"basis"`
	SourceSHA256 string                    `json:"source_sha256"`
	Coverage     string                    `json:"coverage"`
	Counts       *RootDependencyCounts     `json:"counts"`
	RowsOmitted  *bool                     `json:"rows_omitted"`
	References   []RootDependencyReference `json:"references"`
}
type RootDependencyCounts struct {
	Declarations *int `json:"declarations"`
	CommitPins   *int `json:"commit_pins"`
	DigestPins   *int `json:"digest_pins"`
	Floating     *int `json:"floating"`
	Local        *int `json:"local"`
	Expressions  *int `json:"expressions"`
	Unknown      *int `json:"unknown"`
}
type RootDependencyReference struct {
	JobIndex        *int    `json:"job_index"`
	StepIndex       *int    `json:"step_index"`
	Line            *int    `json:"line"`
	Declaration     string  `json:"declaration"`
	Kind            string  `json:"kind"`
	Pin             string  `json:"pin"`
	ReferenceSHA256 string  `json:"reference_sha256"`
	Revision        *string `json:"revision"`
}

const RootDependencyRows = 50

func ValidRootDependencies(e *RootDependencies, digest string) bool {
	if e == nil {
		return true
	}
	if e.Version != "root-dependencies-v1" || e.Basis != "root_uses_declarations" || e.SourceSHA256 != digest || !LowerHex(digest, 64) || e.RowsOmitted == nil || e.References == nil || len(e.References) > RootDependencyRows {
		return false
	}
	if e.Coverage == "unsupported" {
		return e.Counts == nil && !*e.RowsOmitted && len(e.References) == 0
	}
	c := e.Counts
	if e.Coverage != "enumerated" || c == nil {
		return false
	}
	values := []*int{c.Declarations, c.CommitPins, c.DigestPins, c.Floating, c.Local, c.Expressions, c.Unknown}
	for _, v := range values {
		if v == nil || *v < 0 || *v > 10000 {
			return false
		}
	}
	n := *c.Declarations
	counts := map[string]int{"commit": *c.CommitPins, "digest": *c.DigestPins, "floating": *c.Floating, "local": *c.Local, "expression": *c.Expressions, "unknown": *c.Unknown}
	total := 0
	for _, v := range counts {
		total += v
	}
	if n != total || n < len(e.References) || *e.RowsOmitted != (n > len(e.References)) {
		return false
	}
	seen := map[string]bool{}
	shown := map[string]int{}
	for _, r := range e.References {
		if r.JobIndex == nil || r.StepIndex == nil || r.Line == nil || *r.JobIndex < 0 || *r.JobIndex > 20000 || *r.StepIndex < -1 || *r.StepIndex > 20000 || *r.Line < 1 || *r.Line > 262144 || !LowerHex(r.ReferenceSHA256, 64) {
			return false
		}
		key := strconv.Itoa(*r.JobIndex) + ":" + strconv.Itoa(*r.StepIndex)
		if seen[key] {
			return false
		}
		seen[key] = true
		if r.Declaration == "job_workflow" {
			if *r.StepIndex != -1 {
				return false
			}
		} else if r.Declaration != "step_action" || *r.StepIndex < 0 {
			return false
		}
		kind := "unknown"
		switch r.Kind {
		case "remote":
			if r.Pin != "commit" && r.Pin != "floating" {
				return false
			}
			kind = r.Pin
		case "docker":
			if r.Declaration != "step_action" || r.Pin != "digest" && r.Pin != "floating" {
				return false
			}
			kind = r.Pin
		case "local", "expression", "unknown":
			if r.Pin != "unresolved" {
				return false
			}
			kind = r.Kind
		default:
			return false
		}
		if r.Pin == "commit" {
			if r.Revision == nil || !LowerHex(*r.Revision, 40) {
				return false
			}
		} else if r.Pin == "digest" {
			if r.Revision == nil || !strings.HasPrefix(*r.Revision, "sha256:") || !LowerHex(strings.TrimPrefix(*r.Revision, "sha256:"), 64) {
				return false
			}
		} else if r.Revision != nil {
			return false
		}
		shown[kind]++
	}
	for kind, v := range counts {
		if v < shown[kind] || v-shown[kind] > n-len(e.References) {
			return false
		}
	}
	return true
}

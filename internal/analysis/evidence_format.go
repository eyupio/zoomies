package analysis

import "strconv"

// What the evidence a controller returns for a comparison must look like, written
// once. The CLI and the MCP tools each decode it into types of their own, on
// purpose (what a human sees and what a model is sent are separate whitelists),
// and each checks the decoded evidence against these before using any of it. A
// number or a version that is restated in each can differ between them without a
// test noticing, and the evidence one accepts and the other refuses is the one
// that has to be explained.
const (
	// WorkflowEvidenceVersion and RunnerEvidenceVersion are the versions of the
	// two kinds of evidence a comparison carries.
	WorkflowEvidenceVersion = "workflow-evidence-v1"
	RunnerEvidenceVersion   = "runner-evidence-v1"
	// RootSourceBasis is the only way a workflow's root file is chosen: the file
	// at the head commit of a push.
	RootSourceBasis = "push_head_sha_root_file"
	// MaxEvidenceRows bounds the attempts either arm of a comparison selects and
	// the attempt rows and job rows it shows, so no side of a comparison carries
	// more evidence than that.
	MaxEvidenceRows = 100
	// MaxJobsPerAttempt bounds the jobs one attempt can have, which bounds the
	// runner evidence's job count by the attempts selected.
	MaxJobsPerAttempt = 500
	// MaxRootSourceBytes is the largest root file the evidence describes.
	MaxRootSourceBytes = 128 * 1024
)

// LowerHex reports whether s is exactly n lower-case hexadecimal digits: the form
// of every digest and commit the evidence carries. Upper case is refused, so one
// value has one spelling and evidence cannot differ from itself in case.
func LowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// DecimalID reports whether s is a positive 64-bit number written the one way it
// can be, with no sign, no leading zero and no space: the form of a run's ID.
func DecimalID(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

// EventName reports whether s could be the name of a workflow event: lower case
// letters, digits and underscores, at most 64 of them.
func EventName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

package workflow

import (
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxTimeoutMinutes bounds the value an operator may supply. GitHub's own
// ceiling for a hosted job is 360; a self-hosted fleet can run longer, but a
// value above a day is a decision to keep a job without a timeout in all but
// name, and the preview refuses it rather than write it.
const MaxTimeoutMinutes = 1440

var errTimeoutUnsupported = errors.New("workflow: a timeout can be added to an executable job with a one-line runs-on and no timeout-minutes")

// TimeoutChange returns the exact bytes to give one job a timeout-minutes. The
// job must be one Inspect reports as having none, and its runs-on must be a
// plain one-line scalar: the new key goes on the line after it, at its
// indentation, and a runs-on that spans lines or sits in a flow mapping is a
// shape this function will not guess its way around. The caller supplies the
// minutes because the right value is a fact about the job, not about the file.
//
// before runs from the job's own key to the end of its runs-on line, so it
// names the job and is unique even when two jobs run on the same label; the
// bytes outside it are untouched, so comments and line endings are preserved.
func TimeoutChange(data []byte, jobIndex, minutes int) (before, after string, line int, err error) {
	if minutes < 1 || minutes > MaxTimeoutMinutes {
		return "", "", 0, errTimeoutUnsupported
	}
	facts, err := Inspect(data)
	if err != nil {
		return "", "", 0, err
	}
	missing := false
	for _, at := range facts.NoTimeout {
		if at.JobIndex == jobIndex {
			missing = true
		}
	}
	if !missing {
		return "", "", 0, errTimeoutUnsupported
	}
	root, err := parseRoot(data)
	if err != nil {
		return "", "", 0, err
	}
	jobs := field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || jobIndex < 0 || jobIndex >= len(jobs.Content)/2 {
		return "", "", 0, errTimeoutUnsupported
	}
	jobKey, job := jobs.Content[jobIndex*2], jobs.Content[jobIndex*2+1]
	if job.Kind != yaml.MappingNode || job.Style&yaml.FlowStyle != 0 {
		return "", "", 0, errTimeoutUnsupported
	}
	runsKey, runs := keyAndValue(job, "runs-on")
	if runsKey == nil || runs == nil || runs.Kind != yaml.ScalarNode || runs.Line != runsKey.Line {
		return "", "", 0, errTimeoutUnsupported
	}
	lines := strings.SplitAfter(string(data), "\n")
	if jobKey.Line < 1 || runsKey.Line < jobKey.Line || runsKey.Line > len(lines) {
		return "", "", 0, errTimeoutUnsupported
	}
	runsLine := lines[runsKey.Line-1]
	ending := "\n"
	if strings.HasSuffix(runsLine, "\r\n") {
		ending = "\r\n"
	}
	span := lines[jobKey.Line-1 : runsKey.Line]
	before = strings.Join(span, "")
	if !strings.HasSuffix(runsLine, "\n") {
		// The file ends on this line. The key is added after a line break that
		// the original did not have, and the preview shows that.
		span = append(append([]string(nil), span[:len(span)-1]...), runsLine+ending)
	}
	indent := strings.Repeat(" ", max(runsKey.Column-1, 0))
	after = strings.Join(span, "") + indent + "timeout-minutes: " + strconv.Itoa(minutes) + ending
	if strings.Count(string(data), before) != 1 {
		return "", "", 0, errTimeoutUnsupported
	}
	// The scalar's node starts on the key's line whether or not it ends there:
	// a folded or literal block (`runs-on: >-`) or a quoted scalar that wraps
	// holds its text on the lines after, and a key put between them breaks the
	// file or is swallowed into the label. So the edit is read back, and is
	// offered only if the job gained exactly the timeout and kept its runner.
	if !timeoutEditHolds(data, before, after, jobIndex, minutes, runs.Value) {
		return "", "", 0, errTimeoutUnsupported
	}
	return before, after, runsKey.Line + 1, nil
}

// timeoutEditHolds says whether the file with the edit applied is still the
// same workflow with one added key on the same job: the document parses, no
// job was added or lost, the job has the timeout and its runner label as
// before, and it has exactly one key more.
func timeoutEditHolds(data []byte, before, after string, jobIndex, minutes int, runner string) bool {
	edited := []byte(strings.Replace(string(data), before, after, 1))
	orig, err := parseRoot(data)
	if err != nil {
		return false
	}
	root, err := parseRoot(edited)
	if err != nil {
		return false
	}
	was, now := field(orig, "jobs"), field(root, "jobs")
	if was == nil || now == nil || now.Kind != yaml.MappingNode || len(now.Content) != len(was.Content) || jobIndex*2+1 >= len(now.Content) {
		return false
	}
	job, prior := now.Content[jobIndex*2+1], was.Content[jobIndex*2+1]
	if job.Kind != yaml.MappingNode || len(job.Content) != len(prior.Content)+2 {
		return false
	}
	_, timeout := keyAndValue(job, "timeout-minutes")
	_, runs := keyAndValue(job, "runs-on")
	return timeout != nil && timeout.Kind == yaml.ScalarNode && timeout.Value == strconv.Itoa(minutes) &&
		runs != nil && runs.Kind == yaml.ScalarNode && runs.Value == runner
}

// TimeoutJobName is the job's own name for a preview to show: its literal name
// when it has one, or else its key. An expression or an oversized name is
// refused so that no text from the file reaches a page unchecked.
func TimeoutJobName(data []byte, jobIndex int) (string, error) {
	root, err := parseRoot(data)
	if err != nil {
		return "", err
	}
	jobs := field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || jobIndex < 0 || jobIndex >= len(jobs.Content)/2 {
		return "", errTimeoutUnsupported
	}
	literal := func(n *yaml.Node) (string, bool) {
		if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" || len(n.Value) > 128 || strings.TrimSpace(n.Value) != n.Value || strings.Contains(n.Value, "${{") || strings.ContainsAny(n.Value, "\r\n\x00") {
			return "", false
		}
		return n.Value, true
	}
	if name, ok := literal(field(jobs.Content[jobIndex*2+1], "name")); ok {
		return name, nil
	}
	if key, ok := literal(jobs.Content[jobIndex*2]); ok {
		return key, nil
	}
	return "", errTimeoutUnsupported
}

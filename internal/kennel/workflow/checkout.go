package workflow

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// CheckoutDepthChange preserves the original line, including comments and line
// endings. Unsupported syntax is refused rather than reserialised as new YAML.
func CheckoutDepthChange(data []byte, jobIndex, stepIndex int) (before, after string, line int, err error) {
	if _, err = Inspect(data); err != nil {
		return
	}
	root, parseErr := parseRoot(data)
	if parseErr != nil {
		err = parseErr
		return
	}
	jobs := field(root, "jobs")
	if jobIndex < 0 || jobIndex >= len(jobs.Content)/2 {
		err = errUnsupported
		return
	}
	job := jobs.Content[jobIndex*2+1]
	steps := field(job, "steps")
	if field(job, "uses") != nil || steps == nil || steps.Kind != yaml.SequenceNode || stepIndex < 0 || stepIndex >= len(steps.Content) {
		err = errUnsupported
		return
	}
	step := steps.Content[stepIndex]
	uses := field(step, "uses")
	if field(step, "run") != nil || uses == nil || uses.Kind != yaml.ScalarNode || !strings.HasPrefix(uses.Value, "actions/checkout@") || !commitSHA.MatchString(strings.TrimPrefix(uses.Value, "actions/checkout@")) {
		err = errUnsupported
		return
	}
	with := field(step, "with")
	if with == nil || with.Kind != yaml.MappingNode || with.Style&yaml.FlowStyle != 0 {
		err = errUnsupported
		return
	}
	depth := field(with, "fetch-depth")
	if depth == nil || depth.Kind != yaml.ScalarNode || depth.Value != "0" || depth.Style != 0 || depth.Tag != "!!int" || field(with, "fetch-tags") != nil || field(with, "ref") != nil || field(with, "repository") != nil {
		err = errUnsupported
		return
	}
	lines := strings.SplitAfter(string(data), "\n")
	line = depth.Line
	if line < 1 || line > len(lines) {
		err = errUnsupported
		return
	}
	before = lines[line-1]
	offset := depth.Column - 1
	if offset < 0 || offset >= len(before) || before[offset] != '0' || strings.Count(string(data), before) != 1 {
		err = errUnsupported
		return
	}
	after = before[:offset] + "1" + before[offset+1:]
	return
}

// CheckoutNames supplies a conservative association with API names. It does not
// attest executed bytes. Dynamic/matrix/reusable job names cannot prove a position.
func CheckoutNames(data []byte, jobIndex, stepIndex int) (jobName, stepName string, err error) {
	if _, _, _, err = CheckoutDepthChange(data, jobIndex, stepIndex); err != nil {
		return
	}
	return namesAt(data, jobIndex, stepIndex)
}

// namesAt reads the literal job and step names at one position. Explicit names
// avoid depending on GitHub's generated naming rules; duplicate names in the
// file or the job make the association ambiguous.
func namesAt(data []byte, jobIndex, stepIndex int) (jobName, stepName string, err error) {
	root, err := parseRoot(data)
	if err != nil {
		return "", "", err
	}
	jobs := field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || jobIndex < 0 || jobIndex >= len(jobs.Content)/2 {
		return "", "", errUnsupported
	}
	literal := func(n *yaml.Node) (string, bool) {
		if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" || len(n.Value) > 128 || strings.TrimSpace(n.Value) != n.Value || strings.Contains(n.Value, "${{") || strings.ContainsAny(n.Value, "\r\n\x00") {
			return "", false
		}
		return n.Value, true
	}
	seen := map[string]bool{}
	for i := 0; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i+1]
		if field(job, "strategy") != nil || field(job, "uses") != nil {
			return "", "", errUnsupported
		}
		name := field(job, "name")
		if name == nil {
			name = jobs.Content[i]
		}
		text, ok := literal(name)
		if !ok || seen[text] {
			return "", "", errUnsupported
		}
		seen[text] = true
		if i/2 == jobIndex {
			if field(job, "name") == nil {
				return "", "", errUnsupported
			}
			jobName = text
		}
	}
	steps := field(jobs.Content[jobIndex*2+1], "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode || stepIndex < 0 || stepIndex >= len(steps.Content) {
		return "", "", errUnsupported
	}
	seen = map[string]bool{}
	for i, step := range steps.Content {
		name, ok := literal(field(step, "name"))
		if !ok || seen[name] {
			return "", "", errUnsupported
		}
		seen[name] = true
		if i == stepIndex {
			stepName = name
		}
	}
	return jobName, stepName, nil
}

// ErrCheckoutNotFound and ErrCheckoutAmbiguous say why FindCheckout could not
// choose, so a caller can tell somebody whether to look at another job or to name
// the step themselves.
var (
	ErrCheckoutNotFound  = errors.New("workflow: no checkout step in that job can be previewed")
	ErrCheckoutAmbiguous = errors.New("workflow: more than one checkout step in that job can be previewed")
)

// FindCheckout locates the one previewable checkout step in the job GitHub
// reports under jobName, so nobody has to count positions in a file. Every
// condition CheckoutNames and CheckoutDepthChange impose still applies to the
// step it returns; this only does the counting. Two candidates is an error, not a
// guess.
func FindCheckout(data []byte, jobName string) (jobIndex, stepIndex int, err error) {
	root, parseErr := parseRoot(data)
	if parseErr != nil {
		return 0, 0, parseErr
	}
	jobs := field(root, "jobs")
	if jobs == nil {
		return 0, 0, ErrCheckoutNotFound
	}
	found := 0
	for j := 0; j < len(jobs.Content)/2; j++ {
		steps := field(jobs.Content[j*2+1], "steps")
		if steps == nil {
			continue
		}
		for s := range steps.Content {
			name, _, namesErr := CheckoutNames(data, j, s)
			if namesErr != nil || name != jobName {
				continue
			}
			jobIndex, stepIndex = j, s
			found++
		}
	}
	switch found {
	case 0:
		return 0, 0, ErrCheckoutNotFound
	case 1:
		return jobIndex, stepIndex, nil
	}
	return 0, 0, ErrCheckoutAmbiguous
}

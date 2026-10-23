package workflow

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

var errCacheUnsupported = errors.New("setup cache change needs one pinned setup-node step with a block with mapping")

// SetupCacheChange returns the exact bytes to add a package-manager cache to
// one actions/setup-node step. The step must be pinned to a commit and must not
// already name a cache. The caller supplies the package manager because the
// lockfile it was read from decides it; this function does not guess.
//
// The edit inserts the new key immediately after the step's with: line, at the
// indentation of the first key already there. The bytes before and after are
// contiguous slices, so the existing comments and line endings are preserved.
func SetupCacheChange(data []byte, jobIndex, stepIndex int, manager string) (before, after string, line int, err error) {
	if manager != "npm" && manager != "yarn" && manager != "pnpm" {
		return "", "", 0, errCacheUnsupported
	}
	if _, err = Inspect(data); err != nil {
		return "", "", 0, err
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
	if field(step, "run") != nil || uses == nil || uses.Kind != yaml.ScalarNode || !strings.HasPrefix(uses.Value, "actions/setup-node@") || !commitSHA.MatchString(strings.TrimPrefix(uses.Value, "actions/setup-node@")) {
		err = errUnsupported
		return
	}
	withKey, with := keyAndValue(step, "with")
	if with == nil || with.Kind != yaml.MappingNode || with.Style&yaml.FlowStyle != 0 || field(with, "cache") != nil || len(with.Content) < 2 {
		err = errUnsupported
		return
	}
	lines := strings.SplitAfter(string(data), "\n")
	if withKey.Line < 1 || withKey.Line > len(lines) {
		err = errUnsupported
		return
	}
	firstKey := with.Content[0]
	if firstKey.Line <= withKey.Line || firstKey.Line > len(lines) {
		err = errUnsupported
		return
	}
	indent := strings.Repeat(" ", max(firstKey.Column-1, 0))
	withLine := lines[withKey.Line-1]
	throughFirst := strings.Join(lines[withKey.Line-1:firstKey.Line], "")
	before = throughFirst
	ending := "\n"
	if strings.HasSuffix(withLine, "\r\n") {
		ending = "\r\n"
	}
	insert := indent + "cache: " + manager + ending
	if !strings.HasSuffix(withLine, "\n") {
		withLine += ending
	}
	after = withLine + insert + strings.Join(lines[withKey.Line:firstKey.Line], "")
	if strings.Count(string(data), before) != 1 {
		return "", "", 0, errCacheUnsupported
	}
	return before, after, withKey.Line + 1, nil
}

var (
	ErrSetupNodeNotFound  = errors.New("no pinned actions/setup-node step")
	ErrSetupNodeAmbiguous = errors.New("more than one pinned actions/setup-node step")
)

// FindSetupNode returns the one pinned actions/setup-node step in a workflow.
// Two candidates are an explained refusal rather than a guess: a cache added to
// the wrong one would change the wrong package manager's behaviour.
func FindSetupNode(data []byte) (jobIndex, stepIndex int, err error) {
	if _, err = Inspect(data); err != nil {
		return 0, 0, err
	}
	root, parseErr := parseRoot(data)
	if parseErr != nil {
		return 0, 0, parseErr
	}
	jobs := field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return 0, 0, errUnsupported
	}
	found := false
	for i := 0; i < len(jobs.Content); i += 2 {
		index := i / 2
		job := jobs.Content[i+1]
		if job.Kind != yaml.MappingNode || field(job, "uses") != nil {
			continue
		}
		steps := field(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for j, step := range steps.Content {
			uses := field(step, "uses")
			if step.Kind != yaml.MappingNode || uses == nil || uses.Kind != yaml.ScalarNode {
				continue
			}
			if !strings.HasPrefix(uses.Value, "actions/setup-node@") || !commitSHA.MatchString(strings.TrimPrefix(uses.Value, "actions/setup-node@")) {
				continue
			}
			if found {
				return 0, 0, ErrSetupNodeAmbiguous
			}
			jobIndex, stepIndex, found = index, j, true
		}
	}
	if !found {
		return 0, 0, ErrSetupNodeNotFound
	}
	return jobIndex, stepIndex, nil
}

// SetupNames returns the literal job and step names for the one pinned
// setup-node step. The cache preview matches them against the fleet job's
// GitHub-reported names; an unnamed step cannot be matched safely.
func SetupNames(data []byte, jobIndex, stepIndex int) (jobName, stepName string, err error) {
	foundJob, foundStep, err := FindSetupNode(data)
	if err != nil || foundJob != jobIndex || foundStep != stepIndex {
		return "", "", errUnsupported
	}
	return namesAt(data, jobIndex, stepIndex)
}

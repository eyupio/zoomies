package workflow

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrFixedSleepNotFound and ErrFixedSleepAmbiguous say why a readiness preview
// could not choose a wait. A workflow with two fixed sleeps needs the operator
// to pick one; this function does not guess.
var (
	ErrFixedSleepNotFound  = errors.New("workflow: no fixed sleep step on an Ubuntu runner can be previewed")
	ErrFixedSleepAmbiguous = errors.New("workflow: more than one fixed sleep step on Ubuntu runners can be previewed")
)

var fixedSleep = regexp.MustCompile(`^sleep[ \t]+([0-9]{1,5})$`)

// FindFixedSleep returns the one step whose run script is exactly a whole
// number of seconds of sleep, on a job with a literal Ubuntu runs-on label.
// A block scalar must contain that one command and nothing else. Comments,
// other commands, other shells and expressions are refused rather than
// interpreted: the preview must not turn a script someone else wrote into a
// different one.
func FindFixedSleep(data []byte) (jobIndex, stepIndex, seconds int, err error) {
	if _, err = Inspect(data); err != nil {
		return 0, 0, 0, err
	}
	root, parseErr := parseRoot(data)
	if parseErr != nil {
		err = parseErr
		return
	}
	jobs := field(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		err = errUnsupported
		return
	}
	found := false
	for i := 0; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i+1]
		if job.Kind != yaml.MappingNode || field(job, "uses") != nil {
			continue
		}
		runs := field(job, "runs-on")
		if runs == nil || runs.Kind != yaml.ScalarNode || runs.Tag != "!!str" || runs.Style != 0 || !strings.HasPrefix(runs.Value, "ubuntu-") || strings.Contains(runs.Value, "${{") || strings.ContainsAny(runs.Value, "\r\n\x00") {
			continue
		}
		steps := field(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for j, step := range steps.Content {
			if step.Kind != yaml.MappingNode || field(step, "uses") != nil {
				continue
			}
			run := field(step, "run")
			n, ok := fixedSleepNode(run)
			if !ok {
				continue
			}
			if found {
				return 0, 0, 0, ErrFixedSleepAmbiguous
			}
			jobIndex, stepIndex, seconds, found = i/2, j, n, true
		}
	}
	if !found {
		return 0, 0, 0, ErrFixedSleepNotFound
	}
	return jobIndex, stepIndex, seconds, nil
}

func fixedSleepNode(n *yaml.Node) (int, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Style != 0 && n.Style&yaml.LiteralStyle == 0 {
		return 0, false
	}
	value := strings.TrimSuffix(n.Value, "\n")
	if strings.ContainsAny(value, "\r\n\x00") || strings.Contains(value, "#") {
		return 0, false
	}
	m := fixedSleep.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return 0, false
	}
	seconds, err := strconv.Atoi(m[1])
	if err != nil || seconds < 2 || seconds > 3600 {
		return 0, false
	}
	return seconds, true
}

// ReadinessNames returns the literal job and step names for the one fixed sleep
// step, so the preview can be matched to the fleet job GitHub reported.
func ReadinessNames(data []byte, jobIndex, stepIndex int) (jobName, stepName string, err error) {
	foundJob, foundStep, _, err := FindFixedSleep(data)
	if err != nil || foundJob != jobIndex || foundStep != stepIndex {
		return "", "", errUnsupported
	}
	return namesAt(data, jobIndex, stepIndex)
}

// ReadinessChange replaces the fixed sleep's run scalar with one bounded shell
// loop. The script is supplied by the caller and must be one line: this
// function does not infer a condition. Bytes outside the run block are
// contiguous slices, so comments and line endings elsewhere are preserved.
func ReadinessChange(data []byte, jobIndex, stepIndex int, script string) (before, after string, line int, err error) {
	if script == "" || len(script) > 4096 || strings.ContainsAny(script, "\r\n\x00") {
		return "", "", 0, errUnsupported
	}
	foundJob, foundStep, _, err := FindFixedSleep(data)
	if err != nil || foundJob != jobIndex || foundStep != stepIndex {
		return "", "", 0, errUnsupported
	}
	root, parseErr := parseRoot(data)
	if parseErr != nil {
		err = parseErr
		return
	}
	jobs := field(root, "jobs")
	job := jobs.Content[jobIndex*2+1]
	step := field(job, "steps").Content[stepIndex]
	runKey, run := keyAndValue(step, "run")
	if runKey == nil || run == nil || run.Style != 0 && run.Style&yaml.LiteralStyle == 0 {
		return "", "", 0, errUnsupported
	}
	lines := strings.SplitAfter(string(data), "\n")
	if runKey.Line < 1 || runKey.Line > len(lines) {
		return "", "", 0, errUnsupported
	}
	keyLine := lines[runKey.Line-1]
	indent := strings.Repeat(" ", max(runKey.Column-1, 0))
	ending := "\n"
	if strings.HasSuffix(keyLine, "\r\n") {
		ending = "\r\n"
	}
	end := runKey.Line
	trailing := ""
	if run.Style&yaml.LiteralStyle != 0 {
		// Consume the indented block. A line that is not blank and is indented
		// no further than the key belongs to the next field.
		contentEnd := end
		for end < len(lines) {
			next := lines[end]
			trimmed := strings.TrimRight(next, "\r\n")
			if strings.TrimSpace(trimmed) != "" && leadingSpaces(trimmed) <= runKey.Column-1 {
				break
			}
			end++
		}
		contentEnd = end
		for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++
		}
		trailing = strings.Join(lines[contentEnd:end], "")
	} else {
		if strings.Contains(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(keyLine), "run:")), "#") {
			return "", "", 0, errUnsupported
		}
	}
	before = strings.Join(lines[runKey.Line-1:end], "")
	after = indent + "run: |" + ending + indent + "  " + script + ending + trailing
	if strings.Count(string(data), before) != 1 {
		return "", "", 0, errUnsupported
	}
	return before, after, runKey.Line, nil
}

func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

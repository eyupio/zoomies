package workflow

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrShardNotFound and ErrShardAmbiguous say why a sharding preview could not
// choose a test step. A workflow with two Playwright runs needs the operator to
// pick one; this function does not guess.
var (
	ErrShardNotFound  = errors.New("workflow: no single Playwright test step on an Ubuntu runner can be previewed")
	ErrShardAmbiguous = errors.New("workflow: more than one Playwright test step can be previewed")
)

// ShardRefusal is a refusal with a reason an operator can act on. Sharding can
// drop tests, rename a job a required check depends on and hide a failure, so
// every shape it does not understand is refused by name rather than repaired.
type ShardRefusal struct{ Reason string }

func (e *ShardRefusal) Error() string { return "workflow: sharding refused: " + e.Reason }

func refuse(reason string) error { return &ShardRefusal{Reason: reason} }

// shardMaxBlockLines and shardMaxBlockBytes keep the reviewed change small
// enough to read as one block: a job too large to review is not previewed.
const (
	shardMaxBlockLines = 60
	shardMaxBlockBytes = 3000
)

var (
	playwrightPrefix = []string{"npx playwright test", "pnpm exec playwright test", "yarn playwright test"}
	// Only arguments that neither select tests nor change which files run.
	playwrightSafeArg = regexp.MustCompile(`^(--reporter=[a-z]{2,16}|--workers=[0-9]{1,2}|--retries=[0-9]|--timeout=[0-9]{1,7}|--project=[A-Za-z0-9._-]{1,64})$`)
	narrowingArg      = regexp.MustCompile(`^(--shard|--grep|--grep-invert|-g|--only-changed|--last-failed|--list|--ui|--debug|-x|--max-failures)(=|$)`)
	shardJobID        = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	shardSafeName     = regexp.MustCompile(`^[A-Za-z0-9 ._()/-]{1,100}$`)
)

// FindPlaywrightTest returns the one step whose run script is exactly one
// Playwright test invocation, on a job with a literal Ubuntu runs-on label and
// nothing that sharding would break. A command that already shards, filters or
// narrows, a job that already has a strategy, outputs, services or a condition,
// and a job that uploads artifacts or coverage are refused with a reason.
func FindPlaywrightTest(data []byte) (jobIndex, stepIndex int, err error) {
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
		job := jobs.Content[i+1]
		if job.Kind != yaml.MappingNode || field(job, "uses") != nil {
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
			if _, _, ok := playwrightCommand(field(step, "run")); !ok {
				continue
			}
			if found {
				return 0, 0, ErrShardAmbiguous
			}
			jobIndex, stepIndex, found = i/2, j, true
		}
	}
	if !found {
		return 0, 0, ErrShardNotFound
	}
	if err := shardEligible(jobs, jobIndex, stepIndex); err != nil {
		return 0, 0, err
	}
	return jobIndex, stepIndex, nil
}

// playwrightCommand reports whether a run scalar is exactly one Playwright test
// invocation. The second result says why a recognised command is refused.
func playwrightCommand(run *yaml.Node) (command string, reason string, ok bool) {
	if run == nil || run.Kind != yaml.ScalarNode || run.Tag != "!!str" || run.Style != 0 && run.Style&yaml.LiteralStyle == 0 {
		return "", "", false
	}
	value := strings.TrimSpace(strings.TrimSuffix(run.Value, "\n"))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", "", false
	}
	prefix := ""
	for _, p := range playwrightPrefix {
		if value == p || strings.HasPrefix(value, p+" ") {
			prefix = p
			break
		}
	}
	if prefix == "" {
		return "", "", false
	}
	for _, arg := range strings.Fields(strings.TrimPrefix(value, prefix)) {
		switch {
		case narrowingArg.MatchString(arg):
			return value, "the command already shards, filters or narrows the run (" + arg + ")", true
		case !playwrightSafeArg.MatchString(arg):
			return value, "the command passes an argument Zoomies does not recognise (" + arg + "), so it cannot show that every test still runs", true
		}
	}
	return value, "", true
}

// shardEligible holds every refusal for the job and step the finder chose.
func shardEligible(jobs *yaml.Node, jobIndex, stepIndex int) error {
	if jobIndex < 0 || jobIndex*2+1 >= len(jobs.Content) {
		return errUnsupported
	}
	id := jobs.Content[jobIndex*2].Value
	job := jobs.Content[jobIndex*2+1]
	steps := field(job, "steps")
	if steps == nil || stepIndex < 0 || stepIndex >= len(steps.Content) {
		return errUnsupported
	}
	step := steps.Content[stepIndex]
	if _, reason, _ := playwrightCommand(field(step, "run")); reason != "" {
		return refuse(reason)
	}
	runs := field(job, "runs-on")
	if runs == nil || runs.Kind != yaml.ScalarNode || runs.Tag != "!!str" || runs.Style != 0 || !strings.HasPrefix(runs.Value, "ubuntu-") || strings.Contains(runs.Value, "${{") || strings.ContainsAny(runs.Value, "\r\n\x00") {
		return refuse("the job needs a literal Ubuntu runs-on label")
	}
	if !shardJobID.MatchString(id) {
		return refuse("the job ID is not a plain identifier")
	}
	for i := 0; i < len(jobs.Content); i += 2 {
		if jobs.Content[i].Value == id+"-shards" {
			return refuse("a job named " + id + "-shards already exists")
		}
	}
	for _, c := range []struct{ key, why string }{
		{"strategy", "the job already has a strategy or matrix"},
		{"services", "the job starts services that Zoomies cannot show are safe to run once per shard"},
		{"outputs", "the job declares outputs that a later job may read"},
		{"if", "the job has a condition"},
		{"continue-on-error", "the job can pass while failing"},
		{"container", "the job runs in a container Zoomies cannot show is safe to repeat per shard"},
		{"concurrency", "the job sets its own concurrency group"},
	} {
		if field(job, c.key) != nil {
			return refuse(c.why)
		}
	}
	if name := field(job, "name"); name != nil && (name.Kind != yaml.ScalarNode || name.Tag != "!!str" || name.Style != 0 || !shardSafeName.MatchString(name.Value) || strings.TrimSpace(name.Value) != name.Value) {
		return refuse("the job name is not a plain literal")
	}
	if field(step, "if") != nil || field(step, "continue-on-error") != nil {
		return refuse("the test step is conditional or can pass while failing")
	}
	stepName := field(step, "name")
	if stepName == nil || stepName.Kind != yaml.ScalarNode || stepName.Tag != "!!str" || stepName.Value == "" || len(stepName.Value) > 128 || strings.Contains(stepName.Value, "${{") || strings.ContainsAny(stepName.Value, "\r\n\x00") {
		return refuse("the test step needs a literal name so the fleet's history can be matched to it")
	}
	for _, s := range steps.Content {
		uses := field(s, "uses")
		if uses != nil && (strings.Contains(strings.ToLower(uses.Value), "upload-artifact") || strings.Contains(strings.ToLower(uses.Value), "codecov") || strings.Contains(strings.ToLower(uses.Value), "coveralls")) {
			return refuse("the job uploads artifacts or coverage under one name, which every shard would overwrite")
		}
		for _, k := range []string{"name", "run"} {
			if v := field(s, k); v != nil && strings.Contains(strings.ToLower(v.Value), "coverage") {
				return refuse("the job reads or writes coverage that a single shard would only partly cover")
			}
		}
	}
	return nil
}

// ShardNames returns the literal job and step names for the one test step, so
// the preview can be matched to the fleet job GitHub reported. A job without a
// name is reported under its ID, which is what GitHub shows.
func ShardNames(data []byte, jobIndex, stepIndex int) (jobName, stepName string, err error) {
	foundJob, foundStep, err := FindPlaywrightTest(data)
	if err != nil || foundJob != jobIndex || foundStep != stepIndex {
		return "", "", errUnsupported
	}
	root, err := parseRoot(data)
	if err != nil {
		return "", "", err
	}
	jobs := field(root, "jobs")
	job := jobs.Content[jobIndex*2+1]
	jobName = jobs.Content[jobIndex*2].Value
	if n := field(job, "name"); n != nil {
		jobName = n.Value
	}
	return jobName, field(field(job, "steps").Content[stepIndex], "name").Value, nil
}

// ShardChange rewrites the one job into a shard job plus an aggregator that
// keeps the original job ID and name, so a required check that names the job
// still resolves. The Playwright command gains --shard and nothing else. Bytes
// outside the job's block are untouched, and the job block is returned whole
// because the change is one reviewable replacement of it.
func ShardChange(data []byte, jobIndex, stepIndex, count int) (before, after string, line int, err error) {
	if count < 2 || count > 8 {
		return "", "", 0, errUnsupported
	}
	foundJob, foundStep, err := FindPlaywrightTest(data)
	if err != nil || foundJob != jobIndex || foundStep != stepIndex {
		return "", "", 0, errUnsupported
	}
	root, err := parseRoot(data)
	if err != nil {
		return "", "", 0, err
	}
	jobs := field(root, "jobs")
	keyNode := jobs.Content[jobIndex*2]
	job := jobs.Content[jobIndex*2+1]
	id := keyNode.Value
	lines := strings.SplitAfter(string(data), "\n")
	if keyNode.Line < 1 || keyNode.Line > len(lines) {
		return "", "", 0, errUnsupported
	}
	start := keyNode.Line - 1
	keyIndent := keyNode.Column - 1
	if strings.TrimRight(lines[start], " \r\n") != strings.Repeat(" ", keyIndent)+id+":" {
		return "", "", 0, refuse("the job header carries a comment or an unusual layout")
	}
	end := start + 1
	for end < len(lines) {
		trimmed := strings.TrimSpace(lines[end])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && leadingSpaces(strings.TrimRight(lines[end], "\r\n")) <= keyIndent {
			break
		}
		end++
	}
	// Blank lines and comments just before the next job belong to it.
	for end > start+1 {
		prev := strings.TrimRight(lines[end-1], "\r\n")
		if strings.TrimSpace(prev) == "" || strings.HasPrefix(strings.TrimSpace(prev), "#") && leadingSpaces(prev) <= keyIndent {
			end--
			continue
		}
		break
	}
	block := lines[start:end]
	if len(block) > shardMaxBlockLines || len(strings.Join(block, "")) > shardMaxBlockBytes {
		return "", "", 0, refuse("the job is too large to review as one block")
	}
	ending := "\n"
	if strings.HasSuffix(lines[start], "\r\n") {
		ending = "\r\n"
	}
	childIndent := keyIndent + 2
	if len(job.Content) > 0 && job.Content[0].Column-1 > keyIndent {
		childIndent = job.Content[0].Column - 1
	}
	pad := strings.Repeat(" ", childIndent)
	jobName := id
	if n := field(job, "name"); n != nil {
		jobName = n.Value
	}
	runsOn := field(job, "runs-on").Value

	rel := func(l int) (int, bool) {
		i := l - 1 - start
		return i, i > 0 && i < len(block)
	}
	// The test command line.
	steps := field(job, "steps")
	run := field(steps.Content[stepIndex], "run")
	runKey, _ := keyAndValue(steps.Content[stepIndex], "run")
	cmdLine := runKey.Line
	if run.Style&yaml.LiteralStyle != 0 {
		cmdLine = runKey.Line + 1
	}
	cmdRel, ok := rel(cmdLine)
	if !ok {
		return "", "", 0, errUnsupported
	}
	cmd := strings.TrimRight(block[cmdRel], "\r\n")
	if strings.Contains(cmd, "#") || !strings.HasSuffix(strings.TrimSpace(cmd), strings.TrimSpace(run.Value)) {
		return "", "", 0, refuse("the test command line carries a comment or an unusual layout")
	}
	out := make([]string, len(block))
	copy(out, block)
	out[cmdRel] = cmd + " --shard=${{ matrix.shard }}/" + strconv.Itoa(count) + ending
	out[0] = strings.Repeat(" ", keyIndent) + id + "-shards:" + ending

	// The shard job's name and strategy, placed after the header, or replacing
	// the name line when the job already has one.
	shardName := pad + "name: " + strconv.Quote(jobName+" (shard ${{ matrix.shard }}/"+strconv.Itoa(count)+")") + ending
	strategy := pad + "strategy:" + ending +
		pad + "  fail-fast: false" + ending +
		pad + "  matrix:" + ending +
		pad + "    shard: [" + shardList(count) + "]" + ending
	var head []string
	if nameKey, _ := keyAndValue(job, "name"); nameKey != nil {
		nameRel, ok := rel(nameKey.Line)
		if !ok || strings.Contains(block[nameRel], "#") {
			return "", "", 0, refuse("the job name line carries a comment or an unusual layout")
		}
		out[nameRel] = shardName + strategy
	} else {
		head = []string{shardName + strategy}
	}
	rebuilt := out[0] + strings.Join(head, "") + strings.Join(out[1:], "")
	if !strings.HasSuffix(rebuilt, ending) {
		rebuilt += ending
	}
	aggregator := ending +
		strings.Repeat(" ", keyIndent) + id + ":" + ending +
		pad + "name: " + shardScalar(jobName) + ending +
		pad + "needs: " + id + "-shards" + ending +
		pad + "if: ${{ always() }}" + ending +
		pad + "runs-on: " + runsOn + ending +
		pad + "steps:" + ending +
		pad + "  - name: Require every shard to pass" + ending +
		pad + "    run: test \"${{ needs." + id + "-shards.result }}\" = \"success\"" + ending

	before = strings.Join(block, "")
	if !strings.HasSuffix(before, ending) {
		return "", "", 0, errUnsupported
	}
	after = rebuilt + aggregator
	if strings.Count(string(data), before) != 1 {
		return "", "", 0, errUnsupported
	}
	return before, after, keyNode.Line, nil
}

func shardList(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = strconv.Itoa(i + 1)
	}
	return strings.Join(parts, ", ")
}

// shardScalar writes a name back the way it was safe to read: plain when it
// only holds characters that need no quoting, double-quoted otherwise.
func shardScalar(s string) string {
	if shardSafeName.MatchString(s) && !strings.HasPrefix(s, "-") && !strings.HasPrefix(s, ".") {
		return s
	}
	return strconv.Quote(s)
}

// Package toolscan reads GitHub Actions workflows for the toolchains their jobs
// install, so a fleet can have them in place before the jobs arrive.
//
// Every setup-python, setup-node, setup-go, setup-java and setup-dotnet step
// downloads what it is asked for unless the runner's tool cache already holds
// it, and on an ephemeral runner the tool cache starts empty every time. What
// a pool's jobs ask for is written down in their workflows, so it can be read
// ahead of time rather than discovered one download at a time.
//
// It is pure: it takes a file and returns what the file asks for. Fetching the
// files and deciding which pool would run each job are the controller's.
//
// Only what can be known from the file is reported as known. A version that
// comes from an expression, or from a file in the repository such as
// .python-version or go.mod, is reported with the reason it is not resolved
// rather than guessed at or dropped: an operator reading the result has to be
// able to tell "nothing asks for Python" from "something does, and it says
// which in a file this did not read".
package toolscan

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tool is a toolchain a setup action installs.
type Tool string

// The toolchains this package recognises, by the action that installs each.
const (
	Python Tool = "python"
	Node   Tool = "node"
	Go     Tool = "go"
	Java   Tool = "java"
	DotNet Tool = "dotnet"
)

// setupAction describes one setup action: the inputs that name versions
// directly, and the ones that name a file the versions are read from.
type setupAction struct {
	tool         Tool
	versionInput string
	fileInputs   []string
}

// setupActions is keyed by the action's owner/name in lower case, which is how
// GitHub resolves `uses:` -- case-insensitively.
var setupActions = map[string]setupAction{
	"actions/setup-python": {Python, "python-version", []string{"python-version-file"}},
	"actions/setup-node":   {Node, "node-version", []string{"node-version-file"}},
	"actions/setup-go":     {Go, "go-version", []string{"go-version-file"}},
	"actions/setup-java":   {Java, "java-version", []string{"java-version-file"}},
	"actions/setup-dotnet": {DotNet, "dotnet-version", []string{"global-json-file"}},
}

// maxCombinations bounds how far a matrix is expanded. GitHub itself refuses a
// matrix of more than 256 jobs, so a file that asks for more is not one that
// runs.
const maxCombinations = 256

// Requirement is one toolchain version one job asks for.
type Requirement struct {
	// Workflow is the file's path in its repository, and Job the job's key in
	// it.
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	// Labels are the job's runs-on labels. Nil means they are not known from
	// the file -- an expression this package could not resolve -- which is
	// different from a job with no labels at all.
	Labels []string `json:"labels"`
	Tool   Tool     `json:"tool"`
	// Version is the version or range as the workflow writes it, after any
	// matrix value has been substituted: "3.12", "22.x", "lts/*". Empty when
	// Unresolved is set.
	Version string `json:"version,omitempty"`
	// Distribution is setup-java's distribution input, which decides which
	// JDK a version means. Empty for every other tool.
	Distribution string `json:"distribution,omitempty"`
	// Unresolved says why the version could not be read from the file.
	Unresolved string `json:"unresolved,omitempty"`
}

// workflow is the part of a workflow file this package reads. Everything is a
// yaml.Node so that values keep the text they were written as: to a YAML
// decoder `python-version: 3.10` is the float 3.1, and GitHub passes the text
// "3.10" to the action.
type workflow struct {
	Jobs map[string]struct {
		RunsOn   yaml.Node `yaml:"runs-on"`
		Strategy struct {
			Matrix yaml.Node `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []struct {
			Uses string               `yaml:"uses"`
			With map[string]yaml.Node `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// Scan returns every toolchain requirement in one workflow file, in a stable
// order. A file that is not valid YAML is an error; a job this package cannot
// fully read is not -- what it can read is returned, and what it cannot is
// marked Unresolved.
func Scan(path, content string) ([]Requirement, error) {
	var wf workflow
	if err := yaml.Unmarshal([]byte(content), &wf); err != nil {
		return nil, fmt.Errorf("toolscan: %s is not valid YAML: %w", path, err)
	}
	jobs := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobs = append(jobs, name)
	}
	sort.Strings(jobs)

	var out []Requirement
	seen := map[string]bool{}
	for _, name := range jobs {
		job := wf.Jobs[name]
		combos, matrixNote := expandMatrix(&job.Strategy.Matrix)
		for _, combo := range combos {
			labels := runsOn(&job.RunsOn, combo)
			for _, step := range job.Steps {
				action, ok := setupActions[actionName(step.Uses)]
				if !ok {
					continue
				}
				for _, r := range stepRequirements(action, step.With, combo) {
					r.Workflow, r.Job, r.Labels = path, name, labels
					// A matrix this package could not read explains a
					// missing matrix value better than the value does.
					if matrixNote != "" && strings.HasPrefix(r.Unresolved, "set by matrix.") {
						r.Unresolved = matrixNote
					}
					key := fmt.Sprintf("%s\x00%q\x00%s\x00%s\x00%s\x00%s", r.Job, r.Labels, r.Tool, r.Version, r.Distribution, r.Unresolved)
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, r)
				}
			}
		}
	}
	return out, nil
}

// actionName turns `actions/setup-python@v6` into "actions/setup-python". A
// local action (./path) or a Docker one (docker://) is not a setup action.
func actionName(uses string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(uses), "@")
	return strings.ToLower(name)
}

// stepRequirements reads the versions one setup step asks for.
func stepRequirements(action setupAction, with map[string]yaml.Node, combo map[string]string) []Requirement {
	distribution := ""
	if action.tool == Java {
		if n, ok := with["distribution"]; ok {
			d, unresolved := substitute(n.Value, combo)
			if unresolved == "" {
				distribution = d
			}
		}
	}

	n, ok := with[action.versionInput]
	if !ok || strings.TrimSpace(nodeText(&n)) == "" {
		for _, in := range action.fileInputs {
			if f, ok := with[in]; ok && strings.TrimSpace(f.Value) != "" {
				return []Requirement{{Tool: action.tool, Distribution: distribution,
					Unresolved: fmt.Sprintf("read from %s in the repository", strings.TrimSpace(f.Value))}}
			}
		}
		// No version at all: setup-go and setup-dotnet then read go.mod or
		// global.json on their own, and the others use whatever is on PATH.
		return []Requirement{{Tool: action.tool, Distribution: distribution,
			Unresolved: "no version given; the action falls back to the repository or the runner"}}
	}

	var out []Requirement
	for _, v := range versions(&n) {
		value, unresolved := substitute(v, combo)
		if unresolved != "" {
			out = append(out, Requirement{Tool: action.tool, Distribution: distribution, Unresolved: unresolved})
			continue
		}
		out = append(out, Requirement{Tool: action.tool, Version: value, Distribution: distribution})
	}
	return out
}

// versions splits a version input into the versions it names. setup-python,
// setup-java and setup-dotnet all take several, one per line, and YAML also
// lets a workflow write them as a sequence.
func versions(n *yaml.Node) []string {
	var raw []string
	if n.Kind == yaml.SequenceNode {
		for _, c := range n.Content {
			raw = append(raw, c.Value)
		}
	} else {
		raw = strings.Split(n.Value, "\n")
	}
	var out []string
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// nodeText is a node's text whichever kind it is, for "is anything written
// here".
func nodeText(n *yaml.Node) string {
	if n.Kind == yaml.SequenceNode {
		return strings.Join(versions(n), "\n")
	}
	return n.Value
}

// substitute replaces ${{ matrix.<key> }} with the combination's value. Any
// other expression -- an input, an env var, a step's output -- is not
// knowable from the file, and is reported as the reason.
func substitute(s string, combo map[string]string) (string, string) {
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "${{")
		if i < 0 {
			b.WriteString(rest)
			return strings.TrimSpace(b.String()), ""
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return "", fmt.Sprintf("the expression in %q is not closed", s)
		}
		expr := strings.TrimSpace(rest[i+3 : i+j])
		key, ok := strings.CutPrefix(expr, "matrix.")
		if !ok {
			return "", fmt.Sprintf("set by the expression ${{ %s }}", expr)
		}
		v, ok := combo[key]
		if !ok {
			return "", fmt.Sprintf("set by matrix.%s, which the matrix does not list as a value", key)
		}
		b.WriteString(rest[:i])
		b.WriteString(v)
		rest = rest[i+j+2:]
	}
}

// runsOn reads a job's labels, or nil when they are not knowable.
//
// runs-on is a label, a list of labels, or a mapping with a group and labels;
// a runner group is not a label and does not narrow which pool matches here,
// so only the labels are kept.
func runsOn(n *yaml.Node, combo map[string]string) []string {
	var raw []string
	switch n.Kind {
	case yaml.ScalarNode:
		raw = []string{n.Value}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			raw = append(raw, c.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value != "labels" {
				continue
			}
			v := n.Content[i+1]
			if v.Kind == yaml.SequenceNode {
				for _, c := range v.Content {
					raw = append(raw, c.Value)
				}
			} else {
				raw = append(raw, v.Value)
			}
		}
	default:
		return nil
	}
	labels := []string{}
	for _, l := range raw {
		v, unresolved := substitute(l, combo)
		if unresolved != "" {
			return nil
		}
		// `runs-on: ${{ matrix.os }}` whose value is itself a list arrives
		// here as one comma-free value per combination, so no splitting is
		// needed; a label is never empty.
		if v != "" {
			labels = append(labels, v)
		}
	}
	return labels
}

// expandMatrix returns the matrix's combinations, as key to value, and a note
// when the matrix could not be read in full. A job with no matrix has one
// combination, the empty one.
//
// Only scalar values are substituted. A matrix value that is itself a mapping
// or a list (`include: [{os: ..., versions: [...]}]`) is left out of the
// combination, so a step that uses it is reported as unresolved rather than
// given a wrong answer.
func expandMatrix(m *yaml.Node) ([]map[string]string, string) {
	if m.Kind == 0 {
		return []map[string]string{{}}, ""
	}
	if m.Kind != yaml.MappingNode {
		// `matrix: ${{ fromJSON(...) }}` is built at run time.
		return []map[string]string{{}}, "set by a matrix built at run time"
	}
	var axes []axis
	var include, exclude []map[string]string
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i].Value, m.Content[i+1]
		switch key {
		case "include":
			include = mappings(val)
		case "exclude":
			exclude = mappings(val)
		default:
			if val.Kind != yaml.SequenceNode {
				continue
			}
			var vs []string
			for _, c := range val.Content {
				if c.Kind == yaml.ScalarNode {
					vs = append(vs, c.Value)
				}
			}
			if len(vs) > 0 {
				axes = append(axes, axis{key, vs})
			}
		}
	}

	combos := []map[string]string{{}}
	for _, a := range axes {
		var next []map[string]string
		for _, c := range combos {
			for _, v := range a.values {
				n := make(map[string]string, len(c)+1)
				for k, x := range c {
					n[k] = x
				}
				n[a.key] = v
				next = append(next, n)
				if len(next) > maxCombinations {
					return combos, "set by a matrix larger than GitHub runs"
				}
			}
		}
		combos = next
	}

	kept := combos[:0]
	for _, c := range combos {
		if !matchesAny(c, exclude) {
			kept = append(kept, c)
		}
	}
	combos = kept

	// With no axes, every include entry is a job of its own: that is what
	// makes `include` alone a list of jobs.
	if len(axes) == 0 {
		if len(include) == 0 {
			return []map[string]string{{}}, ""
		}
		return include, ""
	}

	// Otherwise an include entry extends every combination whose axis values
	// it agrees with, and one that agrees with none is a combination of its
	// own. That is GitHub's rule.
	for _, inc := range include {
		extended := false
		for _, c := range combos {
			if agrees(c, inc, axes) {
				// Never an axis value: GitHub does not let an include
				// overwrite one of the matrix's own.
				for k, v := range inc {
					if !isAxisKey(k, axes) {
						c[k] = v
					}
				}
				extended = true
			}
		}
		if !extended {
			n := make(map[string]string, len(inc))
			for k, v := range inc {
				n[k] = v
			}
			combos = append(combos, n)
		}
	}
	if len(combos) == 0 {
		return []map[string]string{{}}, ""
	}
	return combos, ""
}

// axis is one of a matrix's own keys and the values it takes.
type axis struct {
	key    string
	values []string
}

// mappings reads an include or exclude list, keeping scalar values only.
func mappings(n *yaml.Node) []map[string]string {
	if n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []map[string]string
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		m := map[string]string{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			if item.Content[i+1].Kind == yaml.ScalarNode {
				m[item.Content[i].Value] = item.Content[i+1].Value
			}
		}
		out = append(out, m)
	}
	return out
}

func matchesAny(c map[string]string, patterns []map[string]string) bool {
	for _, p := range patterns {
		match := true
		for k, v := range p {
			if c[k] != v {
				match = false
				break
			}
		}
		if match && len(p) > 0 {
			return true
		}
	}
	return false
}

// agrees reports whether an include entry's values for the matrix's own axes
// match a combination; keys that are not axes do not have to.
func agrees(c, inc map[string]string, axes []axis) bool {
	for k, v := range inc {
		if isAxisKey(k, axes) && c[k] != v {
			return false
		}
	}
	return true
}

func isAxisKey(k string, axes []axis) bool {
	for _, a := range axes {
		if a.key == k {
			return true
		}
	}
	return false
}

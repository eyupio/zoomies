package prrepair

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	// errInvalidReply is a reply the repair cannot read as one of its plans.
	// It is a sentinel because Generate tells the model the same sentence and
	// tries again, rather than ending the attempt on the first malformed answer.
	errInvalidReply = errors.New("model did not return a valid repair plan")
	// errMultiplePlans is a reply that contains two answers. Two read requests
	// are one investigation and are merged; a fix, a giving up, or a fix beside
	// a read is two answers, and one of them would be silently ignored.
	errMultiplePlans = errors.New("model returned more than one repair plan")
)

// checkReplyActions rejects ambiguous steps without choosing which action to
// ignore. These mistakes can be corrected within the same bounded retry loop
// as malformed JSON; path and patch validation still run after correction.
func checkReplyActions(p Plan) error {
	reads := len(p.Read) + len(p.Windows)
	changes := len(p.Edits) + len(p.Files)
	switch {
	case p.CannotFix != "" && reads+changes > 0:
		return fmt.Errorf("%w: cannot_fix must stand alone; choose an explanation, a source request or a fix", errInvalidReply)
	case reads > 0 && changes > 0:
		return fmt.Errorf("%w: do not combine source requests with changes; request source first, then propose a fix in a later reply", errInvalidReply)
	case reads > MaxEdits:
		return fmt.Errorf("%w: a source request may contain at most %d read and windows entries in total; this reply contained %d, so split it across turns", errInvalidReply, MaxEdits, reads)
	case len(p.Edits) > 0 && len(p.Files) > 0:
		return fmt.Errorf("%w: use targeted edits or complete replacements, not both", errInvalidReply)
	}
	return nil
}

// decode reads a model's reply as one repair plan.
//
// A model does not always write the one bare JSON object the system prompt asks
// for. It wraps it in a code fence or a sentence, writes the file it wants to
// read and the window it wants as two objects in one answer, or gives a key the
// repair has no use for alongside the ones it does. None of those is a second
// answer, and ending a repair on them was a repair lost to punctuation. The
// reply is searched for its JSON objects, the known keys are read, and objects
// that only ask for source are merged into one investigation. A reply that
// proposes two changes, or a change beside a read, is still refused: the repair
// must know which answer the model meant.
func decode(raw string) (Plan, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > MaxReplyBytes {
		return Plan{}, fmt.Errorf("model response exceeded the repair limit")
	}
	objects := replyObjects(raw)
	plans := make([]Plan, 0, len(objects))
	for _, obj := range objects {
		p, ok, err := planFromObject(obj.body)
		if err != nil {
			return Plan{}, errInvalidReply
		}
		if ok {
			plans = append(plans, p)
		}
	}
	switch {
	case len(plans) == 0:
		return Plan{}, errInvalidReply
	case len(plans) == 1 && len(objects) == 1:
		return plans[0], nil
	case len(objects) != len(plans):
		// Something in the reply was not a plan. Beside a plan it is a second
		// answer, not a fragment to ignore.
		return Plan{}, errMultiplePlans
	}
	for i := 1; i < len(objects); i++ {
		if !fenceOrSpace(raw[objects[i-1].end:objects[i].start]) {
			// Prose between two objects is the model talking, not one reply
			// split in two. A retry asks for the one object it meant.
			return Plan{}, errMultiplePlans
		}
	}
	merged := Plan{}
	for _, p := range plans {
		if len(p.Read)+len(p.Windows) == 0 || len(p.Edits)+len(p.Files) > 0 || p.CannotFix != "" || strings.TrimSpace(p.Summary) != "" {
			return Plan{}, errMultiplePlans
		}
		merged.Read = append(merged.Read, p.Read...)
		merged.Windows = append(merged.Windows, p.Windows...)
	}
	merged.Read = uniqueStrings(merged.Read)
	merged.Windows = uniqueWindows(merged.Windows)
	return merged, nil
}

// replyObject is one JSON object found in a reply, with where it sat so that
// the text between two objects can be judged.
type replyObject struct {
	body       []byte
	start, end int
}

// replyObjects finds the JSON objects in a reply, in the order the model wrote
// them. A decoder that reaches the end of one object continues after it, so a
// brace inside a string or inside a supplied code file is not mistaken for the
// start of another.
func replyObjects(raw string) []replyObject {
	var out []replyObject
	for i := 0; i < len(raw); {
		next := strings.IndexByte(raw[i:], '{')
		if next < 0 {
			break
		}
		start := i + next
		dec := json.NewDecoder(strings.NewReader(raw[start:]))
		var obj json.RawMessage
		if err := dec.Decode(&obj); err == nil && len(obj) > 0 && obj[0] == '{' {
			end := start + int(dec.InputOffset())
			out = append(out, replyObject{body: obj, start: start, end: end})
			i = end
			continue
		}
		// Not an object here: a brace in prose, or the start of one the model
		// never closed. Look at the next one.
		i = start + 1
	}
	return out
}

// fenceOrSpace is whether the text between two objects is only the whitespace
// or the code fences a model wraps each object in. Anything else is prose.
func fenceOrSpace(gap string) bool {
	for _, line := range strings.Split(gap, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		return false
	}
	return true
}

// planFromObject reads one JSON object as a plan, keeping only the keys the
// repair knows. Unknown keys at the top are dropped rather than refused: a model
// that adds "confidence" or "notes" to an otherwise good plan did not propose a
// second change. ok is false for an object with no action in it, which is a
// fragment of a larger reply and not an answer.
func planFromObject(raw []byte) (Plan, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Plan{}, false, err
	}
	kept := map[string]json.RawMessage{}
	action := false
	for key, value := range fields {
		name := strings.ToLower(strings.TrimSpace(key))
		switch name {
		case "read", "windows", "edits", "files", "cannot_fix", "summary":
		default:
			continue
		}
		if _, duplicate := kept[name]; duplicate {
			// Two spellings of one key are two answers, not one.
			return Plan{}, true, fmt.Errorf("the reply named %q twice", name)
		}
		normalised, ok := normalisePlanValue(name, value)
		if !ok {
			return Plan{}, true, fmt.Errorf("the reply's %q is not a repair value", name)
		}
		if normalised == nil {
			// The key was present but empty, which is how a model writes a
			// field it has nothing for. It is not an action.
			continue
		}
		kept[name] = normalised
		if name != "summary" {
			action = true
		}
	}
	if !action {
		return Plan{}, false, nil
	}
	body, err := json.Marshal(kept)
	if err != nil {
		return Plan{}, true, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	var p Plan
	if err := dec.Decode(&p); err != nil {
		return Plan{}, true, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Plan{}, true, fmt.Errorf("the reply's plan has trailing data")
	}
	return p, true, nil
}

// normalisePlanValue accepts the shapes a model writes for a field that are
// clearer than a type error and still mean the same thing: one path as a string,
// one window as an object, one edit as an object rather than a list. A nil
// result means the field was present but empty, and is no action. Anything else
// is left for the Plan decoder, which refuses a type the repair cannot use.
func normalisePlanValue(name string, value json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(value)
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		return nil, true
	}
	switch name {
	case "read":
		var v any
		if err := json.Unmarshal(value, &v); err != nil {
			return nil, false
		}
		paths, ok := pathsFrom(v)
		if !ok {
			if list, isList := v.([]any); isList && len(list) == 0 {
				return nil, true
			}
			return nil, false
		}
		out, err := json.Marshal(paths)
		return out, err == nil
	case "windows":
		var v any
		if err := json.Unmarshal(value, &v); err != nil {
			return nil, false
		}
		windows, ok := windowsFrom(v)
		if !ok {
			if list, isList := v.([]any); isList && len(list) == 0 {
				return nil, true
			}
			return nil, false
		}
		out, err := json.Marshal(windows)
		return out, err == nil
	case "edits", "files":
		if !onlyEntryKeys(trimmed, name) {
			return nil, false
		}
		if trimmed[0] == '{' {
			out := make([]byte, 0, len(trimmed)+2)
			out = append(out, '[')
			out = append(out, trimmed...)
			out = append(out, ']')
			return out, true
		}
	case "cannot_fix":
		var reason string
		if err := json.Unmarshal(value, &reason); err == nil && reason == "" {
			return nil, true
		}
	case "summary":
		var summary string
		if err := json.Unmarshal(value, &summary); err == nil && summary == "" {
			return nil, true
		}
	}
	return value, true
}

// onlyEntryKeys refuses a file or edit that carries a key the repair has no word
// for. A model that tries to set a file's mode is asking for a change Eli does
// not make; dropping the key would silently give it the original mode and call
// that the answer.
func onlyEntryKeys(raw []byte, name string) bool {
	allowed := map[string]bool{"path": true, "content": true, "start_line": true, "partial": true}
	if name == "edits" {
		allowed = map[string]bool{"path": true, "before": true, "after": true}
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		var one map[string]json.RawMessage
		if json.Unmarshal(raw, &one) != nil {
			return true // the Plan decoder will refuse the shape
		}
		entries = []map[string]json.RawMessage{one}
	}
	for _, entry := range entries {
		for key := range entry {
			if !allowed[strings.ToLower(strings.TrimSpace(key))] {
				return false
			}
		}
	}
	return true
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func uniqueWindows(in []ReadWindow) []ReadWindow {
	type key struct {
		path       string
		start, end int
	}
	seen := make(map[key]bool, len(in))
	out := make([]ReadWindow, 0, len(in))
	for _, w := range in {
		k := key{w.Path, w.StartLine, w.EndLine}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, w)
	}
	return out
}

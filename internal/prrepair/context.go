package prrepair

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// allowanceError is a read that the rest of the source allowance cannot cover.
// It is for the model to hear and not for an operator to read in the history:
// nothing has gone wrong with the repair, the request was too wide, and a model
// told what remains can ask for a window instead. Ending the repair on it made a
// PR whose changed files had already taken most of the allowance unrepairable
// the moment the model asked for a large file it had not been shown, however
// much room the provider's output limit gave it to write the fix.
type allowanceError struct {
	path       string
	need, left int
}

// A denied read is feedback for the model, not a reason to discard an otherwise
// useful investigation. The path check still runs before any reader is called.
type protectedReadError struct{ path string }

func (e *protectedReadError) Error() string {
	return fmt.Sprintf("%q was not supplied: the path is protected by the repository repair policy. Do not request it again. Work from allowed source, or return cannot_fix explaining why this file is needed.", Redact(e.path))
}

func (e *allowanceError) Error() string { return e.said(e.left) }

// said is the refusal as the model is told it. left is what remains when the
// model reads it, which is less than e.left whenever a window later in the same
// request was supplied: a model that sized its next window from the larger
// figure was refused again, and each refusal costs one of the repair's rounds.
func (e *allowanceError) said(left int) string {
	if left <= 0 {
		return fmt.Sprintf("%s was not supplied: the whole %d KiB source allowance is used up. Work from the source already supplied, or return cannot_fix.", e.path, MaxSourceBytes/1024)
	}
	return fmt.Sprintf("%s was not supplied: it would add %d bytes of source and %d bytes of the %d KiB allowance remain. Ask for a smaller line window of it, work from the source already supplied, or return cannot_fix.", e.path, e.need, left, MaxSourceBytes/1024)
}

// stepLimitError is what a repair ends with when its rounds run out. When the
// allowance turned reads away it says so, because the round limit on its own
// sends whoever reads it to the model, when it was the request that was too
// wide: a retry sends the same snapshot and the model asks for the same reads.
// A path counts as turned away if it was refused once, even if a window of it
// fitted later, since the rounds spent finding that out are the rounds it ran
// out of.
func stepLimitError(refused, denied []string) error {
	message := "the repair reached the model-step limit without a fix"
	if len(refused) > 0 {
		message += fmt.Sprintf("; the model asked for %s, which the %d KiB source allowance had no room for: name the file and the lines to change in the request, so that it asks for a window that fits", namedFiles(refused), MaxSourceBytes/1024)
	}
	if len(denied) > 0 {
		message += fmt.Sprintf("; the model requested %s, which the repository repair policy kept protected: point Eli at allowed source, or fix the protected file by hand", namedFiles(denied))
	}
	return errors.New(message)
}

// namedFiles lists up to three paths and counts the rest. Each sits in a code
// span, which a path pushed by somebody else cannot close: a backtick in it
// becomes an apostrophe.
func namedFiles(paths []string) string {
	names := []string{}
	for _, p := range paths[:min(len(paths), 3)] {
		names = append(names, "`"+strings.ReplaceAll(p, "`", "'")+"`")
	}
	if more := len(paths) - len(names); more > 0 {
		return strings.Join(names, ", ") + fmt.Sprintf(" and %d more", more)
	}
	if n := len(names); n > 1 {
		return strings.Join(names[:n-1], ", ") + " and " + names[n-1]
	}
	return names[0]
}

type sourceSpan struct{ start, end int }
type sourceContext struct {
	reader    Reader
	workflows bool
	originals map[string]File
	shown     map[string][]sourceSpan
	// inDiff is the head-side text of each hunk the model was shown, by path.
	// It is supplied source too, even though it was not read through a window.
	inDiff map[string][]string
	bytes  int
	// refused is the paths the allowance turned away, in the order it first did,
	// for the error a repair ends with if it runs out of rounds.
	refused []string
	denied  []string
}

func newSourceContext(reader Reader, workflows bool) *sourceContext {
	return &sourceContext{reader: reader, workflows: workflows, originals: map[string]File{}, shown: map[string][]sourceSpan{}, inDiff: map[string][]string{}}
}
func (s *sourceContext) shownInDiff(path, text string) bool {
	for _, hunk := range s.inDiff[path] {
		if strings.Contains(hunk, text) {
			return true
		}
	}
	return false
}
func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
func (s *sourceContext) complete(path string) bool {
	f, ok := s.originals[path]
	spans := s.shown[path]
	return ok && len(spans) == 1 && spans[0].start == 0 && spans[0].end == len(f.Content)
}
func (s *sourceContext) reveal(ctx context.Context, w ReadWindow) ([]File, error) {
	if !AllowedPath(w.Path, s.workflows) {
		if !slices.Contains(s.denied, w.Path) {
			s.denied = append(s.denied, w.Path)
		}
		return nil, &protectedReadError{path: w.Path}
	}
	f, ok := s.originals[w.Path]
	if !ok {
		var err error
		f, err = s.reader.ReadFile(ctx, w.Path)
		if err != nil {
			var readError *ReadError
			if errors.As(err, &readError) {
				return nil, err
			}
			return nil, &ReadError{Path: w.Path, Err: err}
		}
		if len(f.Content) > MaxBlobBytes || !validText(f.Content) {
			return nil, &ReadError{Path: w.Path}
		}
		f.Path = w.Path
		s.originals[w.Path] = f
	}
	start, end := 0, len(f.Content)
	if w.StartLine != 0 || w.EndLine != 0 {
		if w.StartLine < 1 || w.EndLine < w.StartLine || w.EndLine-w.StartLine > 400 {
			return nil, fmt.Errorf("file windows need 1 to 401 lines")
		}
		lines := strings.SplitAfter(f.Content, "\n")
		if w.StartLine > len(lines) {
			return nil, fmt.Errorf("requested line is past the end of %s", w.Path)
		}
		for _, line := range lines[:w.StartLine-1] {
			start += len(line)
		}
		end = start
		for _, line := range lines[w.StartLine-1 : min(w.EndLine, len(lines))] {
			end += len(line)
		}
	}
	if end-start > MaxFileBytes {
		end = start + MaxFileBytes
		for end > start && !utf8.ValidString(f.Content[start:end]) {
			end--
		}
	}
	// Subtract previously disclosed spans so overlapping reads neither spend the
	// source allowance twice nor grow the transcript with duplicate source.
	fresh := []sourceSpan{{start, end}}
	for _, old := range s.shown[w.Path] {
		next := []sourceSpan{}
		for _, v := range fresh {
			if old.end <= v.start || old.start >= v.end {
				next = append(next, v)
				continue
			}
			if v.start < old.start {
				next = append(next, sourceSpan{v.start, old.start})
			}
			if v.end > old.end {
				next = append(next, sourceSpan{old.end, v.end})
			}
		}
		fresh = next
	}
	if len(f.Content) == 0 && s.complete(w.Path) {
		return nil, nil
	}
	size := 0
	for _, v := range fresh {
		size += v.end - v.start
	}
	if s.bytes+size > MaxSourceBytes {
		if !slices.Contains(s.refused, w.Path) {
			s.refused = append(s.refused, w.Path)
		}
		return nil, &allowanceError{path: w.Path, need: size, left: MaxSourceBytes - s.bytes}
	}
	s.bytes += size
	out := []File{}
	for _, v := range fresh {
		out = append(out, File{Path: w.Path, Content: Redact(f.Content[v.start:v.end]), StartLine: 1 + strings.Count(f.Content[:v.start], "\n"), Partial: v.start != 0 || v.end != len(f.Content)})
	}
	spans := append(s.shown[w.Path], fresh...)
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := []sourceSpan{}
	for _, v := range spans {
		if len(merged) > 0 && v.start <= merged[len(merged)-1].end {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, v.end)
		} else {
			merged = append(merged, v)
		}
	}
	s.shown[w.Path] = merged
	return out, nil
}

func (s *sourceContext) apply(edits []Edit) ([]File, error) {
	if len(edits) > 32 {
		return nil, fmt.Errorf("a repair may contain at most 32 targeted edits")
	}
	type change struct {
		sourceSpan
		after string
	}
	changes := map[string][]change{}
	order := []string{}
	size := 0
	for _, e := range edits {
		f, ok := s.originals[e.Path]
		if !ok || !AllowedPath(e.Path, s.workflows) || e.Before == "" || strings.Count(f.Content, e.Before) != 1 {
			return nil, fmt.Errorf("targeted edit needs an exact, unique preimage in a supplied file")
		}
		start := strings.Index(f.Content, e.Before)
		end := start + len(e.Before)
		visible := false
		for _, v := range s.shown[e.Path] {
			if start >= v.start && end <= v.end {
				visible = true
			}
		}
		if !visible {
			visible = s.shownInDiff(e.Path, e.Before)
		}
		if !visible {
			return nil, fmt.Errorf("targeted edit changes text that was not supplied")
		}
		for _, v := range changes[e.Path] {
			if start < v.end && end > v.start {
				return nil, fmt.Errorf("targeted edits overlap")
			}
		}
		size += len(e.Before) + len(e.After)
		if size > 64<<10 {
			return nil, fmt.Errorf("targeted edits exceed the patch limit")
		}
		if _, ok := changes[e.Path]; !ok {
			order = append(order, e.Path)
		}
		changes[e.Path] = append(changes[e.Path], change{sourceSpan{start, end}, e.After})
	}
	out := []File{}
	for _, path := range order {
		f := s.originals[path]
		list := changes[path]
		sort.Slice(list, func(i, j int) bool { return list[i].start > list[j].start })
		for _, e := range list {
			f.Content = f.Content[:e.start] + e.after + f.Content[e.end:]
		}
		out = append(out, f)
	}
	return out, nil
}

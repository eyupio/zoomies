// Package conflictresolve asks a model to resolve the text conflicts a merge
// worker found, and returns the complete files it produced.
//
// It is deliberately separate from publication: the model edits only the
// conflicted files, and the caller combines those files with the merge worker's
// other changes before anything is committed or pushed. No repository code runs
// and no file outside the conflict set can be edited.
package conflictresolve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/prrepair"
)

var (
	// ErrUnsupported says a conflict is not one a model may resolve: a path the
	// repair policy protects, or a set larger than the editing allowance.
	ErrUnsupported = errors.New("conflictresolve: a conflict is outside what a model may resolve")
	// ErrUnresolved says the model's answer did not produce a usable file for
	// every conflict, or produced one still carrying conflict markers.
	ErrUnresolved = errors.New("conflictresolve: the model did not resolve every conflict")
)

// Resolution is one conflicted file as the model resolved it. Mode is the mode
// the merge worker reported for the file; the caller keeps the worker's mode
// when it combines this with the rest of the merged tree.
type Resolution struct {
	Path    string
	Mode    string
	Content string
}

// Resolve asks model to rewrite each conflicted file so that both sides'
// intent is kept and no conflict marker remains.
//
// The files supplied to the model are the standard conflict-marked form of the
// two sides, not the worker's own merge blob: the worker reports the three
// texts, and this package is the one that gives them the shape a model edits.
func Resolve(ctx context.Context, model prrepair.Model, conflicts []gitmerge.Conflict, workflows bool) ([]Resolution, error) {
	if len(conflicts) == 0 {
		return nil, nil
	}
	if model == nil {
		return nil, fmt.Errorf("%w: no model was supplied", ErrUnsupported)
	}
	if len(conflicts) > prrepair.MaxEdits {
		return nil, fmt.Errorf("%w: %d conflicted files is more than the %d a repair may edit", ErrUnsupported, len(conflicts), prrepair.MaxEdits)
	}

	files := make([]prrepair.File, 0, len(conflicts))
	originals := make(map[string]prrepair.File, len(conflicts))
	evidence := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		if !prrepair.AllowedPath(conflict.Path, workflows) {
			return nil, fmt.Errorf("%w: %s is protected by the repository repair policy", ErrUnsupported, conflict.Path)
		}
		merged := conflictMarked(conflict)
		if len(merged) > prrepair.MaxBlobBytes {
			return nil, fmt.Errorf("%w: %s is larger than the %d KiB the model may read", ErrUnsupported, conflict.Path, prrepair.MaxBlobBytes/1024)
		}
		file := prrepair.File{Path: conflict.Path, Mode: "100644", Content: merged}
		files = append(files, file)
		originals[conflict.Path] = file
		if strings.TrimSpace(conflict.Ancestor) != "" {
			evidence = append(evidence, "The common ancestor of `"+conflict.Path+"` was:\n"+conflict.Ancestor)
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	plan, err := prrepair.Generate(ctx, model, conflictReader{files: originals},
		prrepair.Snapshot{
			Title:    "Resolve the merge conflicts on this pull request",
			Files:    files,
			Failures: evidence,
		},
		"Resolve every conflict in the supplied files. Keep the intent of both sides: the target branch's side is above the separator and the pull request's side is below it. Do not keep the conflict markers, do not change a file outside the supplied conflicts, and do not widen the change beyond what resolving them needs.",
		workflows)
	if err != nil {
		return nil, err
	}

	resolved := make(map[string]string, len(plan.Files))
	for _, file := range plan.Files {
		if _, ok := originals[file.Path]; !ok {
			return nil, fmt.Errorf("%w: the model changed %s, which is not a conflicted file", ErrUnresolved, file.Path)
		}
		if hasConflictMarkers(file.Content) {
			return nil, fmt.Errorf("%w: the model left conflict markers in %s", ErrUnresolved, file.Path)
		}
		resolved[file.Path] = file.Content
	}
	out := make([]Resolution, 0, len(conflicts))
	for _, conflict := range conflicts {
		content, ok := resolved[conflict.Path]
		if !ok {
			return nil, fmt.Errorf("%w: the model did not resolve %s", ErrUnresolved, conflict.Path)
		}
		out = append(out, Resolution{Path: conflict.Path, Mode: "100644", Content: content})
	}
	return out, nil
}

// conflictMarked renders the two sides the way a person would see them, so the
// model is asked for the same shape it is told to remove.
func conflictMarked(conflict gitmerge.Conflict) string {
	var b strings.Builder
	b.WriteString("<<<<<<< target branch\n")
	b.WriteString(conflict.Base)
	if !strings.HasSuffix(conflict.Base, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("=======\n")
	b.WriteString(conflict.Head)
	if !strings.HasSuffix(conflict.Head, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(">>>>>>> pull request head\n")
	return b.String()
}

// hasConflictMarkers reports whether a resolved file still has a line that is
// only the marker form. A document may quote a marker in prose; what this
// refuses is a tree Git would still consider conflicted.
func hasConflictMarkers(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "<<<<<<< "), strings.HasPrefix(line, ">>>>>>> "), line == "=======":
			return true
		}
	}
	return false
}

// conflictReader serves the conflict-marked files. A path outside the conflict
// set is refused rather than invented: the model may ask for context, but this
// stage only has the conflicted files and says so instead of guessing.
type conflictReader struct {
	files map[string]prrepair.File
}

func (r conflictReader) ReadFile(_ context.Context, path string) (prrepair.File, error) {
	file, ok := r.files[path]
	if !ok {
		return prrepair.File{}, fmt.Errorf("%s is not one of the conflicted files supplied for this merge", path)
	}
	return file, nil
}

// ApplyChanges puts the resolved files back into the merge worker's change
// list. The worker's mode is kept, so an executable file stays executable, and
// a path the worker did not report as changed is refused rather than added.
func ApplyChanges(changes []gitmerge.Change, resolutions []Resolution) ([]gitmerge.Change, error) {
	if len(resolutions) == 0 {
		return changes, nil
	}
	index := make(map[string]int, len(changes))
	for i, change := range changes {
		index[change.Path] = i
	}
	seen := make(map[string]bool, len(resolutions))
	for _, resolution := range resolutions {
		i, ok := index[resolution.Path]
		if !ok {
			return nil, fmt.Errorf("%w: %s is not one of the merge worker's changed files", ErrUnresolved, resolution.Path)
		}
		if seen[resolution.Path] {
			return nil, fmt.Errorf("%w: %s was resolved twice", ErrUnresolved, resolution.Path)
		}
		seen[resolution.Path] = true
		change := changes[i]
		if change.Deleted {
			return nil, fmt.Errorf("%w: %s is deleted by the merge and cannot also be resolved", ErrUnresolved, resolution.Path)
		}
		if change.Mode != "100644" && change.Mode != "100755" {
			return nil, fmt.Errorf("%w: %s has mode %s, which a model may not rewrite", ErrUnresolved, resolution.Path, change.Mode)
		}
		if hasConflictMarkers(resolution.Content) {
			return nil, fmt.Errorf("%w: %s still carries conflict markers", ErrUnresolved, resolution.Path)
		}
		changes[i].Content = []byte(resolution.Content)
		if changes[i].Mode == "" {
			changes[i].Mode = "100644"
		}
	}
	return changes, nil
}

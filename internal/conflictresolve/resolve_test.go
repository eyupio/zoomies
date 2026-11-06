package conflictresolve

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eyupio/zoomies/internal/gitmerge"
	"github.com/eyupio/zoomies/internal/prrepair"
)

// replyModel answers with the replies it was given, in order, and records what
// the model was shown so a test can check the two sides reached it.
type replyModel struct {
	replies  []string
	messages [][]prrepair.Message
}

func (m *replyModel) Answer(_ context.Context, messages []prrepair.Message) (string, error) {
	m.messages = append(m.messages, append([]prrepair.Message(nil), messages...))
	reply := m.replies[0]
	if len(m.replies) > 1 {
		m.replies = m.replies[1:]
	}
	return reply, nil
}

func conflict(path, ancestor, base, head string) gitmerge.Conflict {
	return gitmerge.Conflict{Path: path, Ancestor: ancestor, Base: base, Head: head}
}

func TestResolveAsksForBothSidesAndReturnsTheWholeFile(t *testing.T) {
	model := &replyModel{replies: []string{
		`{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nB\nC\n"}]}`,
	}}
	resolved, err := Resolve(context.Background(), model, []gitmerge.Conflict{
		conflict("file.txt", "a\nb\nc\n", "a\nb\nC\n", "a\nB\nc\n"),
	}, false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Path != "file.txt" || resolved[0].Content != "a\nB\nC\n" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if len(model.messages) != 1 {
		t.Fatalf("model calls = %d", len(model.messages))
	}
	shown := model.messages[0][0].Content
	for _, side := range []string{"target branch", `a\nb\nC`, `a\nB\nc`, "pull request head", `a\nb\nc`} {
		if !strings.Contains(shown, side) {
			t.Errorf("the model was not shown %q:\n%s", side, shown)
		}
	}
}

func TestResolveRefusesAFileTheModelLeftConflicted(t *testing.T) {
	model := &replyModel{replies: []string{
		`{"summary":"still conflicted","files":[{"path":"file.txt","content":"<<<<<<< target branch\na\n=======\nb\n>>>>>>> pull request head\nand a note\n"}]}`,
	}}
	_, err := Resolve(context.Background(), model, []gitmerge.Conflict{
		conflict("file.txt", "", "a\n", "b\n"),
	}, false)
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("error = %v, want ErrUnresolved", err)
	}
}

func TestResolveRefusesAProtectedConflictBeforeCallingTheModel(t *testing.T) {
	model := &replyModel{replies: []string{`{"summary":"x","files":[{"path":".github/workflows/ci.yml","content":"name: ci\n"}]}`}}
	_, err := Resolve(context.Background(), model, []gitmerge.Conflict{
		conflict(".github/workflows/ci.yml", "", "name: a\n", "name: b\n"),
	}, false)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
	if len(model.messages) != 0 {
		t.Fatal("the model was called for a protected path")
	}
}

func TestResolveRefusesMoreConflictsThanARepairMayEdit(t *testing.T) {
	conflicts := make([]gitmerge.Conflict, prrepair.MaxEdits+1)
	for i := range conflicts {
		conflicts[i] = conflict("file"+string(rune('a'+i))+".txt", "", "left\n", "right\n")
	}
	_, err := Resolve(context.Background(), &replyModel{}, conflicts, false)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

func TestResolveWithNoConflictsDoesNothing(t *testing.T) {
	resolved, err := Resolve(context.Background(), &replyModel{}, nil, false)
	if err != nil || len(resolved) != 0 {
		t.Fatalf("resolved = %+v, err = %v", resolved, err)
	}
}

func TestResolveRequiresAFileForEveryConflict(t *testing.T) {
	model := &replyModel{replies: []string{
		`{"summary":"only one","files":[{"path":"a.txt","content":"resolved a\n"}]}`,
	}}
	_, err := Resolve(context.Background(), model, []gitmerge.Conflict{
		conflict("a.txt", "", "a\n", "A\n"),
		conflict("b.txt", "", "b\n", "B\n"),
	}, false)
	if err == nil {
		t.Fatal("a missing resolution was accepted")
	}
	if !errors.Is(err, ErrUnresolved) && !strings.Contains(err.Error(), "proposed path") {
		t.Fatalf("error = %v, want the missing file named", err)
	}
}

// The model's plan is JSON, as every repair plan is; this holds the shape the
// prompt promises without asserting the prose around it.
func TestResolveReadsAPlanTheModelAnswers(t *testing.T) {
	var plan struct {
		Summary string `json:"summary"`
		Files   []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	raw := `{"summary":"keep both edits","files":[{"path":"file.txt","content":"a\nB\nC\n"}]}`
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Summary == "" || len(plan.Files) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestApplyChangesKeepsTheWorkersModeAndOrder(t *testing.T) {
	changes := []gitmerge.Change{
		{Path: "run.sh", Mode: "100755", Content: []byte("old sh\n")},
		{Path: "added.txt", Mode: "100644", Content: []byte("base side\n")},
	}
	got, err := ApplyChanges(changes, []Resolution{{Path: "run.sh", Content: "resolved sh\n"}})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if len(got) != 2 || got[0].Path != "run.sh" || string(got[0].Content) != "resolved sh\n" || got[0].Mode != "100755" {
		t.Fatalf("changes = %+v", got)
	}
	if string(got[1].Content) != "base side\n" {
		t.Fatalf("an unrelated change was altered: %+v", got[1])
	}
}

func TestApplyChangesRefusesAPathTheMergeDidNotChange(t *testing.T) {
	_, err := ApplyChanges([]gitmerge.Change{{Path: "other.txt", Mode: "100644"}}, []Resolution{{Path: "missing.txt", Content: "x\n"}})
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("error = %v, want ErrUnresolved", err)
	}
}

func TestApplyChangesRefusesADeletedFileAndAMarker(t *testing.T) {
	if _, err := ApplyChanges([]gitmerge.Change{{Path: "gone.txt", Deleted: true}}, []Resolution{{Path: "gone.txt", Content: "x\n"}}); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("deleted file error = %v", err)
	}
	if _, err := ApplyChanges([]gitmerge.Change{{Path: "file.txt", Mode: "100644"}}, []Resolution{{Path: "file.txt", Content: "<<<<<<< target\nx\n"}}); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("marker error = %v", err)
	}
}

func TestResolveBudgetsALargeAncestorAsWindowedSource(t *testing.T) {
	model := &replyModel{replies: []string{`{"summary":"keep both edits","files":[{"path":"file.txt","content":"resolved\n"}]}`}}
	resolved, err := Resolve(t.Context(), model, []gitmerge.Conflict{
		conflict("file.txt", strings.Repeat("ancestor line\n", 16000), "base\n", "head\n"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || len(model.messages) != 1 {
		t.Fatalf("resolved %+v, calls %d", resolved, len(model.messages))
	}
	if len(model.messages[0][0].Content) >= prrepair.MaxContextBytes {
		t.Fatal("ancestor exceeded the context budget")
	}
}

func TestResolveKeepsTheExecutableMode(t *testing.T) {
	model := &replyModel{replies: []string{`{"summary":"keep both edits","files":[{"path":"run.sh","content":"resolved\n"}]}`}}
	c := conflict("run.sh", "ancestor\n", "base\n", "head\n")
	c.Mode = "100755"
	resolved, err := Resolve(t.Context(), model, []gitmerge.Conflict{c}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Mode != "100755" {
		t.Fatalf("resolved %+v", resolved)
	}
}

func TestResolveRefusesWithoutAModelOrWithAnUnusableConflict(t *testing.T) {
	if _, err := Resolve(context.Background(), nil, []gitmerge.Conflict{conflict("a.txt", "", "a\n", "b\n")}, false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("nil model error = %v, want ErrUnsupported", err)
	}
	big := strings.Repeat("x", prrepair.MaxBlobBytes)
	if _, err := Resolve(context.Background(), &replyModel{}, []gitmerge.Conflict{conflict("big.txt", "", big, "b\n")}, false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("oversized conflict error = %v, want ErrUnsupported", err)
	}
	c := conflict("link", "", "a\n", "b\n")
	c.Mode = "120000"
	if _, err := Resolve(context.Background(), &replyModel{}, []gitmerge.Conflict{c}, false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported mode error = %v, want ErrUnsupported", err)
	}
}

func TestResolveAddsTheNewlineTheSidesLack(t *testing.T) {
	marked := conflictMarked(conflict("file.txt", "", "target", "pull"))
	if !strings.Contains(marked, "target\n=======\n") || !strings.Contains(marked, "pull\n>>>>>>> pull request head\n") {
		t.Fatalf("conflict-marked text = %q", marked)
	}
	if !hasConflictMarkers("a\n=======\nb\n") {
		t.Fatal("a bare separator was not a conflict marker")
	}
}

func TestResolveReportsAModelFailure(t *testing.T) {
	model := &replyModel{replies: []string{"this is not a plan"}}
	if _, err := Resolve(context.Background(), model, []gitmerge.Conflict{conflict("file.txt", "", "a\n", "b\n")}, false); err == nil {
		t.Fatal("an unreadable plan was accepted")
	}
}

// An ancestor is supplied as read-only source, so a model that edits it has
// changed something that is not a conflicted file.
func TestResolveRefusesAModelEditOfAnAncestor(t *testing.T) {
	model := &replyModel{replies: []string{
		`{"summary":"edited the ancestor","files":[{"path":".eli-ancestors/0.txt","content":"changed\n"}]}`,
	}}
	_, err := Resolve(context.Background(), model, []gitmerge.Conflict{
		conflict("file.txt", "ancestor\n", "a\n", "b\n"),
	}, false)
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("error = %v, want ErrUnresolved", err)
	}
}

func TestConflictReaderRefusesAPathItWasNotGiven(t *testing.T) {
	_, err := (conflictReader{files: map[string]prrepair.File{"a.txt": {Path: "a.txt"}}}).ReadFile(context.Background(), "elsewhere.txt")
	if err == nil || !strings.Contains(err.Error(), "not one of the conflicted files") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyChangesWithNoResolutionsReturnsTheChanges(t *testing.T) {
	changes := []gitmerge.Change{{Path: "a.txt", Mode: "100644", Content: []byte("a\n")}}
	got, err := ApplyChanges(changes, nil)
	if err != nil || len(got) != 1 || string(got[0].Content) != "a\n" {
		t.Fatalf("changes = %+v, err = %v", got, err)
	}
}

func TestApplyChangesRefusesADuplicateModeOrAnUnsetModeItCanFill(t *testing.T) {
	if _, err := ApplyChanges(
		[]gitmerge.Change{{Path: "a.txt", Mode: "100644"}},
		[]Resolution{{Path: "a.txt", Content: "one\n"}, {Path: "a.txt", Content: "two\n"}},
	); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("duplicate resolution error = %v", err)
	}
	if _, err := ApplyChanges([]gitmerge.Change{{Path: "a.txt", Mode: "160000"}}, []Resolution{{Path: "a.txt", Content: "x\n"}}); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("submodule mode error = %v", err)
	}
	// The worker always names a mode; an empty one is refused like any other
	// mode a model may not rewrite, before the later default can apply.
	if _, err := ApplyChanges([]gitmerge.Change{{Path: "a.txt"}}, []Resolution{{Path: "a.txt", Content: "x\n"}}); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("empty mode error = %v", err)
	}
}

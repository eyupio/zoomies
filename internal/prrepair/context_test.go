package prrepair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type countingReader struct {
	reads int
	file  File
}

func (r *countingReader) ReadFile(_ context.Context, path string) (File, error) {
	r.reads++
	f := r.file
	f.Path = path
	return f, nil
}

// A request can mix useful source with a path the policy forbids. Refusing the
// latter must neither read it nor throw away the rest of the investigation.
func TestProtectedReadsAreRefusedToTheModelAndAllowedReadsStillArrive(t *testing.T) {
	for _, protected := range []string{".env", "../outside", ".github/workflows/ci.yml"} {
		for _, windows := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/windows=%t", protected, windows), func(t *testing.T) {
				request := Plan{Read: []string{protected, "main.go"}}
				if windows {
					request = Plan{Windows: []ReadWindow{{Path: protected, StartLine: 1, EndLine: 2}, {Path: "main.go", StartLine: 1, EndLine: 2}}}
				}
				raw, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				reader := &countingReader{file: File{Content: "old\n", Mode: "100644"}}
				model := &usage{replies: &replies{values: []string{string(raw), `{"summary":"Fix the source","files":[{"path":"main.go","content":"fixed\n"}]}`}}}
				plan, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", false)
				if err != nil {
					t.Fatal(err)
				}
				if reader.reads != 1 || len(model.used) != 2 || model.used[1] != len("old\n") {
					t.Fatalf("protected read reached the reader or spent source: reads %d, usage %v", reader.reads, model.used)
				}
				note := model.seen[len(model.seen)-1].Content
				if !strings.Contains(note, protected) || !strings.Contains(note, "protected") || !strings.Contains(note, `"path":"main.go"`) || strings.Contains(note, "already supplied") {
					t.Fatalf("missing source or misleading refusal: %s", note)
				}
				if len(plan.Files) != 1 || plan.Files[0].Path != "main.go" || plan.Files[0].Content != "fixed\n" {
					t.Fatalf("unexpected fix: %+v", plan)
				}
			})
		}
	}
}

func TestRepeatedProtectedReadsStopAtTheStepLimitAndNameThePath(t *testing.T) {
	model := &replies{}
	for range MaxSteps {
		model.values = append(model.values, `{"read":[".github/workflows/ci.yml"]}`)
	}
	reader := &countingReader{}
	_, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", false)
	if err == nil || !strings.Contains(err.Error(), "model-step limit") || !strings.Contains(err.Error(), ".github/workflows/ci.yml") || !strings.Contains(err.Error(), "protected") || strings.Contains(err.Error(), "allowance") {
		t.Fatalf("unhelpful outcome: %v", err)
	}
	if reader.reads != 0 {
		t.Fatalf("read a protected path %d times", reader.reads)
	}
}

func TestRefusingAProtectedReadStillLetsTheModelExplainWhyItCannotFix(t *testing.T) {
	model := &replies{values: []string{`{"read":[".env"]}`, `{"cannot_fix":"the required configuration is protected"}`}}
	_, err := Generate(t.Context(), model, &countingReader{}, Snapshot{}, "fix the failure", false)
	if !errors.Is(err, ErrCannotFix) || !strings.Contains(err.Error(), "required configuration is protected") {
		t.Fatalf("lost the model's diagnosis: %v", err)
	}
	note := model.seen[len(model.seen)-1].Content
	if strings.Contains(note, "already supplied") || !strings.Contains(note, ".env") {
		t.Fatalf("misleading refusal: %s", note)
	}
}

func TestWorkflowAccessIsExplainedAndEditsStillRequireTheOptIn(t *testing.T) {
	const workflow = ".github/workflows/ci.yml"
	for _, enabled := range []bool{false, true} {
		for _, targeted := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%t/targeted=%t", enabled, targeted), func(t *testing.T) {
				fix := Plan{Summary: "Fix the step", Files: []File{{Path: workflow, Content: "fixed\n"}}}
				if targeted {
					fix.Files = nil
					fix.Edits = []Edit{{Path: workflow, Before: "old", After: "fixed"}}
				}
				raw, err := json.Marshal(fix)
				if err != nil {
					t.Fatal(err)
				}
				model := &replies{values: []string{`{"read":["` + workflow + `"]}`, string(raw)}}
				reader := &countingReader{file: File{Content: "old\n", Mode: "100644"}}
				plan, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", enabled)
				if enabled {
					if err != nil || len(plan.Files) != 1 || plan.Files[0].Content != "fixed\n" || reader.reads != 1 {
						t.Fatalf("opted-in workflow fix failed: %+v, reads %d, error %v", plan, reader.reads, err)
					}
				} else if err == nil || len(plan.Files) != 0 || reader.reads != 0 {
					t.Fatalf("protected workflow was read or edited: %+v, reads %d, error %v", plan, reader.reads, err)
				}
				state := "disabled"
				if enabled {
					state = "enabled"
				}
				if !strings.Contains(model.seen[0].Content, "Workflow file reads and edits are "+state) {
					t.Fatal("the model was not told the active workflow policy")
				}
			})
		}
	}
}

func TestProtectedAndOversizedRequestsStillSupplyTheFileThatFits(t *testing.T) {
	reader := readerWithFour(28 << 10)
	reader["large.go"] = File{Content: source(MaxFileBytes)}
	reader["tiny.go"] = File{Content: "package tiny\n"}
	model := &usage{replies: &replies{values: []string{
		`{"read":["a.go","b.go","c.go","d.go"]}`,
		`{"read":["large.go",".env","tiny.go"]}`,
		`{"cannot_fix":"enough"}`,
	}}}
	_, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", false)
	if !errors.Is(err, ErrCannotFix) {
		t.Fatal(err)
	}
	note := model.seen[len(model.seen)-1].Content
	if !strings.Contains(note, "16371 bytes of the 128 KiB allowance remain") || !strings.Contains(note, `"path":"tiny.go"`) || !strings.Contains(note, `".env" was not supplied`) || strings.Contains(note, "an empty list means already supplied") {
		t.Fatalf("misleading mixed read result: %s", note)
	}
	if len(model.used) != 3 || model.used[2] != 114688+len("package tiny\n") {
		t.Fatalf("wrong source accounting after refusals: %v", model.used)
	}
}
func TestRepeatedSourceReadsDoNotSpendTheAllowanceAgain(t *testing.T) {
	r := &countingReader{file: File{Content: strings.Repeat("x", MaxFileBytes)}}
	s := newSourceContext(r, false)
	for _, path := range []string{"a", "b", "c", "d"} {
		if _, err := s.reveal(context.Background(), ReadWindow{Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		parts, err := s.reveal(context.Background(), ReadWindow{Path: "a"})
		if err != nil || len(parts) != 0 {
			t.Fatalf("duplicate read: %v %+v", err, parts)
		}
	}
	if s.bytes != MaxSourceBytes || r.reads != 4 {
		t.Fatalf("bytes %d reads %d", s.bytes, r.reads)
	}
}
func TestOverlappingWindowsOnlyDiscloseNewSource(t *testing.T) {
	s := newSourceContext(files{"a": {Content: "one\ntwo\nthree\nfour\n"}}, false)
	_, err := s.reveal(context.Background(), ReadWindow{Path: "a", StartLine: 1, EndLine: 2})
	if err != nil {
		t.Fatal(err)
	}
	parts, err := s.reveal(context.Background(), ReadWindow{Path: "a", StartLine: 2, EndLine: 3})
	if err != nil || len(parts) != 1 || parts[0].Content != "three\n" || s.bytes != len("one\ntwo\nthree\n") {
		t.Fatalf("overlap: %+v %v", parts, err)
	}
}
func TestTargetedEditsPreserveUnreadContentInLargeFiles(t *testing.T) {
	original := "fix me\n" + strings.Repeat("untouched\n", 10000)
	raw, _ := json.Marshal(Plan{Summary: "Fix first line", Edits: []Edit{{Path: "large.go", Before: "fix me", After: "fixed"}}})
	plan, err := Generate(context.Background(), &replies{values: []string{string(raw)}}, files{}, Snapshot{Files: []File{{Path: "large.go", Content: original, Mode: "100755"}}}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Content != "fixed\n"+strings.Repeat("untouched\n", 10000) || plan.Files[0].Mode != "100755" {
		t.Fatal("untouched content or executable mode changed")
	}
}
func TestTargetedEditsRejectUnseenAmbiguousAndOverlappingPreimages(t *testing.T) {
	for _, edits := range [][]Edit{
		{{Path: "a", Before: "same", After: "new"}},
		{{Path: "a", Before: "unseen", After: "new"}},
		{{Path: "a", Before: "unique text", After: "new"}, {Path: "a", Before: "text", After: "other"}},
	} {
		s := newSourceContext(files{"a": {Content: "same same unique text\n" + strings.Repeat("x", MaxFileBytes) + "unseen"}}, false)
		if _, err := s.reveal(context.Background(), ReadWindow{Path: "a"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.apply(edits); err == nil {
			t.Fatalf("accepted unsafe edits %+v", edits)
		}
	}
}
func TestCompleteReplacementCannotOverwriteAnUnreadTail(t *testing.T) {
	_, err := Generate(context.Background(), &replies{values: []string{`{"summary":"fix","files":[{"path":"a","content":"short"}]}`}}, files{}, Snapshot{Files: []File{{Path: "a", Content: strings.Repeat("x", MaxFileBytes+1)}}}, "", false)
	if err == nil || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("got %v", err)
	}
}

func TestRepositoryInventoryDoesNotCrowdOutRelevantSource(t *testing.T) {
	inventory := []string{}
	for range 1000 {
		inventory = append(inventory, strings.Repeat("long-directory/", 15)+"main.go")
	}
	model := &replies{values: []string{`{"summary":"fix","files":[{"path":"main.go","content":"fixed"}]}`}}
	_, err := Generate(context.Background(), model, files{}, Snapshot{Inventory: inventory, Files: []File{{Path: "main.go", Content: "old"}}}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.seen[0].Content, `"inventory_truncated":true`) {
		t.Fatal("inventory truncation not disclosed")
	}
}

// source is n bytes of 16-byte lines, so a test can say what a file costs the
// allowance without counting. n is a multiple of 16.
func source(n int) string { return strings.Repeat("0123456789abcde\n", n/16) }

// bigWorkflow is a workflow of some four thousand lines, far past one window,
// whose lines all differ so that a targeted edit can name one of them.
func bigWorkflow() File {
	var b strings.Builder
	for i := 0; b.Len() < 91<<10; i++ {
		fmt.Fprintf(&b, "  step-%05d: echo ok\n", i)
	}
	return File{Path: ".github/workflows/ci.yml", Content: b.String(), Mode: "100644"}
}

// usage records how much of the allowance had been spent before each model call.
type usage struct {
	*replies
	used []int
}

func (u *usage) SourceUsed(n int) { u.used = append(u.used, n) }

// fixStep edits the one line of bigWorkflow that the tests below name.
func fixStep(t *testing.T, path string) string {
	t.Helper()
	raw, err := json.Marshal(Plan{Summary: "Fix the step", Edits: []Edit{{Path: path, Before: "  step-00007: echo ok\n", After: "  step-00007: echo fixed\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// readerWithFour returns a reader holding the four files a model reads to fill
// most of the allowance, each size bytes, beside the workflow it will then want.
func readerWithFour(size int) files {
	ci := bigWorkflow()
	reader := files{ci.Path: ci}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		reader[name] = File{Path: name, Content: source(size), Mode: "100644"}
	}
	return reader
}

// A PR that changed several large files used to take the whole allowance before
// the model asked for anything, so the first file it asked for that the PR had
// not changed (the workflow its failing job runs) could not be read, and the
// repair ended there.
func TestChangedFilesLeaveRoomForWhatTheModelAsksFor(t *testing.T) {
	ci := bigWorkflow()
	snapshot := Snapshot{}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		snapshot.Files = append(snapshot.Files, File{Path: name, Content: source(40 << 10), Mode: "100644"})
	}
	model := &replies{values: []string{`{"read":["` + ci.Path + `"]}`, fixStep(t, ci.Path)}}
	plan, err := Generate(context.Background(), model, files{ci.Path: ci}, snapshot, "fix the failing check", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != ci.Path {
		t.Fatalf("plan changes %d files", len(plan.Files))
	}
	if plan.Files[0].Content != strings.Replace(ci.Content, "  step-00007: echo ok\n", "  step-00007: echo fixed\n", 1) {
		t.Fatal("the edit was not applied to the whole workflow, or other lines changed")
	}
}

// A large file arrives as its first window, and the hunk the failing change is
// in usually lies past it. The diff is sent to the model, so an edit built from
// the head side of that hunk quotes text the model has seen, and refusing it as
// unseen source ended a repair whose fix was readable all along.
func TestATargetedEditMayQuoteTheHeadSideOfTheSuppliedDiff(t *testing.T) {
	const path = "package-lock.json"
	content := source(MaxFileBytes) + "context before\nplatform package\ncontext after\n"
	snapshot := Snapshot{
		Files: []File{{Path: path, Content: content, Mode: "100644"}},
		Diff:  path + "\n@@ -1792,6 +1792,7 @@\n context before\n-old package\n+platform package\n context after\n",
	}
	raw, err := json.Marshal(Plan{Summary: "Drop the package", Edits: []Edit{{Path: path, Before: "context before\nplatform package\n", After: "context before\nremoved package\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	model := &replies{values: []string{string(raw)}}
	plan, err := Generate(context.Background(), model, files{}, snapshot, "fix the failure", false)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(content, "context before\nplatform package\n", "context before\nremoved package\n", 1)
	if len(plan.Files) != 1 || plan.Files[0].Content != want {
		t.Fatal("the edit was not applied to the window past the first one")
	}
}

// The diff is source the model was shown, not a reason to stop checking. Text
// that is in neither a supplied window nor the diff is still refused.
func TestATargetedEditOutsideBothTheWindowsAndTheDiffIsRefused(t *testing.T) {
	const path = "package-lock.json"
	content := source(MaxFileBytes) + "context before\nplatform package\ncontext after\n"
	snapshot := Snapshot{
		Files: []File{{Path: path, Content: content, Mode: "100644"}},
		Diff:  path + "\n@@ -1792,6 +1792,7 @@\n another context\n+another package\n",
	}
	raw, err := json.Marshal(Plan{Summary: "Drop the package", Edits: []Edit{{Path: path, Before: "context before\nplatform package\n", After: "removed\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Generate(context.Background(), &replies{values: []string{string(raw)}}, files{}, snapshot, "fix the failure", false)
	if err == nil || !strings.Contains(err.Error(), "text that was not supplied") {
		t.Fatalf("got %v", err)
	}
}

// The allowance is spent as the model reads, so a read can ask for more than is
// left. That is a request that was too wide and not a repair that went wrong:
// the model is told what remains, spends nothing on the refusal, and can ask
// for a window of the file instead.
func TestARequestThatDoesNotFitIsRefusedToTheModelAndTheRepairGoesOn(t *testing.T) {
	ci := bigWorkflow()
	model := &usage{replies: &replies{values: []string{
		`{"read":["a.go","b.go","c.go","d.go"]}`,
		`{"read":["` + ci.Path + `"]}`,
		`{"windows":[{"path":"` + ci.Path + `","start_line":1,"end_line":40}]}`,
		fixStep(t, ci.Path),
	}}}
	plan, err := Generate(context.Background(), model, readerWithFour(28<<10), Snapshot{}, "fix the failing check", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != ci.Path {
		t.Fatalf("plan changes %d files", len(plan.Files))
	}
	// Four files of 28 KiB are 114688 bytes, which leaves 131072 - 114688 = 16384.
	if len(model.used) != 4 || model.used[1] != 114688 || model.used[2] != 114688 {
		t.Fatalf("allowance used before each call: %v", model.used)
	}
	var told strings.Builder
	for _, m := range model.seen {
		told.WriteString(m.Content)
	}
	if !strings.Contains(told.String(), "16384") {
		t.Fatal("the model was not told how much of the allowance remained")
	}
}

// One request can name several files. A file that does not fit must not take
// the others down with it, or a model that asks for its workflow and a small
// helper together gets neither.
func TestARefusedReadDoesNotWithholdTheRestOfTheSameRequest(t *testing.T) {
	ci := bigWorkflow()
	reader := readerWithFour(28 << 10)
	reader["tiny.go"] = File{Path: "tiny.go", Content: "package tiny\n", Mode: "100644"}
	model := &usage{replies: &replies{values: []string{
		`{"read":["a.go","b.go","c.go","d.go"]}`,
		`{"read":["` + ci.Path + `","tiny.go"]}`,
		`{"cannot_fix":"enough"}`,
	}}}
	_, err := Generate(context.Background(), model, reader, Snapshot{}, "fix the failing check", true)
	if !errors.Is(err, ErrCannotFix) {
		t.Fatalf("got %v", err)
	}
	// "package tiny\n" is 13 bytes.
	if len(model.used) != 3 || model.used[2] != 114688+13 {
		t.Fatalf("allowance used before each call: %v", model.used)
	}
	last := model.seen[len(model.seen)-1].Content
	if !strings.Contains(last, "package tiny") {
		t.Fatal("the file that fitted was not supplied")
	}
}

// With nothing left there is nothing to narrow a request to. The model must
// still reach its own answer, which is what an operator reads, rather than the
// repair ending on a limit the model could not have known it was approaching.
func TestAnUsedUpAllowanceLeavesTheModelAbleToGiveUp(t *testing.T) {
	ci := bigWorkflow()
	model := &replies{values: []string{
		`{"read":["a.go","b.go","c.go","d.go"]}`,
		`{"read":["` + ci.Path + `"]}`,
		`{"cannot_fix":"the workflow could not be read"}`,
	}}
	_, err := Generate(context.Background(), model, readerWithFour(MaxFileBytes), Snapshot{}, "fix the failing check", true)
	if !errors.Is(err, ErrCannotFix) || !strings.Contains(err.Error(), "the workflow could not be read") {
		t.Fatalf("got %v", err)
	}
}

// A refusal says what remains so that the model can size the window it asks for
// next, which only helps if the figure is true when the model reads it. One
// request can name a file that does not fit before one that does, and the
// second spends part of what is left, so a figure taken at the first refusal
// overstates it. A model that sized its window from that figure was refused
// again, and each refusal costs one of the repair's six rounds.
func TestARefusalSaysWhatRemainsOnceTheWholeRequestHasBeenAnswered(t *testing.T) {
	reader := files{}
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		reader[name] = File{Path: name, Content: source(MaxFileBytes), Mode: "100644"}
	}
	reader["pad.go"] = File{Path: "pad.go", Content: source(2048), Mode: "100644"}
	reader["big.go"] = File{Path: "big.go", Content: source(40 << 10), Mode: "100644"}
	reader["mid.go"] = File{Path: "mid.go", Content: source(20 << 10), Mode: "100644"}
	model := &usage{replies: &replies{values: []string{
		// Three files of 32768 and one of 2048 are 100352 bytes, which leaves 30720.
		`{"read":["a.go","b.go","c.go","pad.go"]}`,
		// big.go would add 32768 and is refused first. mid.go adds 20480 and fits,
		// which leaves 10240 for the next request.
		`{"read":["big.go","mid.go"]}`,
		`{"cannot_fix":"enough"}`,
	}}}
	if _, err := Generate(context.Background(), model, reader, Snapshot{}, "fix the failing check", false); !errors.Is(err, ErrCannotFix) {
		t.Fatalf("got %v", err)
	}
	remaining := MaxSourceBytes - model.used[2]
	if remaining != 10240 {
		t.Fatalf("the scenario should leave 10240 bytes, not %d", remaining)
	}
	last := model.seen[len(model.seen)-1].Content
	want := fmt.Sprintf("%d bytes of the %d KiB allowance remain", remaining, MaxSourceBytes/1024)
	if !strings.Contains(last, want) {
		t.Fatalf("the model was not told %q, which is what remained when it read the answer. It was told:\n%s", want, refusals(last))
	}
}

// refusals is what a note says was not supplied: the lines after the one that
// carries the source, which a failure has no reason to print.
func refusals(note string) string {
	_, after, _ := strings.Cut(note, "\n")
	return after
}

// The changed files may take half of the allowance before the model asks for
// anything, and no more: the rest is what its own requests are promised, the
// workflow its failing job runs among them, which the PR did not change. Three
// files of one window each are 98304 bytes, so the third is the one that does
// not load, and it is named so that the model knows it may ask for it.
func TestTheChangedFilesTakeAtMostHalfTheAllowanceAndTheRestAreNamed(t *testing.T) {
	snapshot := Snapshot{}
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		snapshot.Files = append(snapshot.Files, File{Path: name, Content: source(MaxFileBytes), Mode: "100644"})
	}
	model := &usage{replies: &replies{values: []string{`{"cannot_fix":"enough"}`}}}
	if _, err := Generate(context.Background(), model, files{}, snapshot, "fix the failing check", false); !errors.Is(err, ErrCannotFix) {
		t.Fatalf("got %v", err)
	}
	// Half is what the documentation promises, so the number is written here and
	// not read from the constant that holds it.
	if len(model.used) != 1 || model.used[0] != 65536 {
		t.Fatalf("source supplied before the first call: %v, want 65536", model.used)
	}
	first := model.seen[0].Content
	for name, loaded := range map[string]bool{"a.go": true, "b.go": true, "c.go": false} {
		if named := strings.Contains(first, name+": not loaded initially"); named == loaded {
			t.Errorf("%s: named as not loaded = %v, but loaded = %v", name, named, loaded)
		}
	}
}

// The allowance is a count of bytes, and both of its edges matter: a read that
// needs exactly what is left must fit, or a repair loses the last byte of room
// it was promised, and one that needs a byte more must not, or the allowance
// drifts upwards with every read that is allowed to stretch it.
func TestAReadNeedingExactlyWhatRemainsFitsAndOneByteMoreDoesNot(t *testing.T) {
	window := strings.Repeat("x", MaxFileBytes)
	for _, tc := range []struct {
		name string
		// extra is what has been read beyond three whole windows, which leaves
		// either exactly one window of the allowance or a byte less.
		extra int
		fits  bool
	}{
		{"a read needing exactly what remains", 0, true},
		{"a read needing one byte more than remains", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := files{"a": {Content: window}, "b": {Content: window}, "c": {Content: window}, "d": {Content: window}, "e": {Content: strings.Repeat("e", tc.extra)}}
			s := newSourceContext(reader, false)
			for _, name := range []string{"a", "b", "c", "e"} {
				if _, err := s.reveal(context.Background(), ReadWindow{Path: name}); err != nil {
					t.Fatal(err)
				}
			}
			before := s.bytes
			_, err := s.reveal(context.Background(), ReadWindow{Path: "d"})
			var over *allowanceError
			switch {
			case tc.fits && (err != nil || s.bytes != MaxSourceBytes):
				t.Fatalf("the last window was refused or miscounted: %v, %d bytes of %d", err, s.bytes, MaxSourceBytes)
			case !tc.fits && (!errors.As(err, &over) || over.need != MaxFileBytes || over.left != MaxSourceBytes-before || s.bytes != before):
				t.Fatalf("a window one byte too many was admitted or spent something: %v, %d bytes, was %d", err, s.bytes, before)
			}
		})
	}
}

// With nothing left there is no window to ask for, and the model is told so in
// as many words, not that some number of bytes remain: the sentence is what
// sends it to its own answer, and a figure of zero would send it looking for a
// window to shrink.
func TestAnUsedUpAllowanceIsSaidToTheModelAsSuch(t *testing.T) {
	ci := bigWorkflow()
	model := &replies{values: []string{
		`{"read":["a.go","b.go","c.go","d.go"]}`,
		`{"read":["` + ci.Path + `"]}`,
		`{"cannot_fix":"enough"}`,
	}}
	if _, err := Generate(context.Background(), model, readerWithFour(MaxFileBytes), Snapshot{}, "fix the failing check", true); !errors.Is(err, ErrCannotFix) {
		t.Fatalf("got %v", err)
	}
	last := model.seen[len(model.seen)-1].Content
	if !strings.Contains(last, "the whole 128 KiB source allowance is used up") || strings.Contains(last, "bytes of the 128 KiB allowance remain") {
		t.Fatalf("the model was told:\n%s", refusals(last))
	}
}

// A model that keeps asking for what does not fit is stopped by the round limit,
// and "reached the model-step limit" on its own sends whoever reads it to the
// model, when it was the request that was too wide for the allowance. The error
// says which reads were turned away, as the one it replaced did when the refusal
// itself ended the repair, so that the person narrows the request and does not
// simply ask again.
func TestARepairThatRunsOutOfRoundsOnRefusedReadsNamesThem(t *testing.T) {
	reader := files{}
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		reader[name] = File{Path: name, Content: source(MaxFileBytes), Mode: "100644"} // four of them are the whole allowance
	}
	for _, name := range []string{"x1.go", "x2.go", "x3.go", "x4.go", "x5.go"} {
		reader[name] = File{Path: name, Content: source(40 << 10), Mode: "100644"}
	}
	for _, tc := range []struct {
		name string
		asks []string
		want string
	}{
		{"one file", []string{"x1.go"}, "asked for `x1.go`, which"},
		{"two files", []string{"x1.go", "x2.go"}, "asked for `x1.go` and `x2.go`, which"},
		{"three files", []string{"x1.go", "x2.go", "x3.go"}, "asked for `x1.go`, `x2.go` and `x3.go`, which"},
		{"more than three files", []string{"x1.go", "x2.go", "x3.go", "x4.go", "x5.go"}, "asked for `x1.go`, `x2.go`, `x3.go` and 2 more, which"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ask, err := json.Marshal(map[string][]string{"read": tc.asks})
			if err != nil {
				t.Fatal(err)
			}
			// The first round fills the allowance, and every round after it asks
			// for more than is left.
			values := []string{`{"read":["a.go","b.go","c.go","d.go"]}`}
			for len(values) < MaxSteps {
				values = append(values, string(ask))
			}
			_, err = Generate(context.Background(), &replies{values: values}, reader, Snapshot{}, "fix the failing check", false)
			if err == nil || !strings.Contains(err.Error(), "model-step limit") || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "128 KiB source allowance") {
				t.Fatalf("got %v, want the round limit, %q and the allowance", err, tc.want)
			}
		})
	}
}

// "An empty list means already supplied" answers a model that asks for source it
// holds, and it is wrong for one that asked for source it was refused: the list
// is empty because nothing was supplied. Told both, a model can take a refusal
// for a repeat of something it already has and go on to edit a file it never saw.
func TestAnEmptyListIsOnlyCalledAlreadySuppliedWhenNothingWasRefused(t *testing.T) {
	ci := bigWorkflow()
	for _, tc := range []struct {
		name    string
		values  []string
		refused bool
	}{
		{"a repeated read", []string{`{"read":["a.go"]}`, `{"read":["a.go"]}`, `{"cannot_fix":"enough"}`}, false},
		{"a refused read", []string{`{"read":["a.go","b.go","c.go","d.go"]}`, `{"read":["` + ci.Path + `"]}`, `{"cannot_fix":"enough"}`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &replies{values: tc.values}
			if _, err := Generate(context.Background(), model, readerWithFour(MaxFileBytes), Snapshot{}, "fix the failing check", true); !errors.Is(err, ErrCannotFix) {
				t.Fatalf("got %v", err)
			}
			last := model.seen[len(model.seen)-1].Content
			if said := strings.Contains(last, "an empty list means already supplied"); said == tc.refused {
				t.Fatalf("the note calls an empty list already supplied = %v, but the request was refused = %v:\n%s", said, tc.refused, last)
			}
		})
	}
}

// A window wider than the repair reads used to end the attempt as an
// unsupported fix, though the model had only asked for six hundred lines of a
// lockfile at once. It is told the bound, and a window that fits still reaches
// it in the same repair.
func TestAWindowOutsideTheLineBoundIsSaidToTheModelAndTheRepairGoesOn(t *testing.T) {
	var content strings.Builder
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&content, "line-%04d\n", i)
	}
	reader := files{"lock.json": {Path: "lock.json", Content: content.String(), Mode: "100644"}}
	fix, err := json.Marshal(Plan{Summary: "Fix the lock entry", Edits: []Edit{{Path: "lock.json", Before: "line-0950\n", After: "line-0950-fixed\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	values := []string{
		`{"windows":[{"path":"lock.json","start_line":900,"end_line":1500}]}`,
		`{"windows":[{"path":"lock.json","start_line":900,"end_line":1200}]}`,
		string(fix),
	}
	notes := []string{}
	model := budgetModel(func(messages []Message) (string, error) {
		notes = append(notes, messages[len(messages)-1].Content)
		return values[len(notes)-1], nil
	})
	plan, err := Generate(context.Background(), model, reader, Snapshot{}, "fix the failing check", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || !strings.Contains(plan.Files[0].Content, "line-0950-fixed\n") {
		t.Fatalf("the repair did not go on to the fix: %+v", plan)
	}
	refusal := notes[1]
	if !strings.Contains(refusal, "file window needs 1 to 401 lines") || !strings.Contains(refusal, "lock.json") || strings.Contains(refusal, "an empty list means already supplied") {
		t.Fatalf("the too-wide window was not said clearly: %s", refusal)
	}
}

// A model that keeps asking for a window past the line bound is stopped by the
// round limit, and the error says which window it never got, rather than
// blaming the source allowance it did not spend.
func TestARepairThatRunsOutOfRoundsOnAWideWindowNamesIt(t *testing.T) {
	reader := files{"lock.json": {Path: "lock.json", Content: strings.Repeat("line\n", 2000), Mode: "100644"}}
	model := &replies{}
	for range MaxSteps {
		model.values = append(model.values, `{"windows":[{"path":"lock.json","start_line":1,"end_line":1500}]}`)
	}
	_, err := Generate(context.Background(), model, reader, Snapshot{}, "fix the failing check", false)
	if err == nil || !strings.Contains(err.Error(), "model-step limit") || !strings.Contains(err.Error(), "lock.json") || !strings.Contains(err.Error(), "too wide") || strings.Contains(err.Error(), "source allowance") {
		t.Fatalf("unhelpful outcome: %v", err)
	}
}

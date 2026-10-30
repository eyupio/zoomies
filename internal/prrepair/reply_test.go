package prrepair

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The model wrote the file it wanted to read and the window it wanted as two
// objects in one answer. They are one investigation, so they are merged rather
// than ending the repair as "more than one repair plan".
const readAndWindowReply = "\n\n{\"read\":[\".github/workflows/ci.yml\",\"tsconfig.json\"]}\n\n{\"windows\":[{\"path\":\"package-lock.json\",\"start_line\":1750,\"end_line\":1880},{\"path\":\"package-lock.json\",\"start_line\":3560,\"end_line\":3700}]}"

func TestARepairReplyIsReadThroughProseAndCodeFences(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		want  Plan
	}{
		{"a fenced reply", "```json\n{\"read\":[\"a.go\"]}\n```", Plan{Read: []string{"a.go"}}},
		{"a fence with no language", "```\n{\"cannot_fix\":\"needs a secret\"}\n```", Plan{CannotFix: "needs a secret"}},
		{"a sentence before the reply", "Here is the next step:\n{\"read\":[\"a.go\"]}", Plan{Read: []string{"a.go"}}},
		{"a sentence after the reply", "{\"read\":[\"a.go\"]}\nI will read it now.", Plan{Read: []string{"a.go"}}},
		{"a key the repair does not use", "{\"read\":[\"a.go\"],\"confidence\":0.9,\"notes\":\"because\"}", Plan{Read: []string{"a.go"}}},
		{"one path written as a string", "{\"read\":\"a.go\"}", Plan{Read: []string{"a.go"}}},
		{"one window written as an object", "{\"windows\":{\"path\":\"a.go\",\"start_line\":1,\"end_line\":2}}", Plan{Windows: []ReadWindow{{Path: "a.go", StartLine: 1, EndLine: 2}}}},
		{"a key in capitals", "{\"Read\":[\"a.go\"]}", Plan{Read: []string{"a.go"}}},
		{"a file written as one object", "{\"summary\":\"why\",\"files\":{\"path\":\"a.go\",\"content\":\"fixed\"}}", Plan{Summary: "why", Files: []File{{Path: "a.go", Content: "fixed"}}}},
		{"a read and a window in one object", "{\"read\":[\"a.go\"],\"windows\":[{\"path\":\"b.go\",\"start_line\":1,\"end_line\":2}]}", Plan{Read: []string{"a.go"}, Windows: []ReadWindow{{Path: "b.go", StartLine: 1, EndLine: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decode(tc.reply)
			if err != nil {
				t.Fatalf("decode(%q): %v", tc.reply, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decode(%q) = %+v, want %+v", tc.reply, got, tc.want)
			}
		})
	}
}

func TestTheModelWritingReadAndWindowsAsTwoObjectsIsOneInvestigation(t *testing.T) {
	got, err := decode(readAndWindowReply)
	if err != nil {
		t.Fatal(err)
	}
	want := Plan{
		Read: []string{".github/workflows/ci.yml", "tsconfig.json"},
		Windows: []ReadWindow{
			{Path: "package-lock.json", StartLine: 1750, EndLine: 1880},
			{Path: "package-lock.json", StartLine: 3560, EndLine: 3700},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decode = %+v, want %+v", got, want)
	}
}

// Two answers are still two answers. A model that proposes two changes, or one
// change beside a read, has not said which one it meant.
func TestARepairReplyThatHoldsTwoAnswersIsRefused(t *testing.T) {
	for _, reply := range []string{
		`{"summary":"one","files":[{"path":"a.go","content":"one"}]} {"summary":"two","files":[{"path":"a.go","content":"two"}]}`,
		`{"read":["a.go"]} {"summary":"one","files":[{"path":"a.go","content":"one"}]}`,
		`{"cannot_fix":"no"} {"read":["a.go"]}`,
		`{"read":["a.go"]} {"cannot_fix":"no"}`,
		`{"read":["a.go"]} {}`,
		`{"read":["a.go"]} I will read it. {"windows":[{"path":"b.go","start_line":1,"end_line":2}]}`,
	} {
		t.Run(reply, func(t *testing.T) {
			if _, err := decode(reply); !errors.Is(err, errMultiplePlans) {
				t.Fatalf("decode(%q) = %v, want %v", reply, err, errMultiplePlans)
			}
		})
	}
}

func TestAFileEntryCannotSetItsMode(t *testing.T) {
	if _, err := decode(`{"summary":"why","files":[{"path":"a.go","content":"fixed","mode":"120000"}]}`); !errors.Is(err, errInvalidReply) {
		t.Fatalf("decode = %v, want %v", err, errInvalidReply)
	}
}

func TestAReplyWithNoPlanIsRefused(t *testing.T) {
	for _, reply := range []string{
		"",
		"I could not decide what to do.",
		`{"summary":"a plan without an action"}`,
		`{"notes":"nothing the repair uses"}`,
		`{"read":[`,
		`{"read":[7]}`,
	} {
		t.Run(reply, func(t *testing.T) {
			if _, err := decode(reply); !errors.Is(err, errInvalidReply) {
				t.Fatalf("decode(%q) = %v, want %v", reply, err, errInvalidReply)
			}
		})
	}
}

type scriptedReplies struct {
	replies []string
	seen    [][]Message
}

func (m *scriptedReplies) Answer(_ context.Context, messages []Message) (string, error) {
	m.seen = append(m.seen, append([]Message(nil), messages...))
	if len(m.replies) == 0 {
		return "", errors.New("no scripted reply")
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return reply, nil
}

func TestARepairAsksTheModelToCorrectAMalformedReply(t *testing.T) {
	m := &scriptedReplies{replies: []string{
		"I will look at main.go first.",
		`{"summary":"Fix it","files":[{"path":"main.go","content":"fixed"}]}`,
	}}
	p, err := Generate(context.Background(), m, files{"main.go": {Path: "main.go", Content: "old", Mode: "100644"}}, Snapshot{Files: []File{{Path: "main.go", Content: "old", Mode: "100644"}}}, "fix the failing check", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 || p.Files[0].Content != "fixed" || p.Summary != "Fix it" {
		t.Fatalf("plan = %+v", p)
	}
	if len(m.seen) != 2 {
		t.Fatalf("model was asked %d times, want 2", len(m.seen))
	}
	last := m.seen[1]
	if len(last) < 2 {
		t.Fatalf("the correction did not reach the model: %+v", last)
	}
	if last[len(last)-2].Role != "assistant" || last[len(last)-2].Content != "I will look at main.go first." {
		t.Errorf("the model did not see its own reply: %+v", last[len(last)-2])
	}
	if !strings.Contains(last[len(last)-1].Content, "not one repair plan") || !strings.Contains(last[len(last)-1].Content, "No tools") && !strings.Contains(last[len(last)-1].Content, "Do not call tools") {
		t.Errorf("the correction does not say what to send: %q", last[len(last)-1].Content)
	}
}

func TestARepairStopsAfterTheReplyRetryLimit(t *testing.T) {
	m := &scriptedReplies{replies: []string{"not a plan", "still not a plan", "not a plan either", "would have been fine"}}
	_, err := Generate(context.Background(), m, files{}, Snapshot{}, "fix the failing check", false)
	if !errors.Is(err, errInvalidReply) {
		t.Fatalf("Generate = %v, want %v", err, errInvalidReply)
	}
	if len(m.seen) != maxReplyRetries+1 {
		t.Fatalf("model was asked %d times, want %d", len(m.seen), maxReplyRetries+1)
	}
}

// These are valid JSON, so the old parse-only retry skipped them and ended
// rea#996 with "a repair step must request files or propose changes".
func TestARepairAsksForOneBoundedActionBeforeReadingOrApplyingAnything(t *testing.T) {
	tooManyReads := make([]string, MaxEdits+1)
	for i := range tooManyReads {
		tooManyReads[i] = fmt.Sprintf(`"source%d.go"`, i)
	}
	for _, tc := range []struct {
		name, reply, reason string
	}{
		{"reads and files", `{"read":["unread.go"],"summary":"Fix it","files":[{"path":"main.go","content":"wrong"}]}`, "do not combine"},
		{"windows and edits", `{"windows":[{"path":"unread.go","start_line":1,"end_line":2}],"summary":"Fix it","edits":[{"path":"main.go","before":"old","after":"wrong"}]}`, "do not combine"},
		{"too many reads", `{"read":[` + strings.Join(tooManyReads, ",") + `]}`, "at most 8"},
		{"combined read count", `{"read":[` + strings.Join(tooManyReads[:MaxEdits], ",") + `],"windows":[{"path":"unread.go","start_line":1,"end_line":2}]}`, "at most 8"},
		{"edits and files", `{"summary":"Fix it","edits":[{"path":"main.go","before":"old","after":"wrong"}],"files":[{"path":"main.go","content":"wrong"}]}`, "not both"},
		{"giving up and reading", `{"cannot_fix":"no","read":["unread.go"]}`, "cannot_fix must stand alone"},
		{"giving up and fixing", `{"cannot_fix":"no","summary":"Fix it","files":[{"path":"main.go","content":"wrong"}]}`, "cannot_fix must stand alone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &scriptedReplies{replies: []string{tc.reply, `{"summary":"Fix it","edits":[{"path":"main.go","before":"old","after":"fixed"}]}`}}
			reader := &countingReader{}
			p, err := Generate(t.Context(), m, reader, Snapshot{Files: []File{{Path: "main.go", Content: "old", Mode: "100755"}}}, "fix the failing check", false)
			if err != nil || len(p.Files) != 1 || p.Files[0].Content != "fixed" || p.Files[0].Mode != "100755" {
				t.Fatalf("corrected plan = %+v: %v", p, err)
			}
			if reader.reads != 0 {
				t.Fatalf("invalid step read %d files before correction", reader.reads)
			}
			if len(m.seen) != 2 {
				t.Fatalf("model was asked %d times, want 2", len(m.seen))
			}
			last := m.seen[1]
			if last[len(last)-2].Content != tc.reply || !strings.Contains(last[len(last)-1].Content, tc.reason) {
				t.Fatalf("correction lost the reply or its specific cause: %+v", last)
			}
		})
	}
}

func TestMalformedAndMixedRepliesShareTheCorrectionLimit(t *testing.T) {
	mixed := `{"read":["unread.go"],"files":[{"path":"main.go","content":"wrong"}]}`
	m := &scriptedReplies{replies: []string{mixed, "not JSON", mixed, `{"cannot_fix":"unused"}`}}
	reader := &countingReader{}
	_, err := Generate(t.Context(), m, reader, Snapshot{}, "fix it", false)
	if err == nil || !strings.Contains(err.Error(), "do not combine") || len(m.seen) != maxReplyRetries+1 || reader.reads != 0 {
		t.Fatalf("calls %d, reads %d: %v", len(m.seen), reader.reads, err)
	}
}

func TestAValidSourceTurnResetsCorrectionsWithoutExtendingTheTurnLimit(t *testing.T) {
	mixed := `{"read":["unread.go"],"edits":[{"path":"a","before":"old","after":"wrong"}]}`
	m := &scriptedReplies{}
	for step := range MaxSteps {
		if step%3 == 2 {
			m.replies = append(m.replies, `{"read":["a"]}`)
		} else {
			m.replies = append(m.replies, mixed)
		}
	}
	reader := &countingReader{}
	_, err := Generate(t.Context(), m, reader, Snapshot{}, "fix it", false)
	if err == nil || !strings.Contains(err.Error(), "model-step limit") || len(m.seen) != MaxSteps || reader.reads != 1 {
		t.Fatalf("calls %d, reads %d: %v", len(m.seen), reader.reads, err)
	}
}

func TestCorrectionDoesNotPermitAnUnreadOrProtectedEdit(t *testing.T) {
	for _, path := range []string{"unread.go", ".env"} {
		t.Run(path, func(t *testing.T) {
			m := &scriptedReplies{replies: []string{
				`{"read":["main.go"],"files":[{"path":"main.go","content":"wrong"}]}`,
				fmt.Sprintf(`{"summary":"Fix it","files":[{"path":%q,"content":"fixed"}]}`, path),
				`{"cannot_fix":"that path is not available"}`,
			}}
			_, err := Generate(t.Context(), m, &countingReader{}, Snapshot{}, "fix it", false)
			if !errors.Is(err, ErrCannotFix) || len(m.seen) != 3 {
				t.Fatalf("calls %d: %v", len(m.seen), err)
			}
			if last := m.seen[2][len(m.seen[2])-1].Content; !strings.Contains(last, "not supplied or is protected") {
				t.Fatalf("the model was not told why the path was refused: %s", last)
			}
		})
	}
}

func TestARepairStillEndsWhenTheModelSaysItCannotFix(t *testing.T) {
	m := &scriptedReplies{replies: []string{`{"cannot_fix":"the failure is infrastructure"}`}}
	_, err := Generate(context.Background(), m, files{}, Snapshot{}, "fix the failing check", false)
	if !errors.Is(err, ErrCannotFix) {
		t.Fatalf("Generate = %v, want %v", err, ErrCannotFix)
	}
	if len(m.seen) != 1 {
		t.Fatalf("model was asked %d times, want 1", len(m.seen))
	}
}

// A preimage that occurs twice is a mistake in the plan, not a repair that
// cannot be made. The model is told, and a corrected edit in the same attempt
// is applied. Before this, the first non-unique preimage ended the repair.
func TestAFixableMistakeInAFixIsCorrectedRatherThanEndingTheRepair(t *testing.T) {
	m := &scriptedReplies{replies: []string{
		`{"summary":"Fix it","edits":[{"path":"a","before":"same","after":"new"}]}`,
		`{"summary":"Fix it","edits":[{"path":"a","before":"unique text","after":"new"}]}`,
	}}
	plan, err := Generate(t.Context(), m, files{"a": {Path: "a", Content: "same same unique text\n", Mode: "100644"}}, Snapshot{
		Files: []File{{Path: "a", Content: "same same unique text\n", Mode: "100644"}},
	}, "fix it", false)
	if err != nil || len(plan.Files) != 1 || plan.Files[0].Content != "same same new\n" {
		t.Fatalf("plan %+v: %v", plan, err)
	}
	if len(m.seen) != 2 {
		t.Fatalf("model was asked %d times, want 2", len(m.seen))
	}
	if last := m.seen[1][len(m.seen[1])-1].Content; !strings.Contains(last, "exact, unique preimage") {
		t.Fatalf("the correction does not say what was wrong: %s", last)
	}
}

// A final turn has no correction turn after it. Returning the step limit for an
// invalid final fix hides the mistake the operator can act on, so the plan's own
// error is the one that ends the repair.
func TestAFixRefusedOnTheFinalTurnSaysWhyNotJustTheStepLimit(t *testing.T) {
	m := &scriptedReplies{}
	for range MaxSteps - 1 {
		m.replies = append(m.replies, `{"read":["a"]}`)
	}
	m.replies = append(m.replies, `{"summary":"","files":[{"path":"a","content":"new"}]}`)
	_, err := Generate(t.Context(), m, files{"a": {Path: "a", Content: "old", Mode: "100644"}}, Snapshot{Files: []File{{Path: "a", Content: "old", Mode: "100644"}}}, "fix it", false)
	if err == nil || !strings.Contains(err.Error(), "bounded fix") || strings.Contains(err.Error(), "step limit") {
		t.Fatalf("got %v", err)
	}
}

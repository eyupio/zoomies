package prrepair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type replies struct {
	values []string
	seen   []Message
}

func (m *replies) Answer(_ context.Context, messages []Message) (string, error) {
	m.seen = append([]Message(nil), messages...)
	if len(m.values) == 0 {
		return "", fmt.Errorf("no reply")
	}
	v := m.values[0]
	m.values = m.values[1:]
	return v, nil
}

type files map[string]File

func (f files) ReadFile(_ context.Context, name string) (File, error) { return f[name], nil }
func TestRepairPathsProtectCredentialsAndWorkflowPermissions(t *testing.T) {
	for _, p := range []string{".Git/config", ".SSH/id_rsa", ".AWS/config", "secrets/token", ".GITHUB/workflows/ci.yml", "../a", "a/../b", "/tmp/a", `a\b`, ".git/config", ".env", "dir/.env.local", "key.pem", "credentials.json", "secrets.yaml", ".ssh/id_rsa", ".github/workflows/test.yml"} {
		if AllowedPath(p, false) {
			t.Errorf("allowed %q", p)
		}
	}
	if !AllowedPath(".github/workflows/test.yml", true) || !AllowedPath("src/main.go", false) {
		t.Fatal("ordinary source or opted-in workflow blocked")
	}
}
func TestRepairReadsPinnedSourceAndPreservesExecutableMode(t *testing.T) {
	m := &replies{values: []string{`{"read":["bin/run.sh"]}`, `{"summary":"Fix exit code","files":[{"path":"bin/run.sh","content":"exit 0\n"}]}`}}
	p, e := Generate(context.Background(), m, files{"bin/run.sh": {Path: "bin/run.sh", Content: "exit 1\n", Mode: "100755"}}, Snapshot{}, "fix failure", false)
	if e != nil || len(p.Files) != 1 || p.Files[0].Mode != "100755" {
		t.Fatalf("plan %+v: %v", p, e)
	}
	if !strings.Contains(m.seen[len(m.seen)-1].Content, "exit 1") {
		t.Fatal("model did not receive requested source")
	}
}
func TestRepairRefusesUnreadPathsAndIncompleteOrSecretPlans(t *testing.T) {
	for _, reply := range []string{
		`{"summary":"fix","files":[{"path":"unread.go","content":"fixed"}]}`,
		`{"summary":"fix","files":[{"path":"main.go","content":"old"}]}`,
		`{"summary":"fix","files":[{"path":"main.go","content":"ghp_abcdefghijklmnop"}]}`,
		`{"summary":"fix","files":[{"path":"main.go","content":"fixed","mode":"120000"}]}`,
		`{"summary":"fix","files":[{"path":"main.go","content":"fixed"}]} {}`,
		`{"read":[".env"]}`, `{"cannot_fix":"infrastructure failure"}`, `{"read":["main.go"],"files":[{"path":"main.go","content":"fixed"}]}`,
	} {
		t.Run(reply, func(t *testing.T) {
			_, e := Generate(context.Background(), &replies{values: []string{reply}}, files{}, Snapshot{Files: []File{{Path: "main.go", Content: "old", Mode: "100644"}}}, "", false)
			if e == nil {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
}
func TestRepairNeverSendsProtectedInitialFilesAndRedactsTokens(t *testing.T) {
	m := &replies{values: []string{`{"cannot_fix":"no evidence"}`}}
	_, _ = Generate(context.Background(), m, files{}, Snapshot{Files: []File{{Path: ".env", Content: "private-value"}, {Path: "main.go", Content: "ghp_abcdefghijklmnop"}}}, "sk-proj-abcdefghijklmnop", false)
	if len(m.seen) != 1 {
		t.Fatal("no request")
	}
	s := m.seen[0].Content
	if strings.Contains(s, "private-value") || strings.Contains(s, "ghp_") || strings.Contains(s, "sk-proj-") {
		t.Fatalf("private data sent: %s", s)
	}
}
func TestRepairStopsAfterItsStepBudget(t *testing.T) {
	m := &replies{}
	for i := 0; i < MaxSteps; i++ {
		m.values = append(m.values, `{"read":["a"]}`)
	}
	_, e := Generate(context.Background(), m, files{"a": {Path: "a"}}, Snapshot{}, "", false)
	// Nothing was refused, so the allowance is not what ran the repair out of rounds.
	if e == nil || !strings.Contains(e.Error(), "step limit") || strings.Contains(e.Error(), "allowance") {
		t.Fatalf("%v", e)
	}
}

type budgetModel func([]Message) (string, error)

func (m budgetModel) Answer(_ context.Context, messages []Message) (string, error) {
	return m(messages)
}

func TestRepairCanFinishAfterMoreThanSixSourceRounds(t *testing.T) {
	reader := files{}
	// The old six-call cap ended after supplying source, before the model
	// could use it. Each round here asks for new evidence rather than looping.
	for i := range 7 {
		name := fmt.Sprintf("source%d.json", i)
		reader[name] = File{Path: name, Content: "old", Mode: "100644"}
	}
	calls := 0
	model := budgetModel(func(messages []Message) (string, error) {
		defer func() { calls++ }()
		if calls < 7 {
			return fmt.Sprintf(`{"read":["source%d.json"]}`, calls), nil
		}
		if !strings.Contains(messages[len(messages)-1].Content, `source6.json`) {
			t.Fatal("last requested source was not supplied")
		}
		return `{"summary":"Fix the reported cause","edits":[{"path":"source6.json","before":"old","after":"fixed"}]}`, nil
	})
	plan, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", false)
	if err != nil || calls != 8 || len(plan.Files) != 1 || plan.Files[0].Content != "fixed" {
		t.Fatalf("calls %d, plan %+v: %v", calls, plan, err)
	}
}

func TestRepairCountsDownAndReservesItsFinalReplyForADecision(t *testing.T) {
	calls := 0
	reader := &countingReader{}
	model := budgetModel(func(messages []Message) (string, error) {
		calls++
		last := messages[len(messages)-1].Content
		if calls == MaxSteps {
			if !strings.Contains(last, "this is your final reply") || !strings.Contains(last, "cannot_fix") {
				t.Fatalf("final turn has no decision instruction: %s", last)
			}
			return `{"read":["last.go"]}`, nil
		}
		if !strings.Contains(last, fmt.Sprintf("%d replies remain", MaxSteps-calls+1)) {
			t.Fatalf("turn %d has no remaining budget: %s", calls, last)
		}
		// A corrected malformed response must spend a turn too.
		if calls == 1 {
			return "not JSON", nil
		}
		return `{"read":["a"]}`, nil
	})
	_, err := Generate(t.Context(), model, reader, Snapshot{}, "fix the failure", false)
	if err == nil || !strings.Contains(err.Error(), "model-step limit") || calls != MaxSteps {
		t.Fatalf("calls %d: %v", calls, err)
	}
	if reader.reads != 1 {
		t.Fatal("read source that no remaining turn could use")
	}
}

func TestRepairAcceptsAFixOrAnExplanationOnItsFinalTurn(t *testing.T) {
	for _, cannotFix := range []bool{false, true} {
		t.Run(fmt.Sprintf("cannot_fix=%t", cannotFix), func(t *testing.T) {
			model := &replies{}
			for range MaxSteps - 1 {
				model.values = append(model.values, `{"read":["a.go"]}`)
			}
			last := `{"summary":"Fix the failure","edits":[{"path":"a.go","before":"old","after":"fixed"}]}`
			if cannotFix {
				last = `{"cannot_fix":"A dependency upgrade needs evidence not supplied"}`
			}
			model.values = append(model.values, last)
			plan, err := Generate(t.Context(), model, files{"a.go": {Path: "a.go", Content: "old"}}, Snapshot{}, "fix the failure", false)
			if cannotFix {
				if !errors.Is(err, ErrCannotFix) || !strings.Contains(err.Error(), "evidence not supplied") {
					t.Fatalf("explanation lost: %v", err)
				}
			} else if err != nil || len(plan.Files) != 1 || plan.Files[0].Content != "fixed" {
				t.Fatalf("final fix refused: %+v, %v", plan, err)
			}
		})
	}
}

func TestRepairRedactionRemovesTheWholePrivateKeyRatherThanJustItsHeader(t *testing.T) {
	key := "-----BEGIN RSA PRIVATE KEY-----\nPRIVATE_CONTENT\n-----END RSA PRIVATE KEY-----"
	got := Redact("before\n" + key + "\nafter")
	if strings.Contains(got, "PRIVATE_CONTENT") || strings.Contains(got, "PRIVATE KEY") || !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("incomplete redaction: %q", got)
	}
	if strings.Contains(Redact("-----BEGIN PRIVATE KEY-----\nPRIVATE_CONTENT"), "PRIVATE_CONTENT") {
		t.Fatal("unterminated key leaked")
	}
}

// The pattern for an API key has to find keys and not words. It had no word
// boundary, so "task-queue-worker-pool" held what looked like one ("sk-queue-
// worker-pool" is twelve characters of letters and hyphens): the model was shown
// "ta[redacted credential]", and any fix that touched the file was refused as one
// containing credentials.
func TestOrdinaryHyphenatedWordsAreNotCredentials(t *testing.T) {
	for _, word := range []string{"task-queue-worker-pool", "disk-usage-threshold", "-webkit-mask-box-image-source", "risk-assessment-framework"} {
		if got := Redact("use " + word + " here"); got != "use "+word+" here" {
			t.Errorf("Redact(%q) = %q; want it left alone", word, got)
		}
		if credential.MatchString(word) {
			t.Errorf("a file containing %q is refused as one containing credentials", word)
		}
	}
	for _, key := range []string{"sk-proj-abcdefghijklmnopqrstuv", "sk-abcdefghijklmnopqrstuv", "key=sk-abcdefghijklmnopqrstuv"} {
		if got := Redact(key); strings.Contains(got, "abcdefghijkl") {
			t.Errorf("Redact(%q) = %q; the key was not removed", key, got)
		}
	}
}

// The text is redacted as one JSON document, and a private key with no end
// matched to the end of it: the fields after the key, which hold the failures the
// model is there to read, went with it, and what was left was not a document.
func TestAnUnterminatedPrivateKeyDoesNotSwallowWhatFollows(t *testing.T) {
	body, err := json.Marshal(Snapshot{
		Title: "t", Body: "see -----BEGIN RSA PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQE",
		Failures: []string{"test: boom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := Redact(string(body))
	if strings.Contains(got, "MIIEvQIBADAN") {
		t.Errorf("the key's own text was left in:\n%s", got)
	}
	var back Snapshot
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("what was left is not a snapshot any more (%v):\n%s", err, got)
	}
	if len(back.Failures) != 1 || back.Failures[0] != "test: boom" {
		t.Errorf("the failures after the key were lost: %+v", back.Failures)
	}
}

// What a model is sent has to lose the shapes credentials take in a failed job's
// log, not only the four the repair's own pattern knows. GitHub masks only the
// secrets it holds, so a registry password in a URL, a bearer token a test
// printed or a setting a script echoed arrives as it was written.
func TestEverythingBoundForTheModelLosesTheCommonSecretShapes(t *testing.T) {
	for name, tc := range map[string]struct{ text, secret string }{
		"a password in an address": {"pulling https://ci:hunter2@registry.example.com/img", "hunter2"},
		"a bearer token":           {"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9abcdefghijkl", "eyJhbGciOiJIUzI1NiJ9abcdefghijkl"},
		"a Slack token":            {"posting with xoxb-1234567890-abcdefghij", "xoxb-1234567890-abcdefghij"},
		"a Google API key":         {"key " + "AIza" + strings.Repeat("a", 35), strings.Repeat("a", 35)},
		"an npm token":             {"//registry:_authToken=npm_" + strings.Repeat("a", 36), strings.Repeat("a", 36)},
		"a setting that says so":   {"DB_PASSWORD=supersecretvalue", "supersecretvalue"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Scrub(tc.text); strings.Contains(got, tc.secret) {
				t.Fatalf("Scrub(%q) = %q; the secret is still in it", tc.text, got)
			}
		})
	}
	if got := Scrub("run task-queue-worker-pool"); got != "run task-queue-worker-pool" {
		t.Errorf("Scrub took a word for a secret: %q", got)
	}
}

// The text ends up in a PR comment, so it names the file and says what to do.
func TestAFileEliCannotReadIsNamedAndComesWithAdvice(t *testing.T) {
	for _, e := range []*ReadError{{Path: "big.sql"}, {Path: "x.go", Err: fmt.Errorf("not found")}} {
		got := e.Error()
		if !strings.Contains(got, "`"+e.Path+"`") || !strings.Contains(got, "fix that file by hand") {
			t.Errorf("unhelpful message %q", got)
		}
	}
}

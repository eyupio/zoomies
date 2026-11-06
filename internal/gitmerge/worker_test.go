package gitmerge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// post runs one request through the whole protocol and decodes the answer.
//
// The answer is decoded even when the call failed, because the worker promises
// one JSON object either way: a controller that had to parse a message out of
// stderr would be guessing at a contract this one states.
func post(t *testing.T, req WorkerRequest) (WorkerResponse, error) {
	t.Helper()
	var in bytes.Buffer
	if err := json.NewEncoder(&in).Encode(req); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Serve(context.Background(), &in, &out)
	var resp WorkerResponse
	if decodeErr := json.Unmarshal(out.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("the answer is not one JSON object: %v\n%s", decodeErr, out.String())
	}
	if err == nil && resp.Code != "" {
		t.Fatalf("the worker answered %q and returned no error", resp.Code)
	}
	return resp, err
}

func TestServeAnswersACleanMergeAsATree(t *testing.T) {
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("base\nmain\n"))
	base := commit(t, dir, "main")

	resp, err := post(t, WorkerRequest{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatalf("a clean merge failed: %v", err)
	}
	if resp.Result == nil || resp.Result.Tree == "" || len(resp.Result.Conflicts) != 0 {
		t.Fatalf("answer %+v", resp)
	}
}

// A conflict is a successful answer: it is the evidence the caller resolves.
// Refusing it would leave the caller with nothing to repair.
func TestServeAnswersAConflictWithItsThreeSides(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.txt", []byte("a\nb\nc\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.txt", []byte("a\nB\nc\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "file.txt", []byte("a\nb\nC\n"))
	base := commit(t, dir, "main")

	resp, err := post(t, WorkerRequest{Dir: dir, Ancestor: ancestor, Base: base, Head: side})
	if err != nil {
		t.Fatalf("a conflicted merge failed: %v", err)
	}
	if resp.Result == nil || len(resp.Result.Conflicts) != 1 {
		t.Fatalf("answer %+v", resp)
	}
	got := resp.Result.Conflicts[0]
	if got.Path != "file.txt" || got.Ancestor != "a\nb\nc\n" || got.Head != "a\nB\nc\n" || got.Base != "a\nb\nC\n" {
		t.Fatalf("conflict %+v", got)
	}
}

// A field the worker does not know is refused rather than ignored, so a request
// from a newer build is never half-answered and nothing can smuggle an option
// past the three limits.
func TestServeRefusesAFieldItDoesNotKnow(t *testing.T) {
	body := `{"dir":"/work","ancestor":"` + strings.Repeat("a", 40) + `","base":"` +
		strings.Repeat("b", 40) + `","head":"` + strings.Repeat("c", 40) + `","hooks_path":"/tmp"}`
	var out bytes.Buffer
	err := Serve(context.Background(), strings.NewReader(body), &out)
	var resp WorkerResponse
	if decodeErr := json.Unmarshal(out.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("the answer is not one JSON object: %v\n%s", decodeErr, out.String())
	}
	if resp.Code != CodeInvalidRequest || err == nil {
		t.Fatalf("answer %+v, error %v", resp, err)
	}
	if !strings.Contains(resp.Error, "hooks_path") {
		t.Errorf("the answer does not name the field it refused: %q", resp.Error)
	}
}

// The directory is an absolute path or the request is refused before Git runs:
// a relative one would resolve against the worker's own working directory,
// which is the one place it keeps nothing.
func TestServeRefusesARelativeDirectory(t *testing.T) {
	resp, err := post(t, WorkerRequest{Dir: "../work", Ancestor: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), Head: strings.Repeat("c", 40)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("a relative directory answered %v", err)
	}
	if resp.Code != CodeInvalidRequest {
		t.Fatalf("answer %+v", resp)
	}
}

// The two commits are the caller's claim about the incident; a repository that
// computes a different merge base is a different incident, so the answer says
// which one was wrong instead of merging the wrong pair.
func TestServeNamesAMergeBaseMismatch(t *testing.T) {
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("main\n"))
	base := commit(t, dir, "main")

	resp, err := post(t, WorkerRequest{Dir: dir, Ancestor: side, Base: base, Head: ancestor})
	if !errors.Is(err, ErrMergeBaseMismatch) {
		t.Fatalf("a wrong merge base answered %v", err)
	}
	if resp.Code != CodeMergeBaseMismatch {
		t.Fatalf("answer %+v", resp)
	}
}

// A request bigger than the budget is refused without being parsed, so the only
// work a hostile or confused caller can make the worker do is reading the bytes
// it was told to stop at.
func TestServeRefusesARequestOverItsBudget(t *testing.T) {
	body := `{"dir":"` + strings.Repeat("x", 4096) + `"}`
	var out bytes.Buffer
	err := serve(context.Background(), strings.NewReader(body), &out, 512, MaxResponseBytes)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an oversized request answered %v", err)
	}
	var resp WorkerResponse
	if decodeErr := json.Unmarshal(out.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("the answer is not one JSON object: %v\n%s", decodeErr, out.String())
	}
	if resp.Code != CodeTooLarge {
		t.Fatalf("answer %+v", resp)
	}
}

// An answer that does not fit is refused whole. A caller must never receive half
// an object and read the truncation as a conflict-free tree.
func TestServeRefusesAnAnswerOverItsBudget(t *testing.T) {
	dir := repo(t)
	write(t, dir, "file.txt", []byte("a\nb\nc\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "file.txt", []byte("a\nB\nc\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "file.txt", []byte("a\nb\nC\n"))
	base := commit(t, dir, "main")

	var in bytes.Buffer
	if err := json.NewEncoder(&in).Encode(WorkerRequest{Dir: dir, Ancestor: ancestor, Base: base, Head: side}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := serve(context.Background(), &in, &out, MaxRequestBytes, 64)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an oversized answer returned %v", err)
	}
	var resp WorkerResponse
	if decodeErr := json.Unmarshal(out.Bytes(), &resp); decodeErr != nil {
		t.Fatalf("the answer is not one JSON object: %v\n%s", decodeErr, out.String())
	}
	if resp.Code != CodeTooLarge || resp.Result != nil {
		t.Fatalf("answer %+v", resp)
	}
}

// Two requests on one input is one too many: the worker's contract is one merge
// per process, and answering the first would leave the second to exit as a
// success nobody read.
func TestServeRefusesASecondRequest(t *testing.T) {
	dir := repo(t)
	write(t, dir, "base.txt", []byte("base\n"))
	ancestor := commit(t, dir, "base")
	mustGit(t, dir, "checkout", "-qb", "side")
	write(t, dir, "side.txt", []byte("side\n"))
	side := commit(t, dir, "side")
	mustGit(t, dir, "checkout", "-q", "-")
	write(t, dir, "base.txt", []byte("main\n"))
	base := commit(t, dir, "main")

	one := WorkerRequest{Dir: dir, Ancestor: ancestor, Base: base, Head: side}
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	if err := enc.Encode(one); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(one); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), &in, &out); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("two requests answered %v", err)
	}
}

// The floor is a fact about Git, not a preference: below it there is no
// merge-tree --write-tree and nothing this package does can work. A worker image
// built on such a Git must fail its build, which is what Check is for.
func TestSupportsMergeTreeReadsTheVersionFloor(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"2.38.0", true},
		{"2.39.5", true},
		{"2.39.5.windows.1", true},
		{"2.50.1", true},
		{"3.0.0", true},
		{"2.37.9", false},
		{"2.30.2", false},
		{"1.8.3.1", false},
		{"", false},
		{"git version unknown", false},
	} {
		if got := supportsMergeTree(tc.version); got != tc.want {
			t.Errorf("supportsMergeTree(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

// This host's Git is the one the tests above ran on, so the check that the image
// runs at build time is the same check here.
func TestCheckAcceptsTheGitThisHostHas(t *testing.T) {
	toolchain, err := Check(context.Background())
	if err != nil {
		t.Fatalf("the Git this host has cannot merge: %v", err)
	}
	if strings.TrimSpace(toolchain.Git) == "" {
		t.Fatal("the check named no Git version")
	}
}

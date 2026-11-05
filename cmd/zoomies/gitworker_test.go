package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitWorkerRepo builds two branches that both edit one line, so a merge of them
// is a text conflict: the incident the worker exists to answer.
func gitWorkerRepo(t *testing.T) (dir, ancestor, base, head string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.test")
	git("config", "user.name", "Test")
	writeFile("file.txt", "a\nb\nc\n")
	git("add", "-A")
	git("commit", "-qm", "base")
	ancestor = git("rev-parse", "HEAD")
	git("checkout", "-qb", "side")
	writeFile("file.txt", "a\nB\nc\n")
	git("commit", "-qam", "side")
	head = git("rev-parse", "HEAD")
	git("checkout", "-q", "main")
	writeFile("file.txt", "a\nb\nC\n")
	git("commit", "-qam", "main")
	base = git("rev-parse", "HEAD")
	return dir, ancestor, base, head
}

// The worker answers one request with one JSON object, and a conflicted merge is
// an answer rather than an error: it is the evidence the repair is made from.
func TestGitWorkerAnswersOneConflictAsJSON(t *testing.T) {
	dir, ancestor, base, head := gitWorkerRepo(t)
	request, err := json.Marshal(map[string]string{"dir": dir, "ancestor": ancestor, "base": base, "head": head})
	if err != nil {
		t.Fatal(err)
	}

	e, out, errOut := newTestEnv(t)
	e.in = bytes.NewReader(request)
	if code := dispatch(context.Background(), e, []string{"git-worker"}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	var answer struct {
		Result struct {
			Tree      string `json:"tree"`
			Conflicts []struct {
				Path     string `json:"path"`
				Ancestor string `json:"ancestor"`
				Base     string `json:"base"`
				Head     string `json:"head"`
			} `json:"conflicts"`
		} `json:"result"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("the worker did not answer one JSON object: %v\n%s", err, out.String())
	}
	if answer.Code != "" || answer.Result.Tree == "" || len(answer.Result.Conflicts) != 1 {
		t.Fatalf("answer %+v", answer)
	}
	conflict := answer.Result.Conflicts[0]
	if conflict.Path != "file.txt" || conflict.Head != "a\nB\nc\n" || conflict.Base != "a\nb\nC\n" {
		t.Fatalf("conflict %+v", conflict)
	}
	// Nothing but the answer goes to stdout: a caller decoding the stream must
	// not find a progress line beside it.
	if strings.Count(strings.TrimSpace(out.String()), "\n") != 0 {
		t.Errorf("stdout carries more than one line:\n%s", out.String())
	}
}

// A request the worker refuses still answers in JSON and exits non-zero, so the
// caller reads one object either way and the container log says what was wrong.
func TestGitWorkerRefusesABadRequestInJSONAndExitsNonZero(t *testing.T) {
	e, out, errOut := newTestEnv(t)
	e.in = strings.NewReader(`{"dir":"relative","ancestor":"` + strings.Repeat("a", 40) +
		`","base":"` + strings.Repeat("b", 40) + `","head":"` + strings.Repeat("c", 40) + `"}`)
	if code := dispatch(context.Background(), e, []string{"git-worker"}); code != exitError {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	var answer struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
		t.Fatalf("the refusal is not JSON: %v\n%s", err, out.String())
	}
	if answer.Code != "invalid_request" || answer.Error == "" {
		t.Fatalf("answer %+v", answer)
	}
	if !strings.Contains(errOut.String(), "zoomies git-worker: ") {
		t.Errorf("the container log does not name the command:\n%s", errOut)
	}
	if strings.Contains(errOut.String(), "gitmerge: gitmerge:") {
		t.Errorf("the message repeats its package name:\n%s", errOut)
	}
}

// The image's last build step asks Git whether it can merge at all, so a base
// image that regressed below merge-tree --write-tree fails the build rather than
// the first conflict an operator asked for.
func TestGitWorkerCheckNamesTheGitItFound(t *testing.T) {
	out, _ := runCLI(t, "git-worker", "--check")
	if !strings.Contains(out, "git ") || !strings.Contains(out, "can run the bounded merge") {
		t.Fatalf("--check said %q", out)
	}
}

// The agent runs the worker as a one-shot container with the workspace mounted,
// so the request and the answer are files beside the repository. Reading them
// from standard input would mean plumbing an attach into a container runtime for
// a process that runs once.
func TestGitWorkerReadsAndWritesFilesWhenToldTo(t *testing.T) {
	dir, ancestor, base, head := gitWorkerRepo(t)
	workspace := t.TempDir()
	requestPath := filepath.Join(workspace, "request.json")
	answerPath := filepath.Join(workspace, "answer.json")
	request, err := json.Marshal(map[string]string{"dir": dir, "ancestor": ancestor, "base": base, "head": head})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, request, 0o644); err != nil {
		t.Fatal(err)
	}

	e, out, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"git-worker", "--request", requestPath, "--answer", answerPath}); code != exitOK {
		t.Fatalf("exit code = %d\n%s", code, errOut)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("the answer also went to stdout:\n%s", out.String())
	}

	body, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatalf("reading the answer file: %v", err)
	}
	var answer struct {
		Result struct {
			Conflicts []struct {
				Path string `json:"path"`
			} `json:"conflicts"`
		} `json:"result"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the answer file is not one JSON object: %v\n%s", err, body)
	}
	if answer.Code != "" || len(answer.Result.Conflicts) != 1 || answer.Result.Conflicts[0].Path != "file.txt" {
		t.Fatalf("answer %+v", answer)
	}
}

// A path that cannot be read is a usage error naming the path, not a hang and not
// an answer with an empty request in it.
func TestGitWorkerSaysWhichFileItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere.json")
	e, _, errOut := newTestEnv(t)
	if code := dispatch(context.Background(), e, []string{"git-worker", "--request", missing}); code != exitUsage {
		t.Fatalf("exit code = %d, want %d\n%s", code, exitUsage, errOut)
	}
	if !strings.Contains(errOut.String(), missing) {
		t.Errorf("the error does not name the file:\n%s", errOut)
	}
}

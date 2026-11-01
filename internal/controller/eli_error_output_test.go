package controller

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

// errorOutputSetup is a person who may read a job's error output: linked,
// confirmed and able to write to the repository. A test then takes one of those
// away, and counts how often GitHub was asked and for which job.
func errorOutputSetup(t *testing.T) (*commandSetup, *[]int64) {
	t.Helper()
	s := newCommandSetup(t)
	s.link(t, true)
	var reads []int64
	s.fake.onFailures = func(jobID int64) ([]string, error) {
		reads = append(reads, jobID)
		return []string{"npm ERR! test failed", "Error: expected 3 got 4"}, nil
	}
	return s, &reads
}

// Error output is the log of a job, and a log is where credentials end up. It
// is read for the people who could fix the pull request and nobody else, so a
// caller who is not linked, has not confirmed the link, or cannot write to the
// repository gets nothing, and a copy another person's read left in memory is
// not a way round that.
func TestErrorOutputRefusesAnUnlinkedOrReadOnlyCaller(t *testing.T) {
	s, reads := errorOutputSetup(t)
	other := &store.User{Username: "stranger", Role: store.RoleViewer}
	if err := s.h.st.CreateUser(s.h.ctx, other); err != nil {
		t.Fatal(err)
	}
	unconfirmed := &store.User{Username: "unconfirmed", Role: store.RoleViewer}
	if err := s.h.st.CreateUser(s.h.ctx, unconfirmed); err != nil {
		t.Fatal(err)
	}
	if err := s.h.st.SetEliIdentity(s.h.ctx, store.EliIdentity{UserID: unconfirmed.ID, GitHubUserID: 43, GitHubLogin: "unconfirmed"}); err != nil {
		t.Fatal(err)
	}

	// Warm the cache with a read that is allowed.
	if _, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7); err != nil {
		t.Fatal(err)
	}
	if len(*reads) != 1 {
		t.Fatalf("%d reads of GitHub, want 1", len(*reads))
	}

	for name, tc := range map[string]struct {
		user    string
		noWrite bool
	}{
		"nobody linked":        {user: other.ID},
		"a link not confirmed": {user: unconfirmed.ID},
		"no such user":         {user: "usr_nobody"},
		"read-only on GitHub":  {user: s.user.ID, noWrite: true},
	} {
		s.fake.noWrite = tc.noWrite
		got, err := s.h.c.EliErrorOutput(s.h.ctx, tc.user, s.repo, 7)
		if !errors.Is(err, ErrEliErrorOutputDenied) {
			t.Errorf("%s: error %v, want ErrEliErrorOutputDenied", name, err)
		}
		if len(got.Lines) != 0 {
			t.Errorf("%s: handed %d lines to someone who may not read them", name, len(got.Lines))
		}
	}
	s.fake.noWrite = false
	if len(*reads) != 1 {
		t.Errorf("%d reads of GitHub, want only the allowed one: a refused caller must not cost a read", len(*reads))
	}
}

// A log can hold anything the job printed, and the page that shows it is not
// where a leaked token should be found.
func TestErrorOutputRedactsWhatItReturns(t *testing.T) {
	s, _ := errorOutputSetup(t)
	token := "ghp_" + strings.Repeat("a1B2", 9)
	s.fake.onFailures = func(int64) ([]string, error) {
		return []string{"npm ERR! publish failed with token " + token, "Error: plain line"}, nil
	}
	got, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 2 || strings.Contains(strings.Join(got.Lines, "\n"), token) {
		t.Fatalf("lines = %q, want the token gone", got.Lines)
	}
	if !strings.Contains(got.Lines[0], "[redacted credential]") || got.Lines[1] != "Error: plain line" {
		t.Errorf("lines = %q, want the token replaced and the other line untouched", got.Lines)
	}
}

// Opening a failure twice, or two people opening the same one, should not cost
// GitHub a log download each: the second read inside a minute is the first.
func TestErrorOutputReadsGitHubOncePerMinute(t *testing.T) {
	s, reads := errorOutputSetup(t)
	read := func(job int64) EliErrorOutput {
		t.Helper()
		got, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, job)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := read(7)
	s.h.advance(30 * time.Second)
	second := read(7)
	if len(*reads) != 1 || strings.Join(first.Lines, "|") != strings.Join(second.Lines, "|") {
		t.Fatalf("two reads inside a minute made %d requests of GitHub", len(*reads))
	}
	read(8)
	if len(*reads) != 2 || (*reads)[1] != 8 {
		t.Fatalf("another job was served from the first job's copy: reads %v", *reads)
	}
	s.h.advance(31 * time.Second)
	read(7)
	if len(*reads) != 3 || (*reads)[2] != 7 {
		t.Fatalf("a read after the minute was served from memory: reads %v", *reads)
	}
}

// Nothing here keeps what it reads: a copy lives a minute in memory, never more
// than a few hundred of them, and expired ones go as new ones arrive, so a long
// run of different failures cannot grow it.
func TestErrorOutputKeepsAFewCopiesForAMinuteAtMost(t *testing.T) {
	s, reads := errorOutputSetup(t)
	for job := int64(1); job <= 300; job++ {
		if _, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, job); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.h.c.errorOutput.size(); n > 256 {
		t.Fatalf("%d copies held, want at most 256", n)
	}
	s.h.advance(2 * time.Minute)
	if _, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 1000); err != nil {
		t.Fatal(err)
	}
	if n := s.h.c.errorOutput.size(); n != 1 {
		t.Fatalf("%d copies held after a minute with no reads, want only the new one", n)
	}
	if len(*reads) != 301 {
		t.Errorf("%d reads of GitHub, want one for each of 301 different jobs", len(*reads))
	}
}

// An expired log, or a job that printed no error lines, is an answer. It comes
// back as an empty list and a sentence, never as null, so the page can say it.
func TestErrorOutputSaysSoWhenTheLogIsGone(t *testing.T) {
	s, _ := errorOutputSetup(t)
	s.fake.onFailures = func(int64) ([]string, error) { return nil, nil }
	got, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines == nil || len(got.Lines) != 0 {
		t.Errorf("lines = %#v, want an empty list that is not null", got.Lines)
	}
	if got.Note != "GitHub no longer has this log, or the job printed no error lines." {
		t.Errorf("note = %q", got.Note)
	}
}

// A failure to read is not an answer and is not kept: the next ask tries again.
func TestErrorOutputDoesNotRememberAFailedRead(t *testing.T) {
	s, reads := errorOutputSetup(t)
	broken := true
	s.fake.onFailures = func(job int64) ([]string, error) {
		*reads = append(*reads, job)
		if broken {
			return nil, fmt.Errorf("github is unavailable")
		}
		return []string{"Error: x"}, nil
	}
	if _, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7); err == nil || errors.Is(err, ErrEliErrorOutputDenied) {
		t.Fatalf("error %v, want the read's own failure", err)
	}
	broken = false
	got, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7)
	if err != nil || len(got.Lines) != 1 || len(*reads) != 2 {
		t.Fatalf("lines %v, error %v, reads %v: the failure was kept", got.Lines, err, *reads)
	}
}

// What a job printed is the workflow's, and may be a secret that redaction does
// not know. It is shown to the person who asked and kept nowhere: not in a
// store row, and not in a log line the controller writes.
func TestErrorOutputLogsNoLinesAndWritesNothing(t *testing.T) {
	s, _ := errorOutputSetup(t)
	var logged bytes.Buffer
	s.h.c.log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s.fake.onFailures = func(int64) ([]string, error) {
		return []string{"password=hunter2 rejected by registry"}, nil
	}
	if _, err := s.h.c.EliErrorOutput(s.h.ctx, s.user.ID, s.repo, 7); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "hunter2") {
		t.Errorf("a line of the log reached the controller's log: %s", logged.String())
	}
	if rows := s.rows(t); len(rows) != 0 {
		t.Errorf("reading error output wrote %d repairs", len(rows))
	}
}

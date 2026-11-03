package terminalui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskStringWritesAndCopiesStaySafeWhileProgressReadsLogs(t *testing.T) {
	logs := &taskOutput{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_, _ = io.WriteString(logs, "checking\n")
			_, _ = io.Copy(logs, io.LimitReader(strings.NewReader("copied\n"), 7))
			runtime.Gosched()
		}
	}()
	for {
		_ = logs.latest()
		select {
		case <-done:
			if got := logs.buffer.String(); got != strings.Repeat("checking\ncopied\n", 1000) {
				t.Fatal("progress lost log output")
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func TestRedirectedTasksKeepTheirOutputAndErrorWithoutTerminalControls(t *testing.T) {
	var out bytes.Buffer
	want := errors.New("release refused")
	calls := 0
	err := Task(context.Background(), &out, "download", false, func(_ context.Context, w io.Writer) error {
		calls++
		_, _ = io.WriteString(w, "checking\nrefused\n")
		return want
	})
	if calls != 1 || !errors.Is(err, want) || out.String() != "checking\nrefused\n" {
		t.Fatalf("calls=%d error=%v output=%q", calls, err, out.String())
	}
}

func TestRendererFailureCancelsAndJoinsWorkWithoutRetrying(t *testing.T) {
	var out bytes.Buffer
	var calls atomic.Int32
	cleaned := false
	uiErr := errors.New("terminal closed")
	err := runTask(context.Background(), &out, "backup", func(ctx context.Context, w io.Writer) error {
		calls.Add(1)
		<-ctx.Done()
		_, _ = io.WriteString(w, "cleanup finished\n")
		cleaned = true
		return ctx.Err()
	}, func(taskModel) error { return uiErr })
	if calls.Load() != 1 || !cleaned || out.String() != "cleanup finished\n" || !errors.Is(err, uiErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls=%d cleanup=%v error=%v output=%q", calls.Load(), cleaned, err, out.String())
	}
}

func TestTaskCompletionRestoresEveryOutputByteBeforeReturningItsError(t *testing.T) {
	var out bytes.Buffer
	want := errors.New("checksum mismatch")
	err := runTask(context.Background(), &out, "verify", func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "checksum details\n")
		return want
	}, func(m taskModel) error {
		result := <-m.done
		next, cmd := m.Update(result)
		if cmd == nil || next.(taskModel).View().Content != "" {
			t.Fatal("progress was not cleared on completion")
		}
		return nil
	})
	if !errors.Is(err, want) || out.String() != "checksum details\n" {
		t.Fatalf("error=%v output=%q", err, out.String())
	}
}

func TestTaskAnimationShowsElapsedWorkAndFitsANarrowTerminal(t *testing.T) {
	start := time.Unix(100, 0)
	logs := &taskOutput{}
	_, _ = io.WriteString(logs, "first\nlatest observation\n")
	m := taskModel{label: "Checking the installed deployment", start: start, now: start, logs: logs, width: 28}
	next, cmd := m.Update(TickMsg(start.Add(3 * time.Second)))
	view := next.(taskModel).View().Content
	if cmd == nil || !strings.Contains(view, "latest observation") {
		t.Fatal(view)
	}
	for _, row := range strings.Split(view, "\n") {
		if Width(row) > 28 {
			t.Fatalf("wide row %q", row)
		}
	}
	m.width, m.now = 100, start.Add(3*time.Second)
	if !strings.Contains(m.View().Content, "3s") {
		t.Fatal("elapsed time missing")
	}
}

func TestClippingPreservesColourAndWideCharacterBoundaries(t *testing.T) {
	text := Paint(true, "36", "日本語 é runner")
	for width := 1; width < 20; width++ {
		if got := Fit(text, width); Width(got) > width {
			t.Fatalf("width %d: %q (%d cells)", width, got, Width(got))
		}
	}
}

func TestNoColourAndReducedMotionAreIndependent(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("ZOOMIES_NO_ANIMATION", "1")
	if Colour() || Motion() {
		t.Fatal("terminal preferences ignored")
	}
	if got := Brand(false); got != "zoomies" {
		t.Fatalf("plain brand = %q", got)
	}
}

func TestWorkingViewKeepsPawsTaskAndElapsedTimeOnOneRow(t *testing.T) {
	start := time.Unix(100, 0)
	logs := &taskOutput{}
	m := taskModel{label: "Checking the deployment", start: start, now: start.Add(3 * time.Second), logs: logs, width: 80}
	view := m.View().Content
	if strings.Count(view, "\n") != 0 || !strings.Contains(view, "🐾") || !strings.Contains(view, "Checking the deployment") || !strings.Contains(view, "3s") {
		t.Fatal(view)
	}
	if strings.Contains(strings.ToLower(view), "off the lead") {
		t.Fatal("each task repeats the tagline")
	}
	_, _ = logs.Write([]byte("Latest observation\n"))
	if strings.Count(m.View().Content, "\n") != 1 {
		t.Fatal("observations need only one extra row")
	}
}

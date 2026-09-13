package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// The level an operator typed decides what is logged, and a word nobody
// recognises must not silence the process: an unreadable log.level should
// leave the default in place rather than turn logging off.
func TestTheLevelIsReadFromWhatTheOperatorWrote(t *testing.T) {
	cases := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"chatty", slog.LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			l := setupForTest(t, Options{Level: tc.level})
			if !l.Enabled(context.Background(), tc.want) {
				t.Errorf("level %q does not log at %s", tc.level, tc.want)
			}
			if tc.want > slog.LevelDebug && l.Enabled(context.Background(), tc.want-4) {
				t.Errorf("level %q logs below %s as well", tc.level, tc.want)
			}
		})
	}
}

// The format is JSON unless the operator asked for text: a log shipped to a
// collector is parsed, and the one a person reads over SSH is not.
func TestTheFormatIsJSONUnlessTextWasAskedFor(t *testing.T) {
	for _, format := range []string{"", "json", "anything else"} {
		l := setupForTest(t, Options{Format: format})
		if _, ok := l.Handler().(*slog.JSONHandler); !ok {
			t.Errorf("format %q gave a %T, want JSON", format, l.Handler())
		}
	}
	for _, format := range []string{"text", "TEXT"} {
		l := setupForTest(t, Options{Format: format})
		if _, ok := l.Handler().(*slog.TextHandler); !ok {
			t.Errorf("format %q gave a %T, want text", format, l.Handler())
		}
	}
}

// Setup installs what it built, because everything that logs through the
// package default -- every library, and every call site that never took a
// logger -- has to be redacting too.
func TestSetupInstallsTheLoggerAsTheDefault(t *testing.T) {
	l := setupForTest(t, Options{Level: "debug"})
	if slog.Default() != l {
		t.Error("the logger Setup built was not installed as the default")
	}
}

// The redaction is central precisely so that no call site can forget it: a
// value under one of these keys never reaches the log, whatever case it was
// written in and whatever kind of value it holds.
func TestASensitiveValueNeverReachesTheLog(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redact}))

	secret := "s3cr3t-do-not-log"
	for key := range sensitiveKeys {
		buf.Reset()
		l.Info("something happened", strings.ToUpper(key), secret)
		if strings.Contains(buf.String(), secret) {
			t.Errorf("%q was logged in full under %q", secret, key)
		}
		var line map[string]any
		if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
			t.Fatalf("unmarshalling the log line: %v", err)
		}
		if got := line[strings.ToUpper(key)]; got != "[redacted]" {
			t.Errorf("%s = %v, want [redacted]", key, got)
		}
	}
}

// Everything else is logged as it was passed. Redacting by guesswork would
// cost an operator the field they were reading the log for.
func TestAnOrdinaryAttributeIsLeftAlone(t *testing.T) {
	for _, key := range []string{"host", "pool", "runner", "tokens_remaining", "secretive"} {
		a := redact(nil, slog.String(key, "visible"))
		if a.Value.String() != "visible" {
			t.Errorf("%s = %q, want it logged as it was passed", key, a.Value)
		}
	}
}

// A request-scoped logger travels on the context so that every line a handler
// writes carries the request ID, without the request ID being threaded
// through every function it calls.
func TestTheRequestLoggerTravelsOnTheContext(t *testing.T) {
	var buf bytes.Buffer
	scoped := slog.New(slog.NewJSONHandler(&buf, nil)).With("request_id", "abc123")

	ctx := WithLogger(context.Background(), scoped)
	FromContext(ctx).Info("handled")
	if !strings.Contains(buf.String(), "abc123") {
		t.Errorf("log = %q, want the request's own logger to have written it", buf.String())
	}
}

// A context that carries none -- background work, a startup path -- still
// logs, through the default rather than through nothing.
func TestAContextWithNoLoggerFallsBackToTheDefault(t *testing.T) {
	l := setupForTest(t, Options{})
	if got := FromContext(context.Background()); got != l {
		t.Errorf("FromContext gave %p, want the default logger %p", got, l)
	}
	//nolint:staticcheck // a nil logger under the key is the same absence as no key at all.
	if got := FromContext(context.WithValue(context.Background(), ctxKey{}, (*slog.Logger)(nil))); got != l {
		t.Error("a nil logger on the context was returned instead of the default")
	}
}

// setupForTest runs Setup and puts the process-wide default back afterwards,
// so one test's level cannot decide another's.
func setupForTest(t *testing.T, o Options) *slog.Logger {
	t.Helper()
	before := slog.Default()
	t.Cleanup(func() { slog.SetDefault(before) })
	return Setup(o)
}

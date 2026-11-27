package silent

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	listOut  []byte
	listErr  error
	runCode  int
	runErr   error
	listArgs []string
	runArgs  [][]string
}

func (f *fakeRunner) Output(_ context.Context, a ...string) ([]byte, error) {
	f.listArgs = a
	return f.listOut, f.listErr
}
func (f *fakeRunner) Run(_ context.Context, a ...string) (int, error) {
	f.runArgs = append(f.runArgs, a)
	return f.runCode, f.runErr
}

func utf16le(s string) []byte {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

func TestIsRunningReadsUTF16ListsWholeNamesIgnoringCase(t *testing.T) {
	out := utf16le("Ubuntu-24.04\r\nDebian\r\n")
	for name, want := range map[string]bool{"debian": true, "Ubuntu-24.04": true, "Ubuntu": false, "": false} {
		if got := IsRunning(out, name); got != want {
			t.Errorf("IsRunning(%q) = %v, want %v", name, got, want)
		}
	}
	if !IsRunning([]byte("D\x00e\x00b\x00i\x00a\x00n\x00\r\x00\n\x00"), "Debian") {
		t.Error("UTF-16 without a byte-order mark was not read")
	}
}

func TestEnsureDoesNothingWhenTheDistroIsAlreadyRunning(t *testing.T) {
	r := &fakeRunner{listOut: utf16le("Ubuntu\r\n")}
	if code := Ensure(context.Background(), r, "Ubuntu", &bytes.Buffer{}); code != 0 || len(r.runArgs) != 0 {
		t.Fatalf("code=%d started=%v", code, r.runArgs)
	}
	if !reflect.DeepEqual(r.listArgs, []string{"-l", "--running", "-q"}) {
		t.Fatalf("asked %v", r.listArgs)
	}
}

// The command is the one the PowerShell script ran, so a distro is started the
// same way as before, as the user the distro's default is, with no shell.
func TestEnsureStartsAStoppedDistroTheWayItAlwaysDid(t *testing.T) {
	r := &fakeRunner{listOut: utf16le("Debian\r\n")}
	if code := Ensure(context.Background(), r, "Ubuntu", &bytes.Buffer{}); code != 0 {
		t.Fatalf("code %d", code)
	}
	want := []string{"-d", "Ubuntu", "--exec", "/bin/sleep", "120"}
	if len(r.runArgs) != 1 || !reflect.DeepEqual(r.runArgs[0], want) {
		t.Fatalf("started %v", r.runArgs)
	}
}

func TestEnsureStartsAnywayWhenItCannotAskAndSaysSo(t *testing.T) {
	r := &fakeRunner{listErr: errors.New("boom")}
	var log bytes.Buffer
	if code := Ensure(context.Background(), r, "Ubuntu", &log); code != 0 || len(r.runArgs) != 1 {
		t.Fatalf("code=%d runs=%v", code, r.runArgs)
	}
	if !strings.Contains(log.String(), "boom") {
		t.Fatalf("log: %q", log.String())
	}
}

func TestEnsurePropagatesWSLsExitCodeAndLogsIt(t *testing.T) {
	r := &fakeRunner{runCode: 4294967295 & 0x7fffffff}
	var log bytes.Buffer
	if code := Ensure(context.Background(), r, "Ubuntu", &log); code != r.runCode {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(log.String(), "exited with") {
		t.Fatalf("log: %q", log.String())
	}
}

func TestEnsureReportsAProcessThatCouldNotStartAsFailure(t *testing.T) {
	r := &fakeRunner{runErr: errors.New("file not found")}
	var log bytes.Buffer
	if code := Ensure(context.Background(), r, "Ubuntu", &log); code != 1 || !strings.Contains(log.String(), "file not found") {
		t.Fatalf("code=%d log=%q", code, log.String())
	}
}

func TestEnsureWithoutADistroIsAUsageError(t *testing.T) {
	if code := Ensure(context.Background(), &fakeRunner{}, "", &bytes.Buffer{}); code != 2 {
		t.Fatalf("code %d", code)
	}
}

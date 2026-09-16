package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

// timeoutError is what net/http hands back when the daemon accepted the
// connection and never answered: the shape the real failure arrives in.
type timeoutError struct{}

func (timeoutError) Error() string   { return "net/http: timeout awaiting response headers" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

// A daemon that did not answer in time is not a daemon that is missing, and
// the fleet has to say which. An operator sent to "check whether the daemon is
// running" finds it running, finds the socket where the agent said it was, and
// learns to stop reading the fix line -- while the machine that is actually
// oversubscribed goes on failing every create.
func TestADaemonThatTimesOutIsNotADaemonThatIsMissing(t *testing.T) {
	c := &APIClient{host: "unix:///var/run/docker.sock"}

	stalled := c.unavailable(timeoutError{})
	if got := Fault(stalled); got != store.FaultBackendBusy {
		t.Fatalf("a timeout classified as %q, want %q", got, store.FaultBackendBusy)
	}
	// It stays an unavailable backend for everything that already matched on
	// that: the category rides alongside the sentinel rather than replacing it.
	if !errors.Is(stalled, ErrUnavailable) {
		t.Error("a timeout stopped being ErrUnavailable, which every caller that retries matches on")
	}
	if !errors.Is(stalled, ErrDaemonBusy) {
		t.Error("a timeout is not tagged ErrDaemonBusy")
	}
	// And the sentence is still the daemon's own, with nothing added: the tag
	// is for the code.
	if want := "did not answer in time"; !strings.Contains(stalled.Error(), want) {
		t.Errorf("the message lost %q: %s", want, stalled)
	}

	// The failures that really are a backend to go and fix stay where they
	// were, or the new category would swallow the old one.
	for name, err := range map[string]error{
		"no socket":         c.unavailable(fmt.Errorf("dial: %w", syscall.ENOENT)),
		"nothing listening": c.unavailable(fmt.Errorf("dial: %w", syscall.ECONNREFUSED)),
		"permission denied": c.unavailable(fmt.Errorf("dial: %w", syscall.EACCES)),
	} {
		if got := Fault(err); got != store.FaultBackend {
			t.Errorf("%s classified as %q, want %q", name, got, store.FaultBackend)
		}
	}

	// A cancelled request is the controller or the operator giving up, not the
	// host being overloaded, so it must not read as either.
	if got := Fault(c.unavailable(context.Canceled)); got != store.FaultBackend {
		t.Errorf("a cancelled request classified as %q, want %q", got, store.FaultBackend)
	}

	// A full disk still wins: the daemon answered, and what it said is the
	// only evidence there is for it.
	full := busyErr(errors.New("no space left on device"))
	if got := Fault(full); got != store.FaultOutOfDisk {
		t.Errorf("a full disk classified as %q, want %q", got, store.FaultOutOfDisk)
	}
}

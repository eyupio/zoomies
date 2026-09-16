package backend

import (
	"errors"
	"strings"

	"github.com/eyupio/zoomies/internal/store"
)

// The categories a backend failure can be, as sentinels a caller matches with
// errors.Is. They are wrapped in at the point the answer is known -- inside the
// call that got the daemon's reply -- and never guessed at above it, because by
// the time an error has been reworded twice on its way to the controller the
// only evidence left is prose.
var (
	// ErrImageUnavailable is a runner image that could not be made ready: a tag
	// that is not there, a registry that refused the credential, a digest that
	// does not resolve. It is kept apart from ErrDaemon because the fix is in a
	// different place -- the pool's image, or the host's access to a registry,
	// rather than the daemon itself.
	ErrImageUnavailable = errors.New("backend: the runner image could not be made ready")
	// ErrDaemon is the container backend refusing the work or not answering.
	// This is the "cannot start the runner container" case, and on a fleet
	// where it is happening it is happening to every runner in the pool.
	ErrDaemon = errors.New("backend: the container backend would not do that")
)

// noSpace is the daemon's own words for a full disk, and the only evidence
// there is for it: Docker reports it as an ordinary 500 with this in the
// message. Matching here rather than in the controller is the point -- this is
// the package holding the daemon's reply, and a controller matching on prose
// would be matching on a sentence three layers of wrapping had already
// rewritten.
const noSpace = "no space left on device"

// imageErr and daemonErr attach a category to an error without adding a word
// to what it says. The sentence an operator reads stays the daemon's own --
// "manifest unknown", "permission denied" -- and the category rides alongside
// it for the code. Wrapping the sentinel into the message instead would give
// every one of these two prefixes and say nothing new in the second.
func imageErr(err error) error  { return tagged{err: err, kind: ErrImageUnavailable} }
func daemonErr(err error) error { return tagged{err: err, kind: ErrDaemon} }

type tagged struct{ err, kind error }

func (t tagged) Error() string   { return t.err.Error() }
func (t tagged) Unwrap() []error { return []error{t.err, t.kind} }

// Fault categorises a backend error for the fleet's failure taxonomy.
//
// An error it cannot place is FaultBackend rather than the unclassified kind:
// everything that reaches here came from the backend, so "the container
// backend would not do that" is true even when nothing narrower is. The
// unclassified kind is for a runner that stopped for reasons nobody observed,
// which is a different admission.
func Fault(err error) store.FaultKind {
	switch {
	case err == nil:
		return ""
	case strings.Contains(strings.ToLower(err.Error()), noSpace):
		return store.FaultOutOfDisk
	case errors.Is(err, ErrImageUnavailable):
		return store.FaultImage
	}
	return store.FaultBackend
}

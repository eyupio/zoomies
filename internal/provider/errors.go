package provider

import (
	"errors"
	"strings"
	"time"
)

// FailureKind is why a provider call failed, in the terms the reconciler's next
// move is decided in. Every category exists because something different happens
// next; a category nothing branches on would be prose, and prose belongs in
// Error.Message.
type FailureKind string

const (
	// FailureConfig is a setting that is wrong. Nothing but an edit will fix it.
	FailureConfig FailureKind = "config"
	// FailureAuth is a credential the provider rejected outright.
	FailureAuth FailureKind = "auth"
	// FailurePermission is a credential that is valid and not allowed to do
	// this. It is kept apart from FailureAuth because the advice differs: one is
	// "this token is wrong", the other is "this token is right and lacks a
	// privilege on a path", and an operator sent to the wrong one of those loses
	// an afternoon. The message names the privilege and the path.
	FailurePermission FailureKind = "permission"
	// FailureUnreachable is a provider we could not talk to. It is never
	// evidence about a resource: nothing may be created, deleted or failed on
	// the strength of it.
	FailureUnreachable FailureKind = "unreachable"
	// FailureQuota is a refusal for want of resources or a limit. Retryable,
	// slowly, and never by asking for more.
	FailureQuota FailureKind = "quota"
	// FailureConflict is something else holding the resource -- a lock, a
	// migration, an identifier another caller took. Resolved by observation.
	FailureConflict FailureKind = "conflict"
	// FailureNotFound is a resource that is not there. For a delete that is
	// success, which is why it is a category rather than an error to report.
	FailureNotFound FailureKind = "not_found"
	// FailureRefused is a definite refusal of the provider's own, in its words.
	FailureRefused FailureKind = "refused"
	// FailureAmbiguous is the one that matters: we do not know whether it
	// happened. A timeout on a create is this, not a failure. A machine whose
	// last operation ended here is reconciled by identity before anything else
	// is attempted, and is NEVER retried blindly.
	FailureAmbiguous FailureKind = "ambiguous"
	// FailureInternal is anything we could not classify, including a kind from a
	// newer contract than this build knows. It retries nothing.
	FailureInternal FailureKind = "internal"
)

// Valid reports whether k is a category this build knows. It is the check
// KindOf makes before trusting a kind that arrived from anywhere but this
// package's own constants.
func (k FailureKind) Valid() bool {
	switch k {
	case FailureConfig, FailureAuth, FailurePermission, FailureUnreachable,
		FailureQuota, FailureConflict, FailureNotFound, FailureRefused,
		FailureAmbiguous, FailureInternal:
		return true
	}
	return false
}

// The sentinels. Every *Error unwraps to exactly one of them, so a caller that
// only asks "was this a not-found" keeps using errors.Is and only a caller that
// wants the remedy or the retry-after reaches for errors.As -- the shape
// github.RateLimitedError already has.
var (
	ErrConfig      = errors.New("provider: configuration is wrong")
	ErrAuth        = errors.New("provider: credential was rejected")
	ErrPermission  = errors.New("provider: credential is not allowed to do that")
	ErrUnreachable = errors.New("provider: could not be reached")
	ErrQuota       = errors.New("provider: refused for want of capacity")
	ErrConflict    = errors.New("provider: resource is held by something else")
	ErrNotFound    = errors.New("provider: resource not found")
	ErrRefused     = errors.New("provider: refused")
	ErrAmbiguous   = errors.New("provider: the outcome of that operation is unknown")
	// ErrUnsupported is a build and a provider that cannot work together: no
	// factory for a kind, or a contract range that does not contain this
	// build's version.
	ErrUnsupported = errors.New("provider: contract version not supported")
)

// sentinels maps a category to the error a caller matches with errors.Is.
var sentinels = map[FailureKind]error{
	FailureConfig:      ErrConfig,
	FailureAuth:        ErrAuth,
	FailurePermission:  ErrPermission,
	FailureUnreachable: ErrUnreachable,
	FailureQuota:       ErrQuota,
	FailureConflict:    ErrConflict,
	FailureNotFound:    ErrNotFound,
	FailureRefused:     ErrRefused,
	FailureAmbiguous:   ErrAmbiguous,
}

// Error is a provider failure with enough structure for the reconciler to
// decide and enough prose for the page to show.
//
// The category is the Kind field alone. It is deliberately not inferred from
// whatever the cause happens to wrap: the classification that decides whether a
// machine is retried or looked at has to be made where the answer is known --
// inside the transport, which is the only layer that knows whether a deadline
// passed before or after the request body went out -- and never rediscovered
// higher up from an error string.
type Error struct {
	Kind FailureKind
	// Op is what was being attempted, in prose written at the call site, so
	// that the message says which step of which machine's life failed.
	Op string
	// Ref is the resource, when there is one.
	Ref string
	// Message is the provider's own words. It is shown verbatim: a provider's
	// sentence about which storage is full is worth more than ours about a
	// status code.
	Message string
	// Remedy is what to change, concretely. An operator who gets a refusal is
	// owed the next action, not the status code.
	Remedy string
	// RetryAfter is how long the provider asked us to wait. Zero means it did
	// not say, and the caller keeps its own backoff.
	RetryAfter time.Duration
	// Cause is what went wrong underneath, kept for the message and for a
	// caller that wants it. It is not unwrapped into: see the type comment.
	Cause error
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		if e.Ref != "" {
			b.WriteString(" ")
			b.WriteString(e.Ref)
		}
		b.WriteString(": ")
	} else if e.Ref != "" {
		b.WriteString(e.Ref)
		b.WriteString(": ")
	}
	b.WriteString(e.sentinel().Error())
	switch {
	case e.Message != "":
		b.WriteString(": ")
		b.WriteString(e.Message)
	case e.Cause != nil:
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}
	if e.Remedy != "" {
		b.WriteString(" Fix: ")
		b.WriteString(e.Remedy)
	}
	return b.String()
}

// Unwrap answers the sentinel for this failure's category, so errors.Is keeps
// working for every caller that only wants to know what sort of refusal it was.
func (e *Error) Unwrap() error { return e.sentinel() }

// sentinel is the error for this failure's category, and ErrUnsupported's
// stablemate for a kind this build does not know: an unrecognised category is
// internal, which retries nothing.
func (e *Error) sentinel() error {
	if s, ok := sentinels[e.Kind]; ok {
		return s
	}
	return errInternal
}

// errInternal is unexported because there is nothing useful a caller can do
// with "we could not classify this" beyond what KindOf already tells it, and an
// exported sentinel for it would invite code to branch on the unclassifiable.
var errInternal = errors.New("provider: unclassified provider failure")

// KindOf reports a failure's category, answering FailureInternal for anything it
// does not recognise -- the safe direction, because internal retries nothing and
// deletes nothing. A kind from a newer contract than this build knows lands
// there too, which is the forward-compatibility rule stated in ContractVersion.
//
// A context cancellation is the caller's own doing rather than a provider
// failure, and is not classified here: check errors.Is(err, context.Canceled)
// before asking this.
func KindOf(err error) FailureKind {
	if err == nil {
		return ""
	}
	var pe *Error
	if errors.As(err, &pe) {
		if pe.Kind.Valid() {
			return pe.Kind
		}
		return FailureInternal
	}
	// A provider may also return a bare sentinel, which is the whole point of
	// exporting them.
	for kind, sentinel := range sentinels {
		if errors.Is(err, sentinel) {
			return kind
		}
	}
	return FailureInternal
}

// Retryable reports whether the same call is worth making again.
//
// It is false for config, auth, permission, ambiguous and internal. The first
// three because nothing but a person changes the answer; internal because we do
// not know what happened; and ambiguous because an operation whose outcome is
// unknown must never be repeated -- its only legal next move is observation,
// and a retry is how one machine becomes two machines and one invoice.
func Retryable(err error) bool {
	switch KindOf(err) {
	case FailureUnreachable, FailureQuota, FailureConflict, FailureNotFound, FailureRefused:
		return true
	}
	return false
}

// RetryAfter is when a caller may try again, given the time it is asking at.
//
// A time already in the past is no answer -- clocks drift and responses queue --
// so it reports unknown and the caller keeps its own backoff rather than
// resuming straight into the same refusal. That is the rule
// github.RateLimitedError.RetryAt already follows, for the same reason.
func RetryAfter(err error, now time.Time) (time.Time, bool) {
	var pe *Error
	if !errors.As(err, &pe) || pe.RetryAfter == 0 {
		return time.Time{}, false
	}
	at := now.Add(pe.RetryAfter)
	if !at.After(now) {
		return time.Time{}, false
	}
	return at, true
}

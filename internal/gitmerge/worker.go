package gitmerge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The worker's whole protocol is one request object in on standard input and
// one answer object out on standard output.
//
// It is this small on purpose. The worker runs beside the controller rather
// than in it, so every field here is something the controller decided to send
// and the worker agreed to accept: a directory, three commits, three limits.
// There is no session, no loop and nothing remembered between requests, because
// each request is one incident and a worker that carried state would carry one
// repository's content into another's merge.
const (
	// MaxRequestBytes bounds the request. It names a directory, three commit
	// ids and three integers, so a request near this size is not one this
	// worker agreed to answer, and reading all of it would be the only work
	// done.
	MaxRequestBytes = 64 << 10
	// MaxResponseBytes bounds the answer. The text it carries is already
	// bounded by MaxTotal; this is the ceiling for JSON escaping on top of it,
	// and an answer past it is refused rather than truncated, because half a
	// JSON object is not an answer a caller can act on.
	MaxResponseBytes = 8 << 20
	// maxErrorBytes bounds the message in a failed answer. Git's own errors are
	// a line or two; this is only here so that a pathological message cannot
	// become the answer's size.
	maxErrorBytes = 4 << 10
)

// The stable codes an answer carries, so a caller branches on the outcome
// rather than on the wording of a message an operator reads.
const (
	CodeInvalidRequest    = "invalid_request"
	CodeMergeBaseMismatch = "merge_base_mismatch"
	CodeUnsafeRepository  = "unsafe_repository"
	CodeMergeFailed       = "merge_failed"
	CodeTooLarge          = "too_large"
)

// ErrTooLarge says the request or the answer did not fit the bytes the worker
// was given.
var ErrTooLarge = errors.New("gitmerge: over the worker's byte budget")

// ErrToolchain says the Git on this host cannot run the merge this worker
// exists for.
var ErrToolchain = errors.New("gitmerge: this host's Git cannot run a bounded merge")

// MinimumGitVersion is the oldest Git that can: merge-tree --write-tree arrived
// in 2.38, and everything here is built on it.
const MinimumGitVersion = "2.38"

// Toolchain says what the worker found when it asked Git what it is.
type Toolchain struct {
	Git string `json:"git"`
}

// Check reports whether the Git on this host can run the bounded merge.
//
// The worker image runs this as its last build step. The alternative is a worker
// that builds, publishes and starts, and then fails the first conflict an
// operator asked it to repair, which is the failure at the worst possible moment
// and about a dependency the build could have named.
func Check(ctx context.Context) (Toolchain, error) {
	out, err := run(ctx, os.TempDir(), "--version")
	if err != nil {
		return Toolchain{}, fmt.Errorf("%w: git did not answer: %v", ErrToolchain, err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "git version ")
	if !supportsMergeTree(version) {
		return Toolchain{Git: version}, fmt.Errorf("%w: git %s is older than %s, which is where merge-tree --write-tree arrived", ErrToolchain, version, MinimumGitVersion)
	}
	return Toolchain{Git: version}, nil
}

// supportsMergeTree reads a Git version string ("2.39.5", "2.39.5.windows.1")
// and reports whether it is at least MinimumGitVersion.
//
// Anything it cannot read is a no: a version this worker cannot compare is one
// it cannot promise merge-tree --write-tree on.
func supportsMergeTree(version string) bool {
	fields := strings.Split(version, ".")
	if len(fields) < 2 {
		return false
	}
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(fields[1])
	if err != nil {
		return false
	}
	if major != 2 {
		return major > 2
	}
	floor, _ := strconv.Atoi(strings.SplitN(MinimumGitVersion, ".", 2)[1])
	return minor >= floor
}

// WorkerRequest is one merge, as it crosses to the worker. It is deliberately
// not Request: a wire type is a contract another process reads, so it must not
// grow a field only because the in-process type did.
type WorkerRequest struct {
	Dir          string `json:"dir"`
	Ancestor     string `json:"ancestor"`
	Base         string `json:"base"`
	Head         string `json:"head"`
	MaxConflicts int    `json:"max_conflicts,omitempty"`
	MaxFileBytes int    `json:"max_file_bytes,omitempty"`
	MaxTotal     int    `json:"max_total,omitempty"`
}

// WorkerResponse is the one answer the worker gives. Exactly one of Result and
// Code is set: a merge either produced a tree (clean, or conflicted and so not
// publishable) or it did not happen.
type WorkerResponse struct {
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
	Code   string  `json:"code,omitempty"`
}

// Serve answers one request from in on out. It is the worker's entire runtime:
// read, merge, answer, exit.
//
// A merge that produced a conflicted tree is a success here. The conflict is
// evidence for whoever resolves it, and refusing to report it would leave the
// caller with nothing to repair.
func Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return serve(ctx, in, out, MaxRequestBytes, MaxResponseBytes)
}

// serve is Serve with the budgets passed in, so a test can hold them small
// enough to reach without building megabytes of conflict text.
func serve(ctx context.Context, in io.Reader, out io.Writer, maxRequest, maxResponse int) error {
	body, err := io.ReadAll(io.LimitReader(in, int64(maxRequest)+1))
	if err != nil {
		return fail(out, maxResponse, CodeInvalidRequest, fmt.Errorf("%w: reading the request: %v", ErrInvalidRequest, err))
	}
	if len(body) > maxRequest {
		return fail(out, maxResponse, CodeTooLarge, fmt.Errorf("%w: the request is larger than %d bytes", ErrTooLarge, maxRequest))
	}

	var req WorkerRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	// Unknown fields are refused, not ignored. This worker has no option a
	// caller could reach by smuggling one in, and a request carrying a field it
	// does not know is either a newer protocol, which it should not half-answer,
	// or an attempt to reach a knob it does not have.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fail(out, maxResponse, CodeInvalidRequest, fmt.Errorf("%w: %v", ErrInvalidRequest, err))
	}
	if err := dec.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return fail(out, maxResponse, CodeInvalidRequest, fmt.Errorf("%w: more than one request on the input", ErrInvalidRequest))
	}

	if !req.usable() {
		return fail(out, maxResponse, CodeInvalidRequest, fmt.Errorf("%w: the repository directory is not an absolute path", ErrInvalidRequest))
	}
	result, err := Merge(ctx, req.request())
	if err != nil {
		return fail(out, maxResponse, codeFor(err), err)
	}
	return answer(out, maxResponse, WorkerResponse{Result: &result})
}

// request turns the wire type into the one Merge takes.
func (r WorkerRequest) request() Request {
	return Request{
		Dir:          filepath.Clean(r.Dir),
		Ancestor:     r.Ancestor,
		Base:         r.Base,
		Head:         r.Head,
		MaxConflicts: r.MaxConflicts,
		MaxFileBytes: r.MaxFileBytes,
		MaxTotal:     r.MaxTotal,
	}
}

// usable reports whether the request names an absolute directory this worker
// will read.
//
// Merge checks that the directory exists; this adds the one thing only a
// separate process needs to be sure of, which is that it was handed a path
// inside the mount the caller made rather than one that climbs out of it. Clean
// first, so a path whose ".." segments cancel (/work/../work) is the ordinary
// path it looks like.
func (r WorkerRequest) usable() bool {
	dir := filepath.Clean(r.Dir)
	if r.Dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	for _, segment := range strings.Split(dir, string(filepath.Separator)) {
		if segment == ".." {
			return false
		}
	}
	return true
}

// codeFor names the outcome a caller switches on. Anything Git itself said is
// merge_failed: the caller's next move is the same for all of them, which is to
// record the failure and stop, and the message is what an operator reads.
func codeFor(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return CodeInvalidRequest
	case errors.Is(err, ErrMergeBaseMismatch):
		return CodeMergeBaseMismatch
	case errors.Is(err, ErrUnsafeRepository):
		return CodeUnsafeRepository
	case errors.Is(err, ErrTooLarge):
		return CodeTooLarge
	default:
		return CodeMergeFailed
	}
}

// fail writes the small answer that says why, then returns the error so the
// process exits non-zero.
//
// Both, rather than one: the controller reads the answer and does not have to
// guess what an exit status meant, and the container's log carries the error
// for whoever is looking at the worker.
func fail(out io.Writer, maxResponse int, code string, err error) error {
	msg := err.Error()
	if len(msg) > maxErrorBytes {
		msg = msg[:maxErrorBytes]
	}
	// Written past the result budget on purpose: an error answer is a few
	// hundred bytes, and a caller that received nothing because the budget was
	// small could not tell that from a worker that crashed.
	if body, encodeErr := encodeAnswer(WorkerResponse{Code: code, Error: msg}); encodeErr == nil {
		_, _ = out.Write(body)
	}
	return err
}

// answer encodes one successful response under the byte budget.
//
// The whole object is encoded before any of it is written, so a response that
// does not fit is refused instead of being cut short: a caller decoding a
// half-written object learns nothing about the merge.
func answer(out io.Writer, maxResponse int, resp WorkerResponse) error {
	body, err := encodeAnswer(resp)
	if err != nil {
		return err
	}
	if len(body) > maxResponse {
		return fail(out, maxResponse, CodeTooLarge, fmt.Errorf("%w: the answer is larger than %d bytes", ErrTooLarge, maxResponse))
	}
	_, err = out.Write(body)
	return err
}

func encodeAnswer(resp WorkerResponse) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		return nil, fmt.Errorf("gitmerge: encode the answer: %w", err)
	}
	return buf.Bytes(), nil
}

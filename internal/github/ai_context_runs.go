package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/aicontext"
	"github.com/eyupio/zoomies/internal/config"
	gh "github.com/google/go-github/v88/github"
)

// ContextRunReader reads what the managed workflow did, so that a context that
// has not been published can be explained instead of only noticed. It is
// read-only and needs the Actions read permission the App already has for the
// fleet; starting a run is ContextWorkflowDispatcher's, and separate.
type ContextRunReader interface {
	// LatestContextRun is the most informative run of the managed workflow for a
	// commit, or nil when it has none.
	LatestContextRun(ctx context.Context, repo, commit string) (*ContextRun, error)
	// ContextJobErrors is the error lines of one job's log, bounded. It is nil,
	// with no error, when the job has no log: it never started, or the log has
	// expired.
	ContextJobErrors(ctx context.Context, repo string, jobID int64) ([]string, error)
	// ContextArtifactUsage is what the repository's unexpired artifacts hold.
	ContextArtifactUsage(ctx context.Context, repo string) (*aicontext.ArtifactUsage, error)
}

// ContextRun is one run, in the terms aicontext.Diagnose reads.
type ContextRun struct {
	ID        int64
	URL       string
	CreatedAt time.Time
	UpdatedAt time.Time
	Facts     aicontext.RunFacts
	// JobIDs are GitHub's ids for Facts.Jobs, in the same order.
	JobIDs []int64
}

const (
	// maxContextLog bounds how much of one job's log is read. The managed
	// workflow's logs are small -- the generator prints paths and sizes, never
	// source -- so this is a ceiling against a log that is not that, not a
	// working size.
	maxContextLog = 8 << 20
	// maxContextErrorLines and maxContextErrorLine bound what is kept of it.
	maxContextErrorLines = 12
	maxContextErrorLine  = 300
	// maxArtifactPages bounds the artifact listing: a thousand artifacts is
	// enough to say what is using the storage, and past it the totals are
	// reported as at least.
	maxArtifactPages = 10
	artifactPageSize = 100
)

// LatestContextRun lists the runs for a commit, newest first, and returns the
// first that says something. A run cancelled by a newer one is not an answer --
// the managed workflow's concurrency group cancels the previous run on purpose
// -- so it is skipped for an older one, and returned only when every run was
// cancelled, so that the caller can still tell nothing has happened since.
func (c *appClient) LatestContextRun(ctx context.Context, repo, commit string) (*ContextRun, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	runs, resp, err := c.asInstallation.Actions.ListWorkflowRunsByFileName(ctx, owner, name, path.Base(aicontext.WorkflowPath),
		&gh.ListWorkflowRunsOptions{HeadSHA: commit, ListOptions: gh.ListOptions{PerPage: 10}})
	if err != nil {
		return nil, c.fail("list context workflow runs", resp, err)
	}
	var pick, cancelled *gh.WorkflowRun
	for _, r := range runs.WorkflowRuns {
		if r.GetStatus() == "completed" && r.GetConclusion() == "cancelled" {
			if cancelled == nil {
				cancelled = r
			}
			continue
		}
		pick = r
		break
	}
	if pick == nil {
		pick = cancelled
	}
	if pick == nil {
		return nil, nil
	}
	run := &ContextRun{
		ID: pick.GetID(), URL: pick.GetHTMLURL(),
		CreatedAt: pick.GetCreatedAt().Time, UpdatedAt: pick.GetUpdatedAt().Time,
		Facts: aicontext.RunFacts{Status: pick.GetStatus(), Conclusion: pick.GetConclusion()},
	}
	// A run that has not finished is reported as it is; its jobs are not needed
	// to know that it must be left alone, and a run that was cancelled or never
	// started has none worth reading.
	if pick.GetStatus() != "completed" || pick.GetConclusion() == "cancelled" || pick.GetConclusion() == "startup_failure" {
		return run, nil
	}
	jobs, resp, err := c.asInstallation.Actions.ListWorkflowJobs(ctx, owner, name, pick.GetID(),
		&gh.ListWorkflowJobsOptions{Filter: "latest", ListOptions: gh.ListOptions{PerPage: 20}})
	if err != nil {
		return nil, c.fail("list context workflow jobs", resp, err)
	}
	for _, j := range jobs.Jobs {
		job := aicontext.JobFacts{Name: j.GetName(), Conclusion: j.GetConclusion()}
		for _, s := range j.Steps {
			job.Steps = append(job.Steps, aicontext.StepFacts{Name: s.GetName(), Conclusion: s.GetConclusion()})
		}
		run.Facts.Jobs = append(run.Facts.Jobs, job)
		run.JobIDs = append(run.JobIDs, j.GetID())
	}
	return run, nil
}

// ContextJobErrors downloads one job's log and keeps only the lines that explain
// a failure: every ##[error] annotation, and the line before it. The generator and
// the upload and publication scripts end by raising SystemExit with their reason,
// which Actions prints as an ordinary line immediately before its own "Process
// completed with exit code 1" annotation, so the reason is the line before.
//
// The log lives at a pre-signed address on another host. The request for it is
// made without the installation's token for exactly that reason, and only to an
// address on the scheme of the API itself.
func (c *appClient) ContextJobErrors(ctx context.Context, repo string, jobID int64) ([]string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	u, resp, err := c.asInstallation.Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 0)
	if err != nil {
		// A log that is gone -- expired, or never written for a job that did not
		// start -- is an answer ("no log"), not a failure to ask.
		if resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone) {
			return nil, nil
		}
		return nil, c.fail("read context job log", resp, err)
	}
	if !fetchableLogAddress(c.asInstallation.BaseURL(), u) {
		return nil, fmt.Errorf("github: the job log address is not one to fetch")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=-8388608")
	httpClient := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: read context job log: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return nil, nil
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("github: read context job log: unexpected status %d", res.StatusCode)
	}
	body, err := readContextLogTail(res.Body)
	if err != nil {
		return nil, fmt.Errorf("github: read context job log: %w", err)
	}
	return errorLines(string(body)), nil
}

// fetchableLogAddress is whether this process may fetch a log from where GitHub
// said it is. The address is GitHub's to name, but it arrives in a response
// header and is about to be requested from the controller's own network, so it
// is held to what an operator-supplied URL is: it is on the API's scheme, names a
// host and no credentials, and does not point at this machine or a private
// network -- unless the API itself does, as an Enterprise Server on a LAN, or a
// test, legitimately does.
func fetchableLogAddress(apiBase string, log *url.URL) bool {
	api, err := url.Parse(apiBase)
	if err != nil || log == nil || log.Host == "" || log.User != nil || log.Scheme != api.Scheme {
		return false
	}
	apiIsPrivate := config.CheckOutboundURL("", apiBase, false) != nil
	return config.CheckOutboundURL("the job log address", log.String(), apiIsPrivate) == nil
}

// errorLines picks the lines of a log that explain why a job failed.
func errorLines(log string) []string {
	lines := strings.Split(log, "\n")
	var out []string
	for i, line := range lines {
		line = strings.TrimRight(stripLogTimestamp(line), "\r")
		if !strings.HasPrefix(line, "##[error]") {
			continue
		}
		message := strings.TrimPrefix(line, "##[error]")
		if exitCodeAnnotation(message) {
			// Actions annotates the exit code itself, so the only ##[error] a
			// great many failed jobs have says nothing about the cause. The
			// line just above it can be the runtime's version banner, and the
			// reason (an uncaught exception, a test runner's report, a shell's
			// complaint) is ordinary output a little further up. Keeping only
			// the banner left a repair with an exit code and no test, selector
			// or line to locate, so it guessed at files and spent its source
			// allowance without reaching the failure.
			out = append(out, errorBlock(lines[:i])...)
			out = append(out, clip(message))
			continue
		}
		if i > 0 {
			if prev := strings.TrimRight(stripLogTimestamp(lines[i-1]), "\r"); prev != "" && !strings.HasPrefix(prev, "##[") {
				out = append(out, clip(prev))
			}
		}
		out = append(out, clip(message))
	}
	if len(out) == 0 {
		for i, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "error:") || strings.Contains(lower, "assertionerror") || strings.Contains(lower, "--- fail:") || strings.Contains(lower, "panic:") || strings.Contains(lower, "traceback") || strings.Contains(lower, "failed:") {
				for _, v := range lines[max(0, i-2):min(len(lines), i+6)] {
					if strings.TrimSpace(v) != "" {
						out = append(out, clip(stripLogTimestamp(v)))
					}
				}
			}
		}
	}
	// The last of them are the ones that ended the job.
	if len(out) > maxContextErrorLines {
		out = out[len(out)-maxContextErrorLines:]
	}
	return out
}

// exitCodeAnnotation is whether an annotation is Actions' own note that a
// process stopped, rather than a reason the process gave itself.
func exitCodeAnnotation(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "process completed with exit code") ||
		strings.Contains(lower, "failed with exit code") ||
		strings.Contains(lower, "exited with code")
}

// errorBlock keeps the output immediately above an exit-code annotation, minus
// blank lines and group markers: the last of it is the exception that ended the
// process. A failure reporter often prints the test's name at the top of a
// block and a summary at the bottom, so a recent failure marker and the lines
// under it are kept too, in place of the oldest tail lines. It leaves one
// line's room for the annotation itself.
func errorBlock(lines []string) []string {
	valid := []string{}
	for _, line := range lines {
		line = strings.TrimRight(stripLogTimestamp(line), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "##[") {
			continue
		}
		valid = append(valid, clip(line))
	}
	if len(valid) == 0 {
		return nil
	}
	tailStart := max(0, len(valid)-(maxContextErrorLines-1))
	header := -1
	for level := strongMarker; level >= weakMarker && header < 0; level-- {
		for i := tailStart - 1; i >= 0; i-- {
			if failureMarker(valid[i]) == level {
				header = i
				break
			}
		}
	}
	keep := map[int]bool{}
	if header >= 0 {
		for i := header; i < len(valid) && i <= header+3; i++ {
			keep[i] = true
		}
	}
	// The frame that names the file and line is often above the tail: a
	// Playwright matcher prints the whole received and expected object below
	// `at .../home.e2e.mjs:1116:19`, so the tail is the object and the frame is
	// lost. Without it a repair had the assertion but not the file that made
	// it, and guessed at whichever test the job happened to be named after.
	if location := sourceLocation(valid, tailStart); location >= 0 {
		keep[location] = true
	}
	// A header earlier in the log must not push out the exception that ended
	// it: the most recent lines fill what the header and its context leave.
	for i := len(valid) - 1; i >= tailStart && len(keep) < maxContextErrorLines-1; i-- {
		keep[i] = true
	}
	indices := make([]int, 0, len(keep))
	for i := range keep {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	out := make([]string, 0, len(indices))
	for _, i := range indices {
		out = append(out, valid[i])
	}
	return out
}

// sourceLocation is a stack frame from a runtime or a test reporter: a path,
// a colon and a line number, with an optional column. It is deliberately
// loose about the path, because the languages a repository can use write one
// differently, and the repair matches what it finds against its own inventory.
var sourceLocationPattern = regexp.MustCompile(`[A-Za-z0-9_.-]*[/.][A-Za-z0-9_./-]*:\d+(?::\d+)?`)

// sourceLocation is the last line above the tail that names a file and a line,
// or -1 when the tail already holds one or none does. The last is the one
// nearest the failure. A frame inside the tail needs no help; adding one above
// it would push the tail's own frame out of the fixed budget.
func sourceLocation(lines []string, tailStart int) int {
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.HasPrefix(line, "node:") || strings.Contains(line, "/node_modules/") {
			continue
		}
		if sourceLocationPattern.MatchString(line) {
			if i >= tailStart {
				return -1
			}
			return i
		}
	}
	return -1
}

const (
	noMarker = iota
	weakMarker
	strongMarker
)

// failureMarker is a line a test runner or runtime uses to begin a failure. A
// strong marker names the test or the crash (the test's own name, a panic, a
// TAP not-ok); a weak one is a reason inside the block. When both are above the
// tail, the strong one is the one that says which test failed. The exit-code
// annotation does not count, because it names no cause.
func failureMarker(line string) int {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(trimmed, "✖"),
		strings.HasPrefix(trimmed, "✘"),
		strings.HasPrefix(trimmed, "✕"),
		strings.HasPrefix(trimmed, "✗"),
		strings.HasPrefix(trimmed, "●"),
		strings.HasPrefix(trimmed, "not ok "),
		strings.HasPrefix(trimmed, "FAIL "),
		strings.Contains(lower, "--- fail:"),
		strings.Contains(lower, "panic:"),
		strings.Contains(lower, "traceback (most recent call last)"):
		return strongMarker
	case strings.Contains(lower, "assertionerror"),
		strings.Contains(lower, "error:"),
		strings.Contains(lower, "failed:"),
		strings.Contains(lower, "timeouterror"):
		return weakMarker
	}
	return noMarker
}

// stripLogTimestamp removes the "2026-10-06T06:47:48.2010869Z " Actions puts in
// front of every line.
func stripLogTimestamp(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if head, rest, ok := strings.Cut(line, " "); ok && strings.HasSuffix(head, "Z") && strings.Contains(head, "T") {
		return rest
	}
	// A blank line in a log is still timestamped, so it has no space to cut at.
	if len(line) >= 20 && line[4] == '-' && line[10] == 'T' && strings.HasSuffix(line, "Z") {
		return ""
	}
	return line
}

func clip(s string) string {
	if len(s) > maxContextErrorLine {
		return s[:maxContextErrorLine]
	}
	return s
}

// ContextArtifactUsage totals the repository's unexpired artifacts and names the
// biggest groups by name. It reads at most maxArtifactPages pages.
func (c *appClient) ContextArtifactUsage(ctx context.Context, repo string) (*aicontext.ArtifactUsage, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	usage := &aicontext.ArtifactUsage{}
	groups := map[string]*aicontext.ArtifactGroup{}
	for page := 1; ; page++ {
		list, resp, err := c.asInstallation.Actions.ListArtifacts(ctx, owner, name, &gh.ListArtifactsOptions{ListOptions: gh.ListOptions{PerPage: artifactPageSize, Page: page}})
		if err != nil {
			return nil, c.fail("list artifacts", resp, err)
		}
		for _, a := range list.Artifacts {
			if a.GetExpired() {
				continue
			}
			usage.Count++
			usage.Bytes += a.GetSizeInBytes()
			g := groups[a.GetName()]
			if g == nil {
				g = &aicontext.ArtifactGroup{Name: a.GetName()}
				groups[a.GetName()] = g
			}
			g.Count++
			g.Bytes += a.GetSizeInBytes()
		}
		if len(list.Artifacts) < artifactPageSize {
			break
		}
		if page == maxArtifactPages {
			usage.Partial = true
			break
		}
	}
	for _, g := range groups {
		usage.Largest = append(usage.Largest, *g)
	}
	sort.Slice(usage.Largest, func(i, j int) bool {
		if usage.Largest[i].Bytes != usage.Largest[j].Bytes {
			return usage.Largest[i].Bytes > usage.Largest[j].Bytes
		}
		return usage.Largest[i].Name < usage.Largest[j].Name
	})
	if len(usage.Largest) > 3 {
		usage.Largest = usage.Largest[:3]
	}
	return usage, nil
}

var _ ContextRunReader = (*appClient)(nil)

// Servers that honour Range return just the tail. A bounded streaming fallback
// keeps late evidence without retaining the entire log in memory.
func readContextLogTail(r io.Reader) ([]byte, error) {
	tail := make([]byte, 0, maxContextLog)
	buf := make([]byte, 64<<10)
	total := 0
	for {
		n, err := r.Read(buf)
		total += n
		if total > 64<<20 {
			return nil, fmt.Errorf("job log exceeds the bounded download limit; inspect its failed step on GitHub")
		}
		if n > 0 {
			if len(tail)+n > maxContextLog {
				drop := len(tail) + n - maxContextLog
				copy(tail, tail[drop:])
				tail = tail[:len(tail)-drop]
			}
			tail = append(tail, buf[:n]...)
		}
		if err == io.EOF {
			return tail, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

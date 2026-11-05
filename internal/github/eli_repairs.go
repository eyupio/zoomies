package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/prrepair"
	gh "github.com/google/go-github/v88/github"
)

// ErrRepairUnsupported identifies a run that cannot be admitted as a PR repair.
var ErrRepairUnsupported = errors.New("unsupported PR repair target")

// RepairTargetChanged describes an authoritative change since a failed run.
// Transport and permission errors never use this type: neither proves a fix.
type RepairTargetChanged struct {
	Code    string
	Message string
}

func (e *RepairTargetChanged) Error() string { return e.Message }
func (e *RepairTargetChanged) Unwrap() error { return ErrRepairUnsupported }

type RepairActor struct {
	ID       int64
	Login    string
	CanWrite bool
}
type RepairPull struct {
	Number   int
	AuthorID int64
	Author   string
	Branch   string
	// Base is the branch the pull request merges into, which is not always the
	// repository's default branch: it can target a release branch.
	Base          string
	BaseSHA       string
	HeadSHA       string
	Title         string
	Body          string
	URL           string
	DefaultBranch string
	Fork          bool
	Draft         bool
	Open          bool
}

// RepairJobStep is one step of a job, as GitHub reported it ended.
type RepairJobStep struct {
	Number      int
	Name        string
	Conclusion  string
	StartedAt   time.Time
	CompletedAt time.Time
}

// RepairFailedJob is one failed job of a run, as GitHub reports it: its own
// id (which is all a log is ever asked with), its name, the step it stopped
// at and its page. It needs no record in the fleet that ran it, so a repair
// of a job GitHub's own runners ran can still say what it was asked about.
type RepairFailedJob struct {
	ID          int64
	Name        string
	URL         string
	Conclusion  string
	Step        string
	StepNumber  int
	Steps       []RepairJobStep
	StartedAt   time.Time
	CompletedAt time.Time
}

// RepairFailureRun is a run and the jobs of it that failed, for the page and
// the kept case of a repair that answers for a failure Zoomies has no record
// of.
type RepairFailureRun struct {
	RunID     int64
	RunURL    string
	Workflow  string
	RunNumber int
	Jobs      []RepairFailedJob
}

type RepairClient interface {
	RepairActor(context.Context, string, string) (RepairActor, error)
	RepairPull(context.Context, string, int) (RepairPull, error)
	RepairPullForRun(context.Context, string, int64) (RepairPull, error)
	RepairSource(context.Context, string, RepairPull, bool) (*RepairSource, error)
	RepairFailures(context.Context, string, RepairPull, int64) ([]string, error)
	// RepairFailedJobs names the failed jobs of a run (run > 0), or of the
	// newest failing run at a head commit, for a failure this fleet never
	// ran. A run with no failed jobs is a nil run and no error, so a check
	// that reads it can say nothing was found without saying GitHub failed.
	RepairFailedJobs(context.Context, string, int64, string) (*RepairFailureRun, error)
	CommitRepair(context.Context, string, RepairPull, prrepair.Plan) (string, error)
	RepairComment(context.Context, string, int, int64, string) (int64, error)
	RepairReact(context.Context, string, int64, string) error
	RepairChecks(context.Context, string, string) (string, error)
}
type RepairSource struct {
	Snapshot prrepair.Snapshot
	files    map[string]repairTreeEntry
	client   *appClient
	repo     string
	commit   string
}
type repairTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (c *appClient) repairRequest(ctx context.Context, method, p string, body, out any) error {
	req, err := c.asInstallation.NewRequest(ctx, method, p, body)
	if err != nil {
		return err
	}
	resp, err := c.asInstallation.Do(req, out)
	if err != nil {
		return c.fail("Eli GitHub operation", resp, err)
	}
	return nil
}
func repairRepo(repo string) (string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	return "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}
func (c *appClient) RepairActor(ctx context.Context, repo, login string) (RepairActor, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return RepairActor{}, err
	}
	var v struct {
		Permission string `json:"permission"`
		User       struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
	}
	err = c.repairRequest(ctx, http.MethodGet, base+"/collaborators/"+url.PathEscape(login)+"/permission", nil, &v)
	return RepairActor{ID: v.User.ID, Login: v.User.Login, CanWrite: v.Permission == "write" || v.Permission == "maintain" || v.Permission == "admin"}, err
}
func (c *appClient) RepairPull(ctx context.Context, repo string, number int) (RepairPull, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return RepairPull{}, err
	}
	var p struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				FullName      string `json:"full_name"`
				DefaultBranch string `json:"default_branch"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
		Draft bool `json:"draft"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/pulls/"+strconv.Itoa(number), nil, &p); err != nil {
		return RepairPull{}, err
	}
	out := RepairPull{Number: p.Number, AuthorID: p.User.ID, Author: p.User.Login, Branch: p.Head.Ref, Base: p.Base.Ref, BaseSHA: p.Base.SHA, HeadSHA: p.Head.SHA, Title: p.Title, Body: p.Body, URL: p.HTMLURL, DefaultBranch: p.Head.Repo.DefaultBranch, Fork: p.Head.Repo.FullName != repo, Draft: p.Draft, Open: p.State == "open"}
	if out.Number > 0 && p.State == "closed" {
		return out, &RepairTargetChanged{Code: "closed", Message: fmt.Sprintf("PR #%d is closed or merged. This failure no longer needs a PR repair. You can still inspect the original run on GitHub.", out.Number)}
	}
	if out.Number <= 0 || !out.Open || out.Fork || out.Draft || out.HeadSHA == "" || out.Branch == "" || out.Branch == out.DefaultBranch {
		return out, fmt.Errorf("%w: an Eli repair needs an open PR on a non-default branch in this repository; forks are not writable", ErrRepairUnsupported)
	}
	return out, nil
}
func (c *appClient) RepairPullForRun(ctx context.Context, repo string, run int64) (RepairPull, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return RepairPull{}, err
	}
	var r struct {
		HeadSHA        string `json:"head_sha"`
		Event          string `json:"event"`
		HeadRepository struct {
			FullName string `json:"full_name"`
		} `json:"head_repository"`
		Pulls []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d", base, run), nil, &r); err != nil {
		return RepairPull{}, err
	}
	if r.HeadRepository.FullName != repo {
		return RepairPull{}, fmt.Errorf("%w: fork workflow runs are not writable", ErrRepairUnsupported)
	}
	// GitHub can omit the run's PR links, including after a PR is merged.
	// Resolve by commit, never by a branch name that may have been reused, and
	// only for a run a PR started: the commit of a push, schedule or manual run
	// on the default branch resolves to the PR whose merge made it, and that
	// PR being closed would settle a failure that never belonged to it.
	if len(r.Pulls) == 0 && r.HeadSHA != "" {
		if r.Event != "pull_request" && r.Event != "pull_request_target" {
			return RepairPull{}, fmt.Errorf("%w: the failed run was not started by a PR (its event is %q), so there is no PR to repair", ErrRepairUnsupported, r.Event)
		}
		if err := c.repairRequest(ctx, http.MethodGet, base+"/commits/"+url.PathEscape(r.HeadSHA)+"/pulls?per_page=2", nil, &r.Pulls); err != nil {
			return RepairPull{}, err
		}
	}
	if len(r.Pulls) != 1 {
		return RepairPull{}, fmt.Errorf("%w: the failed run does not identify a single PR. Choose an open PR or paste its GitHub URL", ErrRepairUnsupported)
	}
	p, err := c.RepairPull(ctx, repo, r.Pulls[0].Number)
	if err != nil {
		return p, err
	}
	if p.HeadSHA != r.HeadSHA {
		return p, &RepairTargetChanged{Code: "superseded", Message: fmt.Sprintf("PR #%d has newer commits than this failed run. This failure is out of date. Check the current PR to see whether its latest checks need a repair.", p.Number)}
	}
	return p, nil
}
func (c *appClient) RepairSource(ctx context.Context, repo string, p RepairPull, workflows bool) (*RepairSource, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return nil, err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/git/commits/"+p.HeadSHA, nil, &commit); err != nil {
		return nil, err
	}
	var tree struct {
		Truncated bool              `json:"truncated"`
		Tree      []repairTreeEntry `json:"tree"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/git/trees/"+commit.Tree.SHA+"?recursive=1", nil, &tree); err != nil {
		return nil, err
	}
	if tree.Truncated || len(tree.Tree) > 10000 {
		return nil, fmt.Errorf("repository inventory exceeds Eli's repair limit")
	}
	source := &RepairSource{client: c, repo: repo, commit: p.HeadSHA, files: map[string]repairTreeEntry{}, Snapshot: prrepair.Snapshot{Title: p.Title, Body: prrepair.Redact(p.Body[:min(len(p.Body), 8000)]), Branch: p.Branch, HeadSHA: p.HeadSHA}}
	for _, f := range tree.Tree {
		source.files[f.Path] = f
		if f.Type == "blob" && (f.Mode == "100644" || f.Mode == "100755") && prrepair.AllowedPath(f.Path, workflows) {
			source.Snapshot.Inventory = append(source.Snapshot.Inventory, f.Path)
		}
	}
	var changed []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		Patch    string `json:"patch"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d/files?per_page=100", base, p.Number), nil, &changed); err != nil {
		return nil, err
	}
	if len(changed) >= 100 {
		return nil, fmt.Errorf("the PR exceeds Eli's changed-file limit")
	}
	total := 0
	for _, f := range changed {
		if len(source.Snapshot.Diff) < 32<<10 {
			patch := f.Filename + "\n" + f.Patch + "\n"
			source.Snapshot.Diff += prrepair.Redact(patch[:min(len(patch), (32<<10)-len(source.Snapshot.Diff))])
		}
		if f.Status == "removed" || !prrepair.AllowedPath(f.Filename, workflows) {
			continue
		}
		file, err := source.ReadFile(ctx, f.Filename)
		// A file Eli may not have (too large, or not text) is left out and
		// named, not the end of the repair: a PR that regenerates a reference
		// of a few thousand lines is still a PR whose other files can be
		// fixed. GitHub refusing a read is a failure, because the next read
		// will be refused too.
		var unusable *prrepair.ReadError
		if errors.As(err, &unusable) && unusable.Err == nil {
			source.Snapshot.Skipped = append(source.Snapshot.Skipped, fmt.Sprintf("%s: larger than %d KiB or not plain text", f.Filename, prrepair.MaxBlobBytes/1024))
			continue
		}
		if err != nil {
			return nil, err
		}
		if total+len(file.Content) > 4<<20 {
			source.Snapshot.Skipped = append(source.Snapshot.Skipped, fmt.Sprintf("%s: left out, the PR's source is over the %d KiB budget", f.Filename, 4096))
			continue
		}
		total += len(file.Content)
		source.Snapshot.Files = append(source.Snapshot.Files, file)
	}
	return source, nil
}
func (s *RepairSource) ReadFile(ctx context.Context, name string) (prrepair.File, error) {
	// Git blobs never resolve symlinks; every parent must still be a tree.
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		if e, ok := s.files[strings.Join(parts[:i], "/")]; ok && e.Type != "tree" {
			return prrepair.File{}, fmt.Errorf("source path crosses a non-directory")
		}
	}
	e, ok := s.files[name]
	if !ok {
		return prrepair.File{Path: name, Mode: "100644"}, nil
	}
	if e.Type != "blob" || e.Mode != "100644" && e.Mode != "100755" {
		return prrepair.File{}, fmt.Errorf("source path is not a regular file")
	}
	base, _ := repairRepo(s.repo)
	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
	}
	if err := s.client.repairRequest(ctx, http.MethodGet, base+"/git/blobs/"+e.SHA, nil, &blob); err != nil {
		return prrepair.File{}, err
	}
	if blob.Size > prrepair.MaxBlobBytes {
		return prrepair.File{}, &prrepair.ReadError{Path: name, Reason: "file_size", Bytes: blob.Size}
	}
	if blob.Encoding != "base64" {
		return prrepair.File{}, &prrepair.ReadError{Path: name, Reason: "unsupported_encoding"}
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return prrepair.File{}, fmt.Errorf("source file could not be decoded")
	}
	if len(raw) > prrepair.MaxBlobBytes {
		return prrepair.File{}, &prrepair.ReadError{Path: name, Reason: "file_size", Bytes: len(raw)}
	}
	if !utf8.ValidString(string(raw)) || strings.ContainsRune(string(raw), 0) {
		return prrepair.File{}, &prrepair.ReadError{Path: name, Reason: "unsupported_text"}
	}
	return prrepair.File{Path: name, Content: string(raw), Mode: e.Mode}, nil
}
func (c *appClient) RepairFailures(ctx context.Context, repo string, p RepairPull, jobID int64) ([]string, error) {
	if jobID > 0 {
		return c.ContextJobErrors(ctx, repo, jobID)
	}
	base, err := repairRepo(repo)
	if err != nil {
		return nil, err
	}
	var runs struct {
		Runs []struct {
			ID      int64  `json:"id"`
			HeadSHA string `json:"head_sha"`
			Status  string `json:"status"`
		} `json:"workflow_runs"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/actions/runs?head_sha="+p.HeadSHA+"&per_page=10", nil, &runs); err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range runs.Runs {
		if r.HeadSHA != p.HeadSHA {
			continue
		}
		var jobs struct {
			Jobs []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Conclusion string `json:"conclusion"`
			} `json:"jobs"`
		}
		if err := c.repairRequest(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d/jobs?per_page=100", base, r.ID), nil, &jobs); err != nil {
			return nil, err
		}
		for _, j := range jobs.Jobs {
			if j.Conclusion != "failure" && j.Conclusion != "timed_out" {
				continue
			}
			lines, err := c.ContextJobErrors(ctx, repo, j.ID)
			if err != nil {
				return nil, err
			}
			if len(lines) == 0 {
				// A job that never started, or whose log expired, has nothing
				// to say. Naming it with an empty log made the evidence look
				// present, so a repair went ahead with "job: " and no reason
				// instead of trying the next failing job.
				continue
			}
			out = append(out, prrepair.Redact(j.Name+": "+strings.Join(lines, "\n")))
			if len(out) >= 5 {
				return out, nil
			}
		}
	}
	return out, nil
}

// repairRunPage is the run a failure is read from: the one named when run is
// given, or the newest failing run at headSHA. GitHub lists newest first, so
// the first completed failure met is the one a person looking at that commit
// is looking at.
func (c *appClient) repairRunPage(ctx context.Context, base string, run int64, headSHA string) (id int64, page RepairFailureRun, err error) {
	var r struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		RunNumber  int    `json:"run_number"`
		HTMLURL    string `json:"html_url"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	if run > 0 {
		if err = c.repairRequest(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d", base, run), nil, &r); err != nil {
			return 0, page, err
		}
		return r.ID, RepairFailureRun{RunID: r.ID, RunURL: r.HTMLURL, Workflow: r.Name, RunNumber: r.RunNumber}, nil
	}
	var runs struct {
		Runs []struct {
			ID         int64  `json:"id"`
			HeadSHA    string `json:"head_sha"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			Name       string `json:"name"`
			RunNumber  int    `json:"run_number"`
			HTMLURL    string `json:"html_url"`
		} `json:"workflow_runs"`
	}
	if err = c.repairRequest(ctx, http.MethodGet, base+"/actions/runs?head_sha="+url.QueryEscape(headSHA)+"&per_page=10", nil, &runs); err != nil {
		return 0, page, err
	}
	for _, r := range runs.Runs {
		if r.HeadSHA != headSHA || r.Status != "completed" {
			continue
		}
		if r.Conclusion != "failure" && r.Conclusion != "timed_out" {
			// A cancelled or successful run has no failed jobs to name.
			continue
		}
		return r.ID, RepairFailureRun{RunID: r.ID, RunURL: r.HTMLURL, Workflow: r.Name, RunNumber: r.RunNumber}, nil
	}
	return 0, page, nil
}

func (c *appClient) RepairFailedJobs(ctx context.Context, repo string, run int64, headSHA string) (*RepairFailureRun, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return nil, err
	}
	if run < 1 && headSHA == "" {
		return nil, nil
	}
	id, runPage, err := c.repairRunPage(ctx, base, run, headSHA)
	if err != nil || id == 0 {
		return nil, err
	}
	var jobs struct {
		Jobs []struct {
			ID          int64     `json:"id"`
			Name        string    `json:"name"`
			Conclusion  string    `json:"conclusion"`
			HTMLURL     string    `json:"html_url"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
			Steps       []struct {
				Number      int       `json:"number"`
				Name        string    `json:"name"`
				Conclusion  string    `json:"conclusion"`
				StartedAt   time.Time `json:"started_at"`
				CompletedAt time.Time `json:"completed_at"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d/jobs?per_page=100", base, id), nil, &jobs); err != nil {
		return nil, err
	}
	for _, j := range jobs.Jobs {
		// failure and timed_out are the conclusions the fleet's own record of
		// a case is built from, so this read names the same jobs as that one.
		if j.Conclusion != "failure" && j.Conclusion != "timed_out" {
			continue
		}
		job := RepairFailedJob{ID: j.ID, Name: j.Name, URL: j.HTMLURL, Conclusion: j.Conclusion, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt}
		for _, s := range j.Steps {
			job.Steps = append(job.Steps, RepairJobStep{Number: s.Number, Name: s.Name, Conclusion: s.Conclusion, StartedAt: s.StartedAt, CompletedAt: s.CompletedAt})
			// The step a job stopped at is the first that did not succeed or
			// was not passed over: every one after it ended as a consequence.
			if job.Step == "" {
				switch s.Conclusion {
				case "", "success", "skipped", "neutral":
				default:
					job.Step, job.StepNumber = s.Name, s.Number
				}
			}
		}
		runPage.Jobs = append(runPage.Jobs, job)
	}
	if len(runPage.Jobs) == 0 {
		return nil, nil
	}
	return &runPage, nil
}
func (c *appClient) CommitRepair(ctx context.Context, repo string, p RepairPull, plan prrepair.Plan) (string, error) {
	fresh, err := c.RepairPull(ctx, repo, p.Number)
	if err != nil {
		return "", err
	}
	if fresh.HeadSHA != p.HeadSHA || fresh.Branch != p.Branch {
		return "", ErrSetupConflict
	}
	owner, name, _ := splitRepo(repo)
	parent, resp, err := c.asInstallation.Git.GetCommit(ctx, owner, name, p.HeadSHA)
	if err != nil {
		return "", c.fail("read repair parent", resp, err)
	}
	entries := []*gh.TreeEntry{}
	for _, f := range plan.Files {
		mode := f.Mode
		if mode == "" {
			mode = "100644"
		}
		entries = append(entries, &gh.TreeEntry{Path: gh.Ptr(f.Path), Mode: gh.Ptr(mode), Type: gh.Ptr("blob"), Content: gh.Ptr(f.Content)})
	}
	tree, resp, err := c.asInstallation.Git.CreateTree(ctx, owner, name, parent.GetTree().GetSHA(), entries)
	if err != nil {
		return "", c.fail("write repair tree", resp, err)
	}
	commit, resp, err := c.asInstallation.Git.CreateCommit(ctx, owner, name, gh.Commit{Message: gh.Ptr("Fix PR checks with Eli"), Tree: tree, Parents: []*gh.Commit{{SHA: gh.Ptr(p.HeadSHA)}}}, nil)
	if err != nil {
		return "", c.fail("write repair commit", resp, err)
	}
	// A concurrent push makes this divergent, so GitHub refuses it. Force is always false.
	_, resp, err = c.asInstallation.Git.UpdateRef(ctx, owner, name, "heads/"+p.Branch, gh.UpdateRef{SHA: commit.GetSHA(), Force: gh.Ptr(false)})
	if err != nil {
		return "", c.fail("advance repair branch without forcing", resp, err)
	}
	return commit.GetSHA(), nil
}
func (c *appClient) RepairComment(ctx context.Context, repo string, pull int, id int64, text string) (int64, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return 0, err
	}
	method := http.MethodPost
	endpoint := fmt.Sprintf("%s/issues/%d/comments", base, pull)
	if id > 0 {
		method = http.MethodPatch
		endpoint = fmt.Sprintf("%s/issues/comments/%d", base, id)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err = c.repairRequest(ctx, method, endpoint, map[string]string{"body": text}, &out)
	return out.ID, err
}

// RepairReact puts a reaction on a comment. GitHub offers a fixed set (there is
// no paw), and posting the same one twice is not an error, so a redelivered
// webhook cannot stack them.
func (c *appClient) RepairReact(ctx context.Context, repo string, commentID int64, content string) error {
	base, err := repairRepo(repo)
	if err != nil {
		return err
	}
	return c.repairRequest(ctx, http.MethodPost, fmt.Sprintf("%s/issues/comments/%d/reactions", base, commentID), map[string]string{"content": content}, nil)
}

type RepairCheck struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	App        string `json:"app,omitempty"`
	URL        string `json:"url,omitempty"`
}

// RepairCheckEvidence is the bounded record of what one head's checks said.
// State is the overall answer the worker acts on; Checks keeps the producers
// and individual outcomes for the page that explains a decision.
type RepairCheckEvidence struct {
	State     string        `json:"state"`
	Checks    []RepairCheck `json:"checks"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated"`
}

const maxRepairCheckEvidence = 50

// RepairEvidenceClient is the richer check read a watcher uses. It is separate
// from RepairClient so clients that only know the overall state keep working.
type RepairEvidenceClient interface {
	RepairCheckEvidence(context.Context, string, string) (RepairCheckEvidence, error)
}

func (c *appClient) RepairChecks(ctx context.Context, repo, sha string) (string, error) {
	evidence, err := c.repairCheckEvidence(ctx, repo, sha)
	return evidence.State, err
}

func (c *appClient) RepairCheckEvidence(ctx context.Context, repo, sha string) (RepairCheckEvidence, error) {
	return c.repairCheckEvidence(ctx, repo, sha)
}

func (c *appClient) repairCheckEvidence(ctx context.Context, repo, sha string) (RepairCheckEvidence, error) {
	out := RepairCheckEvidence{Checks: []RepairCheck{}}
	add := func(check RepairCheck) {
		if len(out.Checks) < maxRepairCheckEvidence {
			out.Checks = append(out.Checks, check)
		}
	}
	base, err := repairRepo(repo)
	if err != nil {
		return out, err
	}
	var checks struct {
		Total int `json:"total_count"`
		Runs  []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			DetailsURL string `json:"details_url"`
			App        struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			} `json:"app"`
		} `json:"check_runs"`
	}
	// Matrix jobs regularly fill a page. Keep the read bounded, but do not
	// turn the first hundred successes into a permanent wait for page two.
	var checksErr error
	for page := 1; page <= 10; page++ {
		part := checks
		part.Runs = nil
		endpoint := base + "/commits/" + url.PathEscape(sha) + "/check-runs?per_page=100"
		if page > 1 {
			endpoint += "&page=" + strconv.Itoa(page)
		}
		if checksErr = c.repairRequest(ctx, http.MethodGet, endpoint, nil, &part); checksErr != nil {
			break
		}
		checks.Total = max(checks.Total, part.Total)
		checks.Runs = append(checks.Runs, part.Runs...)
		if len(checks.Runs) >= checks.Total || len(part.Runs) < 100 {
			break
		}
	}
	var statuses struct {
		Total    int    `json:"total_count"`
		State    string `json:"state"`
		Statuses []struct {
			Context   string `json:"context"`
			State     string `json:"state"`
			TargetURL string `json:"target_url"`
		} `json:"statuses"`
	}
	statusesErr := c.repairRequest(ctx, http.MethodGet, base+"/commits/"+url.PathEscape(sha)+"/status", nil, &statuses)
	out.Total = checks.Total + statuses.Total
	for _, r := range checks.Runs {
		app := r.App.Slug
		if app == "" {
			app = r.App.Name
		}
		add(RepairCheck{Kind: "check_run", Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, App: app, URL: r.DetailsURL})
	}
	for _, st := range statuses.Statuses {
		add(RepairCheck{Kind: "status", Name: st.Context, Status: "completed", Conclusion: st.State, URL: st.TargetURL})
	}
	out.Truncated = len(out.Checks) < out.Total
	if statusesErr == nil && statuses.Total > 0 && (statuses.State == "failure" || statuses.State == "error") {
		out.State = "failed"
		return out, nil
	}
	pending := checks.Total > len(checks.Runs) || statuses.Total > 0 && statuses.State != "success"
	passed := statuses.Total > 0 && statuses.State == "success"
	for _, r := range checks.Runs {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "failure", "timed_out", "action_required", "startup_failure":
			out.State = "failed"
			return out, nil
		case "success":
			passed = true
		case "neutral", "skipped":
		default:
			// A cancellation or missing outcome is not evidence of a fix.
			pending = true
		}
	}
	if checksErr != nil || statusesErr != nil {
		// Runner Apps already have Actions read, but may lack Checks or
		// Commit statuses read. A current failed run is enough to offer a
		// repair; incomplete coverage must never certify a successful fix.
		var runs struct {
			Runs []struct {
				WorkflowID int64  `json:"workflow_id"`
				HeadSHA    string `json:"head_sha"`
				Branch     string `json:"head_branch"`
				Event      string `json:"event"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"workflow_runs"`
		}
		runsErr := c.repairRequest(ctx, http.MethodGet, base+"/actions/runs?head_sha="+url.QueryEscape(sha)+"&per_page=100", nil, &runs)
		if runsErr == nil {
			// GitHub lists newest first. An older failed run of the same
			// workflow must not overrule its successful rerun.
			type key struct {
				workflow      int64
				branch, event string
			}
			seen := map[key]bool{}
			for _, run := range runs.Runs {
				k := key{run.WorkflowID, run.Branch, run.Event}
				if run.HeadSHA != sha || seen[k] {
					continue
				}
				seen[k] = true
				if run.Status == "completed" {
					switch run.Conclusion {
					case "failure", "timed_out", "action_required", "startup_failure":
						out.State = "failed"
						return out, nil
					}
				}
			}
		}
		return out, errors.Join(checksErr, statusesErr, runsErr)
	}
	if pending || !passed {
		out.State = "waiting"
		return out, nil
	}
	out.State = "passed"
	return out, nil
}

// RepairConflict is what GitHub currently says about one pull request's target
// branch and mergeability, with the base tip and merge base pinned for a later
// conflict repair. MergeableKnown is false while GitHub still reports null.
type RepairConflict struct {
	Number         int    `json:"number"`
	HeadSHA        string `json:"head_sha"`
	BaseRef        string `json:"base_ref"`
	BaseSHA        string `json:"base_sha"`
	MergeBaseSHA   string `json:"merge_base_sha,omitempty"`
	Mergeable      bool   `json:"mergeable"`
	MergeableKnown bool   `json:"mergeable_known"`
	MergeState     string `json:"merge_state,omitempty"`
}

// RepairConflictClient is the conflict-detection read a watcher uses. It is
// separate from RepairClient so a repair client without mergeability support
// still works.
type RepairConflictClient interface {
	RepairConflictEvidence(context.Context, string, int) (RepairConflict, error)
}

func (c *appClient) RepairConflictEvidence(ctx context.Context, repo string, number int) (RepairConflict, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return RepairConflict{}, err
	}
	var p struct {
		Number         int    `json:"number"`
		State          string `json:"state"`
		Mergeable      *bool  `json:"mergeable"`
		MergeableState string `json:"mergeable_state"`
		Draft          bool   `json:"draft"`
		Head           struct {
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/pulls/"+strconv.Itoa(number), nil, &p); err != nil {
		return RepairConflict{}, err
	}
	out := RepairConflict{Number: p.Number, HeadSHA: p.Head.SHA, BaseRef: p.Base.Ref, BaseSHA: p.Base.SHA, MergeState: p.MergeableState}
	if p.State == "closed" {
		return out, &RepairTargetChanged{Code: "closed", Message: "The pull request is closed or merged."}
	}
	if p.Number <= 0 || p.State != "open" || p.Head.Repo.FullName != repo || p.Draft || p.Head.SHA == "" || p.Base.SHA == "" {
		return out, fmt.Errorf("%w: an Eli conflict check needs an open, non-draft pull request in this repository", ErrRepairUnsupported)
	}
	if p.Mergeable != nil {
		out.MergeableKnown, out.Mergeable = true, *p.Mergeable
	}
	var compare struct {
		MergeBaseCommit struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := c.repairRequest(ctx, http.MethodGet, base+"/compare/"+url.PathEscape(p.Base.SHA)+"..."+url.PathEscape(p.Head.SHA), nil, &compare); err != nil {
		return out, err
	}
	out.MergeBaseSHA = compare.MergeBaseCommit.SHA
	return out, nil
}

// RepairDiscoveryClient is the extra read a repository-wide watch needs. It is
// separate from RepairClient so an older client can still repair one named pull
// request without pretending it can enumerate a repository.
type RepairDiscoveryClient interface {
	RepairOpenPulls(context.Context, string, int) ([]RepairPull, bool, error)
}

// RepairPullDiscoveryLimit bounds one repository-wide watch admission.
const RepairPullDiscoveryLimit = 500

func (c *appClient) RepairOpenPulls(ctx context.Context, repo string, limit int) ([]RepairPull, bool, error) {
	base, err := repairRepo(repo)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 || limit > RepairPullDiscoveryLimit {
		limit = RepairPullDiscoveryLimit
	}
	pages := (limit + 99) / 100
	out := []RepairPull{}
	for page := 1; page <= pages; page++ {
		var part []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			Draft   bool   `json:"draft"`
			HTMLURL string `json:"html_url"`
			Head    struct {
				Ref  string `json:"ref"`
				SHA  string `json:"sha"`
				Repo struct {
					FullName      string `json:"full_name"`
					DefaultBranch string `json:"default_branch"`
				} `json:"repo"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"base"`
		}
		endpoint := fmt.Sprintf("%s/pulls?state=open&per_page=100&page=%d", base, page)
		if err := c.repairRequest(ctx, http.MethodGet, endpoint, nil, &part); err != nil {
			return nil, false, err
		}
		for _, p := range part {
			out = append(out, RepairPull{
				Number: p.Number, Title: p.Title, Branch: p.Head.Ref, Base: p.Base.Ref,
				BaseSHA: p.Base.SHA, HeadSHA: p.Head.SHA, URL: p.HTMLURL,
				DefaultBranch: p.Head.Repo.DefaultBranch, Fork: p.Head.Repo.FullName != repo,
				Draft: p.Draft, Open: p.State == "open",
			})
			if len(out) >= limit {
				return out, len(part) == 100, nil
			}
		}
		if len(part) < 100 {
			return out, false, nil
		}
	}
	return out, true, nil
}

var _ RepairClient = (*appClient)(nil)

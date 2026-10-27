package controller

import (
	"context"
	"net/url"
	"regexp"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

const (
	// repairCaseText is the most of a name or title a case keeps. GitHub's own
	// limits are lower; this is the bound on what a hostile pull request can
	// make the controller store and a browser draw.
	repairCaseText = 200
	// repairCaseLink is the longest address a case keeps. A longer one is not a
	// GitHub page, and cutting it would leave a link that goes somewhere else.
	repairCaseLink = 500
)

// repairCase is what a repair was started for, in the words a person reading
// the attempt later needs: the pull request, the run, and the jobs that failed
// with the step each stopped at. It prefers the jobs the controller already
// holds, and falls back to GitHub for a failure this fleet never ran, so a
// repair of somebody else's runner's job still says what it was asked about.
// Both are a snapshot: the page shows the failure as it was when the person
// decided, not as GitHub reads after a re-run.
//
// It never fails. Retention removes old jobs and an offer can outlive them, so
// a case with no jobs is the pull request alone, which is still worth showing.
// Everything in it was written by someone who can open a pull request, so each
// text is clipped and each address is kept only if it is a plain web link.
func (c *Controller) repairCase(ctx context.Context, client github.RepairClient, repo string, pr github.RepairPull, runID int64) *store.EliRepairCase {
	out := &store.EliRepairCase{
		PullTitle: clipCaseText(pr.Title),
		PullURL:   safeCaseLink(pr.URL),
		Author:    clipCaseText(pr.Author),
		Branch:    clipCaseText(pr.Branch),
	}
	filter := store.JobFilter{Repos: []string{repo}, Conclusions: []string{"failure", "timed_out"}}
	switch {
	case runID > 0:
		filter.RunIDs = []int64{runID}
	case pr.HeadSHA != "":
		filter.HeadSHA = pr.HeadSHA
	default:
		return out
	}
	jobs, _, err := c.st.ListJobs(ctx, filter, store.Page{Limit: store.MaxEliRepairCaseJobs, Sort: "queued_at"})
	if err != nil {
		c.log.Warn("could not read the failed jobs for a repair's case", "repo", repo, "error", err)
		return out
	}
	if len(jobs) == 0 && client != nil {
		// Nothing on record: an older agent's run, a pruned one, or a job
		// GitHub's own runners ran. GitHub's report of the run is what remains.
		found, ferr := client.RepairFailedJobs(ctx, repo, runID, pr.HeadSHA)
		if ferr != nil {
			c.log.Warn("could not read the failed jobs of the run from GitHub for a repair's case", "repo", repo, "error", ferr)
			return out
		}
		if found == nil {
			return out
		}
		out.Workflow = clipCaseText(found.Workflow)
		out.RunNumber = int64(found.RunNumber)
		out.RunURL = safeCaseLink(found.RunURL)
		for _, j := range found.Jobs {
			if len(out.Jobs) == store.MaxEliRepairCaseJobs {
				break
			}
			job := store.EliRepairCaseJob{Name: clipCaseText(j.Name), URL: safeCaseLink(j.URL), DurationMS: j.CompletedAt.Sub(j.StartedAt).Milliseconds()}
			job.Step = clipCaseText(j.Step)
			out.Jobs = append(out.Jobs, job)
		}
		return out
	}
	for i, j := range jobs {
		if i == 0 {
			out.Workflow = clipCaseText(j.Workflow)
			out.RunNumber = j.RunNumber
			out.RunURL = runURLFromJobURL(j.HTMLURL)
		}
		job := store.EliRepairCaseJob{Name: clipCaseText(j.JobName), URL: safeCaseLink(j.HTMLURL), DurationMS: j.Duration().Milliseconds()}
		if step := j.FailedStep(); step != nil {
			job.Step = clipCaseText(step.Name)
		}
		out.Jobs = append(out.Jobs, job)
	}
	return out
}

// clipCaseText keeps the first repairCaseText characters of s. It counts runes,
// not bytes, so a title in any script is cut between characters and never in
// the middle of one.
func clipCaseText(s string) string {
	if utf8.RuneCountInString(s) <= repairCaseText {
		return s
	}
	r := []rune(s)
	return string(r[:repairCaseText])
}

// safeCaseLink returns raw if it is an address a page may link to, and "" if it
// is not. Only http and https with a host qualify: a pull request title or a
// job name can be anything, and the address that reaches a case is stored and
// drawn as a link, where a javascript: or data: address would run.
func safeCaseLink(raw string) string {
	if raw == "" || len(raw) > repairCaseLink {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

var runPath = regexp.MustCompile(`^(.*/actions/runs/[0-9]+)(?:/job/[0-9]+)?/?$`)

// runURLFromJobURL is the page of the run a job belongs to, from the job's own
// address: GitHub's job page is the run's page and /job/<id>. It is taken from
// the address rather than built from the repository so that a GitHub
// Enterprise Server job gives a link on its own host and not on github.com. An
// address that is not a job's, or not a safe link at all, gives "".
func runURLFromJobURL(jobURL string) string {
	if safeCaseLink(jobURL) == "" {
		return ""
	}
	u, err := url.Parse(jobURL)
	if err != nil {
		return ""
	}
	m := runPath.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: m[1]}).String()
}

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

type EliReadiness struct {
	Ready      bool   `json:"ready"`
	Repo       string `json:"repo"`
	PullNumber int    `json:"pull_number"`
	HeadSHA    string `json:"head_sha"`
	// Pull is the pull request in its own words. It is set only after the
	// caller's linked GitHub account has been shown to write to the repository,
	// so a person who may not see the repository is never told what is in it.
	Pull           *EliReadinessPull `json:"pull,omitempty"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	AllowWorkflows bool              `json:"allow_workflows"`
	Remaining      int               `json:"remaining"`
	ResetAt        *time.Time        `json:"reset_at,omitempty"`
	// PolicyMode is how the repository's policy lets Eli help: automatic,
	// ask_first or manual. A page that offers to start a repair needs it to say
	// whether the repository would have started one itself. It is set once
	// readiness reaches the policy check, and not before.
	PolicyMode string `json:"policy_mode,omitempty"`
	// Blockers is what stands in the way, as it always has been. When the target
	// has changed (fixed, closed, superseded) it is that alone, which is the
	// answer to what the person asked; Checks still say what each check found.
	Blockers []EliBlocker `json:"blockers"`
	// Checks is every check readiness makes, in the order it makes them, so a
	// page can show what passed as well as what did not.
	Checks []EliCheck `json:"checks"`
	// FailedJobs is the failing run's failed jobs, read from GitHub when the
	// caller may see the repository: a run this fleet ran is on record and the
	// page reads that, while a run it never ran exists only on GitHub. Like the
	// pull request they sit behind the write-access check, and a check that
	// stopped earlier leaves them out. Their absence says nothing was found or
	// nothing could be read; it never says nothing failed.
	FailedJobs []EliReadinessJob `json:"failed_jobs,omitempty"`
}

// EliReadinessJob is one failed job of the run a readiness answer is about, as
// GitHub reports it. The id is GitHub's own, which is what a read of its error
// output is asked with. Names and steps are workflow-author text, clipped; the
// address is kept only when it is a plain web link.
type EliReadinessJob struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	Step        string             `json:"step,omitempty"`
	StepNumber  int                `json:"step_number,omitempty"`
	URL         string             `json:"url,omitempty"`
	Conclusion  string             `json:"conclusion"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Steps       []EliReadinessStep `json:"steps,omitempty"`
}

// EliReadinessStep is one step of a failed job, as GitHub reported it ended.
type EliReadinessStep struct {
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
type EliBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EliReadinessPull is the pull request a readiness answer is about.
type EliReadinessPull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	URL    string `json:"url"`
	Open   bool   `json:"open"`
}

// EliCheck is one thing readiness looks at and what it found. Status is
// EliCheckOK, EliCheckBlocked, or EliCheckUnchecked for a check readiness never
// reached because an earlier one stopped it: a check that was not made is not a
// check that passed, and the page must not draw it as one.
type EliCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// Blocker is the code of the blocker that stopped a blocked check. A check
	// says where readiness stopped and a blocker says why, and the two are not
	// the same thing: GitHub not answering is folded into whichever check it was
	// making. When the target has changed the answer's Blockers hold that change
	// alone, and a check blocked before it was found still names what stopped it.
	Blocker string `json:"blocker,omitempty"`
}

const (
	EliCheckOK        = "ok"
	EliCheckBlocked   = "blocked"
	EliCheckUnchecked = "unchecked"
)

// eliCheckCodes are the checks in the order readiness makes them, which is the
// order a page lists them in.
var eliCheckCodes = []string{"identity", "provider", "installation", "access", "policy", "budget", "pull", "checks", "permissions"}

// eliChecks records what readiness found as it goes. Anything it never reached
// is reported as not checked, whichever way the function returned.
type eliChecks map[string]EliCheck

func (c eliChecks) set(code, status, message string) {
	c[code] = EliCheck{Code: code, Status: status, Message: message}
}

// block marks a check blocked, and says by which blocker.
func (c eliChecks) block(code, blocker, message string) {
	c[code] = EliCheck{Code: code, Status: EliCheckBlocked, Message: message, Blocker: blocker}
}

func (c eliChecks) list() []EliCheck {
	out := make([]EliCheck, 0, len(eliCheckCodes))
	for _, code := range eliCheckCodes {
		check, ok := c[code]
		if !ok {
			check = EliCheck{Code: code, Status: EliCheckUnchecked, Message: "Not checked yet."}
		}
		out = append(out, check)
	}
	return out
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// repairMode is the same distinction in the words the API uses.
func repairMode(p *store.EliRepairPolicy) string {
	switch {
	case p.Automatic:
		return "automatic"
	case p.AskFirst:
		return "ask_first"
	}
	return "manual"
}

// repairModeWords names how a repository's policy lets Eli help, for a finding.
func repairModeWords(p *store.EliRepairPolicy) string {
	switch {
	case p.Automatic:
		return "Automatic repair"
	case p.AskFirst:
		return "Ask me first"
	}
	return "Manual repair"
}

func (c *Controller) eliIdentity(ctx context.Context, user string) (*store.EliIdentity, error) {
	ids, err := c.st.EliIdentities(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if id.UserID == user && id.Confirmed {
			return &id, nil
		}
	}
	return nil, store.ErrNotFound
}

// eliFailedJobs names the failed jobs of the run a readiness answer is about,
// read from GitHub: by the run itself when one was named, else by the failing
// run at the pull request's head. It is what lets a repair be asked for a job
// this fleet never ran, whose failure Zoomies holds no record of.
//
// It never blocks readiness: a GitHub that cannot answer leaves the field
// empty, and the page says it has nothing to show rather than that nothing
// failed.
func (c *Controller) eliFailedJobs(ctx context.Context, client github.RepairClient, repo string, run int64, headSHA string) []EliReadinessJob {
	found, err := client.RepairFailedJobs(ctx, repo, run, headSHA)
	if err != nil {
		c.log.Warn("could not read the failed jobs of the run for readiness", "repo", repo, "run", run, "error", err)
		return nil
	}
	if found == nil {
		return nil
	}
	out := make([]EliReadinessJob, 0, min(len(found.Jobs), store.MaxEliRepairCaseJobs))
	for _, j := range found.Jobs {
		if len(out) == store.MaxEliRepairCaseJobs {
			break
		}
		job := EliReadinessJob{ID: j.ID, Name: clipCaseText(j.Name), Conclusion: j.Conclusion, URL: safeCaseLink(j.URL)}
		if j.Step != "" {
			job.Step, job.StepNumber = clipCaseText(j.Step), j.StepNumber
		}
		if !j.StartedAt.IsZero() {
			started := j.StartedAt
			job.StartedAt = &started
		}
		if !j.CompletedAt.IsZero() {
			completed := j.CompletedAt
			job.CompletedAt = &completed
		}
		for _, s := range j.Steps {
			step := EliReadinessStep{Number: s.Number, Name: clipCaseText(s.Name), Conclusion: s.Conclusion}
			if !s.StartedAt.IsZero() {
				started := s.StartedAt
				step.StartedAt = &started
			}
			if !s.CompletedAt.IsZero() {
				completed := s.CompletedAt
				step.CompletedAt = &completed
			}
			job.Steps = append(job.Steps, step)
		}
		out = append(out, job)
	}
	return out
}
func (c *Controller) EliReadiness(ctx context.Context, user, repo string, pull int, run int64) (out EliReadiness) {
	out = EliReadiness{Repo: repo, PullNumber: pull, Blockers: []EliBlocker{}}
	checks := eliChecks{}
	defer func() { out.Checks = checks.list() }()
	// block records a blocker exactly as before and marks the check it belongs to.
	block := func(check, code, message string) {
		out.Blockers = append(out.Blockers, EliBlocker{code, message})
		checks.block(check, code, message)
	}
	id, err := c.eliIdentity(ctx, user)
	if err != nil {
		block("identity", "identity", "Link and confirm your GitHub account in repair setup.")
	} else {
		checks.set("identity", EliCheckOK, "@"+id.GitHubLogin+" is linked and has allowed repairs.")
	}
	provider, err := c.personalChatProvider(ctx, "", user)
	if err != nil {
		block("provider", "provider", "Add an enabled personal model provider in Eli settings.")
	} else {
		out.Provider = provider.Name
		out.Model = provider.Model
		checks.set("provider", EliCheckOK, provider.Name+" is enabled. Eli uses your provider, never a shared one.")
	}
	inst, err := c.st.FindInstallationByTarget(ctx, repo)
	if err != nil {
		block("installation", "installation", "Connect the GitHub installation that serves this repository.")
		return out
	}
	checks.set("installation", EliCheckOK, repo+" is served by the "+inst.Target+" GitHub installation.")
	if id == nil {
		return out
	}
	raw, err := c.ClientFor(ctx, inst.ID)
	if err != nil {
		block("installation", "github", "The GitHub installation is unavailable.")
		return out
	}
	client, ok := raw.(github.RepairClient)
	if !ok {
		block("installation", "github", "This installation cannot repair PRs.")
		return out
	}
	actor, err := client.RepairActor(ctx, repo, id.GitHubLogin)
	if err != nil || !actor.CanWrite || actor.ID != id.GitHubUserID {
		block("access", "access", "Your linked GitHub account needs write access to this repository.")
		return out
	}
	checks.set("access", EliCheckOK, "@"+id.GitHubLogin+" can write to "+repo+".")
	// A failure named by its run has its failed jobs read from GitHub straight
	// away: they are the question's own subject, and a failure this fleet never
	// ran is on record nowhere else. A pasted pull request says which commit it
	// is once the pull request is found, and is read there.
	if run > 0 {
		out.FailedJobs = c.eliFailedJobs(ctx, client, repo, run, "")
	}
	policy, err := c.repairPolicy(ctx, repo, inst.ID)
	if err != nil {
		block("policy", "policy", "An administrator needs to enable repairs for this repository.")
	} else {
		out.AllowWorkflows = policy.AllowWorkflows
		out.PolicyMode = repairMode(policy)
		checks.set("policy", EliCheckOK, repairModeWords(policy)+" is on for this repository. Workflow file edits are "+map[bool]string{true: "on", false: "off"}[policy.AllowWorkflows]+".")
		used, personal, reset, e := c.st.EliRepairBudget(ctx, repo, user)
		if e != nil {
			block("budget", "budget_unavailable", "Repair allowance could not be checked. Try again.")
		} else {
			out.Remaining = max(0, min(policy.DailyLimit-used, store.EliPersonDailyLimit-personal))
			out.ResetAt = reset
			if out.Remaining == 0 {
				block("budget", "budget", "The repair allowance is exhausted. Wait until an attempt leaves the 24-hour window.")
			} else {
				checks.set("budget", EliCheckOK, plural(out.Remaining, "attempt")+" left in the 24-hour window.")
			}
		}
	}
	checked := c.st.Now()
	var pr github.RepairPull
	if run > 0 {
		pr, err = client.RepairPullForRun(ctx, repo, run)
	} else if pull > 0 {
		pr, err = client.RepairPull(ctx, repo, pull)
	} else {
		block("pull", "pull", "Choose an open pull request or paste its GitHub URL.")
		return out
	}
	out.PullNumber = pr.Number
	out.HeadSHA = pr.HeadSHA
	if pr.Number > 0 {
		out.Pull = &EliReadinessPull{Number: pr.Number, Title: pr.Title, Author: pr.Author, Branch: pr.Branch, Base: pr.Base, URL: pr.URL, Open: pr.Open}
	}
	if err != nil {
		var changed *github.RepairTargetChanged
		if errors.As(err, &changed) {
			// A changed target is the answer to the person's original question,
			// even when their provider or repository setup is also incomplete.
			out.Blockers = []EliBlocker{{Code: changed.Code, Message: changed.Message}}
			checks.block("pull", changed.Code, changed.Message)
			if run > 0 {
				_ = c.st.RecordJobAttentionRun(ctx, repo, run, changed.Code, checked, true)
			}
		} else if errors.Is(err, github.ErrRepairUnsupported) {
			message := strings.TrimPrefix(err.Error(), github.ErrRepairUnsupported.Error()+": ")
			if message == github.ErrRepairUnsupported.Error() {
				message = "Choose an open PR in this repository at its current head; forks cannot be repaired."
			}
			block("pull", "pull", message)
		} else {
			block("pull", "github", "GitHub could not resolve this repair target. Try checking readiness again.")
		}
		return out
	}
	checks.set("pull", EliCheckOK, fmt.Sprintf("#%d is open on %s and still at %s.", pr.Number, pr.Branch, shortSHA(pr.HeadSHA)))
	if run == 0 {
		out.FailedJobs = c.eliFailedJobs(ctx, client, repo, 0, pr.HeadSHA)
	}
	state, err := client.RepairChecks(ctx, repo, pr.HeadSHA)
	if err != nil {
		block("checks", "github", "GitHub could not check whether this failure still needs fixing. Try checking readiness again.")
		return out
	}
	if state == "passed" {
		if run > 0 {
			_ = c.st.RecordJobAttentionRun(ctx, repo, run, "fixed", checked, true)
		}
		out.Blockers = []EliBlocker{{Code: "fixed", Message: "The latest checks on this PR have passed. This failure has already been fixed; no repair is needed."}}
		checks.block("checks", out.Blockers[0].Code, out.Blockers[0].Message)
		return out
	}
	if run > 0 {
		_ = c.st.RecordJobAttentionRun(ctx, repo, run, "", checked, state == "failed")
	}
	if state != "failed" {
		block("checks", "checks_pending", "No current failed check is available to repair. Checks may still be running, cancelled or skipped. Check the PR on GitHub, then check readiness again.")
	} else {
		checks.set("checks", EliCheckOK, "The checks on "+shortSHA(pr.HeadSHA)+" are failing.")
	}
	info, err := raw.Probe(ctx)
	if err != nil {
		block("permissions", "permissions", "GitHub App permissions could not be checked.")
	} else {
		var missing []string
		for _, permission := range []string{"contents", "pull_requests", "actions"} {
			value := info.Permissions[permission]
			if value != "write" && (permission != "actions" || value != "read") {
				message := "Update the GitHub App permission: " + permission + map[bool]string{true: " read", false: " write"}[permission == "actions"]
				out.Blockers = append(out.Blockers, EliBlocker{"permissions", message})
				missing = append(missing, message)
			}
		}
		if len(missing) > 0 {
			checks.block("permissions", "permissions", strings.Join(missing, " "))
		} else {
			checks.set("permissions", EliCheckOK, "Contents, pull requests and actions are granted.")
		}
	}
	out.Ready = len(out.Blockers) == 0
	return out
}

// Offers contain repository data, so even administrators must have current
// access through their linked GitHub account before seeing personal offers.
func (c *Controller) EliOpportunities(ctx context.Context, user string) ([]store.EliRepairOpportunity, error) {
	out := []store.EliRepairOpportunity{}
	id, err := c.eliIdentity(ctx, user)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := c.st.EliRepairOpportunities(ctx, user)
	if err != nil {
		return nil, err
	}
	access := map[string]bool{}
	for _, o := range rows {
		key := o.InstallationID + ":" + o.Repo
		allowed, seen := access[key]
		if !seen {
			raw, err := c.ClientFor(ctx, o.InstallationID)
			if err == nil {
				if client, ok := raw.(github.RepairClient); ok {
					actor, e := client.RepairActor(ctx, o.Repo, id.GitHubLogin)
					allowed = e == nil && actor.ID == id.GitHubUserID && actor.CanWrite
				}
			}
			access[key] = allowed
		}
		if allowed {
			out = append(out, o)
		}
	}
	return out, nil
}
func (c *Controller) ActEliOpportunity(ctx context.Context, user, id, action string, admin bool) (*store.EliRepair, error) {
	rows, err := c.EliOpportunities(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, o := range rows {
		if o.ID == id {
			if action == "dismiss" {
				return nil, c.st.DismissEliOpportunity(ctx, id, user)
			}
			if action != "accept" {
				return nil, store.ErrConflict
			}
			row, err := c.requestPinnedEliRepair(ctx, user, o.Repo, o.PullNumber, "", o.HeadSHA, "offer:"+o.ID, o.RunID, admin)
			if err != nil {
				return nil, err
			}
			if err = c.st.SetEliOpportunityState(ctx, id, "accepted"); err != nil {
				return nil, err
			}
			return row, nil
		}
	}
	return nil, store.ErrNotFound
}

// A bounded reconciliation catches jobs learnt through polling and failures
// that existed before setup. It runs independently of opening the UI.
func (c *Controller) reconcileEliOpportunities(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	policies, err := c.st.EliRepairPolicies(ctx)
	if err != nil {
		return
	}
	repos := []string{}
	byRepo := map[string]store.EliRepairPolicy{}
	for _, p := range policies {
		if p.Enabled && p.AskFirst {
			repos = append(repos, p.Repo)
			byRepo[p.Repo] = p
		}
	}
	if len(repos) == 0 {
		return
	}
	since := c.st.Now().Add(-24 * time.Hour)
	jobs, _, err := c.st.ListJobs(ctx, store.JobFilter{Repos: repos, Conclusions: []string{"failure", "timed_out"}, Since: &since}, store.Page{Limit: 50, Sort: "queued_at", Desc: true})
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		key := fmt.Sprintf("%s:%d", j.Repo, j.GitHubRunID)
		if seen[key] || j.GitHubRunID == 0 || j.FaultKind != "" {
			continue
		}
		seen[key] = true
		p := byRepo[j.Repo]
		raw, err := c.ClientFor(ctx, p.InstallationID)
		if err != nil {
			continue
		}
		client, ok := raw.(github.RepairClient)
		if !ok {
			continue
		}
		pr, err := client.RepairPullForRun(ctx, j.Repo, j.GitHubRunID)
		if err != nil {
			continue
		}
		own, e := c.st.IsEliRepairCommit(ctx, j.Repo, pr.HeadSHA)
		if e != nil || own {
			continue
		}
		state, e := client.RepairChecks(ctx, j.Repo, pr.HeadSHA)
		if e != nil || state == "passed" {
			continue
		}
		_ = c.st.OfferEliRepair(ctx, store.EliRepairOpportunity{Repo: j.Repo, InstallationID: p.InstallationID, PullNumber: pr.Number, HeadSHA: pr.HeadSHA, RunID: j.GitHubRunID, Title: pr.Title})
	}
	// Dismissals remain tied to their original head, never to a future commit.
	rows, _ := c.st.EliRepairOpportunities(ctx, "")
	for _, o := range rows {
		raw, e := c.ClientFor(ctx, o.InstallationID)
		if e != nil {
			continue
		}
		client, ok := raw.(github.RepairClient)
		if !ok {
			continue
		}
		pr, e := client.RepairPull(ctx, o.Repo, o.PullNumber)
		if errors.Is(e, github.ErrRepairUnsupported) || e == nil && pr.HeadSHA != o.HeadSHA {
			_ = c.st.SetEliOpportunityState(ctx, o.ID, "superseded")
			continue
		}
		if e == nil {
			state, e := client.RepairChecks(ctx, o.Repo, o.HeadSHA)
			if e == nil && state == "passed" {
				_ = c.st.SetEliOpportunityState(ctx, o.ID, "superseded")
			}
		}
	}
}

func normaliseRepairRepo(repo string) string { return strings.TrimSpace(repo) }

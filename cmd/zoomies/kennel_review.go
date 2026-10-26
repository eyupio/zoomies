package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type kennelReviewItem struct {
	Version            int                     `json:"version"`
	KennelRepositoryID string                  `json:"kennel_repository_id"`
	Repository         string                  `json:"repository"`
	PullNumber         int                     `json:"pull_number"`
	Open               bool                    `json:"open"`
	BaseBranch         string                  `json:"base_branch"`
	BaseSHA            string                  `json:"base_sha"`
	HeadSHA            string                  `json:"head_sha"`
	ReadAt             string                  `json:"read_at"`
	FilesCut           bool                    `json:"files_cut"`
	Complete           bool                    `json:"complete"`
	Introduced         []kennelReviewFinding   `json:"introduced"`
	Resolved           int                     `json:"resolved"`
	Reviewed           []string                `json:"reviewed"`
	RuleChanges        []string                `json:"rule_changes"`
	NotReviewed        []kennelReviewUnchecked `json:"not_reviewed"`
}

type kennelReviewFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Job      int    `json:"job"`
	Line     int    `json:"line"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
	HeadSHA  string `json:"head_sha"`
}

type kennelReviewUnchecked struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func kennelReview(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies kennel review <repository-id> --pull N",
		"Review what a pull request changes in the repository's workflows, with Kennel Club's own checks. It reports only what the head has that the merge base did not, names every changed file it did not judge, and lists changed instruction files without applying them. It reads the pull request from GitHub now, at two pinned commits, and executes nothing, calls no model, posts nothing and keeps nothing. Requires operator, kennel:read and optimisations:read.")
	cf := registerClientFlags(fs, true)
	pull := fs.Int("pull", 0, "the pull request's number (required)")
	fs.example("zoomies kennel review kcr_01abc --pull 42", "zoomies kennel review kcr_01abc --pull 42 --output json")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a Kennel Club repository ID")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(id, "kcr_") || *pull < 1 {
		return usagef("kennel review", "supply a repository ID starting kcr_ and --pull with the pull request's number")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	var out kennelReviewItem
	raw, err := client.get(ctx, "/kennel/repositories/"+url.PathEscape(id)+"/pull-reviews/"+strconv.Itoa(*pull), nil, &out)
	if err != nil {
		return err
	}
	if out.Version != 1 || out.KennelRepositoryID != id || out.PullNumber != *pull || len(out.HeadSHA) != 40 || len(out.BaseSHA) != 40 {
		return errors.New("this controller returned an unsupported or inconsistent pull request review; check the controller version")
	}
	if p.structured() {
		return p.emit(raw)
	}
	state := "complete for the files it covers"
	if !out.Complete {
		state = "incomplete: " + strconv.Itoa(len(out.NotReviewed)) + " changed file(s) were not judged, or the file list was cut"
	}
	p.keyValues([][2]string{
		{"repository", plain(out.Repository)},
		{"pull request", "#" + strconv.Itoa(out.PullNumber)},
		{"bound to", "head " + plain(out.HeadSHA) + " against merge base " + plain(out.BaseSHA)},
		{"read", plain(out.ReadAt)},
		{"review", state},
		{"introduced", strconv.Itoa(len(out.Introduced)) + " finding(s) the change added; " + strconv.Itoa(out.Resolved) + " it removed"},
	})
	if len(out.Introduced) > 0 {
		rows := make([][]string, 0, len(out.Introduced))
		for _, f := range out.Introduced {
			rows = append(rows, []string{plain(f.Severity), plain(f.Code), plain(f.Path) + ":" + strconv.Itoa(f.Line), plain(f.Title), plain(f.Fix)})
		}
		p.table([]string{"severity", "check", "where (head)", "finding", "change"}, rows)
	}
	for _, path := range out.RuleChanges {
		p.note("Changed instruction file, reported and not applied: %s. This review used the controller's built-in rules.", plain(path))
	}
	for _, n := range out.NotReviewed {
		p.note("Not reviewed: %s (%s)", plain(n.Path), plain(n.Reason))
	}
	if len(out.Introduced) == 0 {
		p.note("No new finding in the %d workflow file(s) reviewed. That is not an all-clear for the rest of the change.", len(out.Reviewed))
	}
	return nil
}

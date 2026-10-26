package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/kennel/offline"
)

type kennelReviewReader struct {
	github.Client
	source *github.KennelReviewSource
	quota  *github.RateLimit
	err    error
	calls  int
	pulls  []int
}

func (f *kennelReviewReader) RateLimit(context.Context) (*github.RateLimit, error) {
	return f.quota, nil
}
func (f *kennelReviewReader) ReadKennelReviewSource(_ context.Context, _ string, pull int) (*github.KennelReviewSource, error) {
	f.calls++
	f.pulls = append(f.pulls, pull)
	return f.source, f.err
}

const (
	reviewBaseWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    steps:\n      - run: make\n"
	reviewHeadWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"
)

func kennelReviewFixture(t *testing.T) (*harness, *kennelReviewReader, string) {
	t.Helper()
	h, inst, reader, req := kennelFixFixture(t)
	path := ".github/workflows/ci.yml"
	rr := &kennelReviewReader{
		Client: reader.Client,
		quota:  reader.quota,
		source: &github.KennelReviewSource{
			Version: "kennel-review-source-v1", Repository: inst.Target, Number: 9, Open: true, BaseBranch: "main",
			BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40),
			Changed: []string{"README.md", path},
			Base:    []offline.ReviewFile{{Path: path, Data: []byte(reviewBaseWorkflow)}},
			Head:    []offline.ReviewFile{{Path: path, Data: []byte(reviewHeadWorkflow)}},
		},
	}
	h.c.clients.entries[inst.ID] = &clientEntry{client: rr, updatedAt: inst.UpdatedAt.Truncate(time.Millisecond)}
	h.c.UpdateConfig(func(c *config.Config) { c.Kennel.WorkflowChecks = true })
	return h, rr, req.KennelRepositoryID
}

func TestReviewPullRequestCIReportsWhatTheChangeIntroducedAtTheCommitsItWasReadAt(t *testing.T) {
	h, rr, id := kennelReviewFixture(t)
	got, err := h.c.ReviewPullRequestCI(h.ctx, id, 9)
	if err != nil {
		t.Fatal(err)
	}
	if rr.calls != 1 || rr.pulls[0] != 9 {
		t.Fatalf("read %d times for %v", rr.calls, rr.pulls)
	}
	if got.PullNumber != 9 || got.BaseSHA != strings.Repeat("a", 40) || got.HeadSHA != strings.Repeat("b", 40) || !got.Open || got.ReadAt.IsZero() {
		t.Errorf("report = %+v", got)
	}
	if len(got.Review.Introduced) != 1 || got.Review.Introduced[0].Code != "ci.no_timeout" || !got.Review.Complete {
		t.Errorf("review = %+v, want the missing timeout the change introduced", got.Review)
	}
	if len(got.Review.NotReviewed) != 1 || got.Review.NotReviewed[0].Path != "README.md" {
		t.Errorf("not reviewed = %v, want the README named", got.Review.NotReviewed)
	}
}

// The checks the operator turned off are off for a review too, or the review
// and the repository's own page would disagree about the same file.
func TestReviewPullRequestCIHonoursTheChecksTheOperatorTurnedOff(t *testing.T) {
	h, _, id := kennelReviewFixture(t)
	h.c.UpdateConfig(func(c *config.Config) { c.Kennel.DisabledChecks = []string{"ci.no_timeout"} })
	got, err := h.c.ReviewPullRequestCI(h.ctx, id, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Review.Introduced) != 0 {
		t.Fatalf("introduced = %+v", got.Review.Introduced)
	}
}

func TestReviewPullRequestCIReadsNothingWhenThePolicyOrTheRepositoryForbidsIt(t *testing.T) {
	h, rr, id := kennelReviewFixture(t)
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = false })
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.Is(err, ErrOptimisationPolicy) {
		t.Fatalf("err = %v, want the source-read policy to refuse", err)
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Optimisation.SourceReads = true; c.Kennel.Enabled = false })
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.Is(err, ErrKennelReview) {
		t.Fatalf("err = %v, want Kennel Club off to refuse", err)
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Kennel.Enabled, c.Kennel.WorkflowChecks = true, false })
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.Is(err, ErrKennelReview) {
		t.Fatalf("err = %v, want workflow checks off to refuse rather than read as clear", err)
	}
	h.c.UpdateConfig(func(c *config.Config) { c.Kennel.WorkflowChecks = true })
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 0); !errors.Is(err, ErrKennelReview) {
		t.Fatalf("err = %v, want pull request 0 to be refused", err)
	}
	if rr.calls != 0 {
		t.Errorf("GitHub was read %d times", rr.calls)
	}
}

func TestReviewPullRequestCIWaitsWhenTheInstallationsQuotaIsLow(t *testing.T) {
	h, rr, id := kennelReviewFixture(t)
	rr.quota = &github.RateLimit{Limit: 5000, Remaining: 2500 + github.KennelReviewMaximumRequests - 1, ResetAt: h.c.Now().Add(10 * time.Minute)}
	var admission *PRDelayAdmissionError
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.As(err, &admission) {
		t.Fatalf("err = %v, want an admission refusal that says when to come back", err)
	}
	if rr.calls != 0 {
		t.Errorf("GitHub was read %d times after the quota was refused", rr.calls)
	}
}

func TestReviewPullRequestCIPassesGitHubsRefusalOnAndRefusesAnotherRepositorysRead(t *testing.T) {
	h, rr, id := kennelReviewFixture(t)
	rr.err = github.ErrSetupCacheChanged
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.Is(err, github.ErrSetupCacheChanged) {
		t.Fatalf("err = %v", err)
	}
	rr.err = nil
	rr.source.Repository = "someone/else"
	if _, err := h.c.ReviewPullRequestCI(h.ctx, id, 9); !errors.Is(err, ErrKennelReview) {
		t.Fatalf("err = %v, want a source for another repository refused", err)
	}
}

func TestAPullRequestWithMoreFilesThanWereListedIsNeverReportedComplete(t *testing.T) {
	h, rr, id := kennelReviewFixture(t)
	rr.source.FilesCut = true
	got, err := h.c.ReviewPullRequestCI(h.ctx, id, 9)
	if err != nil {
		t.Fatal(err)
	}
	if got.Review.Complete || !got.FilesCut {
		t.Errorf("report = %+v", got)
	}
}

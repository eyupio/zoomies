package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/kennel/offline"
	"github.com/eyupio/zoomies/internal/store"
)

// KennelReviewView is a CI-focused review of one pull request, as the route
// renders it. Every path, title and sentence in it is either the evaluator's
// own or a path git named; the pull request's text appears nowhere in it.
type KennelReviewView struct {
	Version            int                     `json:"version"`
	KennelRepositoryID string                  `json:"kennel_repository_id"`
	Repository         string                  `json:"repository"`
	PullNumber         int                     `json:"pull_number"`
	Open               bool                    `json:"open"`
	BaseBranch         string                  `json:"base_branch"`
	BaseSHA            string                  `json:"base_sha"`
	HeadSHA            string                  `json:"head_sha"`
	ReadAt             time.Time               `json:"read_at"`
	FilesCut           bool                    `json:"files_cut"`
	Complete           bool                    `json:"complete"`
	Introduced         []offline.ReviewFinding `json:"introduced"`
	Resolved           int                     `json:"resolved"`
	Reviewed           []string                `json:"reviewed"`
	RuleChanges        []string                `json:"rule_changes"`
	NotReviewed        []offline.NotReviewed   `json:"not_reviewed"`
}

func kennelReviewView(r *controller.KennelReviewReport) KennelReviewView {
	return KennelReviewView{Version: r.Review.Version, KennelRepositoryID: r.KennelRepositoryID, Repository: r.Repository, PullNumber: r.PullNumber,
		Open: r.Open, BaseBranch: r.BaseBranch, BaseSHA: r.BaseSHA, HeadSHA: r.HeadSHA, ReadAt: r.ReadAt, FilesCut: r.FilesCut,
		Complete: r.Review.Complete, Introduced: r.Review.Introduced, Resolved: r.Review.Resolved, Reviewed: r.Review.Reviewed,
		RuleChanges: r.Review.RuleChanges, NotReviewed: r.Review.NotReviewed}
}

// handleReviewKennelPullRequest answers GET /api/v1/kennel/repositories/{id}/pull-reviews/{number}.
// It reads the pull request from GitHub now, at two pinned commits, and keeps
// nothing: a later push is a different review.
func (s *Server) handleReviewKennelPullRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	number, err := strconv.Atoi(chiURLParam(r, "number"))
	if err != nil || number < 1 || number > 1<<31-1 {
		badRequest(w, "Supply the pull request's number as a positive whole number.")
		return
	}
	out, err := s.ctrl.ReviewPullRequestCI(r.Context(), chiURLParam(r, "id"), number)
	switch {
	case errors.Is(err, controller.ErrOptimisationPolicy):
		forbidden(w, "Reading pull request source is off. An administrator turns on optimisation.source_reads in Settings.")
	case errors.Is(err, controller.ErrKennelReview):
		conflict(w, err.Error())
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "There is no such repository in Kennel Club, or the installation that read it is gone.")
	case err != nil:
		s.optimisationError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, kennelReviewView(out))
	}
}

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eyupio/zoomies/internal/controller"
)

// The migration endpoints: what moving a repository's workflows onto this
// fleet would change, and then doing it. They are two halves of one thing,
// deliberately shaped like /pools/validate and /pools: a plan is computed and
// shown, and only a second, explicit call writes anything.
//
// The work itself -- reading workflows, proposing a mapping, opening pull
// requests -- is the controller's migration service. These handlers decode a
// request, refuse the shapes that could never be meant, and render what the
// service answers, which is all a transport should do.

type (
	migrationPlanResponse  = controller.MigrationPlan
	migrationApplyResponse = controller.MigrationOutcome
	migrationResult        = controller.MigrationResult
)

// handleMigrationPlan answers POST /api/v1/migrations/plan. It creates nothing.
func (s *Server) handleMigrationPlan(w http.ResponseWriter, r *http.Request) {
	var req controller.MigrationPlanRequest
	if !decode(w, r, &req) {
		return
	}
	if !installationNamed(w, req.InstallationID) {
		return
	}
	plan, err := s.ctrl.PlanMigration(r.Context(), req)
	if err != nil {
		s.migrationFail(w, r, "planning the migration", err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// handleMigrationApply answers POST /api/v1/migrations/pull-requests.
func (s *Server) handleMigrationApply(w http.ResponseWriter, r *http.Request) {
	var req controller.MigrationApplyRequest
	if !decode(w, r, &req) {
		return
	}
	if !installationNamed(w, req.InstallationID) {
		return
	}
	if len(req.Repos) == 0 {
		unprocessable(w, "name the repositories to migrate; this endpoint will not touch every repository an App can see by default",
			[]fieldError{{"repos", "at least one repository is required"}})
		return
	}
	if len(req.Repos) > controller.MaxApplyRepos {
		unprocessable(w, fmt.Sprintf("that is %d repositories; %d is the most one call will open pull requests on, so that a mistake is %d pull requests to close rather than an organisation-wide one",
			len(req.Repos), controller.MaxApplyRepos, controller.MaxApplyRepos),
			[]fieldError{{"repos", fmt.Sprintf("at most %d repositories per call", controller.MaxApplyRepos)}})
		return
	}

	out, err := s.ctrl.ApplyMigration(r.Context(), req)
	if err != nil {
		s.migrationFail(w, r, "opening the pull requests", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "migration.pull_requests", "installation", strings.TrimSpace(req.InstallationID), map[string]any{
		"repos": len(out.Results), "opened": out.Opened, "failed": out.Failed, "branch": out.Branch,
	})
	writeJSON(w, http.StatusOK, out)
}

// installationNamed refuses a request that names no installation: a migration
// reads and writes repositories through one GitHub App, and there is nothing
// sensible to do without knowing which.
func installationNamed(w http.ResponseWriter, id string) bool {
	if strings.TrimSpace(id) != "" {
		return true
	}
	unprocessable(w, "name the installation to migrate: a migration reads and writes repositories through one GitHub App",
		[]fieldError{{"installation_id", "an installation is required"}})
	return false
}

// migrationFail renders the service's refusals as the 422s they are, and
// everything else the way any GitHub-backed call fails.
func (s *Server) migrationFail(w http.ResponseWriter, r *http.Request, doing string, err error) {
	switch {
	case errors.Is(err, controller.ErrNoMigrationPool):
		unprocessable(w, err.Error()+". Create one on the Pools page first, then come back.",
			[]fieldError{{"installation_id", "no enabled pool belongs to this installation"}})
	case errors.Is(err, controller.ErrNothingMapped):
		unprocessable(w, err.Error(), []fieldError{{"mapping", "map at least one label, such as ubuntu-latest, to a pool, or send an override with a runs-on value"}})
	case errors.Is(err, controller.ErrBadOverride):
		unprocessable(w, err.Error(), []fieldError{{"overrides", "each override needs a repo, a workflow path under .github/workflows, and a job name"}})
	case errors.Is(err, controller.ErrNotAWorkflow):
		unprocessable(w, err.Error(), []fieldError{{"workflows", "name workflow files directly under .github/workflows"}})
	default:
		s.githubFail(w, r, doing, err)
	}
}

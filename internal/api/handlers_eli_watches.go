package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/store"
)

// eliWatchState is the state a watch list may filter by. It is not stored as
// one field: enabled is the decision, and the two reasons are what a paused or
// stopped watch carries.
func eliWatchState(w *store.EliRepairWatch) string {
	switch {
	case w.Enabled:
		return "enabled"
	case w.StopReason != "":
		return "stopped"
	default:
		return "paused"
	}
}

func eliWatchMatches(w *store.EliRepairWatch, repo, state string) bool {
	if repo != "" && !strings.EqualFold(w.Repo, repo) {
		return false
	}
	return state == "" || eliWatchState(w) == state
}

// handleEliWatches lists the durable per-PR watches. It is repository-scoped by
// the caller's filter and the same read scope as the repair history; a token
// that may read only its own repairs is not offered a watch it cannot see.
func (s *Server) handleEliWatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo := strings.TrimSpace(q.Get("repo"))
	state := strings.TrimSpace(q.Get("state"))
	switch state {
	case "", "enabled", "paused", "stopped":
	default:
		unprocessable(w, "the watch list could not be filtered as asked", []fieldError{{"state", "use enabled, paused or stopped"}})
		return
	}
	owner, scoped := eliRepairScope(r, auth.ActionAssistantRead)
	rows, err := s.ctrl.Store().EliRepairWatches(r.Context())
	if err != nil {
		s.internal(w, r, "listing Eli repair watches", err)
		return
	}
	matched := make([]*store.EliRepairWatch, 0, len(rows))
	for _, row := range rows {
		if scoped && row.OwnerID != owner || !eliWatchMatches(row, repo, state) {
			continue
		}
		matched = append(matched, row)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].UpdatedAt.After(matched[j].UpdatedAt) })
	page := parsePage(r)
	total := len(matched)
	if page.Offset >= len(matched) {
		matched = []*store.EliRepairWatch{}
	} else {
		end := min(page.Offset+page.Limit, len(matched))
		matched = matched[page.Offset:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": matched, "total": total})
}

type eliWatchAdmissionRequest struct {
	Repo           string   `json:"repo"`
	PullNumber     int      `json:"pull_number"`
	InstallationID string   `json:"installation_id"`
	Capabilities   []string `json:"capabilities"`
}

// handleAdmitEliWatch admits one pull request to continuous watching. The owner
// is the caller, never the body: unattended work is spent by the person who
// asked for it.
func (s *Server) handleAdmitEliWatch(w http.ResponseWriter, r *http.Request) {
	var req eliWatchAdmissionRequest
	if !decode(w, r, &req) {
		return
	}
	owner := Identity(r.Context()).UserID
	if owner == "" {
		conflict(w, "sign in with a personal account to watch a pull request")
		return
	}
	row, err := s.ctrl.AdmitEliRepairWatch(r.Context(), controller.EliWatchAdmission{
		Repo: req.Repo, PullNumber: req.PullNumber, InstallationID: req.InstallationID,
		OwnerID: owner, Capabilities: req.Capabilities,
	})
	if err != nil {
		s.eliWatchRefusal(w, "admitting an Eli repair watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_admit", "eli_repair_watch", row.ID,
		map[string]any{"repo": row.Repo, "pull_number": row.PullNumber, "checks": row.Checks, "conflicts": row.Conflicts})
	writeJSON(w, http.StatusCreated, row)
}

// handlePauseEliWatch stops following one pull request without discarding its
// history or changing the repository's decision.
func (s *Server) handlePauseEliWatch(w http.ResponseWriter, r *http.Request) {
	s.changeEliWatch(w, r, "pause", func(id, reason string) error {
		return s.ctrl.PauseEliRepairWatch(r.Context(), id, reason)
	})
}

// handleStopEliWatch is the terminal form: a new explicit admission is the only
// way back.
func (s *Server) handleStopEliWatch(w http.ResponseWriter, r *http.Request) {
	s.changeEliWatch(w, r, "stop", func(id, reason string) error {
		return s.ctrl.StopEliRepairWatch(r.Context(), id, reason)
	})
}

// handleResumeEliWatch admits the pull request again through the normal path,
// so its provider, access, policy and open state are checked afresh.
func (s *Server) handleResumeEliWatch(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	row, err := s.ctrl.Store().EliRepairWatch(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading an Eli repair watch", err)
		return
	}
	capabilities := watchCapabilities(row)
	resumed, err := s.ctrl.AdmitEliRepairWatch(r.Context(), controller.EliWatchAdmission{
		Repo: row.Repo, PullNumber: row.PullNumber, InstallationID: row.InstallationID,
		OwnerID: row.OwnerID, Capabilities: capabilities,
	})
	if err != nil {
		s.eliWatchRefusal(w, "resuming an Eli repair watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_resume", "eli_repair_watch", resumed.ID,
		map[string]any{"repo": resumed.Repo, "pull_number": resumed.PullNumber})
	writeJSON(w, http.StatusOK, resumed)
}

func (s *Server) changeEliWatch(w http.ResponseWriter, r *http.Request, action string, change func(id, reason string) error) {
	id := chiURLParam(r, "id")
	if _, err := s.ctrl.Store().EliRepairWatch(r.Context(), id); err != nil {
		s.fail(w, r, "reading an Eli repair watch", err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := change(id, strings.TrimSpace(req.Reason)); err != nil {
		s.fail(w, r, action+"ing an Eli repair watch", err)
		return
	}
	row, err := s.ctrl.Store().EliRepairWatch(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the changed Eli repair watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_"+action, "eli_repair_watch", id,
		map[string]any{"repo": row.Repo, "pull_number": row.PullNumber, "reason": req.Reason})
	writeJSON(w, http.StatusOK, row)
}

func watchCapabilities(row *store.EliRepairWatch) []string {
	out := make([]string, 0, 2)
	if row.Checks {
		out = append(out, controller.EliWatchChecks)
	}
	if row.Conflicts {
		out = append(out, controller.EliWatchConflicts)
	}
	return out
}

// eliWatchRefusal maps the admission path's answers: a missing installation or
// pull request is 404, and everything else the controller says is something the
// caller can change, so it is 422 with the controller's own sentence.
func (s *Server) eliWatchRefusal(w http.ResponseWriter, doing string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, err.Error())
		return
	}
	unprocessable(w, err.Error(), nil)
}

// --- Repository-wide decisions --------------------------------------------

func (s *Server) handleEliWatchRepositories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo := strings.TrimSpace(q.Get("repo"))
	state := strings.TrimSpace(q.Get("state"))
	switch state {
	case "", "enabled", "paused":
	default:
		unprocessable(w, "the watch list could not be filtered as asked", []fieldError{{"state", "use enabled or paused"}})
		return
	}
	owner, scoped := eliRepairScope(r, auth.ActionAssistantRead)
	rows, err := s.ctrl.Store().EliRepairWatchRepositories(r.Context())
	if err != nil {
		s.internal(w, r, "listing Eli repository watches", err)
		return
	}
	matched := make([]*store.EliRepairWatchRepository, 0, len(rows))
	for _, row := range rows {
		if scoped && row.OwnerID != owner || repo != "" && !strings.EqualFold(row.Repo, repo) {
			continue
		}
		if state == "enabled" && !row.Enabled || state == "paused" && row.Enabled {
			continue
		}
		matched = append(matched, row)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].UpdatedAt.After(matched[j].UpdatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": matched})
}

type eliRepositoryWatchRequest struct {
	Repo           string   `json:"repo"`
	InstallationID string   `json:"installation_id"`
	Capabilities   []string `json:"capabilities"`
}

func (s *Server) handleAdmitEliWatchRepository(w http.ResponseWriter, r *http.Request) {
	var req eliRepositoryWatchRequest
	if !decode(w, r, &req) {
		return
	}
	owner := Identity(r.Context()).UserID
	if owner == "" {
		conflict(w, "sign in with a personal account to watch a repository")
		return
	}
	report, err := s.ctrl.AdmitEliRepositoryWatches(r.Context(), controller.EliRepositoryWatchAdmission{
		Repo: req.Repo, InstallationID: req.InstallationID, OwnerID: owner, Capabilities: req.Capabilities,
	})
	if err != nil {
		s.eliWatchRefusal(w, "admitting an Eli repository watch", err)
		return
	}
	decision, err := s.ctrl.Store().EliRepairWatchRepositoryFor(r.Context(), strings.TrimSpace(req.Repo), req.InstallationID)
	if err != nil {
		s.fail(w, r, "reading the Eli repository watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_repository_admit", "eli_repair_watch_repository", decision.ID,
		map[string]any{"repo": decision.Repo, "checks": decision.Checks, "conflicts": decision.Conflicts})
	writeJSON(w, http.StatusCreated, map[string]any{"decision": decision, "report": report})
}

func (s *Server) handlePauseEliWatchRepository(w http.ResponseWriter, r *http.Request) {
	row, err := s.ctrl.PauseEliRepositoryWatches(r.Context(), chiURLParam(r, "id"))
	if err != nil {
		s.fail(w, r, "pausing an Eli repository watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_repository_pause", "eli_repair_watch_repository", row.ID,
		map[string]any{"repo": row.Repo})
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleResumeEliWatchRepository(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	report, err := s.ctrl.ResumeEliRepositoryWatches(r.Context(), id)
	if err != nil {
		s.eliWatchRefusal(w, "resuming an Eli repository watch", err)
		return
	}
	row, err := s.ctrl.Store().EliRepairWatchRepositoryByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, "reading the resumed Eli repository watch", err)
		return
	}
	s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.repair_watch_repository_resume", "eli_repair_watch_repository", id,
		map[string]any{"repo": row.Repo})
	writeJSON(w, http.StatusOK, map[string]any{"decision": row, "report": report})
}

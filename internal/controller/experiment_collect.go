package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eyupio/zoomies/internal/github"
	"github.com/eyupio/zoomies/internal/store"
)

var ErrExperimentEvidence = errors.New("comparison evidence is incomplete or inconsistent")

const maxExperimentSelectedAttempts = 100
const maxExperimentCollectedJobs = 2000

type ExperimentAttemptSelection struct {
	RunID   int64 `json:"run_id"`
	Attempt int   `json:"attempt"`
}

type ExperimentRetainedJob struct {
	RunnerCreation *store.RunnerCreationObservation `json:"runner_creation,omitempty"`
	RunnerID       string                           `json:"runner_id,omitempty"`
	PoolID         string                           `json:"pool_id,omitempty"`
	HostID         string                           `json:"host_id,omitempty"`
	RecordedGrant  *ExperimentRecordedGrant         `json:"recorded_grant,omitempty"`
	JobID          string                           `json:"job_id"`
	GitHubJobID    int64                            `json:"github_job_id"`
	HeadSHA        string                           `json:"head_sha"`
	State          store.JobState                   `json:"state"`
	Conclusion     string                           `json:"conclusion"`
	QueuedAt       time.Time                        `json:"queued_at"`
	StartedAt      *time.Time                       `json:"started_at"`
	CompletedAt    *time.Time                       `json:"completed_at"`
}

type ExperimentRetainedAttempt struct {
	Selection ExperimentAttemptSelection `json:"selection"`
	// retained_only is never full workflow coverage, even when the query fits.
	Coverage string                  `json:"coverage"`
	Jobs     []ExperimentRetainedJob `json:"jobs"`
}

type ExperimentRetainedEvidence struct {
	Repository        string                      `json:"repository"`
	InstallationID    string                      `json:"installation_id"`
	SelectionComplete bool                        `json:"selection_complete"`
	Attempts          []ExperimentRetainedAttempt `json:"attempts"`
}

// CollectExperimentAttempts is for a caller already authorised to read jobs.
// It checks installation/repository joins, not the caller's role. No transport
// exposes this yet. The report omits names, log tails and workflow output. A
// later adapter must treat the repository and revision strings as untrusted.
func (c *Controller) CollectExperimentAttempts(ctx context.Context, installation, repo string, selected []ExperimentAttemptSelection) (*ExperimentRetainedEvidence, error) {
	if len(selected) < 1 || len(selected) > maxExperimentSelectedAttempts {
		return nil, fmt.Errorf("%w: select 1 to %d attempts", ErrExperimentEvidence, maxExperimentSelectedAttempts)
	}
	seen := map[ExperimentAttemptSelection]bool{}
	for _, s := range selected {
		if s.RunID <= 0 || s.Attempt <= 0 || seen[s] {
			return nil, fmt.Errorf("%w: select each positive run/attempt identity once", ErrExperimentEvidence)
		}
		seen[s] = true
	}
	if installation == "" || repo == "" {
		return nil, ErrExperimentEvidence
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	inst, err := c.st.GetInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	_, name, kind := github.SplitTarget(repo)
	if kind != store.TargetRepo || name == "" || !installationCovers(inst, repo) {
		return nil, fmt.Errorf("%w: the installation does not cover the selected repository", ErrExperimentEvidence)
	}
	r := &ExperimentRetainedEvidence{Repository: repo, InstallationID: installation, Attempts: make([]ExperimentRetainedAttempt, 0, len(selected))}
	budget := maxExperimentCollectedJobs
	for _, s := range selected {
		a := ExperimentRetainedAttempt{Selection: s, Coverage: "held_back", Jobs: []ExperimentRetainedJob{}}
		if budget > 0 {
			jobs, cut, err := c.st.ListExperimentAttemptJobs(ctx, repo, installation, s.RunID, s.Attempt, min(budget, store.MaxExperimentAttemptJobs))
			if err != nil {
				return nil, err
			}
			for _, j := range jobs {
				grant, err := experimentRecordedGrant(j)
				if err != nil {
					return nil, err
				}
				observation, err := c.experimentRunnerCreation(ctx, installation, j)
				if err != nil {
					return nil, err
				}
				row := ExperimentRetainedJob{RecordedGrant: grant, JobID: j.ID, GitHubJobID: j.GitHubJobID, HeadSHA: j.HeadSHA, State: j.State, Conclusion: j.Conclusion, QueuedAt: j.QueuedAt, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt}
				if observation != nil {
					row.RunnerCreation, row.RunnerID, row.PoolID, row.HostID = observation, j.RunnerID, j.PoolID, j.HostID
				}
				a.Jobs = append(a.Jobs, row)
			}
			budget -= len(jobs)
			switch {
			case cut:
				a.Coverage = "truncated"
			case len(jobs) == 0:
				a.Coverage = "not_available"
			default:
				a.Coverage = "retained_only"
			}
		}
		r.Attempts = append(r.Attempts, a)
	}
	// Retention, non-fleet jobs and missing deliveries can each hide attempts.
	// An uncapped SQL result proves none of those absent records did not exist.
	return r, nil
}

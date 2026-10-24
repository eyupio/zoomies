package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrInvalidSetupHistory = errors.New("invalid setup step history request")

// SetupStepSample is one successful job's matching setup step, with the exact
// local job identity and the GitHub-reported timing. It is what the cache
// preview uses for repeated setup cost.
type SetupStepSample struct {
	JobID       string `json:"job_id"`
	GitHubJobID int64  `json:"github_job_id"`
	GitHubRunID int64  `json:"github_run_id"`
	RunAttempt  int    `json:"run_attempt"`
	HeadSHA     string `json:"head_sha"`
	StepNumber  int    `json:"step_number"`
	StepName    string `json:"step_name"`
	SetupMS     int64  `json:"setup_ms"`
	// LeadMS is how long the job had been running when the matched step began:
	// the earliest recorded step start to this step's start. A sharding preview
	// needs it because every shard repeats that work. Zero when no earlier step
	// has a recorded start.
	LeadMS      int64     `json:"lead_ms"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// RecentSetupStepSamples returns the newest successful jobs for one repository,
// workflow and job name that carry exactly one step with the given literal
// name and a positive GitHub-reported duration. A job with two such steps is
// ambiguous and left out; this never guesses which setup belonged to the job.
func (s *Store) RecentSetupStepSamples(ctx context.Context, repo, workflow, jobName, stepName string, since time.Time, limit int) ([]SetupStepSample, error) {
	if repo == "" || len(repo) > 1024 || workflow == "" || len(workflow) > 512 || jobName == "" || len(jobName) > 512 || stepName == "" || len(stepName) > 512 || strings.ContainsAny(stepName, "\r\n\x00") || limit < 1 || limit > 100 || since.IsZero() {
		return nil, ErrInvalidSetupHistory
	}
	sqlLimit := min(limit*4, 200)
	rows, err := s.read.QueryContext(ctx, `SELECT `+jobCols+` FROM jobs
		WHERE repo = ? AND workflow = ? AND job_name = ? AND conclusion = 'success'
			AND completed_at IS NOT NULL AND completed_at >= ?
		ORDER BY completed_at DESC, id ASC LIMIT ?`, repo, workflow, jobName, ms(since), sqlLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetupStepSample
	for rows.Next() && len(out) < limit {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		var match *JobStep
		for i := range j.Steps {
			step := &j.Steps[i]
			if step.Name != stepName || step.Number <= 0 || step.StartedAt == nil || step.CompletedAt == nil || !step.CompletedAt.After(*step.StartedAt) {
				continue
			}
			if match != nil {
				match = nil
				break
			}
			match = step
		}
		if match == nil {
			continue
		}
		var first *time.Time
		for i := range j.Steps {
			if st := j.Steps[i].StartedAt; st != nil && (first == nil || st.Before(*first)) {
				first = st
			}
		}
		var lead int64
		if first != nil && match.StartedAt.After(*first) {
			lead = match.StartedAt.Sub(*first).Milliseconds()
		}
		out = append(out, SetupStepSample{
			JobID: j.ID, GitHubJobID: j.GitHubJobID, GitHubRunID: j.GitHubRunID, RunAttempt: j.RunAttempt,
			HeadSHA: j.HeadSHA, StepNumber: match.Number, StepName: match.Name,
			SetupMS: match.CompletedAt.Sub(*match.StartedAt).Milliseconds(), LeadMS: lead,
			StartedAt: *match.StartedAt, CompletedAt: *match.CompletedAt,
		})
	}
	return out, rows.Err()
}

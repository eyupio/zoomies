package store

import (
	"context"
	"time"
)

type EliRepairOpportunity struct {
	ID             string    `json:"id"`
	Repo           string    `json:"repo"`
	InstallationID string    `json:"installation_id"`
	PullNumber     int       `json:"pull_number"`
	HeadSHA        string    `json:"head_sha"`
	RunID          int64     `json:"run_id"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *Store) OfferEliRepair(ctx context.Context, o EliRepairOpportunity) error {
	_, err := s.exec(ctx, `INSERT INTO eli_repair_opportunities(id,repo,installation_id,pull_number,head_sha,run_id,title,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'offered',?,?) ON CONFLICT(repo,head_sha,pull_number) DO UPDATE SET run_id=excluded.run_id,title=excluded.title,updated_at=excluded.updated_at`, NewID(PrefixEliOpportunity), o.Repo, o.InstallationID, o.PullNumber, o.HeadSHA, o.RunID, o.Title, ms(s.Now()), ms(s.Now()))
	return err
}
func (s *Store) EliRepairOpportunities(ctx context.Context, user string) ([]EliRepairOpportunity, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT o.id,o.repo,o.installation_id,o.pull_number,o.head_sha,o.run_id,o.title,o.state,o.created_at,o.updated_at FROM eli_repair_opportunities o JOIN eli_repair_policies p ON p.repo=o.repo AND p.installation_id=o.installation_id WHERE o.state='offered' AND p.enabled=1 AND p.ask_first=1 AND NOT EXISTS(SELECT 1 FROM eli_repair_dismissals d WHERE d.opportunity_id=o.id AND d.user_id=?) ORDER BY o.updated_at DESC LIMIT 100`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EliRepairOpportunity{}
	for rows.Next() {
		var o EliRepairOpportunity
		var created, updated int64
		if err := rows.Scan(&o.ID, &o.Repo, &o.InstallationID, &o.PullNumber, &o.HeadSHA, &o.RunID, &o.Title, &o.State, &created, &updated); err != nil {
			return nil, err
		}
		o.CreatedAt, o.UpdatedAt = at(created), at(updated)
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) SetEliOpportunityState(ctx context.Context, id, state string) error {
	if state != "accepted" && state != "superseded" {
		return ErrConflict
	}
	_, err := s.exec(ctx, `UPDATE eli_repair_opportunities SET state=?,updated_at=? WHERE id=? AND state='offered'`, state, ms(s.Now()), id)
	return err
}
func (s *Store) DismissEliOpportunity(ctx context.Context, id, user string) error {
	_, err := s.exec(ctx, `INSERT OR IGNORE INTO eli_repair_dismissals(opportunity_id,user_id) VALUES(?,?)`, id, user)
	return err
}

// EliRepairBudget is the last 24 hours as the two limits see it: every attempt
// on the repository, and every attempt the person asked for anywhere. A repair
// that is both counts in both. The times are oldest first, so FreesAt can say
// when that limit has a place again: the oldest attempt is not necessarily the
// one that frees the binding limit when the repository and the person are
// spending different windows.
func (s *Store) EliRepairBudget(ctx context.Context, repo, user string) (EliRepairUsage, EliRepairUsage, error) {
	var repoUse, personUse EliRepairUsage
	rows, err := s.read.QueryContext(ctx, `SELECT repo,user_id,created_at FROM eli_repairs WHERE created_at>=? AND (repo=? OR (user_id=? AND user_id<>'')) ORDER BY created_at`, ms(s.Now().Add(-24*time.Hour)), repo, user)
	if err != nil {
		return repoUse, personUse, err
	}
	defer rows.Close()
	for rows.Next() {
		var r, u string
		var created int64
		if err := rows.Scan(&r, &u, &created); err != nil {
			return repoUse, personUse, err
		}
		when := at(created)
		if r == repo {
			repoUse.Used++
			repoUse.Times = append(repoUse.Times, when)
		}
		if u == user && user != "" {
			personUse.Used++
			personUse.Times = append(personUse.Times, when)
		}
	}
	return repoUse, personUse, rows.Err()
}

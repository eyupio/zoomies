package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type EliIdentity struct {
	UserID       string `json:"user_id"`
	GitHubUserID int64  `json:"github_user_id"`
	GitHubLogin  string `json:"github_login"`
	Confirmed    bool   `json:"confirmed"`
}
type EliRepairPolicy struct {
	Repo           string `json:"repo"`
	InstallationID string `json:"installation_id"`
	ProviderID     string `json:"provider_id"`
	Enabled        bool   `json:"enabled"`
	AskFirst       bool   `json:"ask_first"`
	Automatic      bool   `json:"automatic"`
	AllowWorkflows bool   `json:"allow_workflows"`
}
type EliRepairStep struct {
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	UsageReported     bool   `json:"usage_reported"`
	RequestBytes      int    `json:"request_bytes"`
	ElapsedMS         int64  `json:"elapsed_ms"`
	Response          string `json:"response,omitempty"`
	ResponseRecorded  bool   `json:"response_recorded,omitempty"`
	ResponseTruncated bool   `json:"response_truncated,omitempty"`
	Error             string `json:"error,omitempty"`
	ValidationError   string `json:"validation_error,omitempty"`
}

// MaxEliRepairCaseJobs is how many failed jobs a repair's case names. A run can
// fail dozens of matrix jobs; the case is a summary of what was being fixed, and
// the rest are one click away in the job list.
const MaxEliRepairCaseJobs = 10

// EliRepairCaseJob is one failed job of the run a repair was started for, with
// the step it stopped at. Names are the workflow's own words and are untrusted.
type EliRepairCaseJob struct {
	Name       string `json:"name"`
	Step       string `json:"step,omitempty"`
	URL        string `json:"url,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// EliRepairCase is what a repair was about, as it was when the repair began:
// the pull request, the run and the jobs that failed. Zoomies' own job records
// are pruned and a pull request's title changes, so the repair keeps its own
// copy; the page that follows it can then say what it was fixing long after.
// It holds no log text.
type EliRepairCase struct {
	PullTitle string             `json:"pull_title,omitempty"`
	PullURL   string             `json:"pull_url,omitempty"`
	Author    string             `json:"author,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	RunURL    string             `json:"run_url,omitempty"`
	Workflow  string             `json:"workflow,omitempty"`
	RunNumber int64              `json:"run_number,omitempty"`
	Jobs      []EliRepairCaseJob `json:"jobs,omitempty"`
}

// EliRepairDetails is the one JSON column the worker rewrites on every save, so
// whatever lives here survives each stage without a second write path. A row
// made before Case existed has none, and every reader has to cope with that.
type EliRepairDetails struct {
	Model       string          `json:"model"`
	Stage       string          `json:"stage"`
	FailureCode string          `json:"failure_code,omitempty"`
	SourceBytes int             `json:"source_bytes"`
	Steps       []EliRepairStep `json:"steps"`
	Case        *EliRepairCase  `json:"case,omitempty"`
}
type EliRepair struct {
	Details        EliRepairDetails `json:"details"`
	PublishedAt    *time.Time       `json:"-"`
	RequesterAdmin bool             `json:"-"`
	ID             string           `json:"id"`
	DedupKey       string           `json:"-"`
	InstallationID string           `json:"installation_id"`
	Repo           string           `json:"repo"`
	PullNumber     int              `json:"pull_number"`
	JobID          int64            `json:"job_id"`
	RunID          int64            `json:"run_id"`
	HeadSHA        string           `json:"head_sha"`
	CommitSHA      string           `json:"commit_sha"`
	UserID         string           `json:"user_id"`
	GitHubUserID   int64            `json:"github_user_id"`
	GitHubLogin    string           `json:"github_login"`
	ProviderID     string           `json:"provider_id"`
	Trigger        string           `json:"trigger"`
	Instruction    string           `json:"-"`
	State          string           `json:"state"`
	Message        string           `json:"message"`
	CommentID      int64            `json:"comment_id"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

func (s *Store) SetEliIdentity(ctx context.Context, in EliIdentity) error {
	_, err := s.exec(ctx, `INSERT INTO eli_github_identities(user_id,github_user_id,github_login,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(user_id) DO UPDATE SET github_user_id=excluded.github_user_id,github_login=excluded.github_login,confirmed=0,updated_at=excluded.updated_at`, in.UserID, in.GitHubUserID, in.GitHubLogin, ms(s.Now()))
	if isUnique(err) {
		// The user_id conflict is the upsert; only another user's row can trip
		// the github_user_id unique index, so this is the held case and not a
		// constraint detail for the caller to decode.
		return ErrEliIdentityHeld
	}
	return wrapWrite(err)
}

// RelinkEliIdentity moves a link ErrEliIdentityHeld refused, or co-locates the
// caller's row with one it already holds. The previous holder's row is removed
// and its membership in the link is returned, so the audit row can say whose
// Zoomies account lost the link; "" means nobody else held it (including the
// caller moving their own row). The new row lands unconfirmed: only the person
// using it can allow provider use again, so a moved link never inherits the
// consent it is dragged past.
func (s *Store) RelinkEliIdentity(ctx context.Context, in EliIdentity) (string, error) {
	holder := ""
	err := s.tx(ctx, func(tx *sql.Tx) error {
		holder = ""
		var held string
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM eli_github_identities WHERE github_user_id=?`, in.GitHubUserID).Scan(&held)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case held != in.UserID:
			if _, err := tx.ExecContext(ctx, `DELETE FROM eli_github_identities WHERE user_id=?`, held); err != nil {
				return err
			}
			holder = held
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO eli_github_identities(user_id,github_user_id,github_login,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(user_id) DO UPDATE SET github_user_id=excluded.github_user_id,github_login=excluded.github_login,confirmed=0,updated_at=excluded.updated_at`, in.UserID, in.GitHubUserID, in.GitHubLogin, ms(s.Now()))
		return wrapWrite(err)
	})
	if err != nil {
		return "", err
	}
	return holder, nil
}
func (s *Store) EliIdentities(ctx context.Context) ([]EliIdentity, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT user_id,github_user_id,github_login,confirmed FROM eli_github_identities ORDER BY github_login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EliIdentity{}
	for rows.Next() {
		var v EliIdentity
		if err := rows.Scan(&v.UserID, &v.GitHubUserID, &v.GitHubLogin, &v.Confirmed); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeleteEliIdentity(ctx context.Context, userID string) error {
	_, err := s.exec(ctx, `DELETE FROM eli_github_identities WHERE user_id=?`, userID)
	return err
}
func (s *Store) SetEliRepairPolicy(ctx context.Context, p EliRepairPolicy) error {
	_, err := s.exec(ctx, `INSERT INTO eli_repair_policies(repo,installation_id,provider_id,enabled,automatic,ask_first,allow_workflows,updated_at) VALUES(?,?,NULLIF(?,''),?,?,?,?,?)
 ON CONFLICT(repo) DO UPDATE SET installation_id=excluded.installation_id,provider_id=excluded.provider_id,enabled=excluded.enabled,automatic=excluded.automatic,ask_first=excluded.ask_first,allow_workflows=excluded.allow_workflows,updated_at=excluded.updated_at`, p.Repo, p.InstallationID, p.ProviderID, boolInt(p.Enabled), boolInt(p.Automatic), boolInt(p.AskFirst), boolInt(p.AllowWorkflows), ms(s.Now()))
	return wrapWrite(err)
}

var eliRepairPoliciesQuery = hot(`SELECT repo,installation_id,COALESCE(provider_id,''),enabled,automatic,ask_first,allow_workflows FROM eli_repair_policies ORDER BY repo`)

func (s *Store) EliRepairPolicies(ctx context.Context) ([]EliRepairPolicy, error) {
	rows, err := s.hotRead().Query(ctx, eliRepairPoliciesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EliRepairPolicy{}
	for rows.Next() {
		var p EliRepairPolicy
		if err := rows.Scan(&p.Repo, &p.InstallationID, &p.ProviderID, &p.Enabled, &p.Automatic, &p.AskFirst, &p.AllowWorkflows); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const eliRepairCols = `id,dedup_key,installation_id,repo,pull_number,job_id,run_id,head_sha,commit_sha,user_id,github_user_id,github_login,provider_id,trigger,requester_admin,instruction,state,message,comment_id,published_at,created_at,updated_at,details`

func scanEliRepair(row interface{ Scan(...any) error }) (*EliRepair, error) {
	var v EliRepair
	var created, updated int64
	var published sql.NullInt64
	var details string
	err := row.Scan(&v.ID, &v.DedupKey, &v.InstallationID, &v.Repo, &v.PullNumber, &v.JobID, &v.RunID, &v.HeadSHA, &v.CommitSHA, &v.UserID, &v.GitHubUserID, &v.GitHubLogin, &v.ProviderID, &v.Trigger, &v.RequesterAdmin, &v.Instruction, &v.State, &v.Message, &v.CommentID, &published, &created, &updated, &details)
	if err == nil {
		err = json.Unmarshal([]byte(details), &v.Details)
	}
	v.CreatedAt, v.UpdatedAt = at(created), at(updated)
	v.PublishedAt = atp(published)
	return &v, err
}

// GetEliRepair reads one repair by its id under the rule the history is read by:
// with scoped, only a repair whose owner is ownerID, and an empty owner matches
// nothing (an automatic repair has none). A missing id and one the caller may
// not see are the same ErrNotFound, so the answer cannot be used to find out
// which ids exist. A repair cleared from the history is still returned: clearing
// hides a row from the list and does nothing else.
func (s *Store) GetEliRepair(ctx context.Context, id, ownerID string, scoped bool) (*EliRepair, error) {
	r, err := scanEliRepair(s.read.QueryRowContext(ctx, `SELECT `+eliRepairCols+` FROM eli_repairs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if scoped && (r.UserID == "" || r.UserID != ownerID) {
		return nil, ErrNotFound
	}
	return r, nil
}

// Deduplication happens in the writer transaction, so a webhook burst cannot enqueue the same repair twice.
func (s *Store) EnqueueEliRepair(ctx context.Context, r *EliRepair) (bool, error) {
	added := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM eli_repairs WHERE dedup_key=?`, r.DedupKey).Scan(&existing)
		if err == nil {
			saved, err := scanEliRepair(tx.QueryRowContext(ctx, `SELECT `+eliRepairCols+` FROM eli_repairs WHERE id=?`, existing))
			if err != nil {
				return err
			}
			if strings.HasPrefix(r.DedupKey, "offer:") && saved.UserID != r.UserID {
				return ErrConflict
			}
			// A new explicit UI request may retry a finished failure. Webhook delivery
			// keys remain permanent, and an in-flight UI request is still idempotent.
			retry := strings.HasPrefix(r.DedupKey, "ui:") && (saved.State == "failed" || saved.State == "blocked" || saved.State == "interrupted" || saved.State == "unverified" || saved.State == "checks_failed" || saved.State == "superseded")
			if !retry {
				// A person asking again for something already done brings it back into
				// view. Without this a person who cleared the repair would be answered
				// with a history that does not contain what they just asked for. Only a
				// key a person made counts: "ui:" is the page and "offer:" is an offer
				// they accepted. A "comment:" duplicate is the webhook redelivering the
				// same comment (only action "created" is admitted, keyed by comment id),
				// and every later failing job on the same head hits the same
				// "automatic:" key, so neither is anyone asking and neither may undo a
				// clear. They still deduplicate here; the row just stays out of sight.
				//
				// cleared_at has three sites and no others: the history's where clause
				// (reads), ClearEliRepairs (writes) and this un-clear (writes). The
				// budget, the own-commit guard and the deduplication must go on counting
				// a cleared row, so none of them may ever look at the column.
				if strings.HasPrefix(r.DedupKey, "ui:") || strings.HasPrefix(r.DedupKey, "offer:") {
					if _, err := tx.ExecContext(ctx, `UPDATE eli_repairs SET cleared_at=NULL WHERE id=? AND cleared_at IS NOT NULL`, saved.ID); err != nil {
						return err
					}
				}
				*r = *saved
				return nil
			}
			if _, err := tx.ExecContext(ctx, `UPDATE eli_repairs SET dedup_key=? WHERE id=?`, saved.DedupKey+":retry:"+saved.ID, saved.ID); err != nil {
				return err
			}

		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r.ID = NewID(PrefixEliRepair)
		r.State = "queued"
		r.CreatedAt = s.Now()
		r.UpdatedAt = r.CreatedAt
		_, err = tx.ExecContext(ctx, `INSERT INTO eli_repairs (`+eliRepairCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.DedupKey, r.InstallationID, r.Repo, r.PullNumber, r.JobID, r.RunID, r.HeadSHA, r.CommitSHA, r.UserID, r.GitHubUserID, r.GitHubLogin, r.ProviderID, r.Trigger, boolInt(r.RequesterAdmin), r.Instruction, r.State, r.Message, r.CommentID, msp(r.PublishedAt), ms(r.CreatedAt), ms(r.UpdatedAt), repairDetailsJSON(r.Details))
		added = err == nil
		return err
	})
	return added, wrapWrite(err)
}
func (s *Store) NextEliRepair(ctx context.Context) (*EliRepair, error) {
	r, err := scanEliRepair(s.read.QueryRowContext(ctx, `SELECT `+eliRepairCols+` FROM eli_repairs WHERE state='queued' ORDER BY created_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}
func (s *Store) SaveEliRepair(ctx context.Context, r *EliRepair) error {
	r.UpdatedAt = s.Now()
	_, err := s.exec(ctx, `UPDATE eli_repairs SET pull_number=?,head_sha=?,commit_sha=?,user_id=?,provider_id=?,state=?,message=?,comment_id=?,published_at=?,updated_at=?,details=? WHERE id=?`, r.PullNumber, r.HeadSHA, r.CommitSHA, r.UserID, r.ProviderID, r.State, r.Message, r.CommentID, msp(r.PublishedAt), ms(r.UpdatedAt), repairDetailsJSON(r.Details), r.ID)
	return err
}

// An interrupted write has an uncertain outcome. Never automatically repeat it after restart.
func (s *Store) InterruptEliRepairs(ctx context.Context) error {
	_, err := s.exec(ctx, `UPDATE eli_repairs SET state='interrupted',message='The controller restarted during this repair. Check the PR before asking again.',updated_at=? WHERE state='working'`, ms(s.Now()))
	return err
}

// Repair commits remain protected from automatic recursion beyond the history page's limit.
func (s *Store) IsEliRepairCommit(ctx context.Context, repo, sha string) (bool, error) {
	var count int
	err := s.read.QueryRowContext(ctx, `SELECT count(*) FROM eli_repairs WHERE repo=? AND commit_sha=?`, repo, sha).Scan(&count)
	return count > 0, err
}
func (s *Store) CheckingEliRepairs(ctx context.Context) ([]*EliRepair, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+eliRepairCols+` FROM eli_repairs WHERE state='checking' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*EliRepair{}
	for rows.Next() {
		r, err := scanEliRepair(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Only the owner can approve a verified link. Pinning the numeric ID prevents a stale
// confirmation screen from approving a different account linked in the meantime.
func (s *Store) ConfirmEliIdentity(ctx context.Context, userID string, githubID int64, enabled bool) error {
	result, err := s.exec(ctx, `UPDATE eli_github_identities SET confirmed=?,updated_at=? WHERE user_id=? AND github_user_id=?`, boolInt(enabled), ms(s.Now()), userID, githubID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func repairDetailsJSON(v EliRepairDetails) string { b, _ := json.Marshal(v); return string(b) }

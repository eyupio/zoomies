package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// The history is read a page at a time, and a select-all takes at most
// MaxEliRepairSelection rows, so one request can never ask the database or the
// browser for a whole table.
const (
	DefaultEliRepairPage  = 50
	MaxEliRepairPage      = 500
	MaxEliRepairSelection = 500
)

// EliRepairGroups are the state groups a history can be narrowed to. They are
// defined here, once, so the API and the UI do not each keep a list of repair
// states that goes stale the day a state is added.
//
// attention is every state that is neither running nor passed, which is why
// a new failure state lands in it without anyone remembering to list it.
var EliRepairGroups = []string{"attention", "active", "passed", "needs_action"}

// EliRepairTriggers are how a repair began: Eli acting on its own, or a person
// asking. The stored value for a person's request is "mention", whatever the
// way they asked, so "requested" is everything that is not automatic.
var EliRepairTriggers = []string{"automatic", "requested"}

// eliRepairActive is the states in which Eli still owns the row: it is queued
// to run, running, or waiting on checks. Such a row is not history yet, and
// clearing it would hide a repair that is about to push a commit.
const eliRepairActive = `('queued','working','checking')`

// EliRepairFilter narrows the repairs a person looks at. The zero value is
// every uncleared repair, newest first.
type EliRepairFilter struct {
	OwnerID string // with Scoped: only rows whose user_id is OwnerID and not empty
	Scoped  bool
	Group   string // "" or one of EliRepairGroups; anything else is treated as ""
	Repo    string // case-insensitive substring after TrimSpace; "" means any
	Trigger string // "" or one of EliRepairTriggers
	Limit   int    // 0 means DefaultEliRepairPage; clamped to MaxEliRepairPage
	Offset  int
}

// where renders the filter. Nothing the caller typed is spliced into the
// query: groups and triggers choose between fixed fragments, and the
// repository search is a bound, escaped LIKE argument. That is also why an
// unknown group or trigger is ignored here rather than quoted; the API is what
// refuses it, because a filter that silently widens shows the wrong history.
//
// clearable additionally drops the rows that are still active, which is the
// set a select-all may take.
func (f EliRepairFilter) where(clearable bool) (string, []any) {
	clauses := []string{"cleared_at IS NULL"}
	args := []any{}
	if f.Scoped {
		// An empty owner matches nobody. Without the second half it would match
		// every repair that has no owner, which is every automatic one.
		clauses = append(clauses, "user_id=? AND user_id<>''")
		args = append(args, f.OwnerID)
	}
	switch f.Group {
	case "needs_action":
		// The inbox is about the newest attempt for each PR. An older failed
		// attempt must not ask for help after a retry passed or is still working.
		// Cleared newer attempts still supersede older ones.
		clauses = append(clauses, `state IN ('failed','blocked','checks_failed','interrupted','unverified') AND NOT EXISTS (
			SELECT 1 FROM eli_repairs newer WHERE newer.repo=eli_repairs.repo COLLATE NOCASE AND newer.pull_number=eli_repairs.pull_number
			AND (newer.created_at>eli_repairs.created_at OR (newer.created_at=eli_repairs.created_at AND newer.id>eli_repairs.id)))`)
	case "active":
		clauses = append(clauses, "state IN "+eliRepairActive)
	case "passed":
		clauses = append(clauses, "state='succeeded'")
	case "attention":
		clauses = append(clauses, "state NOT IN "+eliRepairActive+" AND state<>'succeeded'")
	}
	if repo := strings.TrimSpace(f.Repo); repo != "" {
		clauses = append(clauses, `repo LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(repo))
	}
	switch f.Trigger {
	case "automatic":
		clauses = append(clauses, "trigger='automatic'")
	case "requested":
		clauses = append(clauses, "trigger<>'automatic'")
	}
	if clearable {
		clauses = append(clauses, "state NOT IN "+eliRepairActive)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListEliRepairsPage returns one page of the history and how many rows match in
// all. Cleared rows are not part of either.
//
// The order is newest first with the id as the tie-break. Repairs a webhook
// burst created in one millisecond share a created_at, and without the
// tie-break two pages could each claim one of them or neither.
func (s *Store) ListEliRepairsPage(ctx context.Context, f EliRepairFilter) ([]*EliRepair, int, error) {
	where, args := f.where(false)
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT count(*) FROM eli_repairs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultEliRepairPage
	}
	limit = min(limit, MaxEliRepairPage)
	rows, err := s.read.QueryContext(ctx, `SELECT `+eliRepairCols+` FROM eli_repairs`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*EliRepair{}
	for rows.Next() {
		r, err := scanEliRepair(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// EliRepairSelection is what a select-all takes: the ids of the rows that match
// and can be cleared, newest first and at most MaxEliRepairSelection of them,
// with how many are clearable in all so the caller can say when it was capped.
// Paging in the filter is ignored; a selection is the matches, not a page.
func (s *Store) EliRepairSelection(ctx context.Context, f EliRepairFilter) ([]string, int, error) {
	where, args := f.where(true)
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT count(*) FROM eli_repairs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.read.QueryContext(ctx, `SELECT id FROM eli_repairs`+where+` ORDER BY created_at DESC, id DESC LIMIT ?`,
		append(args, MaxEliRepairSelection)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, total, rows.Err()
}

// EliRepairClear is the outcome for one id of a clear.
//
// OK says the repair is now out of the history. Changed says this call is what
// took it out: a repeat, or an administrator clearing what is already cleared,
// is OK and not Changed. A client has no use for it; it exists so the audit row
// names only what a request really changed, hence no JSON name.
type EliRepairClear struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Changed bool   `json:"-"`
}

// ClearEliRepairs hides repairs from the history and does nothing else to
// them. The row stays, so the daily budget, the deduplication of webhook
// deliveries and the guard against Eli repairing its own commit all go on
// counting it; see migration 0100 for why that matters.
//
// It answers once per distinct id, in the order asked, inside one transaction:
//
//   - an id that does not exist and an id the caller may not see are both "not
//     found", so the answer cannot be used to probe which ids are real;
//   - a repair that is still queued, running or checking is "still running";
//   - a repair that is already cleared is ok but not Changed, so a repeated
//     request is harmless and does not claim work it did not do.
//
// Only cleared_at is written. updated_at belongs to the repair, and clearing it
// from the history is not a change to the repair.
func (s *Store) ClearEliRepairs(ctx context.Context, ids []string, ownerID string, scoped bool) ([]EliRepairClear, error) {
	results := make([]EliRepairClear, 0, len(ids))
	err := s.tx(ctx, func(tx *sql.Tx) error {
		results = results[:0]
		seen := make(map[string]bool, len(ids))
		now := ms(s.Now())
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			var user string
			var active bool
			err := tx.QueryRowContext(ctx, `SELECT user_id, state IN `+eliRepairActive+` FROM eli_repairs WHERE id=?`, id).Scan(&user, &active)
			missing := errors.Is(err, sql.ErrNoRows)
			if err != nil && !missing {
				return err
			}
			switch {
			case missing, scoped && (user == "" || user != ownerID):
				results = append(results, EliRepairClear{ID: id, Error: "not found"})
			case active:
				results = append(results, EliRepairClear{ID: id, Error: "still running"})
			default:
				// Keep the first clearing time if there already is one. The same guard
				// is what lets RowsAffected say whether this call was the one to clear
				// it.
				res, err := tx.ExecContext(ctx, `UPDATE eli_repairs SET cleared_at=? WHERE id=? AND cleared_at IS NULL`, now, id)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil {
					return err
				}
				results = append(results, EliRepairClear{ID: id, OK: true, Changed: n > 0})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

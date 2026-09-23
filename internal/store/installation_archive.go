package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// One installation's rows, as a set
// ---------------------------------------------------------------------------
//
// Deleting an installation removes what a foreign key reaches -- its pools and
// their runners -- and leaves the history behind on purpose, because a job
// record outliving the pool it ran in is what the Jobs page and the usage
// report are for. Two things want the history as well: moving an App to a new
// machine, which should take its past with it, and removing a team's history
// cleanly, which should leave nothing that names it. Both are the same set,
// defined once here, so an export and a purge cannot disagree about what
// "everything about this installation" means.

// ArchiveTable is one table's share of an installation: its column names and
// its rows, in rowid order so the same rows always render the same bytes.
type ArchiveTable struct {
	Table   string           `json:"table"`
	Columns []string         `json:"columns"`
	Rows    [][]ArchiveValue `json:"rows"`
}

// ArchiveValue is one SQLite value carried through JSON. Integers, reals, text
// and NULL are themselves; a blob is {"base64": ...}, because a bare string
// would come back as text and SQLite would keep it as text.
type ArchiveValue struct{ V any }

// MarshalJSON renders the value.
func (v ArchiveValue) MarshalJSON() ([]byte, error) {
	if b, ok := v.V.([]byte); ok {
		return json.Marshal(map[string]string{"base64": base64.StdEncoding.EncodeToString(b)})
	}
	return json.Marshal(v.V)
}

// UnmarshalJSON reads a value back, keeping an integer an integer: decoding
// every number as a float would round a millisecond timestamp past 2^53 and
// turn a count into 3.0.
func (v *ArchiveValue) UnmarshalJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return err
	}
	switch t := x.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(t.String(), 10, 64); err == nil {
			v.V = i
			return nil
		}
		f, err := t.Float64()
		if err != nil {
			return err
		}
		v.V = f
	case map[string]any:
		s, ok := t["base64"].(string)
		if !ok || len(t) != 1 {
			return errors.New("an archive value is an object that is not a blob")
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return err
		}
		v.V = b
	case string, nil, bool:
		v.V = t
	default:
		return fmt.Errorf("an archive value is a %T, which no column holds", t)
	}
	return nil
}

// installationTable is one table in the set and how its rows are found. where
// is a predicate over the table; args builds its arguments from the scope.
type installationTable struct {
	name  string
	where string
	args  func(sc *installationScope) []any
	// importable is false for a table whose rows name something that belongs
	// to the machine rather than the fleet -- a runner row points at a host
	// that exists on the instance it came from and nowhere else. Its history
	// travels as the runner's session, which has no such key.
	importable bool
}

const inList = ` IN (SELECT value FROM json_each(?))`

// installationTables is the set, in the order an import inserts it: a row
// always comes after the row its foreign key names. A purge deletes it in
// reverse.
var installationTables = []installationTable{
	{"installations", `id = ?`, func(sc *installationScope) []any { return []any{sc.id} }, true},
	{"pools", `id` + inList, func(sc *installationScope) []any { return []any{sc.pools} }, true},
	{"runners", `id` + inList, func(sc *installationScope) []any { return []any{sc.runners} }, false},
	{"pool_prewarms", `pool_id` + inList, func(sc *installationScope) []any { return []any{sc.pools} }, false},
	{"jobs", `id` + inList, func(sc *installationScope) []any { return []any{sc.jobs} }, true},
	{"job_events", `job_id` + inList, func(sc *installationScope) []any { return []any{sc.jobs} }, true},
	{"webhook_deliveries", `installation_id = ?`, func(sc *installationScope) []any { return []any{sc.id} }, true},
	{"scaling_events", `pool_id` + inList, func(sc *installationScope) []any { return []any{sc.pools} }, true},
	{"runner_sessions", `runner_id` + inList, func(sc *installationScope) []any { return []any{sc.sessions} }, true},
	{"usage_daily", `installation_id = ? OR (installation_id = '' AND pool_id` + inList + `)`,
		func(sc *installationScope) []any { return []any{sc.id, sc.pools} }, true},
	{"usage_capacity_samples", `pool_id` + inList, func(sc *installationScope) []any { return []any{sc.pools} }, true},
	{"capacity_demand_deliveries", `pool_id` + inList, func(sc *installationScope) []any { return []any{sc.pools} }, true},
	{"audit_events", `target_id` + inList, func(sc *installationScope) []any { return []any{sc.named} }, true},
}

// installationScope is the IDs the set is drawn from, each list a JSON array
// so one parameter carries it into json_each.
type installationScope struct {
	id                                    string
	pools, runners, jobs, sessions, named string
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// scopeInstallation works out the IDs that belong to one installation.
//
// Its pools are the ones it has now and the ones its history names -- a pool
// deleted last month still has scaling events and sessions -- less any pool
// that belongs to another installation now, so a pool moved between
// installations is never taken from the one that holds it.
//
// A job belongs to it when the job says so, or says nothing and ran in one of
// its pools; a job another installation claims is never counted, whichever
// pool it names.
func scopeInstallation(ctx context.Context, q querier, id string) (*installationScope, error) {
	sc := &installationScope{id: id}
	pools, err := collect(ctx, q, `SELECT id FROM pools WHERE installation_id = ?1
		UNION SELECT pool_id FROM runner_sessions WHERE installation_id = ?1
		UNION SELECT pool_id FROM jobs WHERE installation_id = ?1 AND pool_id <> ''
		EXCEPT SELECT id FROM pools WHERE installation_id <> ?1`, id)
	if err != nil {
		return nil, err
	}
	sc.pools = jsonList(pools)
	runners, err := collect(ctx, q, `SELECT id FROM runners WHERE pool_id`+inList, sc.pools)
	if err != nil {
		return nil, err
	}
	sc.runners = jsonList(runners)
	jobs, err := collect(ctx, q, `SELECT id FROM jobs WHERE installation_id = ? OR (installation_id = '' AND pool_id`+inList+`)`, id, sc.pools)
	if err != nil {
		return nil, err
	}
	sc.jobs = jsonList(jobs)
	sessions, err := collect(ctx, q, `SELECT runner_id FROM runner_sessions WHERE installation_id = ? OR (installation_id = '' AND pool_id`+inList+`)`, id, sc.pools)
	if err != nil {
		return nil, err
	}
	sc.sessions = jsonList(sessions)
	// IDs are prefixed and unique across kinds, so an audit row names one of
	// these exactly when its target_id is one of them, whatever its kind says.
	named := append(append(append(append([]string{id}, pools...), runners...), jobs...), sessions...)
	sc.named = jsonList(named)
	return sc, nil
}

func collect(ctx context.Context, q querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func jsonList(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// ExportInstallation reads every row that belongs to one installation, in one
// read transaction so the tables agree with each other. The installation's
// secret columns are returned as stored -- sealed under this instance's key --
// and it is the caller's job to decide what, if anything, leaves with them.
func (s *Store) ExportInstallation(ctx context.Context, id string) ([]ArchiveTable, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM installations WHERE id = ?`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, fmt.Errorf("installation %s: %w", id, ErrNotFound)
	}
	sc, err := scopeInstallation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	out := make([]ArchiveTable, 0, len(installationTables))
	for _, t := range installationTables {
		at, err := dumpTable(ctx, tx, t, sc)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", t.name, err)
		}
		out = append(out, at)
	}
	return out, nil
}

func dumpTable(ctx context.Context, q querier, t installationTable, sc *installationScope) (ArchiveTable, error) {
	rows, err := q.QueryContext(ctx, `SELECT * FROM `+t.name+` WHERE `+t.where+` ORDER BY rowid`, t.args(sc)...)
	if err != nil {
		return ArchiveTable{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return ArchiveTable{}, err
	}
	at := ArchiveTable{Table: t.name, Columns: cols, Rows: [][]ArchiveValue{}}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return ArchiveTable{}, err
		}
		row := make([]ArchiveValue, len(cols))
		for i, v := range vals {
			row[i] = ArchiveValue{V: v}
		}
		at.Rows = append(at.Rows, row)
	}
	return at, rows.Err()
}

// PurgeInstallation deletes every row ExportInstallation would have written,
// in one transaction, and returns the IDs of the runner rows that went so the
// caller can announce them. Rows that belong to any other installation are
// out of scope by construction: nothing is deleted except by the IDs the
// scope names.
func (s *Store) PurgeInstallation(ctx context.Context, id string) ([]string, error) {
	var runners []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM installations WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("installation %s: %w", id, ErrNotFound)
		}
		sc, err := scopeInstallation(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(sc.runners), &runners); err != nil {
			return err
		}
		for i := len(installationTables) - 1; i >= 0; i-- {
			t := installationTables[i]
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t.name+` WHERE `+t.where, t.args(sc)...); err != nil {
				return fmt.Errorf("purging %s: %w", t.name, err)
			}
		}
		return nil
	})
	return runners, err
}

// ImportedInstallation says what an import wrote and what it left out.
type ImportedInstallation struct {
	InstallationID string         `json:"installation_id"`
	Rows           map[string]int `json:"rows"`
	Skipped        map[string]int `json:"skipped"`
}

// ErrArchiveShape is an archive this build cannot read: a table it does not
// hold, a column it has never had, or rows for more than one installation.
var ErrArchiveShape = errors.New("the archive does not fit this instance's schema")

// ImportInstallation writes an exported installation into this database, in
// one transaction: all of it or none. Every row keeps its ID, so the history
// an audit row or a session names is still the history it names.
//
// The installation's secret columns must already be sealed under this
// instance's key; the store has no business with a passphrase. An
// installation, job or other row whose ID or unique key is already here is
// ErrConflict, because silently keeping one of two would decide for the
// operator which history is true.
func (s *Store) ImportInstallation(ctx context.Context, tables []ArchiveTable) (*ImportedInstallation, error) {
	byName := map[string]ArchiveTable{}
	for _, t := range tables {
		if _, dup := byName[t.Table]; dup {
			return nil, fmt.Errorf("%w: %s appears twice", ErrArchiveShape, t.Table)
		}
		byName[t.Table] = t
	}
	known := map[string]bool{}
	for _, t := range installationTables {
		known[t.name] = true
	}
	for name := range byName {
		if !known[name] {
			return nil, fmt.Errorf("%w: it has a table called %q, which is not part of an installation", ErrArchiveShape, name)
		}
	}
	if inst := byName["installations"]; len(inst.Rows) != 1 {
		return nil, fmt.Errorf("%w: it must hold exactly one installation, and holds %d", ErrArchiveShape, len(inst.Rows))
	}
	out := &ImportedInstallation{Rows: map[string]int{}, Skipped: map[string]int{}}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, spec := range installationTables {
			t, ok := byName[spec.name]
			if !ok || len(t.Rows) == 0 {
				continue
			}
			if !spec.importable {
				out.Skipped[spec.name] = len(t.Rows)
				continue
			}
			have, err := tableColumns(ctx, tx, spec.name)
			if err != nil {
				return err
			}
			for _, c := range t.Columns {
				if !have[c] {
					return fmt.Errorf("%w: %s.%s is not a column here; the archive came from a newer Zoomies, so upgrade this instance first",
						ErrArchiveShape, spec.name, c)
				}
			}
			stmt := `INSERT INTO ` + spec.name + ` (` + strings.Join(t.Columns, ", ") + `) VALUES (` +
				strings.TrimSuffix(strings.Repeat("?, ", len(t.Columns)), ", ") + `)`
			for _, row := range t.Rows {
				if len(row) != len(t.Columns) {
					return fmt.Errorf("%w: a row in %s has %d values for %d columns", ErrArchiveShape, spec.name, len(row), len(t.Columns))
				}
				args := make([]any, len(row))
				for i, v := range row {
					args[i] = v.V
					if spec.name == "installations" && t.Columns[i] == "id" {
						out.InstallationID, _ = v.V.(string)
					}
				}
				if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
					if isUnique(err) {
						return fmt.Errorf("%s already holds a row this archive has (%v): %w", spec.name, err, ErrConflict)
					}
					return fmt.Errorf("writing %s: %w", spec.name, err)
				}
			}
			out.Rows[spec.name] = len(t.Rows)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// tableColumns reads a table's columns from the schema, which is what makes
// it safe to put an archive's column names into a statement: a name that is
// not one of these never reaches SQL.
func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

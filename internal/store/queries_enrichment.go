package store

import (
	"context"
	"database/sql"
	"time"
)

type Enrichment struct {
	Kind, Target string
	Generation   int64
}

func (s *Store) QueueEnrichment(ctx context.Context, kind, target string) error {
	_, err := s.exec(ctx, `INSERT INTO control_enrichment(kind,target) VALUES (?,?) ON CONFLICT(kind,target) DO UPDATE SET generation=generation+1`, kind, target)
	return err
}

// Claim under the writer so concurrent workers cannot fetch the same item.
// A crashed worker's lease expires; generation prevents an old ACK from losing
// an event that arrived during its lookup.
func (s *Store) ClaimEnrichment(ctx context.Context, limit int) (out []Enrichment, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT kind,target,generation FROM control_enrichment WHERE next_attempt<=? ORDER BY next_attempt,kind,target LIMIT ?`, ms(s.Now()), limit)
		if e != nil {
			return e
		}
		for rows.Next() {
			var v Enrichment
			if e = rows.Scan(&v.Kind, &v.Target, &v.Generation); e != nil {
				rows.Close()
				return e
			}
			out = append(out, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, v := range out {
			if _, e = tx.ExecContext(ctx, `UPDATE control_enrichment SET next_attempt=? WHERE kind=? AND target=?`, ms(s.Now().Add(time.Minute)), v.Kind, v.Target); e != nil {
				return e
			}
		}
		return nil
	})
	return
}
func (s *Store) FinishEnrichment(ctx context.Context, v Enrichment, success bool) error {
	if !success {
		return nil
	} // the claim's retry deadline provides backoff
	_, err := s.exec(ctx, `DELETE FROM control_enrichment WHERE kind=? AND target=? AND generation=?`, v.Kind, v.Target, v.Generation)
	return err
}

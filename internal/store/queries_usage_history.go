package store

import (
	"context"
	"time"
)

// UsageBucket uses fixed elapsed-hour or elapsed-day intervals anchored at the
// requested start. Counts are events; time is clipped to bucket boundaries.
type UsageBucket struct {
	From             time.Time `json:"from"`
	Queued           int       `json:"queued"`
	Started          int       `json:"started"`
	Succeeded        int       `json:"succeeded"`
	Failed           int       `json:"failed"`
	Cancelled        int       `json:"cancelled"`
	Unknown          int       `json:"unknown"`
	ExecutionSeconds float64   `json:"execution_seconds"`
	AllocatedSeconds float64   `json:"allocated_seconds"`
	CapacitySamples  int       `json:"capacity_samples"`
	CapacityReached  int       `json:"capacity_reached"`
}

// RecordUsageCapacity coalesces reconcile observations. A blocked observation
// must not be erased by a successful placement later in the same minute.
func (s *Store) RecordUsageCapacity(ctx context.Context, pool string, at time.Time, blocked bool) error {
	_, err := s.exec(ctx, `INSERT INTO usage_capacity_samples(pool_id,at,blocked) VALUES(?,?,?)
 ON CONFLICT(pool_id,at) DO UPDATE SET blocked=MAX(blocked,excluded.blocked) WHERE blocked < excluded.blocked`, pool, ms(at.Truncate(time.Minute)), blocked)
	return err
}

func (s *Store) PruneUsageCapacity(ctx context.Context, before time.Time) (int64, error) {
	r, err := s.exec(ctx, `DELETE FROM usage_capacity_samples WHERE at < ?`, ms(before))
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

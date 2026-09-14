package store

import (
	"context"
	"database/sql"
	"time"
)

// HostSample is one host at one minute: its slots, what its agent measured,
// and what the scheduler had promised away on it. It is what the Hosts page
// draws its capacity map from, one series per host.
//
// The pointer fields are measurements the host may not have made. Nil is "not
// measured" and is drawn as a gap; zero is a measured zero. An agent too old to
// report CPU must not read as an idle machine.
type HostSample struct {
	HostID string    `json:"host_id"`
	At     time.Time `json:"at"`
	// Capacity is the slots the host was taking at that minute: the configured
	// capacity stepped down by any throttle, which is what active_runners is
	// measured against.
	Capacity      int      `json:"capacity"`
	ActiveRunners int      `json:"active_runners"`
	CPUPercent    *float64 `json:"cpu_percent,omitempty"`
	LoadAverage1  *float64 `json:"load_average_1m,omitempty"`
	// CPUs and MemoryMB are the machine's size, carried on every row because
	// the ratio is what the chart draws and a host can be resized between
	// samples.
	CPUs                int64   `json:"cpus,omitempty"`
	MemoryMB            int64   `json:"memory_mb,omitempty"`
	MemoryAvailableMB   *int64  `json:"memory_available_mb,omitempty"`
	AllocatableCPUs     float64 `json:"allocatable_cpus,omitempty"`
	AllocatableMemoryMB int64   `json:"allocatable_memory_mb,omitempty"`
	// ReservedCPUs and ReservedMemoryMB are what the live runners had promised
	// away, from the last scheduling pass. Nil before a pass has run.
	ReservedCPUs     *float64 `json:"reserved_cpus,omitempty"`
	ReservedMemoryMB *int64   `json:"reserved_memory_mb,omitempty"`
	DiskTotalMB      int64    `json:"disk_total_mb,omitempty"`
	DiskFreeMB       int64    `json:"disk_free_mb,omitempty"`
}

// RecordHostSamples stores one sample per host for a minute, replacing any
// sample the same host already has for it so a restart mid-minute overwrites
// rather than double-counts. The batch is one transaction: a fleet of forty
// hosts is forty rows a minute, and forty lock acquisitions would be forty
// chances for a reconcile pass to wait.
func (s *Store) RecordHostSamples(ctx context.Context, samples []HostSample) error {
	if len(samples) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, h := range samples {
			minute := h.At.UTC().Truncate(time.Minute)
			if _, err := tx.ExecContext(ctx, `INSERT INTO host_samples
				(host_id, at, capacity, active_runners, cpu_percent, load_average_1m,
				 cpus, memory_mb, memory_available_mb, allocatable_cpus, allocatable_memory_mb,
				 reserved_cpus, reserved_memory_mb, disk_total_mb, disk_free_mb)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(host_id, at) DO UPDATE SET capacity=excluded.capacity,
					active_runners=excluded.active_runners,
					cpu_percent=excluded.cpu_percent,
					load_average_1m=excluded.load_average_1m,
					cpus=excluded.cpus, memory_mb=excluded.memory_mb,
					memory_available_mb=excluded.memory_available_mb,
					allocatable_cpus=excluded.allocatable_cpus,
					allocatable_memory_mb=excluded.allocatable_memory_mb,
					reserved_cpus=excluded.reserved_cpus,
					reserved_memory_mb=excluded.reserved_memory_mb,
					disk_total_mb=excluded.disk_total_mb, disk_free_mb=excluded.disk_free_mb`,
				h.HostID, ms(minute), h.Capacity, h.ActiveRunners, h.CPUPercent, h.LoadAverage1,
				h.CPUs, h.MemoryMB, h.MemoryAvailableMB, h.AllocatableCPUs, h.AllocatableMemoryMB,
				h.ReservedCPUs, h.ReservedMemoryMB, h.DiskTotalMB, h.DiskFreeMB); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListHostSamples returns every host's samples since a cutoff, oldest first
// and grouped by host. An empty hostID means every host; the Hosts page asks
// for all of them at once, since the chart is one picture of the fleet.
func (s *Store) ListHostSamples(ctx context.Context, since time.Time, hostID string) ([]HostSample, error) {
	query := `SELECT host_id, at, capacity, active_runners, cpu_percent, load_average_1m,
		cpus, memory_mb, memory_available_mb, allocatable_cpus, allocatable_memory_mb,
		reserved_cpus, reserved_memory_mb, disk_total_mb, disk_free_mb
		FROM host_samples WHERE at >= ?`
	args := []any{ms(since)}
	if hostID != "" {
		query += ` AND host_id = ?`
		args = append(args, hostID)
	}
	rows, err := s.read.QueryContext(ctx, query+` ORDER BY host_id, at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostSample
	for rows.Next() {
		var h HostSample
		var t int64
		var cpu, load, reservedCPUs sql.NullFloat64
		var memAvail, reservedMem sql.NullInt64
		if err := rows.Scan(&h.HostID, &t, &h.Capacity, &h.ActiveRunners, &cpu, &load,
			&h.CPUs, &h.MemoryMB, &memAvail, &h.AllocatableCPUs, &h.AllocatableMemoryMB,
			&reservedCPUs, &reservedMem, &h.DiskTotalMB, &h.DiskFreeMB); err != nil {
			return nil, err
		}
		h.At = at(t)
		if cpu.Valid {
			h.CPUPercent = &cpu.Float64
		}
		if load.Valid {
			h.LoadAverage1 = &load.Float64
		}
		if memAvail.Valid {
			h.MemoryAvailableMB = &memAvail.Int64
		}
		if reservedCPUs.Valid {
			h.ReservedCPUs = &reservedCPUs.Float64
		}
		if reservedMem.Valid {
			h.ReservedMemoryMB = &reservedMem.Int64
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// PruneHostSamples deletes host samples older than the cutoff.
func (s *Store) PruneHostSamples(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM host_samples WHERE at < ?`, ms(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

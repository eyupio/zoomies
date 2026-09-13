package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Machines are written by three authors -- an operator, a reconcile pass, and
// the provider by way of a pass reporting what it saw -- so every writer below
// is as narrow as what it knows. The two that decide something are
// compare-and-set statements rather than a read followed by a write, because
// the thing being decided is which of two passes may act, and a read both of
// them can make first decides nothing.

const machineCols = `id, provider_id, name, state, message, pool_id,
	owner_controller_id, owner_fingerprint, resource_zone, resource_id, resource_detail,
	address, ownership_verified_at, ownership_error,
	host_id, join_token_id, capacity, labels,
	op_id, op_kind, op_handle, op_holder, op_started_at, op_deadline_at, op_outcome_unknown,
	attempts, next_attempt_at, provider_error, bootstrap_error,
	created_at, updated_at, reservation_expires_at, create_started_at, created_ok_at,
	started_at, bootstrapped_at, enrolled_at, ready_at, idle_since, draining_at,
	delete_started_at, deleted_at`

// nullText renders an optional foreign key. pool_id and host_id are the two
// columns that point at rows an operator can delete, so both are real SQL NULLs
// rather than empty strings: the empty string is not a pool, and a foreign key
// would refuse it.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanMachine(sc interface{ Scan(...any) error }) (*Machine, error) {
	var m Machine
	var poolID, hostID sql.NullString
	var unknown int
	var created, updated int64
	var verified, nextAttempt, opStarted, opDeadline sql.NullInt64
	var reservation, createStarted, createdOK, started, bootstrapped sql.NullInt64
	var enrolled, ready, idle, draining, deleteStarted, deleted sql.NullInt64
	err := sc.Scan(&m.ID, &m.ProviderID, &m.Name, &m.State, &m.Message, &poolID,
		&m.OwnerControllerID, &m.OwnerFingerprint, &m.ResourceZone, &m.ResourceID,
		&m.ResourceDetail, &m.Address, &verified, &m.OwnershipError,
		&hostID, &m.JoinTokenID, &m.Capacity, &m.Labels,
		&m.OpID, &m.OpKind, &m.OpHandle, &m.OpHolder, &opStarted, &opDeadline, &unknown,
		&m.Attempts, &nextAttempt, &m.ProviderError, &m.BootstrapError,
		&created, &updated, &reservation, &createStarted, &createdOK,
		&started, &bootstrapped, &enrolled, &ready, &idle, &draining,
		&deleteStarted, &deleted)
	if err != nil {
		return nil, err
	}
	m.PoolID, m.HostID = poolID.String, hostID.String
	m.OpOutcomeUnknown = unknown == 1
	m.CreatedAt, m.UpdatedAt = at(created), at(updated)
	m.OwnershipVerifiedAt, m.NextAttemptAt = atp(verified), atp(nextAttempt)
	m.OpStartedAt, m.OpDeadlineAt = atp(opStarted), atp(opDeadline)
	m.ReservationExpiresAt, m.CreateStartedAt, m.CreatedOKAt = atp(reservation), atp(createStarted), atp(createdOK)
	m.StartedAt, m.BootstrappedAt, m.EnrolledAt, m.ReadyAt = atp(started), atp(bootstrapped), atp(enrolled), atp(ready)
	m.IdleSince, m.DrainingAt, m.DeleteStartedAt, m.DeletedAt = atp(idle), atp(draining), atp(deleteStarted), atp(deleted)
	return &m, nil
}

// CreateMachine writes the row that exists before the resource does.
//
// It is the whole ordering the design rests on: the identity is ours, minted
// here, and a create whose outcome we never learn is answered by asking the
// provider about this name rather than by creating a second machine.
func (s *Store) CreateMachine(ctx context.Context, m *Machine) error {
	if m.ID == "" {
		m.ID = NewID(PrefixMachine)
	}
	if m.Name == "" {
		m.Name = NewMachineName(m.ID)
	}
	if m.State == "" {
		m.State = MachinePlanned
	}
	if !m.State.Valid() {
		return fmt.Errorf("%w: %q is not a machine state", ErrInvalidTransition, m.State)
	}
	now := s.Now()
	m.CreatedAt, m.UpdatedAt = now, now
	_, err := s.exec(ctx, `INSERT INTO machines (`+machineCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ProviderID, m.Name, string(m.State), m.Message, nullText(m.PoolID),
		m.OwnerControllerID, m.OwnerFingerprint, m.ResourceZone, m.ResourceID,
		m.ResourceDetail, m.Address, msp(m.OwnershipVerifiedAt), m.OwnershipError,
		nullText(m.HostID), m.JoinTokenID, m.Capacity, m.Labels,
		m.OpID, string(m.OpKind), m.OpHandle, m.OpHolder, msp(m.OpStartedAt),
		msp(m.OpDeadlineAt), boolInt(m.OpOutcomeUnknown),
		m.Attempts, msp(m.NextAttemptAt), m.ProviderError, m.BootstrapError,
		ms(m.CreatedAt), ms(m.UpdatedAt), msp(m.ReservationExpiresAt), msp(m.CreateStartedAt),
		msp(m.CreatedOKAt), msp(m.StartedAt), msp(m.BootstrappedAt), msp(m.EnrolledAt),
		msp(m.ReadyAt), msp(m.IdleSince), msp(m.DrainingAt), msp(m.DeleteStartedAt),
		msp(m.DeletedAt))
	return wrapWrite(err)
}

// GetMachine returns one machine by ID.
func (s *Store) GetMachine(ctx context.Context, id string) (*Machine, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id)
	m, err := scanMachine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("machine %s: %w", id, ErrNotFound)
	}
	return m, err
}

// GetMachineByHost returns the machine that enrolled a host, which is what
// makes deleting that host this controller's business at all.
func (s *Store) GetMachineByHost(ctx context.Context, hostID string) (*Machine, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE host_id = ?`, hostID)
	m, err := scanMachine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("machine for host %s: %w", hostID, ErrNotFound)
	}
	return m, err
}

// GetMachineByResource answers the question a sweep asks about every resource
// it finds: is this one of ours, and which row is it? A resource with no row is
// an orphan, and an orphan is never deleted without a person saying so.
func (s *Store) GetMachineByResource(ctx context.Context, providerID, zone, resourceID string) (*Machine, error) {
	row := s.read.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines
		WHERE provider_id = ? AND resource_zone = ? AND resource_id = ?`, providerID, zone, resourceID)
	m, err := scanMachine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("machine for %s %s on %s: %w", providerID, resourceID, zone, ErrNotFound)
	}
	return m, err
}

// MachineFilter narrows a machine listing.
type MachineFilter struct {
	ProviderIDs []string
	PoolIDs     []string
	HostIDs     []string
	States      []MachineState
	Search      string
	// IncludeDeleted keeps confirmed-deleted machines in the result. The
	// default view hides them for the reason the runners grid hides removed
	// rows: a fleet that has been buying machines for a month has far more
	// history than present.
	IncludeDeleted bool
}

var machineSortCols = map[string]string{
	"name":       "name",
	"state":      "state",
	"created_at": "created_at",
	"provider":   "provider_id",
	"pool":       "pool_id",
}

// ListMachines returns a filtered, paginated page of machines plus the total
// number of rows matching the filter.
func (s *Store) ListMachines(ctx context.Context, f MachineFilter, p Page) ([]*Machine, int, error) {
	where, args := machineWhere(f)
	var total int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM machines `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + machineCols + ` FROM machines ` + where +
		` ORDER BY ` + p.orderBy(machineSortCols, "created_at DESC") + ` LIMIT ? OFFSET ?`
	args = append(args, p.limit(50, 500), max(p.Offset, 0))
	rows, err := s.read.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func machineWhere(f MachineFilter) (string, []any) {
	var cond []string
	var args []any
	if !f.IncludeDeleted && len(f.States) == 0 {
		cond = append(cond, `state != 'deleted'`)
	}
	for _, in := range []struct {
		column string
		values []string
	}{
		{"provider_id", f.ProviderIDs},
		{"pool_id", f.PoolIDs},
		{"host_id", f.HostIDs},
	} {
		if len(in.values) == 0 {
			continue
		}
		ph := make([]string, len(in.values))
		for i, v := range in.values {
			ph[i] = "?"
			args = append(args, v)
		}
		cond = append(cond, in.column+` IN (`+strings.Join(ph, ",")+`)`)
	}
	if len(f.States) > 0 {
		ph := make([]string, len(f.States))
		for i, st := range f.States {
			ph[i] = "?"
			args = append(args, string(st))
		}
		cond = append(cond, `state IN (`+strings.Join(ph, ",")+`)`)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		// resource_id is in the search because the identifier an operator has
		// in front of them is usually the hypervisor's, not ours: they are
		// looking at a VM in a console and asking what it belongs to.
		cond = append(cond, `(name LIKE ? ESCAPE '\' OR id LIKE ? ESCAPE '\' OR resource_id LIKE ? ESCAPE '\' OR address LIKE ? ESCAPE '\')`)
		like := likePattern(q)
		args = append(args, like, like, like, like)
	}
	if len(cond) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(cond, " AND "), args
}

// ListMachinesForProvider returns the machines a pass can still do something
// about. It is deliberately unpaginated: every reconcile pass reads all of
// them, and a page boundary would hide a machine from its own lifecycle.
func (s *Store) ListMachinesForProvider(ctx context.Context, providerID string) ([]*Machine, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT `+machineCols+` FROM machines
		WHERE provider_id = ? AND state != 'deleted' ORDER BY created_at, id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountOwnedMachines returns, per provider, how many machines still believe
// they have a resource.
//
// This is the budget: what max_machines is compared against, and what a cost
// estimate is multiplied by. Its WHERE clause is the SQL half of Machine.Owns,
// and a test holds the two definitions equal -- a machine that stopped counting
// here while still counting there is a fleet that buys a machine it is already
// paying for.
func (s *Store) CountOwnedMachines(ctx context.Context) (map[string]int, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT provider_id, COUNT(*) FROM machines
		WHERE resource_id <> '' AND deleted_at IS NULL GROUP BY provider_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// TransitionMachine is the only way a machine's state changes. It stamps
// exactly the timestamps that belong to the transition, refuses one outside the
// allow-list with ErrInvalidTransition, and returns the row as it now stands so
// the caller can publish without a second read.
//
// deleted_at is conspicuously not among the stamps. It is what stops a machine
// counting against its provider's ceiling, so only ConfirmMachineDeleted -- a
// provider that looked and could not find the resource -- may write it. A state
// change that also wrote it would let "we think this is gone" free the budget.
func (s *Store) TransitionMachine(ctx context.Context, id string, to MachineState, message string) (*Machine, error) {
	if !to.Valid() {
		return nil, fmt.Errorf("%w: %q is not a machine state", ErrInvalidTransition, to)
	}
	var out *Machine
	err := s.tx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id)
		m, err := scanMachine(row)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("machine %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if !CanTransitionMachine(m.State, to) {
			return fmt.Errorf("%w: machine %s cannot go %s -> %s", ErrInvalidTransition, id, m.State, to)
		}
		now := s.Now()
		prev := m.State
		m.State = to
		m.UpdatedAt = now
		if message != "" {
			m.Message = message
		}
		// The phase stamps are first-write-wins, so the timeline says when a
		// machine first reached each phase rather than when it last passed
		// through. A drain is the exception: it can be cancelled and started
		// again, and the drain timeout has to count from the latest one or a
		// machine that drained yesterday would be overdue the moment it drains
		// today.
		switch to {
		case MachineCreating:
			stampOnce(&m.CreateStartedAt, now)
		case MachineStarting:
			stampOnce(&m.CreatedOKAt, now)
		case MachineBootstrapping:
			// Reachable straight from creating, for a provider whose create
			// leaves the guest running, so the create's own stamp is filled in
			// here too rather than being lost with the state that skipped it.
			stampOnce(&m.CreatedOKAt, now)
			stampOnce(&m.StartedAt, now)
		case MachineEnrolling:
			stampOnce(&m.BootstrappedAt, now)
		case MachineReady:
			stampOnce(&m.EnrolledAt, now)
			stampOnce(&m.ReadyAt, now)
			// A machine that has come back into service is not idle, whatever
			// it was when it left.
			m.IdleSince = nil
		case MachineDraining:
			if prev != MachineDraining {
				t := now
				m.DrainingAt = &t
			}
		case MachineDeleting:
			stampOnce(&m.DeleteStartedAt, now)
		}
		_, err = tx.ExecContext(ctx, `UPDATE machines SET state=?, message=?, updated_at=?,
			create_started_at=?, created_ok_at=?, started_at=?, bootstrapped_at=?,
			enrolled_at=?, ready_at=?, idle_since=?, draining_at=?, delete_started_at=?
			WHERE id=?`,
			string(m.State), m.Message, ms(m.UpdatedAt), msp(m.CreateStartedAt),
			msp(m.CreatedOKAt), msp(m.StartedAt), msp(m.BootstrappedAt), msp(m.EnrolledAt),
			msp(m.ReadyAt), msp(m.IdleSince), msp(m.DrainingAt), msp(m.DeleteStartedAt), m.ID)
		if err != nil {
			return err
		}
		out = m
		return nil
	})
	return out, err
}

// stampOnce fills in a phase timestamp that has never been written.
func stampOnce(dst **time.Time, now time.Time) {
	if *dst == nil {
		t := now
		*dst = &t
	}
}

// SetMachineResource records the identity the create will use. It is the write
// that MUST land before Create is called, and it refuses to move an identity
// that is already set: a machine has one resource for its whole life, and a
// second one would be a leak with a row pointing away from it.
func (s *Store) SetMachineResource(ctx context.Context, id, zone, resourceID, fingerprint, controllerID string) error {
	if resourceID == "" {
		return fmt.Errorf("machine %s: a resource identity needs an id from the provider; "+
			"allocate one before recording it", id)
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var got string
		return tx.QueryRowContext(ctx, `UPDATE machines SET resource_zone=?, resource_id=?,
			owner_fingerprint=?, owner_controller_id=?, updated_at=?
			WHERE id=? AND resource_id='' RETURNING resource_id`,
			zone, resourceID, fingerprint, controllerID, ms(s.Now()), id).Scan(&got)
	})
	if errors.Is(err, sql.ErrNoRows) {
		current, getErr := s.GetMachine(ctx, id)
		if getErr != nil {
			return getErr
		}
		return fmt.Errorf("%w: machine %s already names %s on %s, so it cannot be given %s; "+
			"a machine has one resource for its whole life",
			ErrConflict, id, current.ResourceID, current.ResourceZone, resourceID)
	}
	return wrapWrite(err)
}

// ClaimMachineOperation is how exactly one pass, in exactly one controller,
// takes a step. The read and the write are one statement (ConfirmRunnerCleanup's
// idiom), so two passes racing cannot both believe they own it. An expired claim
// is takeable, because a controller that died mid-operation must not lock a
// machine for ever -- and taking it is safe precisely because the next move is
// an observation, never a mutation.
//
// What the claim deliberately does not clear is op_handle and
// op_outcome_unknown: they are the only evidence of what the dead controller's
// call actually did, and the pass taking over needs them to ask.
func (s *Store) ClaimMachineOperation(ctx context.Context, id string, op MachineOperation, now time.Time) (*Machine, error) {
	if !op.Kind.Valid() || op.Kind == MachineOpNone {
		return nil, fmt.Errorf("machine %s: %q is not an operation this machine can be doing", id, op.Kind)
	}
	if op.Timeout <= 0 {
		return nil, fmt.Errorf("machine %s: an operation claim needs a timeout; without one a controller "+
			"that dies mid-call holds this machine for ever", id)
	}
	if op.ID == "" {
		op.ID = NewID(PrefixMachineOp)
	}
	deadline := now.Add(op.Timeout)
	var m *Machine
	err := s.tx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `UPDATE machines SET op_id=?, op_kind=?, op_holder=?,
			op_started_at=?, op_deadline_at=?, updated_at=?
			WHERE id=? AND (op_id='' OR op_deadline_at < ?) RETURNING `+machineCols,
			op.ID, string(op.Kind), op.Holder, ms(now), ms(deadline), ms(now), id, ms(now))
		var err error
		m, err = scanMachine(row)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		held, getErr := s.GetMachine(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		deadlineOf := "with no deadline"
		if held.OpDeadlineAt != nil {
			deadlineOf = "until " + held.OpDeadlineAt.Format(time.RFC3339)
		}
		return nil, fmt.Errorf("%w: machine %s is doing %s for %s %s",
			ErrMachineBusy, id, held.OpKind, held.OpHolder, deadlineOf)
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// SetMachineOperationHandle records the provider's own handle for the call in
// flight -- a task id -- which is what a recovery pass asks about after a
// restart.
//
// The first write wins, and a write for an operation this pass no longer holds
// changes nothing: the handle recovery asks about has to be the one the call
// that actually went out came back with.
func (s *Store) SetMachineOperationHandle(ctx context.Context, id, opID, handle string) error {
	_, err := s.exec(ctx, `UPDATE machines SET op_handle=? WHERE id=? AND op_id=? AND op_handle=''`,
		handle, id, opID)
	return err
}

// MarkMachineOutcomeUnknown records that a call went out and was never
// answered.
//
// It leaves the claim in place on purpose. The operation is not finished --
// nobody knows whether it happened -- and the one thing that must not follow is
// a retry that treats silence as failure and creates a second resource.
func (s *Store) MarkMachineOutcomeUnknown(ctx context.Context, id, opID, detail string) error {
	_, err := s.exec(ctx, `UPDATE machines SET op_outcome_unknown=1, provider_error=?, updated_at=?
		WHERE id=? AND op_id=?`, detail, ms(s.Now()), id, opID)
	return err
}

// FinishMachineOperation gives the claim back and settles the retry accounting.
//
// The state is not its business: what an operation's outcome means for the
// lifecycle is TransitionMachine's to say, and keeping the two apart is what
// lets a create that succeeded at the provider but failed to be recorded be
// retried without also being un-created.
func (s *Store) FinishMachineOperation(ctx context.Context, id, opID string, out MachineOpOutcome, message string, nextAttempt *time.Time) error {
	if !out.Valid() {
		return fmt.Errorf("machine %s: %q is not an operation outcome", id, out)
	}
	// Every identifier below is chosen here and never comes from a caller.
	const clear = `op_id='', op_kind='', op_handle='', op_holder='', op_started_at=NULL,
		op_deadline_at=NULL, op_outcome_unknown=0, updated_at=?,
		message=CASE WHEN ?='' THEN message ELSE ? END`
	var counting string
	switch out {
	case MachineOpSucceeded:
		// A success settles the provider's complaint and the backoff with it.
		// bootstrap_error is the other system's and is left alone, for the
		// reason 0022 gives: neither half may answer for the other.
		counting = `attempts=0, next_attempt_at=NULL, provider_error=''`
	case MachineOpFailed:
		counting = `attempts=attempts+1, next_attempt_at=?, provider_error=CASE WHEN ?='' THEN provider_error ELSE ? END`
	case MachineOpReleased:
		counting = `next_attempt_at=?`
	}
	args := []any{ms(s.Now()), message, message}
	switch out {
	case MachineOpFailed:
		args = append(args, msp(nextAttempt), message, message)
	case MachineOpReleased:
		args = append(args, msp(nextAttempt))
	}
	args = append(args, id, opID)
	_, err := s.exec(ctx, `UPDATE machines SET `+clear+`, `+counting+` WHERE id=? AND op_id=?`, args...)
	return err
}

// SetMachineOwnershipVerified records a fresh observation that agreed this
// resource is ours, and clears the complaint it answers.
func (s *Store) SetMachineOwnershipVerified(ctx context.Context, id string, at time.Time) error {
	_, err := s.exec(ctx, `UPDATE machines SET ownership_verified_at=?, ownership_error='' WHERE id=?`,
		ms(at), id)
	return err
}

// SetMachineOwnershipError records that ownership could not be proved, and
// takes the previous proof away with it.
//
// Clearing the stamp is the point: a delete needs a verification from this
// pass, and leaving yesterday's behind would let an observation that has since
// gone wrong keep authorising deletions.
func (s *Store) SetMachineOwnershipError(ctx context.Context, id, detail string) error {
	if detail == "" {
		detail = "ownership could not be verified, and the check did not say why"
	}
	_, err := s.exec(ctx, `UPDATE machines SET ownership_error=?, ownership_verified_at=NULL, updated_at=?
		WHERE id=?`, detail, ms(s.Now()), id)
	return err
}

// LinkMachineHost records that a host enrolled with the token this machine
// minted, which is the only thing that grants deletion authority over that
// host.
//
// The guard is what makes it safe: a machine already linked keeps the host it
// has, and a host already claimed by another machine is refused by the unique
// index. Neither is an accident worth resolving automatically -- both mean the
// enrolment link is not what somebody thinks it is.
func (s *Store) LinkMachineHost(ctx context.Context, id, hostID, joinTokenID string, at time.Time) error {
	res, err := s.exec(ctx, `UPDATE machines SET host_id=?, join_token_id=?,
		enrolled_at=COALESCE(enrolled_at, ?), updated_at=? WHERE id=? AND host_id IS NULL`,
		hostID, joinTokenID, ms(at), ms(s.Now()), id)
	if err != nil {
		return wrapWrite(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		current, getErr := s.GetMachine(ctx, id)
		if getErr != nil {
			return getErr
		}
		return fmt.Errorf("%w: machine %s is already enrolled as host %s, so it cannot also be host %s",
			ErrConflict, id, current.HostID, hostID)
	}
	return nil
}

// SetMachineAddress records where the provider says the guest can be reached.
//
// It is a writer of its own rather than part of a transition because the
// address is the provider's observation, not a decision: a machine that booted
// and never joined is looked at through this, and losing it because the state
// did not happen to move would take away the one thing an operator has to go on.
func (s *Store) SetMachineAddress(ctx context.Context, id, address string) error {
	_, err := s.exec(ctx, `UPDATE machines SET address=?, updated_at=? WHERE id=?`,
		address, ms(s.Now()), id)
	return err
}

// SetMachineJoinToken records the credential this machine was given.
//
// Last write wins, and deliberately so. Only the hash of a join token is kept,
// so a payload that was written into a guest and lost cannot be written again
// with the same token -- the controller mints another and points the row at it.
// That is safe because both are single-use and both are scoped to this
// machine's one name, so at most one of them can ever enrol anything, and it is
// why the caller deletes the one it is replacing.
func (s *Store) SetMachineJoinToken(ctx context.Context, id, joinTokenID string) error {
	_, err := s.exec(ctx, `UPDATE machines SET join_token_id=?, updated_at=? WHERE id=?`,
		joinTokenID, ms(s.Now()), id)
	return err
}

// SetMachineIdleSince records when this machine's host last had nothing to do,
// or clears it. It is the clock scale-down counts from, so it is a writer of
// its own: a pass that rewrote it every time it looked would make a machine
// that has been idle all afternoon look like one that went idle a moment ago.
func (s *Store) SetMachineIdleSince(ctx context.Context, id string, since *time.Time) error {
	_, err := s.exec(ctx, `UPDATE machines SET idle_since=? WHERE id=?`, msp(since), id)
	return err
}

// RecordMachineFailure notes what went wrong and when to try again.
//
// It is deliberately not a transition: a create that failed once is still a
// create, and forcing the machine through the state machine to record a
// complaint would make the fleet's accounting depend on how noisy a provider
// is. Which column it writes is what tells an operator which half to look at --
// the hypervisor, or the guest.
func (s *Store) RecordMachineFailure(ctx context.Context, id string, src MachineErrorSource, detail string, nextAttempt time.Time) error {
	if !src.Valid() {
		return fmt.Errorf("machine %s: %q is not somewhere a failure can come from", id, src)
	}
	if detail == "" {
		detail = "the attempt failed without saying why"
	}
	// The column is chosen here and never comes from a caller.
	column := "provider_error"
	if src == MachineErrorBootstrap {
		column = "bootstrap_error"
	}
	_, err := s.exec(ctx, `UPDATE machines SET `+column+`=?, attempts=attempts+1,
		next_attempt_at=?, updated_at=? WHERE id=?`, detail, ms(nextAttempt), ms(s.Now()), id)
	return err
}

// ConfirmMachineDeleted stamps the confirmation exactly once, so the pass that
// stamps it is the one that publishes the deletion and frees the budget.
//
// "Confirmed" means an inspect that could not find the resource. A 200 from the
// delete call is not that, and treating it as such is how a fleet stops
// counting a VM that is still running.
func (s *Store) ConfirmMachineDeleted(ctx context.Context, id string, now time.Time) (bool, error) {
	var stamped sql.NullInt64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `UPDATE machines SET deleted_at=?, updated_at=?
			WHERE id=? AND deleted_at IS NULL RETURNING deleted_at`, ms(now), ms(now), id).Scan(&stamped)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stamped.Valid, nil
}

// ForgetMachine deletes one machine row outright, without touching anything the
// provider holds.
//
// It is the escape hatch behind POST /machines/{id}/release, and it is
// deliberately the only way a row that still names a resource can leave the
// database: PruneMachines refuses one, and DeleteProvider refuses a whole
// provider while one exists. Forgetting a row that still owns a resource is how
// a fleet loses a VM it is paying for, so the route above this asks for the
// machine's name to be typed before it calls, and the audit row it writes keeps
// the resource identifier the database is about to stop holding.
func (s *Store) ForgetMachine(ctx context.Context, id string) error {
	res, err := s.exec(ctx, `DELETE FROM machines WHERE id=?`, id)
	if err != nil {
		return err
	}
	return affected(res, "machine", id)
}

// PendingMachineWork returns machines with unfinished work in id order after
// afterID. A keyset cursor rather than an offset, so a machine stuck behind an
// unreachable node cannot starve the rest.
func (s *Store) PendingMachineWork(ctx context.Context, afterID string, limit int) ([]*Machine, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.read.QueryContext(ctx, `SELECT `+machineCols+` FROM machines
		WHERE id > ? AND (op_id <> '' OR state <> 'deleted') ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PruneMachines deletes the machines that cannot be about anything live, and
// returns their IDs so each can be announced as deleted.
//
// A failed machine that still names a resource is never pruned, however old it
// is. The row is the only record that something was rented and may still be
// running, and deleting it is how a fleet loses a VM it is still paying for.
func (s *Store) PruneMachines(ctx context.Context, before time.Time) (int64, []string, error) {
	var ids []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ids, err = deletedIDs(ctx, tx, `DELETE FROM machines
			WHERE (state = 'deleted' AND deleted_at IS NOT NULL AND deleted_at < ?)
			   OR (state = 'failed' AND resource_id = '' AND updated_at < ?)
			RETURNING id`, ms(before), ms(before))
		return err
	})
	return int64(len(ids)), ids, err
}

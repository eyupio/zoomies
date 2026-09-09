package store

import (
	"context"
	"fmt"
	"strconv"
)

// SettingRecoveryFenced is the key the recovery fence lives under.
//
// It is a row in this database rather than a line in zoomies.yaml, and that is
// deliberate: the fence belongs to the *data*, not to the host. A restored
// database is fenced wherever it is put, and a copy of it taken to a second
// machine arrives fenced too -- which is the case the fence exists for, since
// two controllers reconciling one fleet's runners from the same rows is how a
// restore turns into an outage.
//
// The `settings` table it uses shares a name with the settings API and none of
// its data: that API renders the effective configuration, which this is not.
const SettingRecoveryFenced = "recovery.fenced"

// SettingRecoveryFencedReason says why, so the problem the controller raises
// can name the restore rather than the flag.
const SettingRecoveryFencedReason = "recovery.fenced_reason"

// RecoveryFence is the fence's state, and why it is on.
type RecoveryFence struct {
	Fenced bool   `json:"fenced"`
	Reason string `json:"reason,omitempty"`
}

// RecoveryFenced reads the fence.
func (s *Store) RecoveryFenced(ctx context.Context) (RecoveryFence, error) {
	raw, err := s.GetSetting(ctx, SettingRecoveryFenced)
	if err != nil {
		return RecoveryFence{}, err
	}
	if raw == "" {
		return RecoveryFence{}, nil
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		// A value nobody can parse is not a reason to run unfenced. The fence
		// is the safe side of this question, and an unreadable setting is
		// exactly the sort of thing a half-finished restore leaves behind.
		return RecoveryFence{Fenced: true, Reason: fmt.Sprintf("%s is set to %q, which is not a yes or a no", SettingRecoveryFenced, raw)}, nil
	}
	if !on {
		return RecoveryFence{}, nil
	}
	reason, err := s.GetSetting(ctx, SettingRecoveryFencedReason)
	if err != nil {
		return RecoveryFence{Fenced: true}, err
	}
	return RecoveryFence{Fenced: true, Reason: reason}, nil
}

// SetRecoveryFence turns the fence on with a reason, or off.
func (s *Store) SetRecoveryFence(ctx context.Context, fenced bool, reason string) error {
	if !fenced {
		if err := s.DeleteSetting(ctx, SettingRecoveryFencedReason); err != nil {
			return err
		}
		return s.DeleteSetting(ctx, SettingRecoveryFenced)
	}
	if err := s.SetSetting(ctx, SettingRecoveryFenced, "true", false); err != nil {
		return err
	}
	return s.SetSetting(ctx, SettingRecoveryFencedReason, reason, false)
}

// ---------------------------------------------------------------------------
// Credentials a restore invalidates
// ---------------------------------------------------------------------------

// DeleteAllSessions signs everyone out.
//
// A restore brings back sessions that were live when the backup was taken, and
// a browser cookie from that afternoon still matches one. Whoever was signed in
// then is signed in now, on a machine they may no longer have anything to do
// with -- so a restore ends every session and everyone signs in again.
func (s *Store) DeleteAllSessions(ctx context.Context) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM sessions`)
	if err != nil {
		return 0, fmt.Errorf("store: ending every session: %w", err)
	}
	return res.RowsAffected()
}

// DeleteUnusedJoinTokens removes the join tokens that were never redeemed.
//
// An unused join token is a credential that enrols a new host, and the backup
// froze it in the state where it still works. The ones already redeemed are
// left: they are history, they cannot be used again, and deleting them would
// take the record of how each host got here.
func (s *Store) DeleteUnusedJoinTokens(ctx context.Context) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM join_tokens WHERE used_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("store: removing unused join tokens: %w", err)
	}
	return res.RowsAffected()
}

// RevokeAllAPITokens disables every API token without deleting its row, so the
// audit trail still says what each one was and when it stopped working.
func (s *Store) RevokeAllAPITokens(ctx context.Context) (int64, error) {
	res, err := s.exec(ctx, `UPDATE api_tokens SET revoked=1 WHERE revoked=0`)
	if err != nil {
		return 0, fmt.Errorf("store: revoking API tokens: %w", err)
	}
	return res.RowsAffected()
}

// ResetAgentTokens forgets every host's agent credential, so each agent has to
// join again.
//
// The behaviour it relies on already exists: an agent whose token this database
// no longer holds exits with the command to re-join rather than retrying
// forever. Hosts, pools and history are kept -- this invalidates the credential
// and nothing else.
func (s *Store) ResetAgentTokens(ctx context.Context) (int64, error) {
	res, err := s.exec(ctx, `UPDATE hosts SET token_hash='' WHERE token_hash<>''`)
	if err != nil {
		return 0, fmt.Errorf("store: resetting agent tokens: %w", err)
	}
	return res.RowsAffected()
}

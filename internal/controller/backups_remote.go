package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
)

// Getting the copies off the machine.
//
// The scheduled backup is a backup against a mistake. This is the other half:
// every copy the fleet takes is put in an S3-compatible bucket somebody else's
// disk is responsible for, because a directory of tidy copies on the volume
// that just died is the worst kind of backup -- everyone believed in it.
//
// The pass is written as "make the bucket hold what the directory holds"
// rather than "upload the backup that was just taken", and that is the whole
// design. A remote that was unreachable for two nights is two backups behind;
// a pass that only ever sent the newest would leave the gap there forever and
// nothing would say so. So every pass lists the remote, works out what is
// missing, sends it oldest first, and applies the remote's own retention
// afterwards.

const (
	// remoteSweep is how often the remotes are reconciled when nothing has
	// happened. Uploads are driven by taking a backup; this is the safety net
	// that catches up after an outage nobody watched, and it is slow because
	// listing a bucket is a request that costs money in some of them.
	remoteSweep = time.Hour
	// remoteRetry is how long a failed remote waits before it is tried again.
	// A bucket that refused the signature at noon refuses it at 12:01, and a
	// warning that re-fires every minute is a warning that gets muted.
	remoteRetry = 15 * time.Minute
	// remoteUploadTimeout bounds one pass over one remote: listing it,
	// sending what it is missing, and pruning it. It is generous because the
	// thing being sent is a whole database over a link nobody promised
	// anything about, and bounded because a transfer that has stalled must
	// not hold the pass open until the process restarts.
	remoteUploadTimeout = 6 * time.Hour
)

// remoteState is what the controller knows about one destination. The bucket
// is the durable record; this is only what the page and the problems drawer
// need to say whether the last attempt worked.
type remoteState struct {
	uploading     bool
	lastAttemptAt time.Time
	lastUploadAt  time.Time
	lastUploadID  string
	lastError     string
	// held and heldBytes are what the last listing found, so the page can say
	// "7 copies, 412 MB" without a request of its own.
	held      int
	heldBytes int64
	listedAt  time.Time
}

// RemoteBackupStatus is one destination as the Backups tab and the problems
// drawer see it.
type RemoteBackupStatus struct {
	// Name is what this destination is called, and what the API addresses it
	// by.
	Name string `json:"name"`
	// Where is the bucket and prefix, and Endpoint the service, so an
	// operator can tell two buckets apart without opening the configuration.
	Where    string `json:"where"`
	Endpoint string `json:"endpoint"`
	// Encrypted says the archive is sealed before it leaves this host.
	Encrypted bool `json:"encrypted"`
	// Keep is this remote's own retention; 0 keeps every copy.
	Keep int `json:"keep"`
	// Disabled is a remote that is configured and switched off, which is
	// shown rather than hidden: a destination nobody can see is one nobody
	// notices has stopped.
	Disabled bool `json:"disabled"`
	// Uploading says something is being sent right now.
	Uploading bool `json:"uploading"`
	// Copies and Bytes are what the last listing found, and ListedAt when.
	Copies   int        `json:"copies"`
	Bytes    int64      `json:"bytes"`
	ListedAt *time.Time `json:"listed_at"`
	// LastUploadAt and LastUploadID are the last copy that reached it.
	LastUploadAt *time.Time `json:"last_upload_at"`
	LastUploadID string     `json:"last_upload_id,omitempty"`
	// LastError is why the last attempt did not work, in the words the
	// service used.
	LastError string `json:"last_error,omitempty"`
}

// remoteStates is the per-destination state, keyed by name, behind the backup
// state's own mutex: a name that leaves the configuration takes its state with
// it at the next pass.
func (c *Controller) remoteState(name string) *remoteState {
	if c.backups.remotes == nil {
		c.backups.remotes = map[string]*remoteState{}
	}
	st, ok := c.backups.remotes[name]
	if !ok {
		st = &remoteState{}
		c.backups.remotes[name] = st
	}
	return st
}

// BackupRemotes reports every configured destination and what became of it,
// in the order the configuration names them.
func (c *Controller) BackupRemotes() []RemoteBackupStatus {
	cfg := c.cfg()
	out := []RemoteBackupStatus{}
	c.backups.mu.Lock()
	defer c.backups.mu.Unlock()
	for _, r := range cfg.Backup.Remotes {
		status := RemoteBackupStatus{
			Name: r.Name, Where: r.Where(), Endpoint: r.Endpoint,
			Encrypted: r.Encrypted(), Keep: r.Keep, Disabled: !r.Enabled(),
		}
		if st, ok := c.backups.remotes[r.Name]; ok {
			status.Uploading = st.uploading
			status.Copies = st.held
			status.Bytes = st.heldBytes
			status.LastUploadID = st.lastUploadID
			status.LastError = st.lastError
			if !st.listedAt.IsZero() {
				at := st.listedAt
				status.ListedAt = &at
			}
			if !st.lastUploadAt.IsZero() {
				at := st.lastUploadAt
				status.LastUploadAt = &at
			}
		}
		out = append(out, status)
	}
	return out
}

// RemoteBackup resolves one destination by name, for the handlers that list,
// fetch from or delete a copy in it.
func (c *Controller) RemoteBackup(name string) (*backup.Remote, error) {
	return backup.FindRemote(c.cfg(), name, c.backupHTTP)
}

// NoteRemoteListing records what a listing found, so that a page which has
// just read a remote leaves the count behind for the page that has not.
func (c *Controller) NoteRemoteListing(name string, copies int, bytes int64) {
	c.backups.mu.Lock()
	defer c.backups.mu.Unlock()
	st := c.remoteState(name)
	st.held = copies
	st.heldBytes = bytes
	st.listedAt = c.Now()
}

// nudgeRemotes asks the backup loop to reconcile the remotes now. Capacity 1,
// like every other nudge in here: it is a flag saying there is something to
// send, not a queue of things to send.
func (c *Controller) nudgeRemotes() {
	select {
	case c.backups.ship <- struct{}{}:
	default:
	}
}

// ShipBackups makes every enabled remote hold what the backup directory holds,
// and returns what it sent.
//
// It is safe to call from anywhere: one pass at a time across the whole
// controller, so the loop's catch-up and an operator pressing "copy offsite
// now" cannot upload the same archive twice.
func (c *Controller) ShipBackups(ctx context.Context) ([]backup.Copy, error) {
	cfg := c.cfg()
	configured := cfg.EnabledBackupRemotes()
	if len(configured) == 0 {
		return nil, nil
	}
	if !c.beginShipping() {
		return nil, ErrShippingRunning
	}
	defer c.endShipping()

	entries, err := backup.List(backup.Dir(cfg))
	if err != nil {
		return nil, err
	}

	var (
		sent  []backup.Copy
		errs  []error
		clock = c.Now
	)
	for _, cfgRemote := range configured {
		// Each destination is built on its own so that one with a typo in its
		// endpoint is recorded against its own name, where the page and the
		// problems drawer show it, and the copies still leave for the others.
		remote, err := backup.NewRemote(cfgRemote, c.backupHTTP)
		if err != nil {
			c.noteRemoteFailure(cfgRemote.Name, err, clock)
			errs = append(errs, err)
			continue
		}
		copies, err := c.shipOne(ctx, remote, entries, clock)
		sent = append(sent, copies...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", remote.Name(), err))
		}
	}
	return sent, errors.Join(errs...)
}

// noteRemoteFailure records a destination that could not even be built.
func (c *Controller) noteRemoteFailure(name string, err error, clock func() time.Time) {
	c.backups.mu.Lock()
	defer c.backups.mu.Unlock()
	st := c.remoteState(name)
	st.uploading = false
	st.lastAttemptAt = clock()
	st.lastError = err.Error()
}

// shipOne reconciles one destination and records what happened to it.
func (c *Controller) shipOne(ctx context.Context, remote *backup.Remote, entries []backup.Entry, clock func() time.Time) ([]backup.Copy, error) {
	name := remote.Name()
	c.backups.mu.Lock()
	st := c.remoteState(name)
	st.uploading = true
	st.lastAttemptAt = clock()
	c.backups.mu.Unlock()

	finish := func(err error, sent []backup.Copy, held []backup.Copy) {
		c.backups.mu.Lock()
		defer c.backups.mu.Unlock()
		st := c.remoteState(name)
		st.uploading = false
		if err != nil {
			st.lastError = err.Error()
			return
		}
		st.lastError = ""
		if n := len(sent); n > 0 {
			st.lastUploadAt = clock()
			st.lastUploadID = sent[n-1].ID
		}
		st.held = len(held)
		st.heldBytes = 0
		for _, item := range held {
			st.heldBytes += item.Bytes
		}
		st.listedAt = clock()
	}

	ctx, cancel := context.WithTimeout(ctx, remoteUploadTimeout)
	defer cancel()

	held, err := remote.List(ctx)
	if err != nil {
		finish(err, nil, nil)
		return nil, err
	}
	var sent []backup.Copy
	for _, entry := range backup.Missing(entries, held, remote.Keep()) {
		copied, err := remote.Upload(ctx, &entry)
		if err != nil {
			finish(err, sent, held)
			c.log.Warn("a backup could not be copied offsite",
				"remote", name, "backup", entry.ID, "where", remote.Where(), "error", err)
			return sent, err
		}
		sent = append(sent, *copied)
		held = append([]backup.Copy{*copied}, held...)
		c.log.Info("copied a backup offsite",
			"remote", name, "backup", entry.ID, "where", remote.Where(),
			"bytes", copied.Bytes, "encrypted", copied.Encrypted)
	}
	if removed, err := remote.Prune(ctx, remote.Keep()); err != nil {
		finish(err, sent, held)
		return sent, err
	} else if len(removed) > 0 {
		c.log.Info("removed old copies from a backup remote",
			"remote", name, "removed", len(removed), "keep", remote.Keep())
		kept := held[:0]
		for _, item := range held {
			if !slices.Contains(removed, item.ID) {
				kept = append(kept, item)
			}
		}
		held = kept
	}
	finish(nil, sent, held)
	return sent, nil
}

// ErrShippingRunning is a second offsite pass asked for while one is running.
var ErrShippingRunning = errors.New("controller: the backups are already being copied offsite; wait for that to finish")

func (c *Controller) beginShipping() bool {
	c.backups.mu.Lock()
	defer c.backups.mu.Unlock()
	if c.backups.shipping {
		return false
	}
	c.backups.shipping = true
	return true
}

func (c *Controller) endShipping() {
	c.backups.mu.Lock()
	c.backups.shipping = false
	c.backups.mu.Unlock()
}

// remotesDue says whether the loop should reconcile the remotes on this tick:
// when something asked it to, when a remote is behind after a failure and the
// retry is up, and once a sweep however quiet things have been.
func (c *Controller) remotesDue(now time.Time) bool {
	c.backups.mu.Lock()
	defer c.backups.mu.Unlock()
	if len(c.backups.remotes) == 0 {
		return true
	}
	for _, st := range c.backups.remotes {
		if st.uploading {
			continue
		}
		wait := remoteSweep
		if st.lastError != "" {
			wait = remoteRetry
		}
		if st.lastAttemptAt.IsZero() || now.Sub(st.lastAttemptAt) >= wait {
			return true
		}
	}
	return false
}

// remoteProblems is what the drawer says about the copies that leave the
// machine.
//
// A remote that is failing is a warning rather than an error for the same
// reason a failing local backup is: the fleet is still running jobs. It is
// never silent, though, because the whole value of an offsite copy is that
// somebody would notice its absence before the day it is needed.
func (c *Controller) remoteProblems() []Problem {
	var out []Problem
	for _, status := range c.BackupRemotes() {
		if status.LastError == "" {
			continue
		}
		at := c.Now()
		c.backups.mu.Lock()
		if st, ok := c.backups.remotes[status.Name]; ok && !st.lastAttemptAt.IsZero() {
			at = st.lastAttemptAt
		}
		c.backups.mu.Unlock()
		detail := fmt.Sprintf("the last attempt, at %s, failed: %s. The controller tries again every %s.",
			at.Format(time.RFC3339), status.LastError, remoteRetry)
		if status.LastUploadID != "" && status.LastUploadAt != nil {
			detail += fmt.Sprintf(" The newest copy it holds is %s, from %s.", status.LastUploadID, status.LastUploadAt.Format(time.RFC3339))
		} else {
			detail += " Nothing has ever reached it."
		}
		out = append(out, Problem{
			Code:     "backup.remote_failed",
			Severity: config.SeverityWarning,
			Setting:  "backup.remotes",
			Title:    "backups are not reaching " + status.Name,
			Detail:   detail,
			Fix:      "read the error: it names the bucket, the credential or the clock. The Backups tab tests the remote on demand, and the copies on this host are unaffected — they are simply all there is.",
			Since:    &at,
		})
	}
	return out
}

package backup

import (
	"context"
	"fmt"
	"strings"

	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/store"
)

// Where a backup destination comes from, and which one wins.
//
// There are two places a destination can be described, and both are right for
// the case they exist for. `backup.remotes` in zoomies.yaml is readable on a
// host whose database is gone, which is the day an offsite copy is for; a row
// in the database is editable, testable and rotatable from the Backups page
// without a file to open or a controller to restart.
//
// A name in both is the file's. That is the rule the rest of the configuration
// follows -- the file and the environment have the last word -- and it is the
// safe way round: an administrator who cannot reach the page can still correct
// a destination by editing the file, and the row being shadowed is reported as
// shadowed rather than silently ignored.
//
// The merge lives here rather than in the controller because three callers
// need the same answer: the controller's pass, the API, and `zoomies restore
// --from-remote` on a host where no controller is running. A second copy of
// this rule would eventually disagree with the first, and the disagreement
// would be about where a fleet's backups are.

// The two places a destination is described, as the API names them.
const (
	OriginFile     = "file"
	OriginDatabase = "database"
)

// RemoteLister is the half of the store this needs: a fleet that cannot read
// its rows still has the file's destinations, and says so.
type RemoteLister interface {
	ListBackupRemotes(ctx context.Context) ([]*store.BackupRemote, error)
}

// ResolvedRemote is one destination ready to be used, and what a page needs to
// know about where it came from.
type ResolvedRemote struct {
	// Remote is the destination itself, secrets in hand.
	Remote config.BackupRemote
	// Origin is OriginFile or OriginDatabase.
	Origin string
	// ID is the database row's id, empty for a file's destination. It is what
	// an audit entry names, because a name can be edited and an id cannot.
	ID string
	// Shadowed is a stored destination the file overrides by name. It is kept
	// in the list so a page can say so: a row that quietly does nothing is the
	// failure this design exists to avoid.
	Shadowed bool
	// Problem is why a stored destination cannot be used at all -- almost
	// always that this controller's key does not open its secrets, which is
	// what a database restored onto a host with the wrong key looks like.
	Problem string
}

// Usable reports whether a copy would actually be sent here.
func (r ResolvedRemote) Usable() bool {
	return !r.Shadowed && r.Problem == "" && r.Remote.Enabled()
}

// ResolveRemotes is every destination a fleet has: the file's first, then the
// stored ones in the order the database lists them.
//
// A stored destination whose secrets the key does not open is returned with
// its Problem set and disabled rather than dropped. Trying it every hour would
// have the service refuse it, which reads as the bucket's fault rather than
// this host's; dropping it would make a destination an operator can see in the
// database vanish from the page with no explanation.
func ResolveRemotes(ctx context.Context, cfg *config.Config, rows RemoteLister, key *cryptox.Key) ([]ResolvedRemote, error) {
	out := make([]ResolvedRemote, 0, len(cfg.Backup.Remotes))
	named := map[string]bool{}
	for _, r := range cfg.Backup.Remotes {
		named[r.Name] = true
		out = append(out, ResolvedRemote{Remote: r, Origin: OriginFile})
	}
	if rows == nil {
		return out, nil
	}

	stored, err := rows.ListBackupRemotes(ctx)
	if err != nil {
		return out, err
	}
	for _, row := range stored {
		resolved := ResolvedRemote{
			Origin: OriginDatabase, ID: row.ID, Shadowed: named[row.Name],
			Remote: config.BackupRemote{
				Name: row.Name, Endpoint: row.Endpoint, Region: row.Region,
				Bucket: row.Bucket, Prefix: row.Prefix, AccessKeyID: row.AccessKeyID,
				PathStyle: row.PathStyle, Keep: row.Keep, Disabled: !row.Enabled,
			},
		}
		if err := openRemoteSecrets(row, key, &resolved.Remote); err != nil {
			resolved.Problem = err.Error()
			resolved.Remote.Disabled = true
		}
		out = append(out, resolved)
	}
	return out, nil
}

// FindResolvedRemote is one destination by the name every surface addresses it
// with, or ErrNoRemote.
func FindResolvedRemote(ctx context.Context, cfg *config.Config, rows RemoteLister, key *cryptox.Key, name string) (ResolvedRemote, error) {
	name = strings.TrimSpace(name)
	resolved, err := ResolveRemotes(ctx, cfg, rows, key)
	if err != nil {
		return ResolvedRemote{}, err
	}
	for _, r := range resolved {
		if r.Remote.Name != name || r.Shadowed {
			continue
		}
		switch {
		case r.Problem != "":
			return ResolvedRemote{}, fmt.Errorf("the backup remote %s cannot be used: %s", name, r.Problem)
		case !r.Remote.Enabled():
			return ResolvedRemote{}, fmt.Errorf("the backup remote %s is switched off or incomplete", name)
		}
		return r, nil
	}
	return ResolvedRemote{}, fmt.Errorf("%w: %q", ErrNoRemote, name)
}

// openRemoteSecrets unseals a stored destination's secret key and passphrase.
func openRemoteSecrets(row *store.BackupRemote, key *cryptox.Key, into *config.BackupRemote) error {
	if len(row.SecretKeyEnc) == 0 && len(row.PassphraseEnc) == 0 {
		return nil
	}
	if key == nil {
		return fmt.Errorf("this controller holds no encryption key, so the secret key stored for %s cannot be opened", row.Name)
	}
	if len(row.SecretKeyEnc) > 0 {
		secret, err := key.OpenString(row.SecretKeyEnc)
		if err != nil {
			return fmt.Errorf("the secret key stored for %s does not open with this instance's encryption key (%s): %w",
				row.Name, key.Fingerprint(), err)
		}
		into.SecretAccessKey = secret
	}
	if len(row.PassphraseEnc) > 0 {
		passphrase, err := key.OpenString(row.PassphraseEnc)
		if err != nil {
			return fmt.Errorf("the passphrase stored for %s does not open with this instance's encryption key (%s): %w",
				row.Name, key.Fingerprint(), err)
		}
		into.Passphrase = passphrase
	}
	return nil
}

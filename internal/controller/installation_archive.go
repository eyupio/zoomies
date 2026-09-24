package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/cryptox"
	"github.com/eyupio/zoomies/internal/events"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/version"
)

// InstallationArchiveKind names the document, so an import can tell an
// installation archive from a settings export or anything else somebody
// points it at before it reads a single row.
const InstallationArchiveKind = "zoomies-installation"

// installationArchiveVersion is bumped when the document changes shape in a
// way an older reader would misread. The rows carry their own column names,
// so a column added to a table is not such a change.
const installationArchiveVersion = 1

// InstallationArchive is everything about one installation, as one document.
type InstallationArchive struct {
	Kind           string    `json:"kind"`
	ArchiveVersion int       `json:"archive_version"`
	ExportedAt     time.Time `json:"exported_at"`
	ExportedFrom   string    `json:"exported_from,omitempty"`
	Version        string    `json:"version"`
	InstallationID string    `json:"installation_id"`
	Target         string    `json:"target"`
	// Secrets is the installation's private key and webhook secret, sealed
	// under the passphrase the export was given. It is absent when none was:
	// the archive is then the installation's history without the means to
	// act as it, and an import asks for the key to be entered by hand.
	Secrets *ArchiveSecrets      `json:"secrets,omitempty"`
	Tables  []store.ArchiveTable `json:"tables"`
}

// ArchiveSecrets are passphrase-sealed values (cryptox.SealWithPassphrase),
// base64 in JSON.
type ArchiveSecrets struct {
	PrivateKey    []byte `json:"private_key,omitempty"`
	WebhookSecret []byte `json:"webhook_secret,omitempty"`
}

// ErrArchive is an archive or passphrase the operator can do something about:
// the wrong file, a missing or wrong passphrase, or no instance key to seal
// the secrets under once they are open.
var ErrArchive = errors.New("installation archive")

// secretColumns are the installation columns sealed under this instance's
// key. They never leave in that form: ciphertext under a key the destination
// does not have is noise at best, and at worst a copy of a credential that
// outlives the purge meant to remove it.
var secretColumns = map[string]bool{"private_key_enc": true, "webhook_secret_enc": true}

// ExportInstallation assembles one installation's archive. With a passphrase
// its secrets are opened with the instance key and sealed again under the
// passphrase; without one they are left out.
func (c *Controller) ExportInstallation(ctx context.Context, id, passphrase string) (*InstallationArchive, error) {
	inst, err := c.st.GetInstallation(ctx, id)
	if err != nil {
		return nil, err
	}
	tables, err := c.st.ExportInstallation(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := range tables {
		if tables[i].Table != "installations" {
			continue
		}
		for col, name := range tables[i].Columns {
			if !secretColumns[name] {
				continue
			}
			for _, row := range tables[i].Rows {
				row[col] = store.ArchiveValue{}
			}
		}
	}
	out := &InstallationArchive{
		Kind:           InstallationArchiveKind,
		ArchiveVersion: installationArchiveVersion,
		ExportedAt:     c.Now(),
		ExportedFrom:   c.cfg().Server.ExternalURL,
		Version:        version.Short(),
		InstallationID: inst.ID,
		Target:         inst.Target,
		Tables:         tables,
	}
	if strings.TrimSpace(passphrase) == "" {
		return out, nil
	}
	secrets := &ArchiveSecrets{}
	for _, s := range []struct {
		sealed []byte
		what   string
		into   *[]byte
	}{
		{inst.PrivateKeyEnc, "the App's private key", &secrets.PrivateKey},
		{inst.WebhookSecretEnc, "the webhook secret", &secrets.WebhookSecret},
	} {
		plain, err := c.unsealString(s.sealed, s.what)
		if err != nil {
			return nil, err
		}
		if plain == "" {
			continue
		}
		if *s.into, err = cryptox.SealWithPassphrase(passphrase, []byte(plain)); err != nil {
			return nil, fmt.Errorf("sealing %s under the passphrase: %w", s.what, err)
		}
	}
	out.Secrets = secrets
	return out, nil
}

// ImportInstallation writes an archive onto this instance. The secrets are
// opened with the passphrase and sealed under this instance's key before the
// row is written, so nothing on disk here is readable with the passphrase
// alone.
func (c *Controller) ImportInstallation(ctx context.Context, a *InstallationArchive, passphrase string) (*store.Installation, *store.ImportedInstallation, error) {
	if a == nil || a.Kind != InstallationArchiveKind {
		return nil, nil, fmt.Errorf("%w: this is not an installation archive; make one with zoomies export --installation", ErrArchive)
	}
	if a.ArchiveVersion > installationArchiveVersion {
		return nil, nil, fmt.Errorf("%w: it was written by Zoomies %s in a newer format; upgrade this instance first", ErrArchive, a.Version)
	}
	sealed := map[string][]byte{}
	if a.Secrets != nil {
		if strings.TrimSpace(passphrase) == "" {
			return nil, nil, fmt.Errorf("%w: it carries the App's credentials sealed under a passphrase; give the one it was exported with", ErrArchive)
		}
		if c.key == nil {
			return nil, nil, fmt.Errorf("%w: this instance has no encryption key to keep the App's credentials under; set security.encryption_key first", ErrArchive)
		}
		for col, v := range map[string][]byte{"private_key_enc": a.Secrets.PrivateKey, "webhook_secret_enc": a.Secrets.WebhookSecret} {
			if len(v) == 0 {
				continue
			}
			plain, err := cryptox.OpenWithPassphrase(passphrase, v)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: the passphrase does not open the App's credentials, or the archive has been altered", ErrArchive)
			}
			if sealed[col], err = c.key.Seal(plain); err != nil {
				return nil, nil, err
			}
		}
	}
	for i := range a.Tables {
		if a.Tables[i].Table != "installations" {
			continue
		}
		for col, name := range a.Tables[i].Columns {
			if !secretColumns[name] {
				continue
			}
			for _, row := range a.Tables[i].Rows {
				if col < len(row) {
					// Whatever the file says is replaced: a secret column is
					// only ever what this instance sealed itself.
					row[col] = store.ArchiveValue{V: sealed[name]}
				}
			}
		}
	}
	done, err := c.st.ImportInstallation(ctx, a.Tables)
	if err != nil {
		return nil, nil, err
	}
	inst, err := c.st.GetInstallation(ctx, done.InstallationID)
	if err != nil {
		return nil, nil, err
	}
	c.publishInstallation(ctx, inst)
	if pools, err := c.st.ListPools(ctx); err == nil {
		for _, p := range pools {
			if p.InstallationID == inst.ID {
				c.PublishPool(ctx, events.KindPoolCreated, p)
			}
		}
	}
	return inst, done, nil
}

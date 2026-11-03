package installer

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/naming"
	"github.com/eyupio/zoomies/internal/store"
	"github.com/eyupio/zoomies/internal/terminalui"
)

// UpgradeOptions selects an existing deployment. Upgrade never enrols a host,
// generates credentials, or rewrites its configuration from installer defaults.
type UpgradeOptions struct {
	// Doctor reports only; it has no tuning path.
	Doctor     func(context.Context, *config.Config)
	ConfigDir  string
	BinaryPath string
	DockerHost string
	Runtime    string
	Image      string
	Mode       Mode
	Check      bool
	// Continuation keeps the binary-download half and service half under one heading.
	Continuation bool
	Out          io.Writer
	// In is where an answer to "add them now?" is read from, and Interactive
	// says somebody is there to give one. AssumeYes is --yes: approval given
	// in advance. With neither, what this release expects and the deployment
	// lacks is reported and left alone. NonInteractive says nobody will be
	// there on the real run either, which lets --check refuse an upgrade that
	// could only stop half way. UpdateHelper is --update-helper, the operator's
	// own approval, given in advance, to add the update helper; --yes does not
	// stand for it, because that approves the deployment's additions and this
	// is a grant of root.
	In             io.Reader
	Interactive    bool
	NonInteractive bool
	NoAnimation    bool
	AssumeYes      bool
	UpdateHelper   bool
	proxmoxRoot    string // injected gateway root for tests
	run            commandRunner
	runInput       inputRunner
	// shared stands in for the host's shared folder and the account it
	// belongs to, so a test does not reach for /var/lib/zoomies.
	shared *sharedTarget
	// socketGroup stands in for reading the group that owns the runtime's
	// socket, which is the test host's own otherwise.
	socketGroup func(path string) int
	// helperHost stands in for what the update helper question reads from the
	// host, so a test does not look at the real /run/systemd or /etc/systemd.
	helperHost *upgradeHelperHost
	// isWSL, hasSystemd and wslConf stand in for what the WSL check reads
	// from the host: the kernel release, /run/systemd and /etc/wsl.conf.
	isWSL      func() bool
	hasSystemd func() bool
	wslConf    string
	// serveTimeout, servePoll and progressEvery shorten the wait for the
	// controller to answer, which is minutes long outside a test.
	serveTimeout, servePoll, progressEvery time.Duration
}

// wslOffer is the question an upgrade asks on a WSL2 distro. isWSL stands in for
// the host in tests.
func (o UpgradeOptions) wslOffer(check bool) wslOffer {
	return wslOffer{Out: o.Out, In: o.In, Interactive: o.Interactive && !o.NonInteractive, AssumeYes: o.AssumeYes,
		Check: check, Binary: o.BinaryPath, isWSL: o.isWSL}
}

type upgradePlan struct {
	opts        UpgradeOptions
	record      DeploymentRecord
	image       string
	unit        string
	nativeUnits []string
	launchd     bool
	client      *backend.APIClient
	replacement *backend.ContainerReplacement
	// move is the settings the operator agreed to take out of the
	// container's environment and into its database; see envsettings.go.
	move []movedSetting
	// sharedMountComing is set when the layout review found the running
	// container without the shared folder, and sharedMountApplied when the
	// review's additions were made, so the next restart gives it the mount.
	// The update helper cannot be installed until it has the mount.
	sharedMountComing, sharedMountApplied bool
	// dbPath is the deployment's database as this host sees it, and
	// beforeMigrate is the newest pre-migration copy beside it before the
	// restart. A copy that appears after the restart means the release
	// migrated the database, so a rollback would leave a binary an older
	// schema cannot open.
	dbPath, beforeMigrate string
}

// withDefaults fills the options a caller may leave out. A database copy taken
// before an upgrade and the upgrade itself must read the same deployment, so
// both begin here.
func (o UpgradeOptions) withDefaults() UpgradeOptions {
	if o.ConfigDir == "" {
		o.ConfigDir = config.ConfigDir()
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.run == nil {
		o.run = runCommand
	}
	if o.runInput == nil {
		o.runInput = runCommandInput
	}
	if o.BinaryPath == "" {
		o.BinaryPath, _ = os.Executable()
	}
	o.BinaryPath = upgradeBinaryPath(o.BinaryPath)
	return o
}

// BackupBeforeUpgrade takes and verifies a copy of the deployment's database
// before anything about the upgrade changes, and returns where it went.
//
// It is a step of its own, run before the installed binary is replaced: an
// upgrade that fails its backup must leave the host exactly as it was, and a
// failure after a new binary was in place would leave the next restart or
// reboot free to migrate a database nothing protected.
func BackupBeforeUpgrade(ctx context.Context, opts UpgradeOptions) (string, error) {
	opts = opts.withDefaults()
	p, err := prepareUpgrade(ctx, opts)
	if err != nil {
		return "", err
	}
	return p.backupBeforeUpgrade(ctx)
}

// Upgrade applies the binary already downloaded by install.sh to the running
// deployment, and refreshes its stock runner images without removing workloads.
func Upgrade(ctx context.Context, opts UpgradeOptions) error {
	if opts.Runtime != "" && opts.Runtime != "docker" && opts.Runtime != "podman" {
		return fmt.Errorf("installer: --runtime must be docker or podman")
	}
	opts = opts.withDefaults()
	if !opts.Check {
		lockPath := filepath.Join(opts.ConfigDir, "upgrade.lock")
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, fs.ErrNotExist) && os.Geteuid() != 0 && runtime.GOOS == "linux" {
			return fmt.Errorf("installer: there is no deployment in %s for this account to upgrade: %w; a deployment set up with sudo lives in /etc/zoomies, so run \"sudo zoomies upgrade\"", opts.ConfigDir, err)
		}
		if err != nil {
			return fmt.Errorf("installer: cannot reserve this deployment for upgrade: %w; if an earlier upgrade was interrupted, remove %s only after checking it has stopped", err, lockPath)
		}
		_ = lock.Close()
		defer os.Remove(lockPath)
	}
	p, err := prepareUpgrade(ctx, opts)
	if err != nil {
		return err
	}
	ui := PaletteFor(opts.Out)
	if !opts.Continuation {
		ui.Title(opts.Out, "Zoomies upgrade", p.describe())
	}
	if opts.Check {
		ui.Rule(opts.Out, "Deployment preview")
	} else {
		ui.Rule(opts.Out, "2/4 Deployment")
	}
	ui.Hint(opts.Out, "Target: %s", p.describe())
	// The database copy is taken by BackupBeforeUpgrade, before this runs and
	// before the binary is replaced: see its comment for why the two are one
	// step in the caller and not here.
	if err := p.settleLayout(ctx); err != nil {
		return err
	}
	p.settleSettings(ctx)
	if opts.Check {
		if err := p.upgradeProxmoxGateways(ctx); err != nil {
			return err
		}
		ui.Done(opts.Out, "Deployment checks passed; no changes made")
		offerWSLSelfManagement(ctx, opts.wslOffer(true))
		if opts.Doctor != nil {
			opts.Doctor(ctx, p.settings(ctx).cfg)
		}
		return nil
	}
	p.offerUpdateHelper(ctx)
	beforeImage := p.localImageID(ctx)
	// The tag can have been pulled already -- by an earlier `docker pull`, a
	// check, or an upgrade whose restart failed -- while the service still
	// runs something older. Comparing the tag alone then says nothing changed
	// on an upgrade that moved the service and migrated its database.
	beforeRunning := p.runningImageID(ctx)
	serving := p.serveCheck(ctx)
	// Snapshot the database and the newest pre-migration copy before the
	// restart, so a failure to answer can tell whether this release migrated
	// it: a copy that appears after the restart means the older binary cannot
	// open the database, and then a rollback is a restore rather than a swap.
	snapshot := p.settings(ctx)
	p.dbPath, p.beforeMigrate = snapshot.dbPath, store.NewestPreMigrationName(snapshot.dbPath)
	if err := p.task(ctx, "Refreshing runner images for future jobs", p.pullRunnerImages); err != nil {
		return err
	}
	err = p.task(ctx, "Updating the installed services", func(ctx context.Context) error {
		switch p.record.Deployment {
		case DeploymentCompose:
			return p.upgradeCompose(ctx)
		case DeploymentDocker:
			return p.upgradeDocker(ctx)
		default:
			return p.restartNative(ctx)
		}
	})
	if err != nil {
		return fmt.Errorf("installer: the upgrade did not finish: %w", err)
	}
	if err := p.upgradeProxmoxGateways(ctx); err != nil {
		return err
	}
	if err := p.restartHealthReporter(ctx); err != nil {
		return err
	}
	ui.Rule(opts.Out, "3/4 Verify")
	if err := p.task(ctx, "Waiting for the service to become healthy", func(ctx context.Context) error { return p.waitServing(ctx, serving) }); err != nil {
		return fmt.Errorf("installer: %w", err)
	}
	if !p.record.Deployment.Containerised() {
		ui.Done(opts.Out, "Native services are active")
	}
	if p.record.Deployment.Containerised() {
		afterImage := p.localImageID(ctx)
		reportImage(opts.Out, p.image, beforeImage, afterImage, beforeRunning, p.runningImageID(ctx))
	}
	ui.Rule(opts.Out, "4/4 Host health")
	if opts.Doctor != nil {
		opts.Doctor(ctx, p.settings(ctx).cfg)
	} else {
		ui.Hint(opts.Out, "Review host health with: zoomies doctor")
	}
	offerWSLSelfManagement(ctx, opts.wslOffer(false))
	fmt.Fprintln(opts.Out)
	ui.Done(opts.Out, "%s", ui.Bold("Upgrade complete."))
	return nil
}

// restartNative restarts the native units this deployment runs, one service at
// a time. The upgrade and a rollback share it, so a rollback brings the
// service back exactly as the upgrade brought it up.
func (p *upgradePlan) restartNative(ctx context.Context) error {
	ui := PaletteFor(p.opts.Out)
	for _, unit := range p.nativeUnits {
		ui.Doing(p.opts.Out, "Restarting %s", shortUnit(unit))
		var err error
		if p.launchd {
			_, err = p.opts.run(ctx, "launchctl", "kickstart", "-k", unit)
		} else {
			_, err = p.opts.run(ctx, "systemctl", "restart", unit)
			if err == nil {
				_, err = p.opts.run(ctx, "systemctl", "is-active", "--quiet", unit)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// engineResultMarker begins a line the engine writes for the update helper
// alone: the copy taken before the change, and whether the engine rolled back.
// The helper reads it into its answer and keeps it out of the log an operator
// reads, so the words a person sees stay prose.
const engineResultMarker = "zoomies-upgrade-result: "

// backupBeforeUpgrade takes and verifies a copy of the deployment's database,
// through the same package the schedule and the command line use, so the copy
// is listable and restorable by the command that restores any other. A
// deployment with no database yet has nothing to lose and takes none.
func (p *upgradePlan) backupBeforeUpgrade(ctx context.Context) (string, error) {
	s := p.settings(ctx)
	if !s.read {
		// The database may exist and be unreadable, or its volume may be one
		// this host cannot open. Either way there is no way to know there is
		// nothing to lose, so the upgrade stops rather than restart into a
		// migration nothing protected.
		return "", fmt.Errorf("could not read this deployment's settings to find its database (%s), so the copy before the upgrade could not be taken; put that right, or run again with --no-backup", strings.TrimSpace(s.why))
	}
	dbPath := s.dbPath
	if dbPath == "" || !exists(dbPath) {
		// A known-absent database, which is a deployment that has never run:
		// there is nothing to lose.
		return "", nil
	}
	st, err := store.Open(ctx, store.Options{Path: dbPath, ReadOnly: true})
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", dbPath, err)
	}
	defer func() { _ = st.Close() }()

	// A database with nothing sealed needs no key to open, so a key file that
	// is not where the configuration's default points (a deployment that keys
	// by environment, or a path that has moved) is not a reason to abandon the
	// copy. A database that is sealed still needs it, and a missing key is
	// then a real failure the operator should see.
	cfg := *s.cfg
	if sealed, err := st.HasSealedSecrets(ctx); err == nil && !sealed {
		cfg.Security.EncryptionKey = ""
		cfg.Security.EncryptionKeyFile = ""
	}

	root, err := preUpgradeBackupDir(s.cfg, dbPath, p.record.Deployment.Containerised())
	if err != nil {
		return "", err
	}
	entry, err := backup.Take(ctx, st, backup.TakeOptions{
		Config: &cfg, Dir: root, Source: backup.SourceUpgrade, TakenBy: "zoomies upgrade",
	})
	if err != nil {
		return "", err
	}
	verified, err := backup.Verify(ctx, root, entry.ID)
	if err != nil {
		return "", fmt.Errorf("verifying the copy taken before the upgrade: %w", err)
	}
	if !verified.OK {
		return "", fmt.Errorf("the copy taken before the upgrade is not sound: %s", strings.Join(verified.Problems, "; "))
	}
	ui := PaletteFor(p.opts.Out)
	ui.Done(p.opts.Out, "Backed up the database as %s and verified it", entry.ID)
	ui.Hint(p.opts.Out, "Restore it with: zoomies restore %s --replace", entry.Dir)
	// For the helper alone: the copy's directory, so an update that goes wrong
	// can offer the restore. It is stripped from the log an operator reads.
	fmt.Fprintf(p.opts.Out, "%sbackup=%s\n", engineResultMarker, entry.Dir)
	// The same ceiling the schedule keeps, so an upgrade per release does not
	// grow the directory without bound.
	if removed, err := backup.Prune(root, s.cfg.Backup.Keep); err != nil {
		ui.Warn(p.opts.Out, "could not tidy old backups in %s: %v", root, err)
	} else if len(removed) > 0 {
		ui.Hint(p.opts.Out, "removed %d older backup(s), keeping %d", len(removed), s.cfg.Backup.Keep)
	}
	return entry.Dir, nil
}

// preUpgradeBackupDir is where an upgrade's own copy goes: the configured
// backup directory when one is set, and otherwise beside the database as this
// host sees it.
//
// A container's configuration speaks in the container's paths, so an absolute
// directory under the container's state directory is the mounted volume, which
// on this host is the directory holding the database. An absolute path
// anywhere else inside the container has no host path, and is refused rather
// than written to the host's own /var/lib by mistake.
func preUpgradeBackupDir(cfg *config.Config, dbPath string, containerised bool) (string, error) {
	dir := strings.TrimSpace(cfg.Backup.Directory)
	if dir == "" {
		return backup.DefaultDir(dbPath), nil
	}
	if !containerised {
		if !filepath.IsAbs(dir) {
			return filepath.Join(filepath.Dir(dbPath), dir), nil
		}
		return dir, nil
	}
	// The container's configuration speaks in the container's paths, which are
	// Linux paths whatever this host is. They are read with slash rules and only
	// joined to the host's own volume with the host's own separator.
	hostVolume := filepath.Dir(dbPath)
	if !path.IsAbs(dir) {
		return filepath.Join(hostVolume, strings.TrimPrefix(dir, "./")), nil
	}
	if dir == ContainerStateDir {
		return hostVolume, nil
	}
	if rest, ok := strings.CutPrefix(dir, ContainerStateDir+"/"); ok {
		return filepath.Join(hostVolume, rest), nil
	}
	return "", fmt.Errorf("backup.directory is %s, which is inside the container and not under its state directory %s, so this host has nowhere to put the copy before the upgrade; set backup.directory under %s, or run again with --no-backup", dir, ContainerStateDir, ContainerStateDir)
}

// A container's read-only reporter is a separate native process. Replacing its
// executable alone leaves the old checks running until the service restarts.
func (p *upgradePlan) restartHealthReporter(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	state, err := p.opts.run(ctx, "systemctl", "show", HostHealthUnit, "--property=ActiveState", "--value")
	if err != nil || strings.TrimSpace(state) != "active" {
		return nil
	}
	start, err := p.opts.run(ctx, "systemctl", "show", HostHealthUnit, "--property=ExecStart", "--value")
	if err != nil {
		return fmt.Errorf("installer: cannot inspect the running host health reporter: %w", err)
	}
	if !strings.Contains(start, "path="+p.opts.BinaryPath+" ;") && !strings.Contains(start, "path="+p.opts.BinaryPath+";") {
		PaletteFor(p.opts.Out).Warn(p.opts.Out, "Host health reporter uses another binary; upgrade it separately")
		return nil
	}
	PaletteFor(p.opts.Out).Doing(p.opts.Out, "Restarting the read-only host health reporter")
	if _, err := p.opts.run(ctx, "systemctl", "restart", HostHealthUnit); err != nil {
		return fmt.Errorf("installer: service updated, but the host health reporter could not restart: %w", err)
	}
	return nil
}

// describe names what is being upgraded in the words an operator would use.
func (p *upgradePlan) describe() string {
	switch {
	case p.record.Deployment == DeploymentCompose:
		return "the Compose deployment"
	case p.record.Deployment == DeploymentDocker:
		return "the Docker deployment"
	case len(p.nativeUnits) > 1:
		return "native controller and agent"
	case p.unit != "":
		return shortUnit(p.unit)
	}
	return "this host"
}

// shortUnit drops the launchd domain prefix, which means nothing to a reader.
func shortUnit(unit string) string {
	if i := strings.LastIndex(unit, "/"); i >= 0 {
		return unit[i+1:]
	}
	return unit
}

// settleLayout says what this release expects that the deployment lacks, and
// adds it when the operator approves: --yes, or an answer at the terminal.
// Unattended, it adds nothing and says how to; see layout.go.
func (p *upgradePlan) settleLayout(ctx context.Context) error {
	changes, err := p.layoutChanges(ctx)
	if err != nil || len(changes) == 0 {
		return err
	}
	out := p.opts.Out
	fmt.Fprintln(out, "Deployment additions to review:")
	required := ""
	for _, c := range changes {
		fmt.Fprintln(out, "  - "+c.what)
		p.sharedMountComing = p.sharedMountComing || c.sharedMount
		if c.required && required == "" {
			required = c.what
		}
	}
	refuse := func() error {
		return fmt.Errorf("installer: the upgrade cannot go on without this: %s; run `zoomies upgrade --yes` to add it, or `zoomies init` to set the deployment up again", required)
	}
	approved := false
	switch {
	case p.opts.Check:
		// install.sh replaces the binary between the check and the real
		// run, so a run that is bound to stop has to stop here, while the
		// old binary is still the installed one.
		if required != "" && p.opts.NonInteractive && !p.opts.AssumeYes {
			return refuse()
		}
		fmt.Fprintln(out, "The upgrade will offer to add these; approval is required.")
		return nil
	case p.opts.AssumeYes:
		approved = true
	case p.opts.Interactive && p.opts.In != nil:
		approved = askApproval(p.opts.In, out, "Add them now? [Y/n] ")
	}
	if !approved {
		if required != "" {
			return refuse()
		}
		fmt.Fprintln(out, "Optional additions skipped. Continuing the upgrade.")
		fmt.Fprintln(out, "Add later: sudo zoomies upgrade --yes")
		return nil
	}
	for _, c := range changes {
		if err := c.apply(ctx); err != nil {
			return fmt.Errorf("installer: could not %s: %w", c.what, err)
		}
	}
	p.sharedMountApplied = true
	fmt.Fprintf(out, "Added %d change(s) this release expects.\n", len(changes))
	return nil
}

// askApproval reads one answer. An empty line is yes, as the [Y/n] says; an
// input that ends before anything is typed is no, because nobody said yes.
func askApproval(in io.Reader, out io.Writer, question string) bool {
	answer, ok := readAnswer(in, out, question)
	if !ok {
		return false
	}
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return true
	}
	return false
}

// readAnswer prints the question and reads one line, a byte at a time so that
// nothing past the newline is consumed from a reader the next question shares.
// The bool is false when the input ended before anything was typed.
func readAnswer(in io.Reader, out io.Writer, question string) (string, bool) {
	fmt.Fprint(out, question)
	var line strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			line.WriteByte(buf[0])
		}
		if err != nil {
			if line.Len() == 0 {
				fmt.Fprintln(out)
				return "", false
			}
			break
		}
	}
	return strings.TrimSpace(line.String()), true
}

func (p *upgradePlan) localImageID(ctx context.Context) string {
	if !p.record.Deployment.Containerised() || p.image == "" {
		return ""
	}
	out, err := p.docker(ctx, "image", "inspect", "--format", "{{.Id}}", p.image)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// runningImageID is the image the service's container was created from, which
// is what an upgrade actually changes; the tag is only what it will be
// created from next.
func (p *upgradePlan) runningImageID(ctx context.Context) string {
	if !p.record.Deployment.Containerised() {
		return ""
	}
	out, err := p.docker(ctx, "inspect", "--format", "{{.Image}}", containerOr(p.record))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Compare the pulled image with the container, not the tag before pulling:
// an earlier pull may already have advanced the tag without updating the
// service. Only a running container can make this upgrade a no-op.
func (p *upgradePlan) keepCurrentContainer(ctx context.Context) (bool, error) {
	image := p.localImageID(ctx)
	if image == "" || image != p.runningImageID(ctx) {
		return false, nil
	}
	running, err := p.docker(ctx, "inspect", "--format", "{{.State.Running}}", containerOr(p.record))
	if err != nil || strings.TrimSpace(running) != "true" {
		return false, nil
	}
	// An explicit image reference can change while resolving to the same
	// build. Remember it for subsequent upgrades without disrupting service.
	if _, _, err := replaceEnvImage(p.record.EnvFile, p.image); err != nil {
		return true, err
	}
	p.record.Image = p.image
	if _, err := WriteDeploymentRecord(p.opts.ConfigDir, p.record); err != nil {
		return true, err
	}
	PaletteFor(p.opts.Out).Done(p.opts.Out, "Service image unchanged; keeping the running container")
	return true, nil
}

// reportImage says what the upgrade did to the image the service runs, and
// separately whether the channel it follows moved, because those differ
// whenever the tag was pulled before the service was recreated from it.
func reportImage(out io.Writer, image, tagBefore, tagAfter, runBefore, runAfter string) {
	ui := PaletteFor(out)
	switch {
	case runBefore != "" && runAfter != "" && runBefore != runAfter:
		ui.Done(out, "Service image updated: %s -> %s", shortImageID(runBefore), shortImageID(runAfter))
	case runBefore != "" && runBefore == runAfter:
		ui.Done(out, "Service image unchanged: %s", shortImageID(runAfter))
	case tagAfter != "":
		ui.Done(out, "Service image applied: %s", shortImageID(tagAfter))
	default:
		ui.Warn(out, "Service restarted; image identity could not be verified")
	}
	ui.Hint(out, "%s", image)
}

func shortImageID(id string) string {
	id = strings.TrimSpace(strings.TrimPrefix(id, "sha256:"))
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func prepareUpgrade(ctx context.Context, opts UpgradeOptions) (*upgradePlan, error) {
	p := &upgradePlan{opts: opts}
	data, err := os.ReadFile(DeploymentRecordPath(opts.ConfigDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("installer: read the existing deployment: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(data, &p.record); err != nil {
			return nil, fmt.Errorf("installer: the deployment record is invalid; restore it before upgrading: %w", err)
		}
		if p.record.Deployment != DeploymentNative && !p.record.Deployment.Containerised() {
			return nil, fmt.Errorf("installer: unknown deployment %q; no service was changed", p.record.Deployment)
		}
	}
	if opts.Mode != "" && p.record.Mode != "" && opts.Mode != p.record.Mode {
		return nil, fmt.Errorf("installer: this is a %s deployment, not %s; run the command on the intended host", p.record.Mode, opts.Mode)
	}
	p.opts.Runtime = deploymentRuntime(p.record, opts.Runtime)
	if !p.record.Deployment.Containerised() {
		if err := p.prepareNative(ctx); err != nil {
			return nil, err
		}
		return p, nil
	}
	if p.record.Directory == "" || p.record.EnvFile == "" {
		return nil, fmt.Errorf("installer: the deployment record has no directory or environment file; restore it before upgrading")
	}
	if _, err := os.ReadFile(p.record.EnvFile); err != nil {
		return nil, fmt.Errorf("installer: read the existing environment: %w", err)
	}
	p.image, err = upgradeImage(p.record, opts.Image)
	if err != nil {
		return nil, err
	}
	if p.record.Deployment == DeploymentCompose {
		// A missing Compose file is one the upgrade offers to write again
		// (see layout.go), and a file it writes uses ZOOMIES_IMAGE by
		// construction; there is nothing to ask Compose about yet.
		if _, err := os.Stat(p.record.ComposeFile()); os.IsNotExist(err) {
			return p, nil
		}
		// Do not claim an upgrade if the operator has replaced the image
		// variable with a fixed reference in the compose file.
		images, err := p.compose(ctx, "config", "--images")
		if err != nil {
			return nil, err
		}
		found := false
		for _, image := range strings.Fields(images) {
			if image == p.image {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("installer: %s does not use ZOOMIES_IMAGE; restore that image setting before upgrading", p.record.ComposeFile())
		}
		return p, nil
	}
	endpoint := opts.DockerHost
	if endpoint == "" {
		endpoint = os.Getenv("DOCKER_HOST")
	}
	if endpoint == "" {
		endpoint = "unix:///var/run/docker.sock"
	}
	p.client, err = backend.NewAPIClient(endpoint)
	if err != nil {
		return nil, err
	}
	p.replacement, err = p.client.PrepareReplacement(ctx, containerOr(p.record), p.image)
	if err != nil {
		return nil, err
	}
	if opts.Image == "" && p.replacement.Image != p.record.Image {
		return nil, fmt.Errorf("installer: the running container uses a different image from its deployment record; pass --image with the intended replacement")
	}
	backup := p.replacement.Name + "-before-upgrade"
	if _, err := p.client.ContainerInspect(ctx, backup); backend.StatusCode(err) != 404 {
		return nil, fmt.Errorf("installer: cannot reserve backup container %s; inspect or remove an earlier upgrade backup before retrying", backup)
	}
	return p, nil
}

func upgradeImage(rec DeploymentRecord, override string) (string, error) {
	if override != "" {
		if strings.HasPrefix(override, "-") || strings.ContainsAny(override, " \t\r\n") {
			return "", fmt.Errorf("installer: --image must be one image reference")
		}
		return override, nil
	}
	base := strings.Split(strings.Split(rec.Image, "@")[0], ":")[0]
	if base != stockControllerRepository && base != stockAgentRepository {
		return "", fmt.Errorf("installer: %s is a custom image; pass --image with the image to upgrade to", rec.Image)
	}
	image := DefaultImage()
	if rec.Mode == ModeAgent || base == stockAgentRepository {
		image, _ = AgentImageFor(image)
	}
	return image, nil
}

func (p *upgradePlan) prepareNative(ctx context.Context) error {
	units := []string{UnitController, UnitAgent}
	// A provider-only host upgrades through its private executable too.
	if filepath.Dir(filepath.Dir(p.opts.BinaryPath)) == "/var/lib/zoomies-proxmox" && proxmoxKey.MatchString(filepath.Base(filepath.Dir(p.opts.BinaryPath))) {
		units = []string{"zoomies-proxmox-" + filepath.Base(filepath.Dir(p.opts.BinaryPath)) + ".service"}
	}
	if p.opts.Mode == ModeAgent {
		units = []string{UnitAgent}
	} else if p.opts.Mode != "" {
		units = []string{UnitController}
	}
	for _, unit := range units {
		if runtime.GOOS == "darwin" {
			label := (ServiceSpec{Unit: unit}).Label()
			path := LaunchdPlistPath(label, os.Geteuid() == 0)
			data, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			binary, err := launchdBinary(data)
			if err != nil || filepath.Clean(binary) != p.opts.BinaryPath {
				return fmt.Errorf("installer: %s does not run %s; pass --installed-binary for its ProgramArguments executable", label, p.opts.BinaryPath)
			}
			domain := fmt.Sprintf("gui/%d/", os.Geteuid())
			if os.Geteuid() == 0 {
				domain = "system/"
			}
			p.nativeUnits = append(p.nativeUnits, domain+label)
			if p.unit == "" {
				p.unit = domain + label
			}
			p.launchd = true
			continue
		}
		state, err := p.opts.run(ctx, "systemctl", "show", "--property=LoadState", "--value", unit)
		if err != nil || strings.TrimSpace(state) != "loaded" {
			continue
		}
		execStart, err := p.opts.run(ctx, "systemctl", "show", "--property=ExecStart", "--value", unit)
		if err != nil {
			return err
		}
		if !strings.Contains(execStart, "path="+p.opts.BinaryPath+" ;") && !strings.Contains(execStart, "path="+p.opts.BinaryPath+";") {
			return fmt.Errorf("installer: %s does not run %s; pass --installed-binary with the path its ExecStart uses", unit, p.opts.BinaryPath)
		}
		p.nativeUnits = append(p.nativeUnits, unit)
		if p.unit == "" {
			p.unit = unit
		}
	}
	if p.unit == "" {
		if p.wslWithoutSystemd() {
			return p.offerWSLSystemd()
		}
		return fmt.Errorf("installer: no existing Zoomies deployment was found; run on the installed host, or pass --config-dir for its deployment record")
	}
	return nil
}

func (p *upgradePlan) docker(ctx context.Context, args ...string) (string, error) {
	name, argv := p.dockerArgv(args...)
	return p.opts.run(ctx, name, argv...)
}

func (p *upgradePlan) dockerArgv(args ...string) (string, []string) {
	if p.opts.Runtime == "podman" {
		if p.opts.DockerHost != "" {
			args = append([]string{"--remote", "--url", p.opts.DockerHost}, args...)
		}
		return "podman", args
	}
	if p.opts.DockerHost != "" {
		args = append([]string{"--host", p.opts.DockerHost}, args...)
	}
	return "docker", args
}

func (p *upgradePlan) compose(ctx context.Context, args ...string) (string, error) {
	name, argv := p.composeArgv(args...)
	return p.opts.run(ctx, name, argv...)
}

func (p *upgradePlan) composeArgv(args ...string) (string, []string) {
	name, command := ComposeArgs(p.record.ComposeCommand, p.record.ComposeFile(), args...)
	prefix := []string{"ZOOMIES_IMAGE=" + p.image}
	if p.opts.DockerHost != "" {
		prefix = append(prefix, "DOCKER_HOST="+p.opts.DockerHost)
	}
	for n, arg := range command {
		if arg == "--env-file" && n+1 < len(command) {
			command[n+1] = p.record.EnvFile
		}
	}
	return "env", append(append(prefix, name), command...)
}

func (p *upgradePlan) pullRunnerImages(ctx context.Context) error {
	if !p.record.Deployment.Containerised() && p.opts.DockerHost == "" {
		s := p.settings(ctx)
		runsAgent := p.unit == UnitAgent || len(p.nativeUnits) > 1 || s.cfg.Agent.Embedded
		if !runsAgent || (s.cfg.Agent.Backend != "docker" && s.cfg.Agent.Backend != "podman") {
			return nil
		}
		p.opts.DockerHost = s.cfg.Agent.DockerHost
		if s.cfg.Agent.Backend == "podman" {
			p.opts.Runtime = "podman"
		}
	}
	images, err := p.docker(ctx, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return fmt.Errorf("read cached runner images: %w", err)
	}
	seen := map[string]bool{}
	var stock []string
	for _, image := range strings.Fields(images) {
		repo, _, _ := naming.SplitImage(image)
		if (repo != naming.RunnerImageRepo && repo != "ghcr.io/eyupio/zoomies-runner-docker" && repo != naming.RunnerFullImageRepo) || strings.HasSuffix(image, ":<none>") || seen[image] {
			continue
		}
		seen[image] = true
		stock = append(stock, image)
	}
	if len(stock) == 0 {
		PaletteFor(p.opts.Out).Hint(p.opts.Out, "No cached stock runner images to refresh")
		return nil
	}
	PaletteFor(p.opts.Out).Doing(p.opts.Out, "Refreshing %d runner image(s) for future jobs; running runners keep theirs", len(stock))
	for _, image := range stock {
		if _, err := p.docker(ctx, "pull", image); err != nil {
			return fmt.Errorf("pull %s; the deployment has not been restarted: %w", image, err)
		}
	}
	PaletteFor(p.opts.Out).Done(p.opts.Out, "Runner images refreshed for future jobs")
	return nil
}

// Only the image line changes. Secrets, comments and hand-edited settings stay
// byte-for-byte intact, and the original is available for a failed restart.
func replaceEnvImage(path, image string) ([]byte, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	value, err := quoteEnvValue("ZOOMIES_IMAGE", image)
	if err != nil {
		return nil, 0, err
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for n, line := range lines {
		key, _, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		if ok && strings.TrimSpace(key) == "ZOOMIES_IMAGE" {
			lines[n] = "ZOOMIES_IMAGE=" + value
			found = true
		}
	}
	updated := strings.Join(lines, "\n")
	if !found {
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		updated += "ZOOMIES_IMAGE=" + value + "\n"
	}
	if err := writeFileAtomic(path, []byte(updated), info.Mode().Perm()); err != nil {
		return nil, 0, err
	}
	return data, info.Mode().Perm(), nil
}

func (p *upgradePlan) upgradeCompose(ctx context.Context) error {
	PaletteFor(p.opts.Out).Doing(p.opts.Out, "Updating service image %s", p.image)
	if _, err := p.compose(ctx, "pull", "zoomies"); err != nil {
		return err
	}
	if kept, err := p.keepCurrentContainer(ctx); kept || err != nil {
		return err
	}
	previous, err := ParseEnvFile(p.record.EnvFile)
	if err != nil {
		return err
	}
	var unmove func() error
	if len(p.move) > 0 {
		unmove = p.moveComposeSettings(ctx)
	}
	old, mode, err := replaceEnvImage(p.record.EnvFile, p.image)
	if err != nil {
		return err
	}
	_, err = p.compose(ctx, "up", "-d", "--no-deps", "--force-recreate", "--timeout", strconv.Itoa(int(serviceStopTimeout.Seconds())), "zoomies")
	if err == nil {
		var running string
		running, err = p.docker(ctx, "inspect", "--format", "{{.State.Running}}", containerOr(p.record))
		if err == nil && strings.TrimSpace(running) != "true" {
			err = fmt.Errorf("the upgraded container did not stay running")
		}
	}
	if err != nil {
		// The files go back as they were before the move as well: the image
		// being rolled back to may predate the settings it would now find
		// only in the database.
		var restore error
		if unmove != nil {
			restore = unmove()
		} else {
			restore = writeFileAtomic(p.record.EnvFile, old, mode)
		}
		if restore == nil {
			rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), serviceStopTimeout+time.Minute)
			defer cancel()
			p.image = previous["ZOOMIES_IMAGE"]
			if p.image == "" {
				p.image = p.record.Image
			}
			_, restore = p.compose(rollback, "up", "-d", "--no-deps", "--force-recreate", "--timeout", strconv.Itoa(int(serviceStopTimeout.Seconds())), "zoomies")
		}
		return errors.Join(err, restore)
	}
	p.record.Image = p.image
	_, err = WriteDeploymentRecord(p.opts.ConfigDir, p.record)
	return err
}

func (p *upgradePlan) upgradeDocker(ctx context.Context) error {
	PaletteFor(p.opts.Out).Doing(p.opts.Out, "Updating service image %s", p.image)
	if _, err := p.docker(ctx, "pull", p.image); err != nil {
		return err
	}
	if kept, err := p.keepCurrentContainer(ctx); kept || err != nil {
		return err
	}
	old := p.replacement
	if err := p.client.ContainerStop(ctx, old.ID, serviceStopTimeout); err != nil {
		return err
	}
	moved := len(p.move) > 0 && p.moveDockerSettings(ctx)
	backup := old.Name + "-before-upgrade"
	if err := p.client.ContainerRename(ctx, old.ID, backup); err != nil {
		if old.Running {
			_ = p.client.ContainerStart(context.WithoutCancel(ctx), old.ID)
		}
		return err
	}
	id, err := p.client.CreateReplacement(ctx, old)
	if err == nil {
		err = p.client.ContainerStart(ctx, id)
	}
	if err == nil {
		var state *backend.ContainerInspect
		state, err = p.client.ContainerInspect(ctx, id)
		if err == nil && (state.State == nil || !state.State.Running) {
			err = fmt.Errorf("replacement container did not stay running")
		}
	}
	if err != nil {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), serviceStopTimeout+time.Minute)
		defer cancel()
		var restore error
		if id != "" {
			restore = p.client.RemoveContainerKeepingVolumes(rollback, id, true)
		}
		if restore == nil {
			restore = p.client.ContainerRename(rollback, old.ID, old.Name)
		}
		if restore == nil && old.Running {
			restore = p.client.ContainerStart(rollback, old.ID)
		}
		return errors.Join(err, restore)
	}
	if _, _, err := replaceEnvImage(p.record.EnvFile, p.image); err != nil {
		return fmt.Errorf("new container is running but its environment file could not be updated: %w", err)
	}
	if moved {
		p.commentDockerEnvFile()
	}
	p.record.Image = p.image
	if _, err := WriteDeploymentRecord(p.opts.ConfigDir, p.record); err != nil {
		return err
	}
	if err := p.client.RemoveContainerKeepingVolumes(ctx, old.ID, false); err != nil {
		return fmt.Errorf("new container is running; remove the stopped %s container when convenient: %w", backup, err)
	}
	return nil
}

// Native services name an absolute executable even when --prefix was relative.
func upgradeBinaryPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func launchdBinary(data []byte) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	arguments := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "key" {
			var key string
			if err := decoder.DecodeElement(&key, &start); err != nil {
				return "", err
			}
			arguments = key == "ProgramArguments"
		} else if arguments && start.Name.Local == "string" {
			var binary string
			err := decoder.DecodeElement(&binary, &start)
			return binary, err
		}
	}
}

// Only stages without questions may use this. Buffered output is restored
// before the next stage, including an error's rollback or recovery advice.
func (p *upgradePlan) task(ctx context.Context, label string, work func(context.Context) error) error {
	out := p.opts.Out
	return terminalui.Task(ctx, out, label, p.opts.NoAnimation || p.opts.NonInteractive, func(ctx context.Context, log io.Writer) error {
		p.opts.Out = log
		defer func() { p.opts.Out = out }()
		return work(ctx)
	})
}

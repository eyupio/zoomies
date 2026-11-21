package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backup"
	"github.com/eyupio/zoomies/internal/config"
	"github.com/eyupio/zoomies/internal/store"
)

// An upgrade used to call itself complete the moment the new container was
// running. But a controller does not serve until its migrations have run, and
// one that grows with the jobs table can take minutes -- so an operator was
// told the upgrade had finished while their site answered nothing, with no
// sign of why. The upgrade now waits for the controller to answer, and says
// what it is doing while it waits.

const (
	// upgradeServeTimeout is how long a controller may take to answer after
	// it restarts. It is long on purpose: a migration on a large database is
	// slow, not broken, and stopping it half way is the one thing that would
	// make it worse.
	upgradeServeTimeout = 30 * time.Minute
	upgradeServePoll    = 2 * time.Second
	// upgradeProgressEvery is how often the latest log line is echoed while
	// the controller is still starting, so a long wait is visibly progress.
	upgradeProgressEvery = 30 * time.Second
)

// serveCheck is how the upgrade learns whether the controller is serving. A
// nil check means there is nothing to wait for: an agent serves nothing, and
// a container with its health check turned off offers no way to ask.
type serveCheck struct {
	what string
	// probe answers nil once the controller is serving.
	probe func(ctx context.Context) error
	// stopped says why the service is no longer running, or "" while it is.
	stopped func(ctx context.Context) string
	// latest is the controller's most recent log line, echoed while waiting.
	latest func(ctx context.Context) string
}

// serveCheck is worked out before the service restarts: a native controller's
// listener comes from its settings, and those are read from its database,
// which the new process may be migrating a moment later.
func (p *upgradePlan) serveCheck(ctx context.Context) *serveCheck {
	if p.record.Deployment.Containerised() && p.record.Mode == ModeAgent {
		return nil
	}
	if p.record.Deployment.Containerised() {
		return p.containerServeCheck()
	}
	// A native host may have no deployment record; the unit found is then
	// what says which half it runs.
	if p.launchd || p.unit == UnitAgent {
		return nil
	}
	// The listener is a stored setting. Without the database to read it
	// from, the address in the file or the default is a guess, and waiting
	// on a guess is a wait that can only time out.
	s := p.settings(ctx)
	if !s.read {
		return nil
	}
	target, client := nativeHealthTarget(s.cfg)
	if target == "" {
		return nil
	}
	return &serveCheck{
		what: target,
		probe: func(ctx context.Context) error {
			return probeHealth(ctx, client, target)
		},
		stopped: func(ctx context.Context) string {
			if _, err := p.opts.run(ctx, "systemctl", "is-active", "--quiet", p.unit); err != nil {
				return p.unit + " is not active"
			}
			return ""
		},
		latest: func(ctx context.Context) string {
			out, _ := p.opts.run(ctx, "journalctl", "--unit", p.unit, "--lines", "1", "--output", "cat", "--no-pager")
			return out
		},
	}
}

// containerServeCheck runs the container's own health check -- the command the
// Compose file or the image declares -- in the container, now, rather than
// waiting up to its interval for the runtime's next scheduled run of it.
func (p *upgradePlan) containerServeCheck() *serveCheck {
	name := containerOr(p.record)
	return &serveCheck{
		what: "the " + name + " container's health check",
		probe: func(ctx context.Context) error {
			out, err := p.docker(ctx, "inspect", "--format", "{{json .Config.Healthcheck}}", name)
			if err != nil {
				return err
			}
			cmd, ok := healthCommand(out)
			if !ok {
				return errNoHealthCheck
			}
			_, err = p.docker(ctx, append([]string{"exec", name}, cmd...)...)
			return err
		},
		stopped: func(ctx context.Context) string {
			out, err := p.docker(ctx, "inspect", "--format", "{{.State.Status}}", name)
			if err != nil {
				return "the " + name + " container could not be inspected: " + err.Error()
			}
			switch status := strings.TrimSpace(out); status {
			case "running", "":
				return ""
			default:
				return "the " + name + " container is " + status
			}
		},
		latest: func(ctx context.Context) string {
			out, _ := p.docker(ctx, "logs", "--tail", "1", name)
			return out
		},
	}
}

var errNoHealthCheck = errors.New("the container declares no health check")

// healthCommand turns a container's declared health check into the command to
// exec in it. It is false when there is none: no health check, or NONE.
func healthCommand(inspect string) ([]string, bool) {
	var hc struct{ Test []string }
	if err := json.Unmarshal([]byte(strings.TrimSpace(inspect)), &hc); err != nil || len(hc.Test) < 2 {
		return nil, false
	}
	switch hc.Test[0] {
	case "CMD":
		return hc.Test[1:], true
	case "CMD-SHELL":
		return []string{"/bin/sh", "-c", strings.Join(hc.Test[1:], " ")}, true
	}
	return nil, false
}

// nativeHealthTarget is the controller's own listener over loopback, and a
// client for it. Verification is off for the same reason it is at install: the
// certificate may be self-signed and the connection never leaves the host.
func nativeHealthTarget(cfg *config.Config) (string, *http.Client) {
	if cfg == nil {
		return "", nil
	}
	host, port, err := net.SplitHostPort(cfg.Server.Bind)
	if err != nil || port == "" {
		return "", nil
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	client := &http.Client{Timeout: 5 * time.Second}
	if cfg.Server.TLS.Mode != config.TLSOff {
		scheme = "https"
		client.Transport = insecureLoopbackTransport()
	}
	return fmt.Sprintf("%s://%s/healthz", scheme, net.JoinHostPort(host, port)), client
}

func probeHealth(ctx context.Context, client *http.Client, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered HTTP %d", target, resp.StatusCode)
	}
	return nil
}

// upgradeRollbackTimeout is how long the previous release is given to answer
// after it is put back. It has run here before, so it does not need the
// migration wait the new release is given.
const upgradeRollbackTimeout = 5 * time.Minute

// waitServing waits until the controller answers. When it does not, it decides
// what can be done: putting the previous release back is only safe when this
// upgrade did not migrate the database, because migrations run one way and an
// older binary cannot open a schema that has moved on.
func (p *upgradePlan) waitServing(ctx context.Context, check *serveCheck) error {
	if check == nil {
		return nil
	}
	timeout, poll, every := p.serveTimings()
	stop := p.answerWait(ctx, check, timeout, poll, every)
	if stop == nil {
		return nil
	}
	logs := p.logsHint()
	// The two ways the wait ends, without advice: what to do about it is the
	// decision below, and a message that named one path here would contradict
	// the path actually taken.
	var base string
	if stop.stoppedWhy != "" {
		base = fmt.Sprintf("the controller stopped after the upgrade: %s. See why with: %s", stop.stoppedWhy, logs)
	} else {
		base = fmt.Sprintf("the controller has not answered in %s (%v)", timeout, stop.lastErr)
	}
	if !p.canRollback() {
		if stop.stoppedWhy != "" {
			return errors.New(base + ". The upgrade was not rolled back: the new release may already have migrated the database, which an older one cannot open")
		}
		return fmt.Errorf("%s. It is still running, so it may still be migrating its database: follow it with %s, and do not stop it part way", base, logs)
	}
	if copy := p.migratedCopy(); copy != "" {
		return fmt.Errorf("%s. The previous release was not put back: this upgrade migrated the database, which an older release cannot open. The copy taken before the migration is %s; stop the service and restore it with: zoomies restore %s --replace", base, copy, copy)
	}
	return p.rollbackBinary(ctx, check, base)
}

// serveTimings is how long to wait for the controller, and how often to ask
// and to echo progress, with the defaults an unattended run leaves.
func (p *upgradePlan) serveTimings() (timeout, poll, every time.Duration) {
	timeout, poll, every = p.opts.serveTimeout, p.opts.servePoll, p.opts.progressEvery
	if timeout == 0 {
		timeout = upgradeServeTimeout
	}
	if poll == 0 {
		poll = upgradeServePoll
	}
	if every == 0 {
		every = upgradeProgressEvery
	}
	return timeout, poll, every
}

// waitStop is why a wait ended: the service was found stopped, or the deadline
// passed with the controller still not answering.
type waitStop struct {
	// stoppedWhy is why the service is not running, and "" while it is.
	stoppedWhy string
	// lastErr is the last probe's error, for the message a timeout writes.
	lastErr error
}

// answerWait polls until the controller answers, and returns nil when it does.
// A container with no health check is not waited on: nil is returned with a
// warning, because there is nothing to ask.
func (p *upgradePlan) answerWait(ctx context.Context, check *serveCheck, timeout, poll, every time.Duration) *waitStop {
	out := p.opts.Out
	logs := p.logsHint()
	start := time.Now()
	lastSaid := start
	announced := false
	for {
		err := check.probe(ctx)
		if err == nil {
			if announced {
				PaletteFor(out).Done(out, "The controller is answering (%s)", time.Since(start).Round(time.Second))
			}
			return nil
		}
		if errors.Is(err, errNoHealthCheck) {
			PaletteFor(out).Warn(out, "Container declares no health check; readiness is unverified")
			PaletteFor(out).Hint(out, "Follow startup: %s", logs)
			return nil
		}
		if why := check.stopped(ctx); why != "" {
			return &waitStop{stoppedWhy: why}
		}
		if !announced {
			PaletteFor(out).Doing(out, "Waiting for the controller to answer")
			PaletteFor(out).Hint(out, "Database migrations may take several minutes. Logs: %s", logs)
			announced = true
		}
		if time.Since(start) >= timeout {
			return &waitStop{lastErr: err}
		}
		if time.Since(lastSaid) >= every {
			lastSaid = time.Now()
			if line := logLineMessage(check.latest(ctx)); line != "" {
				fmt.Fprintf(out, "  still starting after %s; latest: %s\n", time.Since(start).Round(time.Second), line)
			} else {
				fmt.Fprintf(out, "  still starting after %s\n", time.Since(start).Round(time.Second))
			}
		}
		select {
		case <-ctx.Done():
			return &waitStop{lastErr: ctx.Err()}
		case <-time.After(poll):
		}
	}
}

// canRollback says whether the previous release can be put back: a native
// service that runs the binary selfUpdate replaced, and only when the binary
// kept beside it is the one this upgrade wrote. A container's service is its
// image, which this upgrade does not keep the one before.
//
// The kept file is checked against the digest the self-update left (see
// KeptPreviousEnv). Its existing is not enough: an upgrade that replaced
// nothing (install.sh --upgrade, --no-download, "already on the latest
// build") still restarts the service and waits for it, and the file left from
// an earlier upgrade is a release the database may since have been migrated
// past.
func (p *upgradePlan) canRollback() bool {
	if p.record.Deployment.Containerised() || p.opts.BinaryPath == "" || p.opts.KeptPrevious == "" {
		return false
	}
	sum, err := backup.FileSHA256(p.opts.BinaryPath + PreviousSuffix)
	return err == nil && sum == p.opts.KeptPrevious
}

// migratedCopy is the pre-migration copy this upgrade made, or "" when it
// migrated nothing: the newest copy beside the database, when it is newer than
// the one that was there before the restart.
func (p *upgradePlan) migratedCopy() string {
	if p.dbPath == "" {
		return ""
	}
	newest := store.NewestPreMigrationName(p.dbPath)
	if newest == "" || newest <= p.beforeMigrate {
		return ""
	}
	return filepath.Join(filepath.Dir(p.dbPath), store.PreMigrationDir, newest)
}

// rollbackBinary puts the previous release back, restarts the service and waits
// for it to answer. It returns an error either way: the upgrade failed, and
// this says whether the host was put back to what it ran.
func (p *upgradePlan) rollbackBinary(ctx context.Context, check *serveCheck, base string) error {
	PaletteFor(p.opts.Out).Warn(p.opts.Out, "The controller did not answer; putting the previous release back")
	if err := restorePreviousBinary(p.opts.BinaryPath); err != nil {
		return fmt.Errorf("%s; and the previous release could not be put back: %v", base, err)
	}
	if err := p.restartNative(ctx); err != nil {
		return fmt.Errorf("%s; the previous release was put back but the service would not restart: %v", base, err)
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upgradeRollbackTimeout)
	defer cancel()
	_, poll, every := p.serveTimings()
	if stop := p.answerWait(tctx, check, upgradeRollbackTimeout, poll, every); stop != nil {
		why := stop.stoppedWhy
		if why == "" {
			why = stop.lastErr.Error()
		}
		return fmt.Errorf("%s; the previous release was put back and did not answer either (%s). Restore the copy taken before the upgrade, if there is one, with: zoomies restore", base, why)
	}
	// For the helper alone: the previous release is running again, so the
	// failure was undone and the version it reported is not the one asked for
	// because of that, not because the engine did nothing.
	fmt.Fprintf(p.opts.Out, "%srolled_back=true\n", engineResultMarker)
	return fmt.Errorf("%s. The upgrade failed and the previous release is running again", base)
}

// logsHint is the command that shows this deployment's logs.
func (p *upgradePlan) logsHint() string {
	if p.record.Deployment.Containerised() {
		return "zoomies logs"
	}
	return "journalctl --unit " + p.unit + " --follow"
}

// logLineMessage makes one log line fit to echo: the message of a structured
// line, the line itself otherwise, and never a wall of text.
func logLineMessage(line string) string {
	line = strings.TrimSpace(line)
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[i+1:])
	}
	// A Compose log line carries the service's name in front of it.
	if _, rest, ok := strings.Cut(line, "| "); ok && strings.HasPrefix(strings.TrimSpace(rest), "{") {
		line = strings.TrimSpace(rest)
	}
	var structured struct {
		Msg string `json:"msg"`
	}
	if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &structured) == nil && structured.Msg != "" {
		line = structured.Msg
	}
	if len(line) > 160 {
		line = line[:157] + "..."
	}
	return line
}

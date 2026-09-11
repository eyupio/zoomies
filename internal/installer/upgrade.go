package installer

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/backend"
	"github.com/eyupio/zoomies/internal/config"
)

// UpgradeOptions selects an existing deployment. Upgrade never enrols a host,
// generates credentials, or rewrites its configuration from installer defaults.
type UpgradeOptions struct {
	ConfigDir  string
	BinaryPath string
	DockerHost string
	Runtime    string
	Image      string
	Mode       Mode
	Check      bool
	Out        io.Writer
	run        commandRunner
}

type upgradePlan struct {
	opts        UpgradeOptions
	record      DeploymentRecord
	image       string
	unit        string
	launchd     bool
	client      *backend.APIClient
	replacement *backend.ContainerReplacement
}

// Upgrade applies the binary already downloaded by install.sh to the running
// deployment, and refreshes its stock runner images without removing workloads.
func Upgrade(ctx context.Context, opts UpgradeOptions) error {
	if opts.Runtime != "" && opts.Runtime != "docker" && opts.Runtime != "podman" {
		return fmt.Errorf("installer: --runtime must be docker or podman")
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = config.ConfigDir()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.run == nil {
		opts.run = runCommand
	}
	if opts.BinaryPath == "" {
		opts.BinaryPath, _ = os.Executable()
	}
	opts.BinaryPath = upgradeBinaryPath(opts.BinaryPath)
	if !opts.Check {
		lockPath := filepath.Join(opts.ConfigDir, "upgrade.lock")
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
	if opts.Check {
		fmt.Fprintln(opts.Out, "The existing deployment can be upgraded without running setup again.")
		return nil
	}
	beforeImage := p.localImageID(ctx)
	fmt.Fprintln(opts.Out, "Keeping this host's configuration, credentials, identity and data.")
	if err := p.pullRunnerImages(ctx); err != nil {
		return err
	}
	switch p.record.Deployment {
	case DeploymentCompose:
		err = p.upgradeCompose(ctx)
	case DeploymentDocker:
		err = p.upgradeDocker(ctx)
	default:
		fmt.Fprintln(opts.Out, "Restarting "+p.unit+" with the installed binary. Reporting resumes after the restart.")
		if p.launchd {
			_, err = opts.run(ctx, "launchctl", "kickstart", "-k", p.unit)
		} else {
			_, err = opts.run(ctx, "systemctl", "restart", p.unit)
			if err == nil {
				_, err = opts.run(ctx, "systemctl", "is-active", "--quiet", p.unit)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("installer: the upgrade did not finish: %w", err)
	}
	if p.record.Deployment.Containerised() {
		afterImage := p.localImageID(ctx)
		switch {
		case beforeImage != "" && afterImage == beforeImage:
			fmt.Fprintf(opts.Out, "Image channel did not advance: %s still resolves to %s. The service was recreated from the same published image.\n", p.image, shortImageID(afterImage))
		case afterImage != "" && beforeImage != "":
			fmt.Fprintf(opts.Out, "Image channel advanced: %s now resolves to %s (was %s).\n", p.image, shortImageID(afterImage), shortImageID(beforeImage))
		case afterImage != "":
			fmt.Fprintf(opts.Out, "Pulled %s at %s.\n", p.image, shortImageID(afterImage))
		default:
			fmt.Fprintf(opts.Out, "Recreated the service from %s. Confirm its reported build; moving tags advance only after a successful publish.\n", p.image)
		}
	}
	fmt.Fprintln(opts.Out, "Upgrade complete. Check the Hosts page for the agent's next heartbeat.")
	return nil
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
	units := []string{UnitAgent, UnitController}
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
				return fmt.Errorf("installer: %s does not run %s; pass --prefix for its ProgramArguments executable", label, p.opts.BinaryPath)
			}
			domain := fmt.Sprintf("gui/%d/", os.Geteuid())
			if os.Geteuid() == 0 {
				domain = "system/"
			}
			if p.unit != "" {
				return fmt.Errorf("installer: more than one native service exists; choose --mode agent or --mode controller")
			}
			p.unit, p.launchd = domain+label, true
			continue
		}
		state, err := p.opts.run(ctx, "systemctl", "show", "--property=LoadState", "--value", unit)
		if err != nil || strings.TrimSpace(state) != "loaded" {
			continue
		}
		if p.unit != "" {
			return fmt.Errorf("installer: more than one native service exists; choose --mode agent or --mode controller")
		}
		execStart, err := p.opts.run(ctx, "systemctl", "show", "--property=ExecStart", "--value", unit)
		if err != nil {
			return err
		}
		if !strings.Contains(execStart, "path="+p.opts.BinaryPath+" ;") && !strings.Contains(execStart, "path="+p.opts.BinaryPath+";") {
			return fmt.Errorf("installer: %s does not run %s; pass --prefix for the directory its ExecStart uses", unit, p.opts.BinaryPath)
		}
		p.unit = unit
	}
	if p.unit == "" {
		return fmt.Errorf("installer: no existing Zoomies deployment was found; run on the installed host, or pass --config-dir for its deployment record")
	}
	return nil
}

func (p *upgradePlan) docker(ctx context.Context, args ...string) (string, error) {
	if p.opts.Runtime == "podman" {
		if p.opts.DockerHost != "" {
			args = append([]string{"--remote", "--url", p.opts.DockerHost}, args...)
		}
		return p.opts.run(ctx, "podman", args...)
	}
	if p.opts.DockerHost != "" {
		args = append([]string{"--host", p.opts.DockerHost}, args...)
	}
	return p.opts.run(ctx, "docker", args...)
}

func (p *upgradePlan) compose(ctx context.Context, args ...string) (string, error) {
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
	return p.opts.run(ctx, "env", append(append(prefix, name), command...)...)
}

func (p *upgradePlan) pullRunnerImages(ctx context.Context) error {
	if p.opts.DockerHost == "" && !p.record.Deployment.Containerised() {
		return nil
	}
	images, err := p.docker(ctx, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return fmt.Errorf("read cached runner images: %w", err)
	}
	seen := map[string]bool{}
	for _, image := range strings.Fields(images) {
		if !strings.HasPrefix(image, "ghcr.io/eyupio/zoomies-runner") || strings.HasSuffix(image, ":<none>") || seen[image] {
			continue
		}
		seen[image] = true
		fmt.Fprintln(p.opts.Out, "Refreshing "+image+" for future jobs; existing runners keep their image.")
		if _, err := p.docker(ctx, "pull", image); err != nil {
			return fmt.Errorf("pull %s; the deployment has not been restarted: %w", image, err)
		}
	}
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
	fmt.Fprintln(p.opts.Out, "Pulling "+p.image)
	if _, err := p.compose(ctx, "pull", "zoomies"); err != nil {
		return err
	}
	previous, err := ParseEnvFile(p.record.EnvFile)
	if err != nil {
		return err
	}
	old, mode, err := replaceEnvImage(p.record.EnvFile, p.image)
	if err != nil {
		return err
	}
	_, err = p.compose(ctx, "up", "-d", "--no-deps", "--force-recreate", "zoomies")
	if err == nil {
		var running string
		running, err = p.docker(ctx, "inspect", "--format", "{{.State.Running}}", containerOr(p.record))
		if err == nil && strings.TrimSpace(running) != "true" {
			err = fmt.Errorf("the upgraded container did not stay running")
		}
	}
	if err != nil {
		restore := writeFileAtomic(p.record.EnvFile, old, mode)
		if restore == nil {
			rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			p.image = previous["ZOOMIES_IMAGE"]
			if p.image == "" {
				p.image = p.record.Image
			}
			_, restore = p.compose(rollback, "up", "-d", "--no-deps", "--force-recreate", "zoomies")
		}
		return errors.Join(err, restore)
	}
	p.record.Image = p.image
	_, err = WriteDeploymentRecord(p.opts.ConfigDir, p.record)
	return err
}

func (p *upgradePlan) upgradeDocker(ctx context.Context) error {
	fmt.Fprintln(p.opts.Out, "Pulling "+p.image)
	if _, err := p.docker(ctx, "pull", p.image); err != nil {
		return err
	}
	old := p.replacement
	if err := p.client.ContainerStop(ctx, old.ID, 10*time.Minute); err != nil {
		return err
	}
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
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
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

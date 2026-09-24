package installer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eyupio/zoomies/internal/config"
	"gopkg.in/yaml.v3"
)

// An upgrade replaces the binary or the image, and a release sometimes needs
// more of the host than the one that was installed: a folder, a mount, a file
// the installer used to write and somebody has since removed. Left to the next
// `zoomies init`, that is a deployment that upgrades cleanly and then quietly
// lacks what the new release expects -- a pool's tool cache that is never
// kept, with nothing to say why.
//
// So an upgrade looks for what this release expects and the deployment does
// not have, says what it would add, and adds it only with the operator's say:
// --yes, or an answer at the terminal. With neither -- a scheduled run, a
// pipeline -- it changes nothing beyond what it always changed, and says what
// is missing and the command that adds it. Nothing on the host changes
// without approval.

// layoutChange is one thing this release expects that the deployment lacks.
type layoutChange struct {
	// what is said to the operator: what would be added, and where.
	what string
	// required marks a change the upgrade cannot go on without. Only a
	// missing Compose file is: there is nothing to bring the new image up
	// with.
	required bool
	apply    func(ctx context.Context) error
}

// layoutChanges is everything this deployment lacks.
func (p *upgradePlan) layoutChanges() ([]layoutChange, error) {
	switch p.record.Deployment {
	case DeploymentCompose:
		return p.composeLayoutChanges()
	case DeploymentDocker:
		return p.dockerLayoutChanges(), nil
	default:
		return p.nativeLayoutChanges(), nil
	}
}

func (p *upgradePlan) composeLayoutChanges() ([]layoutChange, error) {
	var out []layoutChange
	path := p.record.ComposeFile()
	body, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		env, envErr := ParseEnvFile(p.record.EnvFile)
		if envErr != nil {
			return nil, fmt.Errorf("installer: %s is missing, and it cannot be written again without the environment file: %w", path, envErr)
		}
		spec := composeSpecFromDeployment(p.record, env)
		out = append(out, layoutChange{
			what:     "write " + path + " again from this deployment's record and environment file; it is missing, and the upgraded container is brought up with it",
			required: true,
			apply: func(context.Context) error {
				rendered, err := RenderComposeFile(spec)
				if err != nil {
					return err
				}
				return writeFileAtomic(path, []byte(rendered), 0o640)
			},
		})
	case err != nil:
		return nil, fmt.Errorf("installer: reading %s: %w", path, err)
	default:
		mounted, err := composeMountsShared(body)
		if err != nil {
			return nil, fmt.Errorf("installer: %s is not a Compose file this release can read: %w", path, err)
		}
		if !mounted {
			out = append(out, layoutChange{
				what: fmt.Sprintf("mount %s in %s, where pools keep their tool caches; the file is backed up first", SharedHostDir, path),
				apply: func(context.Context) error {
					return addSharedMountToComposeFile(path)
				},
			})
		}
	}
	return append(out, p.sharedFolderChanges(SharedHostDir, ImageUID, ImageUID)...), nil
}

func (p *upgradePlan) dockerLayoutChanges() []layoutChange {
	var out []layoutChange
	if p.replacement != nil && !p.replacement.HasBind(SharedHostDir) {
		out = append(out, layoutChange{
			what: fmt.Sprintf("mount %s in the replacement container, where pools keep their tool caches", SharedHostDir),
			apply: func(context.Context) error {
				return p.replacement.AddBind(SharedHostDir + ":" + SharedHostDir)
			},
		})
	}
	return append(out, p.sharedFolderChanges(SharedHostDir, ImageUID, ImageUID)...)
}

// nativeLayoutChanges gives the shared folder to whoever owns the state
// directory beside it, which is the account the service runs as.
func (p *upgradePlan) nativeLayoutChanges() []layoutChange {
	dir := config.SharedDir()
	uid, gid := -1, -1
	if fi, err := os.Stat(filepath.Dir(dir)); err == nil {
		if u, g, ok := fileOwner(fi); ok {
			uid, gid = u, g
		}
	}
	return p.sharedFolderChanges(dir, uid, gid)
}

// sharedTarget is where the shared folder is and who has to own it.
type sharedTarget struct {
	dir      string
	uid, gid int
}

// sharedFolderChanges is the shared folder and its layout, where any of it is
// missing or the folder belongs to an account that cannot add to it.
func (p *upgradePlan) sharedFolderChanges(dir string, uid, gid int) []layoutChange {
	if s := p.opts.shared; s != nil {
		dir, uid, gid = s.dir, s.uid, s.gid
	}
	problems := SharedDirProblems(dir, uid)
	if len(problems) == 0 {
		return nil
	}
	owner := ""
	if uid >= 0 {
		owner = fmt.Sprintf(", owned by uid %d so Zoomies can add to it", uid)
	}
	return []layoutChange{{
		what: fmt.Sprintf("create %s and the folders in it (%s)%s", dir, strings.Join(problems, "; "), owner),
		apply: func(context.Context) error {
			if _, err := PrepareSharedDir(dir, uid, gid); err != nil {
				return err
			}
			// A folder that is there but belongs to somebody else is given
			// back; PrepareSharedDir leaves an existing one as it is.
			if uid >= 0 {
				if err := os.Lchown(dir, uid, gid); err != nil {
					return fmt.Errorf("installer: giving %s to uid %d: %w", dir, uid, err)
				}
			}
			return nil
		},
	}}
}

// composeSpecFromDeployment rebuilds the Compose file's spec from what the
// deployment recorded and its environment file says, which is everything the
// installer rendered it from.
func composeSpecFromDeployment(rec DeploymentRecord, env map[string]string) ComposeFileSpec {
	mode := rec.Mode
	if mode == "" {
		mode = ModeSingle
	}
	runsRunners := env["ZOOMIES_AGENT_EMBEDDED"] == "true" || mode == ModeAgent
	socket := socketPath(env["ZOOMIES_DOCKER_HOST"])
	mountSocket := runsRunners && env["ZOOMIES_AGENT_BACKEND"] != "process" && socket != ""
	gid, _ := strconv.Atoi(env["DOCKER_GID"])
	return ComposeFileSpec{
		Mode:          mode,
		ContainerPort: ContainerPort,
		Publish:       mode != ModeAgent,
		MountSocket:   mountSocket,
		SocketPath:    socket,
		GroupAdd:      mountSocket && gid > 0,
		TLSCertFile:   env["ZOOMIES_TLS_CERT_FILE"],
		TLSKeyFile:    env["ZOOMIES_TLS_KEY_FILE"],
		Healthcheck:   mode != ModeAgent,
		Container:     rec.Container,
		Volume:        rec.Volume,
		Network:       rec.Network,
	}
}

// composeMountsShared reports whether the zoomies service mounts the shared
// folder at its own path, in either of Compose's volume syntaxes.
func composeMountsShared(body []byte) (bool, error) {
	var doc struct {
		Services map[string]struct {
			Volumes []yaml.Node `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return false, err
	}
	svc, ok := doc.Services["zoomies"]
	if !ok {
		return false, fmt.Errorf("it has no zoomies service")
	}
	for _, v := range svc.Volumes {
		switch v.Kind {
		case yaml.ScalarNode:
			if parts := strings.Split(v.Value, ":"); len(parts) > 1 && parts[1] == SharedHostDir {
				return true, nil
			}
		case yaml.MappingNode:
			var long struct {
				Target string `yaml:"target"`
			}
			if err := v.Decode(&long); err == nil && long.Target == SharedHostDir {
				return true, nil
			}
		}
	}
	return false, nil
}

// addSharedMountToComposeFile adds the shared folder to the zoomies service's
// volumes, keeping a copy of the file as it was. It edits the file rather than
// writing it again from the template: an operator's own additions -- another
// service, a label, a resource limit -- are theirs, and an upgrade that
// silently dropped them would be a worse problem than the one it solved.
func addSharedMountToComposeFile(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	edited, err := addSharedMount(body)
	if err != nil {
		return err
	}
	backup := fmt.Sprintf("%s.bak.%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, body, 0o640); err != nil {
		return fmt.Errorf("installer: backing up %s: %w", path, err)
	}
	return writeFileAtomic(path, edited, 0o640)
}

// addSharedMount is the edit itself, on the parsed document, so that comments
// and every other key survive it.
func addSharedMount(body []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("the Compose file is empty")
	}
	services := mappingValue(root.Content[0], "services")
	svc := mappingValue(services, "zoomies")
	if svc == nil || svc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the Compose file has no zoomies service to add the shared folder to")
	}
	volumes := mappingValue(svc, "volumes")
	if volumes == nil {
		svc.Content = append(svc.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "volumes"},
			&yaml.Node{Kind: yaml.SequenceNode})
		volumes = svc.Content[len(svc.Content)-1]
	}
	if volumes.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("the zoomies service's volumes are not a list")
	}
	volumes.Content = append(volumes.Content, &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: SharedHostDir + ":" + SharedHostDir,
		HeadComment: "The shared folder, added by zoomies upgrade: runners' caches, one folder per\n" +
			"purpose, mounted at the path it has on this host.",
	})
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// mappingValue is the value under key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

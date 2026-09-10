package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ContainerReplacement retains the full deployment configuration, not just the
// subset the runner backend uses. Dropping an unknown host option while
// upgrading an agent would silently change the operator's isolation or mounts.
type ContainerReplacement struct {
	ID      string
	Name    string
	Image   string
	Running bool
	body    map[string]json.RawMessage
}

// PrepareReplacement reads without changing anything. The returned request
// keeps credentials, mounts, ports and runtime options from the old container.
func (c *APIClient) PrepareReplacement(ctx context.Context, id, image string) (*ContainerReplacement, error) {
	var old struct {
		ID         string `json:"Id"`
		Name       string
		Config     map[string]json.RawMessage
		HostConfig map[string]json.RawMessage
		State      ContainerState
		Mounts     []struct {
			Type, Name, Source, Destination, Mode string
			RW                                    bool
		}
		NetworkSettings struct {
			Networks map[string]map[string]json.RawMessage
		}
	}
	if err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, nil, &old); err != nil {
		return nil, err
	}
	if old.ID == "" || old.Config == nil || old.HostConfig == nil {
		return nil, fmt.Errorf("docker api: container %s has no complete configuration", id)
	}
	if string(old.HostConfig["AutoRemove"]) == "true" {
		return nil, fmt.Errorf("docker api: %s uses automatic removal; recreate it manually so its data can be preserved", id)
	}
	var previousImage string
	_ = json.Unmarshal(old.Config["Image"], &previousImage)
	old.Config["Image"], _ = json.Marshal(image)

	// Image-declared anonymous volumes must be attached by their existing name;
	// otherwise Docker allocates empty ones when the replacement is created.
	var binds []string
	_ = json.Unmarshal(old.HostConfig["Binds"], &binds)
	var explicit []struct{ Target string }
	_ = json.Unmarshal(old.HostConfig["Mounts"], &explicit)
	for _, mount := range old.Mounts {
		if mount.Type != "volume" {
			continue
		}
		found := false
		for _, bind := range binds {
			parts := strings.Split(bind, ":")
			if len(parts) > 1 && parts[1] == mount.Destination {
				found = true
			}
		}
		for _, m := range explicit {
			if m.Target == mount.Destination {
				found = true
			}
		}
		if !found {
			mode := "rw"
			if !mount.RW {
				mode = "ro"
			}
			binds = append(binds, mount.Name+":"+mount.Destination+":"+mode)
		}
	}
	old.HostConfig["Binds"], _ = json.Marshal(binds)
	old.Config["HostConfig"], _ = json.Marshal(old.HostConfig)
	endpoints := map[string]map[string]json.RawMessage{}
	for name, settings := range old.NetworkSettings.Networks {
		endpoint := map[string]json.RawMessage{}
		// Runtime-assigned addresses and endpoint IDs are output, not requests.
		for _, key := range []string{"IPAMConfig", "Links", "DriverOpts", "GwPriority"} {
			if value, ok := settings[key]; ok {
				endpoint[key] = value
			}
		}
		var aliases []string
		_ = json.Unmarshal(settings["Aliases"], &aliases)
		kept := []string{}
		for _, alias := range aliases {
			if alias != old.ID && alias != old.ID[:min(12, len(old.ID))] {
				kept = append(kept, alias)
			}
		}
		endpoint["Aliases"], _ = json.Marshal(kept)
		endpoints[name] = endpoint
	}
	old.Config["NetworkingConfig"], _ = json.Marshal(map[string]any{"EndpointsConfig": endpoints})
	return &ContainerReplacement{ID: old.ID, Name: strings.TrimPrefix(old.Name, "/"), Image: previousImage, Running: old.State.Running, body: old.Config}, nil
}

// CreateReplacement creates a stopped container. Its predecessor can remain
// available under a backup name until the new process has successfully started.
func (c *APIClient) CreateReplacement(ctx context.Context, replacement *ContainerReplacement) (string, error) {
	var result struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodPost, "/containers/create", url.Values{"name": {replacement.Name}}, replacement.body, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

func (c *APIClient) ContainerRename(ctx context.Context, id, name string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/rename", url.Values{"name": {name}}, nil, nil)
}

// RemoveContainerKeepingVolumes is for a deployment replacement, whose data
// must survive even when it was put in an anonymous volume.
func (c *APIClient) RemoveContainerKeepingVolumes(ctx context.Context, id string, force bool) error {
	q := url.Values{"v": {"0"}}
	if force {
		q.Set("force", "1")
	}
	return c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), q, nil, nil)
}

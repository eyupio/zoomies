package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// The administrator tools: the part of a host and of the settings page an
// agent can reach once an administrator has switched `security.mcp_admin_tools`
// on. They sit apart from update_pool and update_host because the people who
// may use them are not the same: the sizing tools need an operator, these need
// an administrator, and an administrator's token is the one an agent steered by
// workflow-written text can least afford to hold by default.
//
// The transport offers them only when the controller's switch is on and the
// credential's role is administrator; the controller still decides each call on
// its own route. What this file adds on top is the settings allowlist below,
// because "may change settings" must not mean "may change how people sign in".

// adminTools is every tool the controller's switch governs.
var adminTools = map[string]bool{
	"edit_pool":           true,
	"edit_host":           true,
	"clear_host_throttle": true,
	"get_settings":        true,
	"update_settings":     true,
}

// IsAdminTool reports whether a tool is one the administrator switch governs.
func IsAdminTool(name string) bool { return adminTools[name] }

// tunableSettings are what update_settings may write: a name ending in a dot is
// every key under it, and any other name is that one key. They are the knobs
// that shape scheduling, retention and limits. Everything that decides who gets in,
// what the controller trusts or where it keeps its state -- security, oidc,
// github, provider, server, database, agent, backup -- is not here, so an
// agent that has been talked into a bad change can at worst make the fleet
// slower or tidier, and not locked open.
//
// updates is here by one key only. How often github.com is asked which release
// is current is as harmless as the rest, but updates.mode decides whether this
// controller replaces its own binary with nobody pressing anything, and
// updates.soak is the wait that stands between a bad release and every host. An
// agent steered by text a workflow wrote must not be able to switch either on
// or shorten it, so a person makes those at the Settings page, and a new key in
// the section is not tunable until it is named here.
var tunableSettings = []string{
	"scheduler.", "capacity_demand.", "retention.", "runners.", "limits.",
	"log.", "metrics.", "images.", "status.", "ui.", "updates.check_interval",
}

func tunable(key string) bool {
	for _, p := range tunableSettings {
		if strings.HasSuffix(p, ".") {
			if strings.HasPrefix(key, p) {
				return true
			}
		} else if key == p {
			return true
		}
	}
	return false
}

// tunableList is the allowlist as a person reads it, with the one rule that
// tells a section from a single key.
func tunableList() string {
	return strings.Join(tunableSettings, " ") + " (a name ending in a dot stands for every key under it)"
}

var pullPolicies = []string{"if-not-present", "always", "pinned-only"}
var cacheScopes = []string{"pool", "repository"}

func adminConfigTools() []*tool {
	return []*tool{
		{
			Name:  "edit_pool",
			Title: "Change a pool's image, pull policy or cache",
			Description: "Change which runner image a pool starts, when an image is pulled, or how the pool's disposable cache is kept, leaving every other " +
				"setting of the pool as it is. These are the settings update_pool does not touch, because they decide what code runs on the fleet and what a " +
				"job can leave behind for the next one: a different image runs whatever it holds on the pool's hosts, and the cache service shares one " +
				"cache between every repository on the pool, so a job that can write to it can plant an entry a later job restores. Do not turn the cache " +
				"service on for a pool that serves a public repository. pull_policy if-not-present starts a runner from the host's copy of an image and " +
				"pulls only when it has none; always asks the registry before every runner, which costs a round trip in front of the job; pinned-only " +
				"refuses a tag and takes only a digest. A moving tag is kept current by the hourly refresh and prewarm_pool, whichever policy is set. " +
				"It applies to runners created after the change. Read the pool first (list_pools) and pass what it showed in expect for every setting you " +
				"change, so a change made since is not written over. Needs an administrator token and `security.mcp_admin_tools` on.",
			InputSchema: object([]string{"pool_id", "expect"}, map[string]any{
				"pool_id":          str("the pool's ID, starting pool_"),
				"expect":           expectSchema("pool", "list_pools"),
				"image":            str("the runner image reference, a tag or a digest; an empty string follows the fleet's default image"),
				"pull_policy":      enum("when an image is pulled", pullPolicies...),
				"cache_enabled":    boolean("whether the pool has a disposable performance cache"),
				"cache_scope":      enum("pool gives the pool one cache; repository gives each repository its own and needs cache_repository on an organisation's installation", cacheScopes...),
				"cache_repository": str("the repository a repository-scoped cache belongs to, as owner/name"),
				"cache_size_limit": map[string]any{"type": "integer", "minimum": 0, "description": "the most bytes the cache may hold, enforced by evicting between one runner and the next; 0 is the default"},
				"cache_tools":      boolean("keep the tool cache (setup-go, setup-node and the like) between runners"),
				"cache_service":    boolean("run a cache service on each host so the Actions cache (actions/cache and the setup-* actions' own caches) is kept on the host, not in GitHub's service"),
			}),
			Annotations: annotations{Destructive: true, Idempotent: true},
			action:      true,
			call:        editPool,
		},
		{
			Name:  "edit_host",
			Title: "Rename, relabel, reserve disk on or cordon a host",
			Description: "Change a host's name, its labels, the disk it keeps back, or whether it is cordoned, leaving every other setting as it " +
				"is. Labels are replaced whole: read the host (list_hosts) and send the full set you want. A cordoned host keeps its running " +
				"runners and takes no new ones. Needs an administrator token and `security.mcp_admin_tools` on. The controller refuses a " +
				"change that would leave a pool with no host that could run it, and this tool never passes confirm=true.",
			InputSchema: object([]string{"host_id"}, map[string]any{
				"host_id":         str("the host's ID, starting host_"),
				"name":            str("what the host is called; non-empty, no control characters, not used by another host"),
				"labels":          map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "the host's whole label set, which replaces the one it has"},
				"reserve_disk_mb": map[string]any{"type": "integer", "minimum": 0, "description": "disk held back from placement, in MiB"},
				"cordoned":        boolean("true stops the host taking new runners, false lets it take them again"),
			}),
			Annotations: annotations{Idempotent: true},
			action:      true,
			call:        editHost,
		},
		{
			Name:  "clear_host_throttle",
			Title: "Lift a host's throttle",
			Description: "Lift the throttle the controller has a host on, so it takes its configured capacity again on the next pass. The next " +
				"heartbeat steps it back up if the pressure is still there, so use it for a host whose cause is known and fixed, not to switch " +
				"throttling off. Needs an administrator token and `security.mcp_admin_tools` on.",
			InputSchema: object([]string{"host_id"}, map[string]any{
				"host_id": str("the host's ID, starting host_"),
			}),
			Annotations: annotations{Idempotent: true},
			action:      true,
			call: func(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
				var a struct {
					HostID string `json:"host_id"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return nil, err
				}
				if err := requireID("host_id", a.HostID); err != nil {
					return nil, err
				}
				bc, ok := c.(BodyCaller)
				if !ok {
					return nil, fmt.Errorf("this transport cannot change a host")
				}
				body, err := bc.CallBody(ctx, http.MethodPost, "/hosts/"+url.PathEscape(a.HostID)+"/throttle/clear", nil, []byte(`{}`))
				if err != nil {
					return nil, err
				}
				return jsonContent(body), nil
			},
		},
		{
			Name:  "get_settings",
			Title: "Read settings",
			Description: "Every configuration key with its value, kind and where it came from, optionally only those under a prefix such as " +
				"scheduler. A secret's value is never sent. Needs an administrator token and `security.mcp_admin_tools` on.",
			InputSchema: object(nil, map[string]any{
				"prefix": str("only keys starting with this, such as scheduler or retention.jobs"),
			}),
			Annotations: readOnly,
			action:      true,
			call:        getSettings,
		},
		{
			Name:  "update_settings",
			Title: "Change tuning settings",
			Description: "Change these settings: " + tunableList() + " -- scheduling, retention, limits and the like. " +
				"A null value clears the stored setting, so the key goes back to the file or the default. Every key is checked before any is " +
				"written and the whole call is refused if one fails. Security, sign-in, GitHub, provider, server, database, agent and backup " +
				"settings are not changeable here: a person makes those at the Settings page. A change the running process cannot apply is " +
				"stored and named in pending_restart. Needs an administrator token and `security.mcp_admin_tools` on.",
			InputSchema: object([]string{"changes"}, map[string]any{
				"changes": map[string]any{"type": "object", "minProperties": 1, "description": "dotted setting key to new value, such as {\"retention.jobs\":\"720h\"}"},
			}),
			Annotations: annotations{Idempotent: true},
			action:      true,
			call:        updateSettings,
		},
	}
}

func editPool(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
	var a struct {
		PoolID          string         `json:"pool_id"`
		Image           *string        `json:"image"`
		PullPolicy      *string        `json:"pull_policy"`
		CacheEnabled    *bool          `json:"cache_enabled"`
		CacheScope      *string        `json:"cache_scope"`
		CacheRepository *string        `json:"cache_repository"`
		CacheSizeLimit  *int64         `json:"cache_size_limit"`
		CacheTools      *bool          `json:"cache_tools"`
		CacheService    *bool          `json:"cache_service"`
		Expect          map[string]any `json:"expect"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := requireID("pool_id", a.PoolID); err != nil {
		return nil, err
	}
	cache := a.CacheEnabled != nil || a.CacheScope != nil || a.CacheRepository != nil || a.CacheSizeLimit != nil || a.CacheTools != nil || a.CacheService != nil
	if a.Image == nil && a.PullPolicy == nil && !cache {
		return nil, fmt.Errorf("name at least one setting to change")
	}
	if a.Image != nil {
		if strings.ContainsAny(*a.Image, " \t\r\n") || strings.TrimSpace(*a.Image) != *a.Image {
			return nil, fmt.Errorf("image %q has whitespace in it: give one image reference, such as ghcr.io/eyupio/zoomies-runner-docker:dev or one ending @sha256:<digest>", *a.Image)
		}
	}
	if a.PullPolicy != nil && !slices.Contains(pullPolicies, *a.PullPolicy) {
		return nil, fmt.Errorf("pull_policy is %q: use one of %s", *a.PullPolicy, strings.Join(pullPolicies, ", "))
	}
	if a.CacheScope != nil && !slices.Contains(cacheScopes, *a.CacheScope) {
		return nil, fmt.Errorf("cache_scope is %q: use one of %s", *a.CacheScope, strings.Join(cacheScopes, ", "))
	}
	if a.CacheSizeLimit != nil && *a.CacheSizeLimit < 0 {
		return nil, fmt.Errorf("cache_size_limit cannot be negative")
	}
	bc, ok := c.(BodyCaller)
	if !ok {
		return nil, fmt.Errorf("this transport cannot change a pool")
	}
	path := "/pools/" + url.PathEscape(a.PoolID)
	current, err := readObject(ctx, c, path, "pool", a.PoolID, "list_pools")
	if err != nil {
		return nil, err
	}
	changes := map[string]change{}
	body := map[string]any{}
	if a.Image != nil {
		changes["image"] = change{Before: current["image"], After: *a.Image}
		body["image"] = *a.Image
	}
	if a.PullPolicy != nil {
		changes["pull_policy"] = change{Before: current["pull_policy"], After: *a.PullPolicy}
		body["pull_policy"] = *a.PullPolicy
	}
	if cache {
		// The cache is replaced whole, so it is read as it stands and only the
		// fields named change: its source folder and anything added since stay.
		merged := copyMap(current["cache"])
		set := func(field, name string, v any) {
			before := merged[field]
			if _, isBool := v.(bool); isBool && before == nil {
				// A flag the pool never set is off, and a model reading it says false.
				before = false
			}
			changes[name] = change{Before: before, After: v}
			merged[field] = v
		}
		if a.CacheEnabled != nil {
			set("enabled", "cache_enabled", *a.CacheEnabled)
		}
		if a.CacheScope != nil {
			set("scope", "cache_scope", *a.CacheScope)
		}
		if a.CacheRepository != nil {
			set("repository", "cache_repository", *a.CacheRepository)
		}
		if a.CacheSizeLimit != nil {
			set("size_limit", "cache_size_limit", *a.CacheSizeLimit)
		}
		if a.CacheTools != nil {
			set("tools", "cache_tools", *a.CacheTools)
		}
		if a.CacheService != nil {
			set("service", "cache_service", *a.CacheService)
		}
		body["cache"] = merged
	}
	if err := checkExpected("pool", "list_pools", a.Expect, changes); err != nil {
		return nil, err
	}
	return send(ctx, bc, path, body, sizingReply{
		ID:      a.PoolID,
		Name:    stringOf(current["name"]),
		Changes: changes,
		Note:    "Applies to runners created from now on; those already running finish as they are. An image or tag the hosts do not have yet is pulled by prewarm_pool or the next hourly refresh.",
	})
}

func editHost(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
	var a struct {
		HostID        string             `json:"host_id"`
		Name          *string            `json:"name"`
		Labels        *map[string]string `json:"labels"`
		ReserveDiskMB *int64             `json:"reserve_disk_mb"`
		Cordoned      *bool              `json:"cordoned"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := requireID("host_id", a.HostID); err != nil {
		return nil, err
	}
	if a.Name == nil && a.Labels == nil && a.ReserveDiskMB == nil && a.Cordoned == nil {
		return nil, fmt.Errorf("name at least one setting to change")
	}
	if a.Name != nil && strings.TrimSpace(*a.Name) == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	bc, ok := c.(BodyCaller)
	if !ok {
		return nil, fmt.Errorf("this transport cannot change a host")
	}
	path := "/hosts/" + url.PathEscape(a.HostID)
	current, err := readObject(ctx, c, path, "host", a.HostID, "list_hosts")
	if err != nil {
		return nil, err
	}
	changes := map[string]change{}
	body := map[string]any{}
	if a.Name != nil {
		changes["name"] = change{Before: current["name"], After: *a.Name}
		body["name"] = *a.Name
	}
	if a.Labels != nil {
		changes["labels"] = change{Before: current["labels"], After: *a.Labels}
		body["labels"] = *a.Labels
	}
	if a.ReserveDiskMB != nil {
		changes["reserve_disk_mb"] = change{Before: current["reserve_disk_mb"], After: *a.ReserveDiskMB}
		body["reserve_disk_mb"] = *a.ReserveDiskMB
	}
	reply := sizingReply{
		ID:      a.HostID,
		Name:    stringOf(current["name"]),
		Changes: changes,
		Note:    "Read the host again (list_hosts) to see it as it is now.",
	}
	if len(body) > 0 {
		// Never confirm=true: see send.
		if _, err := send(ctx, bc, path, body, reply); err != nil {
			return nil, err
		}
	}
	if a.Cordoned != nil {
		payload, err := json.Marshal(map[string]bool{"cordoned": *a.Cordoned})
		if err != nil {
			return nil, err
		}
		if _, err := bc.CallBody(ctx, http.MethodPost, path+"/cordon", nil, payload); err != nil {
			if len(body) > 0 {
				return nil, fmt.Errorf("the other changes were saved, but cordoning failed: %w", err)
			}
			return nil, err
		}
		changes["cordoned"] = change{Before: current["cordoned"], After: *a.Cordoned}
	}
	out, err := json.Marshal(reply)
	if err != nil {
		return nil, err
	}
	return jsonContent(out), nil
}

func getSettings(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
	var a struct {
		Prefix string `json:"prefix"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	body, err := c.Call(ctx, http.MethodGet, "/settings", nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Settings       []json.RawMessage `json:"settings"`
		PendingRestart []string          `json:"pending_restart"`
		Pinned         []string          `json:"pinned_by_environment"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("the settings came back unreadable: %w", err)
	}
	var keep []json.RawMessage
	for _, s := range page.Settings {
		var k struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(s, &k) == nil && strings.HasPrefix(k.Key, a.Prefix) {
			keep = append(keep, s)
		}
	}
	out, err := json.Marshal(map[string]any{
		"settings":              keep,
		"pending_restart":       page.PendingRestart,
		"pinned_by_environment": page.Pinned,
	})
	if err != nil {
		return nil, err
	}
	return jsonContent(out), nil
}

func updateSettings(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
	var a struct {
		Changes map[string]any `json:"changes"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if len(a.Changes) == 0 {
		return nil, fmt.Errorf("name at least one setting to change")
	}
	var refused []string
	for k := range a.Changes {
		if !tunable(k) {
			refused = append(refused, k)
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return nil, fmt.Errorf("%s cannot be changed over MCP: only these can: %s. Make the others at the Settings page",
			strings.Join(refused, ", "), tunableList())
	}
	bc, ok := c.(BodyCaller)
	if !ok {
		return nil, fmt.Errorf("this transport cannot change settings")
	}
	payload, err := json.Marshal(a.Changes)
	if err != nil {
		return nil, err
	}
	answer, err := bc.CallBody(ctx, http.MethodPatch, "/settings", nil, payload)
	if err != nil {
		return nil, err
	}
	var sent struct {
		PendingRestart []string `json:"pending_restart"`
	}
	_ = json.Unmarshal(answer, &sent)
	keys := make([]string, 0, len(a.Changes))
	for k := range a.Changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out, err := json.Marshal(map[string]any{
		"changed":         keys,
		"pending_restart": sent.PendingRestart,
		"note":            "Read the keys again with get_settings to see what is in force; anything in pending_restart waits for a controller restart.",
	})
	if err != nil {
		return nil, err
	}
	return jsonContent(out), nil
}

package mcp

import (
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The MCP tools are a window onto the REST API, and a window that is not kept
// in step with it quietly shows less: a filter the API gained last month that no
// tool accepts, so an assistant asked for "the jobs on this branch" lists every
// job and says nothing. These tests hold the two together in both directions.
//
// Every operation in api/openapi.yaml is either one a tool uses, or is named in
// routesNotOffered with the reason it is not. A new operation that is in neither
// fails the first test, so offering or declining it is a decision somebody makes
// on purpose. Every query parameter of a route a tool reads is either a property
// of that tool, under the name the tool gives it, or is named in skip with the
// reason a tool does not take it.

type apiSpec struct {
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components struct {
		Parameters map[string]apiParameter `yaml:"parameters"`
	} `yaml:"components"`
}

type apiParameter struct {
	Ref  string `yaml:"$ref"`
	Name string `yaml:"name"`
	In   string `yaml:"in"`
}

type apiOperation struct {
	Parameters []apiParameter `yaml:"parameters"`
}

func loadAPISpec(t *testing.T) apiSpec {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("reading the API contract: %v", err)
	}
	var spec apiSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parsing the API contract: %v", err)
	}
	return spec
}

// queryParams is the query parameters of one operation, with a reference to a
// shared parameter followed.
func (s apiSpec) queryParams(t *testing.T, path, method string) []string {
	t.Helper()
	var op apiOperation
	node := s.Paths[path][method]
	if err := node.Decode(&op); err != nil {
		t.Fatalf("reading %s %s: %v", strings.ToUpper(method), path, err)
	}
	var names []string
	for _, p := range op.Parameters {
		if p.Ref != "" {
			p = s.Components.Parameters[strings.TrimPrefix(p.Ref, "#/components/parameters/")]
		}
		if p.In == "query" {
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	return names
}

func (s apiSpec) operations() []string {
	var ops []string
	for path, item := range s.Paths {
		for method := range item {
			switch method {
			case "get", "post", "put", "patch", "delete":
				ops = append(ops, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(ops)
	return ops
}

// routesOffered is every operation a tool calls, and the tool that does. A
// route read by several tools is listed once, under the one that takes its
// parameters.
var routesOffered = map[string]string{
	"GET /analysis/compare":            "compare_periods",
	"GET /analysis/host-pressure":      "host_pressure",
	"GET /analysis/queue-wait":         "queue_wait_breakdown",
	"GET /analysis/recurring-failures": "recurring_failures",
	"GET /analysis/startup-failures":   "startup_failures",
	"GET /audit":                       "list_audit",
	"GET /catalog":                     "get_catalog",
	"GET /comparisons/{id}":            "get_retained_comparison",
	"GET /controller/load":             "controller_load",
	"GET /events":                      "wait_for_change",
	"GET /events/recent":               "recent_events",
	"GET /hosts":                       "list_hosts",
	"GET /hosts/samples":               "host_samples",
	"GET /hosts/{id}":                  "host_health",
	"GET /jobs":                        "list_jobs",
	"GET /jobs/stats":                  "job_stats",
	"GET /jobs/{id}":                   "get_job",
	"GET /jobs/{id}/events":            "get_job",
	"GET /jobs/{id}/explanation":       "get_job",
	"GET /jobs/{id}/pr-delay":          "get_pr_delay",
	"GET /kennel":                      "kennel_overview",
	"GET /kennel/repositories":         "kennel_findings",
	"GET /kennel/repositories/{id}":    "kennel_repository",
	"GET /kennel/repositories/{id}/pull-reviews/{number}": "review_pull_request",
	"GET /label-advice":                          "label_advice",
	"GET /machines":                              "list_machines",
	"GET /machines/{id}":                         "list_machines",
	"GET /meta":                                  "support_bundle",
	"GET /pools":                                 "list_pools",
	"GET /pools/performance":                     "pool_performance",
	"GET /problems":                              "list_problems",
	"GET /providers":                             "list_providers",
	"GET /providers/pairings":                    "provider_pairings",
	"GET /providers/renting":                     "list_providers",
	"GET /providers/{id}":                        "list_providers",
	"GET /runners":                               "list_runners",
	"GET /runners/{id}":                          "list_runners",
	"GET /runners/{id}/logs":                     "get_runner_log",
	"GET /runners/{id}/timeline":                 "runner_timeline",
	"GET /samples":                               "fleet_samples",
	"GET /scaling-events":                        "scaling_decisions",
	"GET /stats":                                 "fleet_status",
	"GET /usage":                                 "get_usage",
	"GET /webhook-deliveries":                    "list_webhook_deliveries",
	"GET /workflow-runs":                         "list_workflow_runs",
	"GET /diagnostics/bundle":                    "support_bundle",
	"GET /settings":                              "get_settings",
	"PATCH /settings":                            "update_settings",
	"GET /ai-context/access":                     "context_overview",
	"GET /ai-context/source/{id}/overview":       "context_overview",
	"GET /ai-context/source/{id}/read":           "context_read",
	"GET /ai-context/source/{id}/search":         "context_search",
	"GET /ai-context/source/{id}/pack":           "context_pack",
	"GET /ai-context/source/{id}/notes":          "context_notes",
	"GET /ai-context/source/{id}/notes/{slug}":   "context_notes",
	"POST /ai-context/source/{id}/notes":         "context_publish",
	"GET /assistant/repairs":                     "list_elis_repairs",
	"GET /assistant/repairs/{id}":                "get_eli_repair",
	"GET /kennel/fixes/timeout/previews/{id}":    "get_kennel_fix",
	"GET /optimisations/cache/previews/{id}":     "get_cache_preview",
	"GET /optimisations/checkout/previews/{id}":  "get_checkout_preview",
	"GET /optimisations/readiness/previews/{id}": "get_readiness_preview",
	"GET /optimisations/sharding/previews/{id}":  "get_sharding_preview",
	"PATCH /pools/{id}":                          "update_pool",
	"PATCH /hosts/{id}":                          "update_host",
	"POST /hosts/{id}/cordon":                    "edit_host",
	"POST /hosts/{id}/throttle/clear":            "clear_host_throttle",
	"POST /jobs/{id}/rerun":                      "rerun_job",
	"POST /runners/{id}/drain":                   "drain_runner",
	"POST /problems/apply":                       "apply_remedy",
	"POST /pools/{id}/prewarm":                   "prewarm_pool",
}

// routeNotOffered says why a group of operations has no tool. The first rule
// whose method and path prefix match decides, so a narrower rule goes before a
// wider one. A reason is for the next person to read, and says which of three
// things it is: a credential or an identity an agent must not hold, a change a
// person makes, or a read nobody has asked for yet.
type routeNotOffered struct {
	method, prefix, reason string
}

const (
	whyIdentity   = "who a caller is and how they sign in; an agent steered by workflow-written text must never change it"
	whyCredential = "holds or hands out a credential"
	whyAgent      = "the host agent's own protocol, spoken with the agent's token and never by an assistant"
	whyPerson     = "a change a person makes at the UI or CLI; an agent steered by workflow-written text is not offered it"
	whyMoney      = "buys, keeps or releases rented machines, so it costs money; a person decides"
	whyState      = "the controller's own state, its backups and its moves between machines, which only a platform operator touches"
	whyGap        = "a read no tool has been written for yet; a gap, not a decision"
)

var routesNotOffered = []routeNotOffered{
	{"*", "/agent/", whyAgent},
	{"*", "/auth/", whyIdentity},
	{"*", "/users", whyIdentity},
	{"*", "/tokens", whyCredential},
	{"*", "/join-tokens", whyCredential},
	{"*", "/mcp-clients", whyCredential},
	{"*", "/mcp-connections", whyCredential},
	{"*", "/backups", whyState},
	{"*", "/transfers", whyState},
	{"*", "/recovery/hosts/", whyState},
	{"POST", "/recovery/unfence", whyState},
	{"GET", "/recovery", whyGap},
	{"GET", "/storage", whyGap},
	{"GET", "/diagnostics/cpu-profile", "a Go profile of the controller process, for a developer"},
	{"GET", "/usage.csv", "the same figures as get_usage, as a file download"},
	{"*", "/settings/", "exports and imports the whole configuration, secrets included in shape; update_settings changes one tunable key at a time"},
	{"*", "/assistant/", "the controller's built-in assistant: its chat, its providers and keys, and Eli's repair controls, which change repositories"},
	{"*", "/installations", whyCredential + " (a GitHub App); reading them is a gap"},
	{"*", "/provider-setups", whyCredential},
	{"*", "/providers", whyMoney},
	{"*", "/machines", whyMoney},
	{"POST", "/pools/{id}/enable", "switching a pool on or off stays with a person; update_pool never does"},
	{"POST", "/pools/{id}/disable", "switching a pool on or off stays with a person; update_pool never does"},
	{"*", "/pools", whyPerson + " (create, delete, import, validate)"},
	{"GET", "/auto-pools", whyGap},
	{"*", "/size-pins", whyGap + " (writes are a person's)"},
	{"*", "/toolchains", whyGap + " (the scan is a person's)"},
	{"*", "/runners", whyPerson + " (removing a runner, bulk actions, a log download)"},
	{"*", "/provisioning", whyPerson},
	{"GET", "/jobs/facets", "the values the Jobs page's filter menus offer"},
	{"*", "/jobs/", whyPerson + " (cancel, acknowledge)"},
	{"*", "/workflow-runs", whyPerson + " (cancel, pause provisioning)"},
	{"GET", "/controller/machine", whyGap},
	{"GET", "/hosts/{id}/recommendation", whyGap},
	{"*", "/hosts", whyPerson + " (retire, update, health check, accepted checks)"},
	{"*", "/kennel", whyPerson + " (waivers, tracking, rechecks, fixes and their publication, agent guidance)"},
	{"GET", "/problems/auto-applied", whyGap},
	{"POST", "/problems/auto-applied/", whyPerson},
	{"GET", "/audit/actions", "the list of action names the Audit page filters by"},
	{"GET", "/status", whyGap},
	{"GET", "/updates", whyGap},
	{"*", "/updates", "updating the controller or the hosts is decided by the update mode and soak, which an agent must never move"},
	{"*", "/optimisations", whyPerson + " (inspections, previews, approval, publication, enablement)"},
	{"*", "/comparisons", "starting a comparison runs workflows on the fleet; get_retained_comparison reads one"},
	{"*", "/migrations", "rewrites a repository's workflows by pull request"},
	{"*", "/webhook-test", "sends a test delivery"},
	{"*", "/ai-context/", whyPerson + " (setting a repository up for AI Context, its members and owners, maintenance)"},
}

func notOffered(op string) (string, bool) {
	method, path, _ := strings.Cut(op, " ")
	for _, r := range routesNotOffered {
		if (r.method == "*" || r.method == method) && strings.HasPrefix(path, r.prefix) {
			return r.reason, true
		}
	}
	return "", false
}

func TestEveryAPIOperationIsOfferedOrDeclinedOnPurpose(t *testing.T) {
	spec := loadAPISpec(t)
	have := map[string]bool{}
	for _, tl := range tools() {
		have[tl.Name] = true
	}
	ops := spec.operations()
	if len(ops) < 100 {
		t.Fatalf("read only %d operations from the contract, so the walk is wrong, not the API", len(ops))
	}
	seen := map[string]bool{}
	for _, op := range ops {
		seen[op] = true
		if tool, ok := routesOffered[op]; ok {
			if !have[tool] {
				t.Errorf("%s is listed as offered by %s, which is not a tool", op, tool)
			}
			continue
		}
		if _, ok := notOffered(op); ok {
			continue
		}
		t.Errorf("%s is neither offered by a tool nor declined in routesNotOffered. Offer it, or add a rule with the reason it is not", op)
	}
	for op := range routesOffered {
		if !seen[op] {
			t.Errorf("routesOffered lists %s, which the API contract no longer has", op)
		}
	}
	for _, r := range routesNotOffered {
		matched := false
		for _, op := range ops {
			if _, ok := routesOffered[op]; ok {
				continue
			}
			method, path, _ := strings.Cut(op, " ")
			if (r.method == "*" || r.method == method) && strings.HasPrefix(path, r.prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("routesNotOffered rule %s %s matches no operation that is not already offered; remove it", r.method, r.prefix)
		}
	}
}

// parameterCoverage is how a tool takes the query parameters of the route it
// reads: alias names the property where the tool calls a parameter something
// else, and skip names a parameter no tool property takes, with why.
type parameterCoverage struct {
	tool  string
	alias map[string]string
	skip  map[string]string
}

const (
	whyUIOnly      = "a column the page sorts its table by; the tools return the newest first, and a sort would break the cursor"
	whyWindowPaged = "the tool pages by narrowing since and until, which cannot skip or repeat a row as the list moves; an offset can"
	whyBrowser     = "the Jobs page's own paused and expedited provisioning state"
)

var routeParameters = map[string]parameterCoverage{
	"GET /audit": {tool: "list_audit", skip: map[string]string{"offset": whyWindowPaged, "sort": whyUIOnly, "order": whyUIOnly}},
	"GET /jobs": {tool: "list_jobs", alias: map[string]string{"q": "query", "faulted": "ours", "workflow_failed": "theirs"}, skip: map[string]string{
		"provisioning": whyBrowser, "sort": whyUIOnly, "order": whyUIOnly, "offset": "the tool pages with before, the cursor the route sends back",
	}},
	"GET /jobs/stats":                  {tool: "job_stats"},
	"GET /jobs/{id}/explanation":       {tool: "get_job", alias: map[string]string{"logs": "log_lines"}},
	"GET /kennel/repositories":         {tool: "kennel_findings", skip: map[string]string{"sort": whyUIOnly, "order": whyUIOnly}},
	"GET /label-advice":                {tool: "label_advice", skip: map[string]string{"window": "the advice is worked out over the window the controller was configured with"}},
	"GET /machines":                    {tool: "list_machines", alias: map[string]string{"provider": "provider_id", "pool": "pool_id", "host": "host_id", "q": "query"}, skip: map[string]string{"sort": whyUIOnly, "order": whyUIOnly}},
	"GET /runners":                     {tool: "list_runners", alias: map[string]string{"q": "query"}, skip: map[string]string{"sort": whyUIOnly, "order": whyUIOnly, "offset": "the tool takes a limit and a filter, and a runner list that moves as it is read cannot be paged by position"}},
	"GET /runners/{id}/logs":           {tool: "get_runner_log", alias: map[string]string{"tail": "lines"}, skip: map[string]string{"follow": "a stream; the tool returns the last lines and is done"}},
	"GET /ai-context/access":           {tool: "context_overview", alias: map[string]string{"q": "query"}},
	"GET /ai-context/source/{id}/pack": {tool: "context_pack", alias: map[string]string{"path": "paths"}},
	"GET /assistant/repairs":           {tool: "list_elis_repairs", skip: map[string]string{"offset": "the tool pages with before, the cursor the route sends back"}},
	"GET /events/recent":               {tool: "recent_events", skip: map[string]string{"wait": "wait_for_change is the tool that holds for the next change"}},
	"GET /usage":                       {tool: "get_usage", alias: map[string]string{"from": "since", "to": "until"}},
	"GET /workflow-runs": {tool: "list_workflow_runs", alias: map[string]string{"faulted": "ours", "workflow_failed": "theirs"}, skip: map[string]string{
		"provisioning": whyBrowser, "sort": whyUIOnly, "order": whyUIOnly, "offset": whyWindowPaged,
	}},
}

func toolProperties(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, tl := range tools() {
		if tl.Name != name {
			continue
		}
		props, ok := tl.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s has no properties in its input schema", name)
		}
		return props
	}
	t.Fatalf("there is no tool %s", name)
	return nil
}

func TestEveryQueryParameterOfAReadRouteIsAToolPropertyOrSkippedWithAReason(t *testing.T) {
	spec := loadAPISpec(t)
	for op, tool := range routesOffered {
		method, path, _ := strings.Cut(op, " ")
		if method != "GET" {
			continue
		}
		params := spec.queryParams(t, path, "get")
		cov, listed := routeParameters[op]
		if len(params) == 0 {
			if listed {
				t.Errorf("routeParameters lists %s, which takes no query parameters; remove it", op)
			}
			continue
		}
		if !listed {
			// A route listed nowhere is held to the plain rule: the tool that
			// reads it takes each parameter under the API's own name.
			cov = parameterCoverage{tool: tool}
		}
		props := toolProperties(t, cov.tool)
		isParam := map[string]bool{}
		for _, p := range params {
			isParam[p] = true
			name := p
			if a, ok := cov.alias[p]; ok {
				name = a
			}
			_, has := props[name]
			reason, skipped := cov.skip[p]
			switch {
			case has && skipped:
				t.Errorf("%s: %s is both a property of %s (as %s) and skipped (%q); drop the skip", op, p, cov.tool, name, reason)
			case !has && !skipped:
				t.Errorf("%s: the API takes %s and %s has no %q property. Add one, or skip it in routeParameters with the reason a tool does not take it", op, p, cov.tool, name)
			}
		}
		for p := range cov.skip {
			if !isParam[p] {
				t.Errorf("%s: skips %s, which the API route does not take", op, p)
			}
		}
		for p := range cov.alias {
			if !isParam[p] {
				t.Errorf("%s: aliases %s, which the API route does not take", op, p)
			}
		}
	}
}

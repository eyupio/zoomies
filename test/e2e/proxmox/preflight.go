package proxmox

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Everything this harness needs from outside itself, and every check that can
// be made before anything exists.
//
// "Before anything exists" is the rule the whole file is written to. The
// end-to-end harness next door once looked for the gh CLI in the middle of its
// scenario, after it had already made an installation and a pool on a real
// organisation, and then skipped -- which is exit code zero. Here the stakes are
// higher again: half a prerequisite means a provider row pointed at a cluster,
// and a provider row pointed at a cluster builds virtual machines. So every
// check that can be made without changing anything is made here, including the
// read-only ones against the cluster itself, and nothing is created until they
// all hold.

// env is the whole configuration of one qualification run.
type env struct {
	// The cluster.
	endpoint   string
	token      string
	secret     string
	caPEMFile  string
	insecure   bool
	node       string
	storage    string
	bridge     string
	pool       string
	templateID int
	// brokenTemplateID is a template that cannot bootstrap -- no guest agent,
	// or no network -- used by the induced bootstrap failure. It is optional:
	// without one that case induces the failure by pointing the machines at a
	// controller address nothing answers on, and the evidence says which method
	// was used.
	brokenTemplateID int
	vmidMin          int
	vmidMax          int
	// controllerURL is how a guest on the cluster reaches this controller. It
	// defaults to the address this host uses to reach the cluster, which is
	// right on a flat network and wrong behind NAT -- hence the override.
	controllerURL string

	// The machine shape and the runner backend inside the guest, which are
	// evidence: a timing without the shape it was measured on is not a
	// measurement, and one combination is what this procedure qualifies.
	cpus     int
	memoryMB int
	diskMB   int
	backend  string

	// GitHub. The names are the end-to-end harness's, because it is the same
	// App, the same organisation and the same fixture workflow; asking for a
	// second set of credentials for the same thing is how a procedure stops
	// being run.
	appID          string
	installationID string
	privateKey     string
	target         string
	targetType     string
	repo           string
	workflow       string
}

// vmidRange is the block this run may allocate inside.
func (e env) vmidRange() Range { return Range{Min: e.vmidMin, Max: e.vmidMax} }

// required reports whether a missing prerequisite is a failure rather than a
// skip. A person qualifying a cluster runs in this mode; a laptop does not.
func required() bool { return os.Getenv("ZOOMIES_PROXMOX_REQUIRED") != "" }

// the six variables without which there is nothing to qualify against.
var clusterVars = []string{
	"ZOOMIES_PROXMOX_URL",
	"ZOOMIES_PROXMOX_TOKEN",
	"ZOOMIES_PROXMOX_NODE",
	"ZOOMIES_PROXMOX_TEMPLATE",
	"ZOOMIES_PROXMOX_STORAGE",
	"ZOOMIES_PROXMOX_VMID_RANGE",
}

// requested reports whether this run was asked to do anything at all. Any one of
// the six means somebody meant to: the rest then being absent is a blockage to
// report, not a laptop to be quiet on.
func requested() bool {
	if required() {
		return true
	}
	for _, v := range clusterVars {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// skipReason is the sentence a run that was never asked for is skipped with. It
// names every variable rather than the first, because somebody reading it is
// about to go and set all of them.
func skipReason() string {
	return "no Proxmox cluster was given, so there is nothing to qualify against.\n" +
		"This harness needs a DISPOSABLE Proxmox VE cluster and a GitHub installation:\n" +
		"  " + strings.Join(clusterVars, "\n  ") + "\n" +
		"  ZOOMIES_E2E_APP_ID, ZOOMIES_E2E_INSTALLATION_ID, ZOOMIES_E2E_PRIVATE_KEY_FILE,\n" +
		"  ZOOMIES_E2E_TARGET, ZOOMIES_E2E_REPO\n" +
		"See test/e2e/proxmox/README.md, and roadmap/validation/proxmox-qualification.md\n" +
		"for the procedure this produces the evidence for. Fixture success is not live qualification."
}

// preflight reads the environment and checks everything that can be checked
// without a network. It creates nothing and dials nothing.
func preflight() (env, []string) {
	e := env{
		endpoint:      strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_URL")),
		token:         strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_TOKEN")),
		secret:        strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_TOKEN_SECRET")),
		caPEMFile:     strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_CA_PEM_FILE")),
		insecure:      truthy(os.Getenv("ZOOMIES_PROXMOX_INSECURE")),
		node:          strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_NODE")),
		storage:       strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_STORAGE")),
		bridge:        orDefault(os.Getenv("ZOOMIES_PROXMOX_BRIDGE"), "vmbr0"),
		pool:          strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_POOL")),
		controllerURL: strings.TrimSpace(os.Getenv("ZOOMIES_PROXMOX_CONTROLLER_URL")),

		cpus:     atoiOr(os.Getenv("ZOOMIES_PROXMOX_CPUS"), 2),
		memoryMB: atoiOr(os.Getenv("ZOOMIES_PROXMOX_MEMORY_MB"), 2048),
		diskMB:   atoiOr(os.Getenv("ZOOMIES_PROXMOX_DISK_MB"), 0),
		backend:  orDefault(os.Getenv("ZOOMIES_PROXMOX_BACKEND"), "docker"),

		appID:          strings.TrimSpace(os.Getenv("ZOOMIES_E2E_APP_ID")),
		installationID: strings.TrimSpace(os.Getenv("ZOOMIES_E2E_INSTALLATION_ID")),
		target:         strings.TrimSpace(os.Getenv("ZOOMIES_E2E_TARGET")),
		targetType:     orDefault(os.Getenv("ZOOMIES_E2E_TARGET_TYPE"), "org"),
		repo:           strings.TrimSpace(os.Getenv("ZOOMIES_E2E_REPO")),
		workflow:       orDefault(os.Getenv("ZOOMIES_PROXMOX_WORKFLOW"), "zoomies-e2e.yml"),
	}

	var missing []string
	add := func(format string, args ...any) { missing = append(missing, fmt.Sprintf(format, args...)) }

	for _, v := range []struct{ name, value string }{
		{"ZOOMIES_PROXMOX_URL", e.endpoint},
		{"ZOOMIES_PROXMOX_TOKEN", e.token},
		{"ZOOMIES_PROXMOX_NODE", e.node},
		{"ZOOMIES_PROXMOX_STORAGE", e.storage},
		{"ZOOMIES_E2E_APP_ID", e.appID},
		{"ZOOMIES_E2E_INSTALLATION_ID", e.installationID},
		{"ZOOMIES_E2E_TARGET", e.target},
		{"ZOOMIES_E2E_REPO", e.repo},
	} {
		if v.value == "" {
			add("%s is not set", v.name)
		}
	}

	e.templateID = atoiOr(os.Getenv("ZOOMIES_PROXMOX_TEMPLATE"), 0)
	if e.templateID <= 0 {
		add("ZOOMIES_PROXMOX_TEMPLATE is not a template VMID; every machine starts as a clone of one")
	}
	e.brokenTemplateID = atoiOr(os.Getenv("ZOOMIES_PROXMOX_BROKEN_TEMPLATE"), 0)

	// The range is the one setting that makes this safe to run at all: without
	// it the provider would allocate identifiers anywhere in the cluster,
	// including ones somebody else's automation is entitled to.
	switch min, max, err := parseRange(os.Getenv("ZOOMIES_PROXMOX_VMID_RANGE")); {
	case err != nil:
		add("ZOOMIES_PROXMOX_VMID_RANGE %v", err)
	case max-min+1 < qualificationCycles:
		// Not strictly required -- the cycles are sequential and reuse
		// identifiers -- but a range smaller than the number of machines the
		// procedure builds is almost always a typo, and finding out at cycle
		// nineteen wastes an afternoon.
		add("ZOOMIES_PROXMOX_VMID_RANGE %d-%d holds %d identifiers and the procedure builds %d machines; "+
			"reserve at least that many", min, max, max-min+1, qualificationCycles)
	default:
		e.vmidMin, e.vmidMax = min, max
	}
	if e.templateID > 0 && e.vmidMin > 0 && e.vmidRange().contains(e.templateID) {
		add("the template %d is inside the VMID range %s this run allocates from; "+
			"the closing reconciliation would count the template as a machine, and a cycle could try to take its identifier",
			e.templateID, e.vmidRange())
	}

	switch e.backend {
	case "docker", "podman", "process":
	default:
		add("ZOOMIES_PROXMOX_BACKEND is %q; the guest runs runners with docker, podman or process", e.backend)
	}
	switch e.targetType {
	case "org", "repo":
	default:
		add("ZOOMIES_E2E_TARGET_TYPE is %q; it must be org or repo", e.targetType)
	}
	if keyFile := strings.TrimSpace(os.Getenv("ZOOMIES_E2E_PRIVATE_KEY_FILE")); keyFile == "" {
		add("ZOOMIES_E2E_PRIVATE_KEY_FILE is not set")
	} else {
		pem, err := os.ReadFile(keyFile)
		switch {
		case err != nil:
			add("ZOOMIES_E2E_PRIVATE_KEY_FILE %s cannot be read: %v", keyFile, err)
		case !strings.Contains(string(pem), "PRIVATE KEY"):
			add("%s does not look like a PEM private key", keyFile)
		default:
			e.privateKey = string(pem)
		}
	}
	if e.caPEMFile != "" {
		if _, err := os.Stat(e.caPEMFile); err != nil {
			add("ZOOMIES_PROXMOX_CA_PEM_FILE %s cannot be read: %v", e.caPEMFile, err)
		}
	}

	// The tools, all of them, up front. Docker is not among them: the runner
	// backend runs inside the guest this harness builds, not on this host.
	for _, tool := range []struct{ bin, why string }{
		{"gh", "the workflow is dispatched and GitHub is asked about leftover registrations through the gh CLI"},
		{"git", "the evidence records the commit under test"},
	} {
		if _, err := exec.LookPath(tool.bin); err != nil {
			add("%s is not on PATH: %s", tool.bin, tool.why)
		}
	}
	if _, err := exec.LookPath("gh"); err == nil {
		if out, err := exec.Command("gh", "auth", "status").CombinedOutput(); err != nil {
			add("the gh CLI is not authenticated: %s", strings.TrimSpace(string(out)))
		}
	}
	if _, err := os.Stat(builtBinary()); err != nil {
		add("the zoomies binary is not built; run `make build-nogui` first")
	}

	// How a guest reaches this controller. This is the prerequisite people get
	// wrong: a controller bound to loopback can be talked to from this machine
	// and from nowhere else, so every machine it builds enrols never.
	if e.controllerURL == "" && e.endpoint != "" {
		if addr, err := outboundAddress(e.endpoint); err != nil {
			add("could not work out which address a machine would reach this controller on (%v); "+
				"set ZOOMIES_PROXMOX_CONTROLLER_URL to a URL the guests can reach, with a port", err)
		} else {
			e.controllerURL = "http://" + net.JoinHostPort(addr, strconv.Itoa(freePortNumber()))
		}
	} else if e.controllerURL != "" {
		if u, err := url.Parse(e.controllerURL); err != nil || u.Host == "" || u.Port() == "" {
			add("ZOOMIES_PROXMOX_CONTROLLER_URL %q is not a URL with a port; "+
				"write it as http://10.0.0.5:8080 -- the port is what this controller binds", e.controllerURL)
		}
	}
	return e, missing
}

// clusterFacts is what a read-only look at the cluster established, and most of
// it is evidence: the version and the template are rows in the qualification
// record, and the identifiers already in the range are what the closing
// reconciliation subtracts.
type clusterFacts struct {
	Version          string
	TemplateName     string
	PreexistingVMIDs []int
	Preexisting      []Guest
}

// preflightCluster asks the cluster whether this run could work, changing
// nothing. Every call in it is a GET.
func preflightCluster(ctx context.Context, c *Cluster, e env) (clusterFacts, []string) {
	var facts clusterFacts
	var missing []string
	add := func(format string, args ...any) { missing = append(missing, fmt.Sprintf(format, args...)) }

	version, err := c.Version(ctx)
	if err != nil {
		// One problem, not eleven: a cluster that cannot be reached fails every
		// check below and listing them all would bury this.
		return facts, []string{fmt.Sprintf("the cluster at %s did not answer: %v", e.endpoint, err)}
	}
	facts.Version = version

	inv, err := c.Take(ctx, e.node, e.storage)
	if err != nil {
		return facts, []string{fmt.Sprintf("the cluster answered its version but not its inventory: %v", err)}
	}

	var sawNode, sawTemplate bool
	for _, g := range inv.Guests {
		if g.Node == e.node {
			sawNode = true
		}
		if g.VMID == e.templateID {
			sawTemplate = true
			facts.TemplateName = g.Name
			if g.Template == 0 {
				add("VM %d is not a template; the provider clones from a template, and cloning a running guest is a different operation", e.templateID)
			}
		}
	}
	if !sawNode && len(inv.Guests) > 0 {
		// Weak evidence -- a node with no guests at all is invisible here -- so
		// it is worded as the doubt it is rather than asserted.
		add("no guest on this cluster is on node %q; check the node name, because a machine has to be built somewhere", e.node)
	}
	if !sawTemplate {
		add("no guest with VMID %d exists on this cluster, so there is no template to clone", e.templateID)
	}
	if e.brokenTemplateID > 0 {
		found := false
		for _, g := range inv.Guests {
			if g.VMID == e.brokenTemplateID {
				found = true
			}
		}
		if !found {
			add("ZOOMIES_PROXMOX_BROKEN_TEMPLATE %d does not exist; leave it unset to induce the bootstrap failure another way", e.brokenTemplateID)
		}
	}

	// What is already in the range. A guest wearing this fleet's marks in a
	// range reserved for this procedure is a leftover, and starting a
	// qualification on top of one would make the closing reconciliation
	// meaningless: it could not tell this run's litter from the last run's.
	for _, g := range inv.InRange(e.vmidRange()) {
		facts.PreexistingVMIDs = append(facts.PreexistingVMIDs, g.VMID)
		facts.Preexisting = append(facts.Preexisting, g)
		if g.Ours() {
			add("%s is inside the reserved range %s and already wears this fleet's marks; "+
				"it is an earlier run's leftover. Delete it, record it in the qualification, and start again",
				g, e.vmidRange())
		}
	}
	return facts, missing
}

// outboundAddress is the address on this host that a packet to the cluster
// leaves from, which is the address a guest on that cluster can almost always
// reach this controller on. It dials nothing: a UDP "connection" only asks the
// routing table.
func outboundAddress(endpoint string) (string, error) {
	host := endpoint
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	conn, err := net.Dial("udp", net.JoinHostPort(host, "8006"))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("the local address is not an IP address")
	}
	return addr.IP.String(), nil
}

// freePortNumber asks the kernel for a port nothing is using. It is used only to
// build the default controller URL, before the controller binds it; the window
// between the two is this harness's own and nothing else on the host is racing
// for it.
func freePortNumber() int {
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return 8080
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// parseRange reads "9000-9099".
func parseRange(raw string) (int, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, fmt.Errorf("is not set; reserve a block of VMIDs for this and nothing else, for example 9000-9099")
	}
	lo, hi, ok := strings.Cut(raw, "-")
	if !ok {
		return 0, 0, fmt.Errorf("%q is not a range; write it as 9000-9099", raw)
	}
	min, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return 0, 0, fmt.Errorf("%q does not begin with a number", raw)
	}
	max, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil {
		return 0, 0, fmt.Errorf("%q does not end with a number", raw)
	}
	if min <= 0 || max <= 0 {
		return 0, 0, fmt.Errorf("%q is not a range of VMIDs; they start at 100", raw)
	}
	if min > max {
		return 0, 0, fmt.Errorf("%q runs backwards", raw)
	}
	return min, max, nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func atoiOr(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}

func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// builtBinary is the zoomies this harness runs. It is the built one, not one
// this test builds: qualification is of the artefact.
func builtBinary() string {
	dir, err := os.Getwd()
	if err != nil {
		return "zoomies"
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "zoomies")
		}
		dir = filepath.Dir(dir)
	}
	return "zoomies"
}

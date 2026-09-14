package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The inventory half: what is actually on the cluster, and whether every owned
// thing on it can be accounted for.
//
// It speaks the Proxmox API itself rather than through internal/provider/proxmox,
// and that duplication is the point. The question this answers is "did the code
// under test leave anything behind", and a check that asks the code under test
// cannot answer it: a bug in how the provider recognises its own guests would
// hide exactly the guests it failed to delete. The end-to-end harness next door
// asks GitHub and the Docker daemon directly for the same reason.
//
// Everything here is read-only. Nothing in this file can change a cluster.

// fleetTag and machineNamePrefix are how an owned guest is recognised. They are
// written out rather than imported so that this check has its own idea of what
// the fleet's marks look like; a test holds the two definitions equal, which is
// the whole value of writing them twice.
const (
	fleetTag          = "zoomies"
	machineNamePrefix = "zoomies-mach-"
)

// Range is the block of VMIDs this qualification was given. Nothing outside it
// is this procedure's business -- a guest at 150 on a cluster somebody else also
// uses is not evidence of anything.
type Range struct{ Min, Max int }

func (r Range) contains(vmid int) bool { return vmid >= r.Min && vmid <= r.Max }
func (r Range) String() string         { return fmt.Sprintf("%d-%d", r.Min, r.Max) }

// ClusterOptions is how to reach one Proxmox VE cluster read-only.
type ClusterOptions struct {
	// Endpoint is the cluster as the operator wrote it: https://pve:8006.
	Endpoint string
	// Token is the whole credential -- "user@realm!tokenid=secret" -- or just
	// the identifier when Secret is given separately.
	Token  string
	Secret string
	// CAPEMFile pins the cluster's own certificate, which is what a Proxmox
	// cluster usually has. Insecure turns verification off; it is offered
	// because a homelab cluster's certificate is its own and refusing would
	// mean the qualification could not be run, and it is reported in the
	// evidence so that a run made without verification says so.
	CAPEMFile string
	Insecure  bool
}

// Cluster is a read-only Proxmox VE client.
type Cluster struct {
	base     string
	auth     string
	insecure bool
	http     *http.Client
}

// NewCluster builds the client. It never dials: an unreachable cluster is
// something the preflight reports, not something a constructor panics about.
func NewCluster(opts ClusterOptions) (*Cluster, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("no cluster endpoint")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%q is not an address: %w", opts.Endpoint, err)
	}
	if u.Scheme != "https" {
		// A hypervisor credential over plain HTTP is a hypervisor credential on
		// the wire. The provider refuses this too.
		return nil, fmt.Errorf("%q is not https; a Proxmox API token must not cross a network in the clear", opts.Endpoint)
	}
	if u.Port() == "" {
		u.Host += ":8006"
	}

	token, secret := strings.TrimSpace(opts.Token), strings.TrimSpace(opts.Secret)
	if secret == "" {
		id, rest, ok := strings.Cut(token, "=")
		if !ok || strings.TrimSpace(rest) == "" {
			return nil, fmt.Errorf("the token is not user@realm!tokenid=secret; " +
				"paste it whole, or give the secret separately")
		}
		token, secret = strings.TrimSpace(id), strings.TrimSpace(rest)
	}
	if !strings.Contains(token, "!") {
		return nil, fmt.Errorf("%q is not a token identifier; it is user@realm!tokenid", token)
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: opts.Insecure}
	if opts.CAPEMFile != "" {
		pem, err := os.ReadFile(opts.CAPEMFile)
		if err != nil {
			return nil, fmt.Errorf("reading the cluster's CA certificate %s: %w", opts.CAPEMFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s holds no certificate this can use", opts.CAPEMFile)
		}
		tlsCfg.RootCAs = pool
	}
	return &Cluster{
		base:     u.String() + "/api2/json",
		auth:     "PVEAPIToken=" + token + "=" + secret,
		insecure: opts.Insecure,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// Verified reports whether this client checked the cluster's certificate. The
// evidence says so either way: a qualification run with verification off is
// still a qualification, but it is a different one.
func (c *Cluster) Verified() bool { return !c.insecure }

// get reads one endpoint. Proxmox wraps every answer in {"data": ...}.
func (c *Cluster) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.http.Do(req)
	if err != nil {
		// The credential is in the header, and an http.Client error quotes the
		// URL but never the headers; the message is safe as it stands.
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body.Data, out); err != nil {
		return fmt.Errorf("decoding %s's data: %w", path, err)
	}
	return nil
}

// Version is what the cluster says it is, verbatim. "It worked" is worth little
// without "against what", and the qualification record has a row for it.
func (c *Cluster) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
		Release string `json:"release"`
		RepoID  string `json:"repoid"`
	}
	if err := c.get(ctx, "/version", &v); err != nil {
		return "", err
	}
	out := v.Version
	if v.Release != "" && !strings.Contains(out, v.Release) {
		out += "/" + v.Release
	}
	if v.RepoID != "" {
		out += " (" + v.RepoID + ")"
	}
	return out, nil
}

// Guest is one virtual machine as the cluster lists it.
type Guest struct {
	VMID     int    `json:"vmid"`
	Node     string `json:"node"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Template int    `json:"template"`
	Tags     string `json:"tags"`
	Pool     string `json:"pool"`
}

// Ours reports whether this guest wears one of the fleet's marks. It is a
// hint and not proof -- anybody with configuration rights can write both marks
// -- but a guest wearing them that nothing can account for is the finding this
// whole procedure is looking for.
func (g Guest) Ours() bool {
	if strings.HasPrefix(strings.ToLower(g.Name), machineNamePrefix) {
		return true
	}
	for _, t := range strings.FieldsFunc(g.Tags, func(r rune) bool {
		return r == ';' || r == ',' || r == ' '
	}) {
		if strings.EqualFold(strings.TrimSpace(t), fleetTag) {
			return true
		}
	}
	return false
}

func (g Guest) String() string {
	return fmt.Sprintf("VM %d on %s (%q, %s)", g.VMID, g.Node, g.Name, g.Status)
}

// Volume is one disk image on the configured storage. A guest that is destroyed
// without its disks leaves these, which is the failure mode that costs an
// operator storage rather than CPU and is therefore the one nobody notices.
type Volume struct {
	VolID  string `json:"volid"`
	VMID   int    `json:"vmid"`
	Size   int64  `json:"size"`
	Format string `json:"format"`
}

// Inventory is one look at the cluster, stamped with when it was taken.
type Inventory struct {
	TakenAt  time.Time `json:"taken_at"`
	Endpoint string    `json:"endpoint"`
	Node     string    `json:"node"`
	Storage  string    `json:"storage"`
	Verified bool      `json:"tls_verified"`
	Guests   []Guest   `json:"guests"`
	Volumes  []Volume  `json:"volumes"`
}

// InRange is the guests inside a VMID range, templates excluded: a template is
// the thing machines are made from, not a machine.
func (inv Inventory) InRange(r Range) []Guest {
	var out []Guest
	for _, g := range inv.Guests {
		if r.contains(g.VMID) && g.Template == 0 {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VMID < out[j].VMID })
	return out
}

// Take lists every guest on the cluster and every volume on one storage.
//
// One cluster-wide call for the guests rather than one per node, because a
// machine that ended up on a node nobody expected is exactly the kind of thing
// this is meant to find.
func (c *Cluster) Take(ctx context.Context, node, storage string) (Inventory, error) {
	inv := Inventory{TakenAt: time.Now().UTC(), Node: node, Storage: storage, Verified: c.Verified(), Endpoint: c.base}
	var guests []Guest
	if err := c.get(ctx, "/cluster/resources?type=vm", &guests); err != nil {
		return inv, err
	}
	inv.Guests = guests
	if storage != "" && node != "" {
		var volumes []Volume
		if err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/storage/"+url.PathEscape(storage)+"/content", &volumes); err != nil {
			return inv, err
		}
		inv.Volumes = volumes
	}
	return inv, nil
}

// Description reads one guest's configured description, which is where the
// ownership record is written. It is only asked for a guest already found in
// the range, because it is one call per guest.
func (c *Cluster) Description(ctx context.Context, node string, vmid int) (string, error) {
	var cfg struct {
		Description string `json:"description"`
		Tags        string `json:"tags"`
	}
	path := "/nodes/" + url.PathEscape(node) + "/qemu/" + strconv.Itoa(vmid) + "/config"
	if err := c.get(ctx, path, &cfg); err != nil {
		return "", err
	}
	return cfg.Description, nil
}

// ---------------------------------------------------------------------------
// The reconciliation
// ---------------------------------------------------------------------------

// Verdict is what one resource on the cluster turned out to be.
type Verdict string

const (
	// Accounted: a machine this run created and confirmed deleted, or a guest
	// that was in the range before the run and is not ours.
	Accounted Verdict = "accounted"
	// LeftBehind: ours, and the run that made it never removed it. Its ledger
	// names it, so the run knew about it -- this is litter, not a mystery.
	LeftBehind Verdict = "left_behind"
	// BelievedDeleted: ours, a ledger says it was deleted, and it is still
	// there. This is the worst of the three: the fleet's own accounting is
	// wrong, which is what the machine row exists to prevent.
	BelievedDeleted Verdict = "believed_deleted"
	// Unexplained: wearing this fleet's marks and named by no ledger at all.
	Unexplained Verdict = "unexplained"
	// Foreign: inside the range, not ours, and not there when the run began.
	// Reported rather than failed: the range was supposed to be reserved, and
	// somebody else using it is a finding for a person.
	Foreign Verdict = "foreign"
)

// Finding is one resource and what became of it.
type Finding struct {
	Verdict Verdict `json:"verdict"`
	VMID    int     `json:"vmid"`
	Node    string  `json:"node,omitempty"`
	Name    string  `json:"name,omitempty"`
	Detail  string  `json:"detail"`
	// RunID is the ledger that explains this resource, when one does.
	RunID string `json:"run_id,omitempty"`
}

// Reconciliation is the closing count, and the thing that decides
// qualification. Its one rule: no owned resource may be left unexplained.
type Reconciliation struct {
	TakenAt time.Time `json:"taken_at"`
	Range   Range     `json:"range"`
	Storage string    `json:"storage"`

	VMsAtStart  int `json:"vms_in_range_at_start"`
	VMsAtEnd    int `json:"vms_in_range_at_end"`
	Created     int `json:"machines_created"`
	Confirmed   int `json:"machines_confirmed_deleted"`
	DisksLeft   int `json:"disks_left_on_storage"`
	Unexplained int `json:"unexplained_owned_resources"`

	Findings []Finding `json:"findings"`
}

// OK is the whole verdict. An orphan found and deliberately left is a finding a
// person records; an orphan nobody noticed is a failure of this procedure
// rather than of the code, so anything owned and unaccounted for fails.
func (r Reconciliation) OK() bool { return r.Unexplained == 0 && r.DisksLeft == 0 }

// Reconcile accounts for every resource in the range against what the ledgers
// say was created.
//
// It is pure so that it can be tested without a cluster, which matters more
// than it sounds: this is the step that decides whether the qualification
// passed, and it would otherwise be the only step nobody could exercise until
// somebody had a hypervisor.
func Reconcile(inv Inventory, ledgers []LedgerRecord, r Range) Reconciliation {
	out := Reconciliation{TakenAt: inv.TakenAt, Range: r, Storage: inv.Storage}

	// What the runs say they did, keyed by the identifier the cluster uses.
	type claim struct {
		runID   string
		removed bool
		name    string
	}
	claims := map[int]claim{}
	preexisting := map[int]bool{}
	for _, led := range ledgers {
		for _, v := range led.PreexistingVMIDs {
			preexisting[v] = true
		}
		if led.Range.Min != 0 || led.Range.Max != 0 {
			out.VMsAtStart = len(led.PreexistingVMIDs)
		}
		for _, res := range led.Resources {
			if res.Kind != KindMachine || res.VMID == 0 {
				continue
			}
			out.Created++
			if res.RemovedAt != nil {
				out.Confirmed++
			}
			claims[res.VMID] = claim{runID: led.RunID, removed: res.RemovedAt != nil, name: res.Name}
		}
	}

	live := map[int]bool{}
	for _, g := range inv.InRange(r) {
		out.VMsAtEnd++
		live[g.VMID] = true
		c, claimed := claims[g.VMID]
		switch {
		case claimed && c.removed:
			out.Unexplained++
			out.Findings = append(out.Findings, Finding{
				Verdict: BelievedDeleted, VMID: g.VMID, Node: g.Node, Name: g.Name, RunID: c.runID,
				Detail: "the run recorded this machine as deleted and the guest is still on the cluster; " +
					"the fleet's accounting disagrees with the hypervisor",
			})
		case claimed:
			out.Unexplained++
			out.Findings = append(out.Findings, Finding{
				Verdict: LeftBehind, VMID: g.VMID, Node: g.Node, Name: g.Name, RunID: c.runID,
				Detail: "created by this run and never removed; delete it by hand and record why the run did not",
			})
		case g.Ours():
			out.Unexplained++
			out.Findings = append(out.Findings, Finding{
				Verdict: Unexplained, VMID: g.VMID, Node: g.Node, Name: g.Name,
				Detail: "wears this fleet's marks and no ledger names it; it belongs to another controller, " +
					"or to a run whose ledger was lost",
			})
		case preexisting[g.VMID]:
			out.Findings = append(out.Findings, Finding{
				Verdict: Accounted, VMID: g.VMID, Node: g.Node, Name: g.Name,
				Detail: "was in the range before the run began and is not ours",
			})
		default:
			out.Findings = append(out.Findings, Finding{
				Verdict: Foreign, VMID: g.VMID, Node: g.Node, Name: g.Name,
				Detail: "appeared in the reserved range during the run and is not ours; " +
					"the range was supposed to be reserved for this and nothing else",
			})
		}
	}

	// A guest a ledger claims, which the cluster no longer has, is the good
	// case and needs no row of its own. Its disks are another matter.
	for _, v := range inv.Volumes {
		if !r.contains(v.VMID) || live[v.VMID] {
			continue
		}
		c, claimed := claims[v.VMID]
		if !claimed {
			continue
		}
		out.DisksLeft++
		out.Findings = append(out.Findings, Finding{
			Verdict: LeftBehind, VMID: v.VMID, Name: v.VolID, RunID: c.runID,
			Detail: "the guest is gone and this disk image is still on " + inv.Storage +
				"; a destroy that did not take its disks costs storage rather than CPU, which is why nobody notices it",
		})
	}

	sort.SliceStable(out.Findings, func(i, j int) bool {
		if out.Findings[i].Verdict != out.Findings[j].Verdict {
			return out.Findings[i].Verdict < out.Findings[j].Verdict
		}
		return out.Findings[i].VMID < out.Findings[j].VMID
	})
	return out
}

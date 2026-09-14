// Command verify is the closing inventory check, on its own.
//
// It lists every VM in the configured VMID range and every disk on the
// configured storage, accounts for each one against the ledgers the
// qualification harness wrote, and exits non-zero if any owned resource is left
// unexplained.
//
// It exists separately from the harness for two reasons. The first is that the
// harness can die -- killed, timed out, or failing early -- and the question
// "did it leave anything on the cluster" then has to be answerable by something
// that is still alive; the ledgers outlive the run precisely so that this can
// read them. The second is that a qualification is a claim about a cluster, and
// a claim nobody can re-check an hour later is not much of one.
//
// It reads. It never deletes: a resource nobody can account for is a finding for
// a person, and a tool that tidied one away would destroy the evidence that the
// procedure exists to produce.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eyupio/zoomies/test/e2e/proxmox"
)

func main() {
	var (
		endpoint = flag.String("url", os.Getenv("ZOOMIES_PROXMOX_URL"), "the cluster, e.g. https://pve.example.com:8006")
		token    = flag.String("token", os.Getenv("ZOOMIES_PROXMOX_TOKEN"), "user@realm!tokenid=secret")
		node     = flag.String("node", os.Getenv("ZOOMIES_PROXMOX_NODE"), "the node whose storage is listed")
		storage  = flag.String("storage", os.Getenv("ZOOMIES_PROXMOX_STORAGE"), "the storage the machines' disks live on")
		vmids    = flag.String("range", os.Getenv("ZOOMIES_PROXMOX_VMID_RANGE"), "the reserved VMID range, e.g. 9000-9099")
		caFile   = flag.String("ca", os.Getenv("ZOOMIES_PROXMOX_CA_PEM_FILE"), "the cluster's own CA certificate")
		insecure = flag.Bool("insecure", os.Getenv("ZOOMIES_PROXMOX_INSECURE") != "", "do not verify the cluster's certificate")
		ledgers  = flag.String("ledgers", proxmox.LedgerDir(), "directory holding the harness's ledgers")
		out      = flag.String("json", "", "also write the reconciliation to this file as JSON")
	)
	flag.Parse()

	if err := run(options{
		endpoint: *endpoint, token: *token, node: *node, storage: *storage,
		vmids: *vmids, caFile: *caFile, insecure: *insecure, ledgers: *ledgers, jsonOut: *out,
	}, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "proxmox-verify: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	endpoint, token, node, storage, vmids, caFile, ledgers, jsonOut string
	insecure                                                        bool
}

func run(o options, w io.Writer) error {
	missing := []string{}
	for _, f := range []struct{ flag, value string }{
		{"-url", o.endpoint}, {"-token", o.token}, {"-node", o.node},
		{"-storage", o.storage}, {"-range", o.vmids},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.flag)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("nothing to check: %s missing.\n"+
			"Each also reads its ZOOMIES_PROXMOX_* variable, so the same environment that ran the "+
			"qualification runs this", strings.Join(missing, ", "))
	}
	rng, err := parseRange(o.vmids)
	if err != nil {
		return err
	}

	cluster, err := proxmox.NewCluster(proxmox.ClusterOptions{
		Endpoint: o.endpoint, Token: o.token, CAPEMFile: o.caFile, Insecure: o.insecure,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	inv, err := cluster.Take(ctx, o.node, o.storage)
	if err != nil {
		return fmt.Errorf("listing the cluster: %w", err)
	}
	records, err := proxmox.ReadLedgers(o.ledgers)
	if err != nil {
		// A ledger that cannot be read is reported and the check goes on: the
		// guests are on the cluster whether or not the file describing them
		// parses, and stopping here would leave them unexamined.
		fmt.Fprintf(w, "warning: %v\n", err)
	}
	if len(records) == 0 {
		fmt.Fprintf(w, "no ledgers in %s: every owned guest in %s will be reported as unexplained, "+
			"because nothing claims it\n", o.ledgers, rng)
	}

	rec := proxmox.Reconcile(inv, records, rng)
	report(rec, w)
	if o.jsonOut != "" {
		raw, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding the reconciliation: %w", err)
		}
		if err := os.WriteFile(o.jsonOut, append(raw, '\n'), 0o644); err != nil {
			return fmt.Errorf("writing the reconciliation: %w", err)
		}
	}
	if !rec.OK() {
		return fmt.Errorf("%d unexplained owned resource(s) and %d disk(s) left on %s; "+
			"this cluster is not clean, and nothing here has deleted anything",
			rec.Unexplained, rec.DisksLeft, o.storage)
	}
	return nil
}

// report prints the reconciliation the way the qualification record wants it
// read: the counts first, then every resource that is not simply accounted for.
func report(rec proxmox.Reconciliation, w io.Writer) {
	fmt.Fprintf(w, "range %s on %s, taken %s\n", rec.Range, rec.Storage, rec.TakenAt.Format(time.RFC3339))
	fmt.Fprintf(w, "  %-34s %d\n", "VMs in the range at the start", rec.VMsAtStart)
	fmt.Fprintf(w, "  %-34s %d\n", "machines created during the runs", rec.Created)
	fmt.Fprintf(w, "  %-34s %d\n", "machines confirmed deleted", rec.Confirmed)
	fmt.Fprintf(w, "  %-34s %d\n", "VMs in the range at the end", rec.VMsAtEnd)
	fmt.Fprintf(w, "  %-34s %d\n", "disks left on the storage", rec.DisksLeft)
	fmt.Fprintf(w, "  %-34s %d\n", "unexplained owned resources", rec.Unexplained)
	for _, f := range rec.Findings {
		if f.Verdict == proxmox.Accounted {
			continue
		}
		fmt.Fprintf(w, "\n%s: VM %d", strings.ToUpper(string(f.Verdict)), f.VMID)
		if f.Node != "" {
			fmt.Fprintf(w, " on %s", f.Node)
		}
		if f.Name != "" {
			fmt.Fprintf(w, " (%s)", f.Name)
		}
		if f.RunID != "" {
			fmt.Fprintf(w, ", run %s", f.RunID)
		}
		fmt.Fprintf(w, "\n  %s\n", f.Detail)
	}
	if rec.OK() {
		fmt.Fprintln(w, "\nevery owned resource in the range is accounted for")
	}
}

// parseRange reads "9000-9099". It is duplicated from the harness rather than
// shared, because this command has to keep working when the harness's build tag
// does not apply -- and because a range read two different ways is a bug worth
// having a test for on both sides.
func parseRange(raw string) (proxmox.Range, error) {
	lo, hi, ok := strings.Cut(strings.TrimSpace(raw), "-")
	if !ok {
		return proxmox.Range{}, fmt.Errorf("%q is not a range; write it as 9000-9099", raw)
	}
	var r proxmox.Range
	if _, err := fmt.Sscanf(strings.TrimSpace(lo), "%d", &r.Min); err != nil {
		return proxmox.Range{}, fmt.Errorf("%q does not begin with a number", raw)
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(hi), "%d", &r.Max); err != nil {
		return proxmox.Range{}, fmt.Errorf("%q does not end with a number", raw)
	}
	if r.Min <= 0 || r.Max < r.Min {
		return proxmox.Range{}, fmt.Errorf("%q is not a usable range of VMIDs", raw)
	}
	return r, nil
}

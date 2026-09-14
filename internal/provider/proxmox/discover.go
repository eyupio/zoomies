package proxmox

// Discovery: what this credential can actually see, for the guided form.
//
// A form that asks somebody to type a node name, a storage name, a bridge and a
// VMID is a form four operators in five get wrong once, and every one of those
// mistakes is only discovered when the first machine fails to build. Asking the
// cluster instead turns all four into menus -- and each option carries the
// consequence of picking it, because a list of identifiers with no consequences
// is a list somebody guesses at.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/provider"
)

// Discover lists the nodes, storages, bridges and templates this token can see.
//
// A node that will not answer is skipped rather than fatal: a cluster with one
// node down still has a form to fill in, and the storages and bridges the other
// nodes offer are still the right answers. Only a discovery that found nothing
// at all is reported as an error, because an empty menu with no explanation
// sends an operator looking for a bug in the wizard.
func Discover(ctx context.Context, c *Client) (provider.Discovery, error) {
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return provider.Discovery{}, err
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Node < nodes[j].Node })

	var d provider.Discovery
	var online []string
	for _, n := range nodes {
		d.Nodes = append(d.Nodes, provider.Choice{
			Value: n.Node, Label: n.Node, Consequence: describeNode(n),
		})
		if n.Online() {
			online = append(online, n.Node)
		}
	}

	storages := map[string]*seenStorage{}
	bridges := map[string]*seenBridge{}
	var firstErr error
	var answered int
	for _, node := range online {
		nodeErr := false
		if found, err := c.Storages(ctx, node); err != nil {
			nodeErr = true
			if firstErr == nil {
				firstErr = err
			}
		} else {
			for _, s := range found {
				// Only storages that can hold a disk image are offered: the
				// others cannot take a clone at all, and listing them invites
				// the one mistake this menu exists to prevent.
				if !s.Accepts("images") || !bool(s.Enabled) {
					continue
				}
				seen, ok := storages[s.Storage]
				if !ok {
					seen = &seenStorage{storage: s}
					storages[s.Storage] = seen
				}
				seen.nodes = append(seen.nodes, node)
				if int64(s.Avail) > int64(seen.storage.Avail) {
					seen.storage.Avail = s.Avail
				}
			}
		}
		if found, err := c.Bridges(ctx, node); err != nil {
			nodeErr = true
			if firstErr == nil {
				firstErr = err
			}
		} else {
			for _, b := range found {
				seen, ok := bridges[b.Iface]
				if !ok {
					seen = &seenBridge{iface: b}
					bridges[b.Iface] = seen
				}
				seen.nodes = append(seen.nodes, node)
			}
		}
		if !nodeErr {
			answered++
		}
	}

	// One call answers for the whole cluster, so templates cost nothing extra
	// however many nodes there are.
	guests, err := c.ClusterVMs(ctx)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	if err == nil {
		d.Templates = describeTemplates(guests)
	}

	if answered == 0 && len(online) > 0 && firstErr != nil {
		return provider.Discovery{}, firstErr
	}
	d.Storages = describeStorages(storages, len(online))
	d.Bridges = describeBridges(bridges, len(online))
	return d, nil
}

type seenStorage struct {
	storage Storage
	nodes   []string
}

type seenBridge struct {
	iface NetworkInterface
	nodes []string
}

func describeNode(n Node) string {
	if !n.Online() {
		return fmt.Sprintf("%s -- machines placed here will not be built until it comes back", n.Status)
	}
	parts := []string{"online"}
	if n.MaxCPU > 0 {
		parts = append(parts, fmt.Sprintf("%d processors", n.MaxCPU.Int()))
	}
	if n.MaxMem > 0 {
		parts = append(parts, gibibytes(n.MaxMem)+" of memory")
	}
	return strings.Join(parts, ", ")
}

func describeStorages(seen map[string]*seenStorage, nodes int) []provider.Choice {
	out := make([]provider.Choice, 0, len(seen))
	for name, s := range seen {
		var parts []string
		switch {
		case bool(s.storage.Shared):
			parts = append(parts, "shared across the cluster")
		case len(s.nodes) < nodes:
			// The trap this menu exists to name: a local storage looks fine
			// until a machine is placed on a node that cannot see it.
			parts = append(parts, "only on "+join(s.nodes)+" -- a machine on another node cannot use it")
		}
		if s.storage.Avail > 0 {
			parts = append(parts, gibibytes(s.storage.Avail)+" free")
		}
		if s.storage.Type != "" {
			parts = append(parts, s.storage.Type)
		}
		out = append(out, provider.Choice{Value: name, Label: name, Consequence: strings.Join(parts, ", ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

func describeBridges(seen map[string]*seenBridge, nodes int) []provider.Choice {
	out := make([]provider.Choice, 0, len(seen))
	for name, b := range seen {
		var parts []string
		if len(b.nodes) < nodes {
			parts = append(parts, "only on "+join(b.nodes)+" -- a machine on another node would have no network")
		} else {
			parts = append(parts, "on every node")
		}
		if c := strings.TrimSpace(b.iface.Comments); c != "" {
			parts = append(parts, c)
		} else if b.iface.CIDR != "" {
			parts = append(parts, b.iface.CIDR)
		}
		out = append(out, provider.Choice{Value: name, Label: name, Consequence: strings.Join(parts, ", ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

func describeTemplates(guests []ClusterVM) []provider.Choice {
	var out []provider.Choice
	for _, g := range guests {
		// Only QEMU templates: a container template is not something this
		// provider can clone into a machine that runs an agent.
		if g.Type != "qemu" || !bool(g.Template) {
			continue
		}
		label := g.Name
		if label == "" {
			label = "VMID " + strconv.Itoa(g.VMID.Int())
		}
		out = append(out, provider.Choice{
			Value:       strconv.Itoa(g.VMID.Int()),
			Label:       label,
			Consequence: fmt.Sprintf("VMID %d on %s", g.VMID.Int(), g.Node),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

// gibibytes renders a byte count the way the console does, because an operator
// comparing a menu against the Proxmox UI should not have to convert.
func gibibytes(n Int) string {
	const gib = 1 << 30
	if n <= 0 {
		return "0 GiB"
	}
	return fmt.Sprintf("%.0f GiB", float64(n)/gib)
}

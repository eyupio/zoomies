package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// runHosts is `zoomies hosts ...`, including the join-token subcommand an
// operator uses to add a machine to the fleet.
func runHosts(ctx context.Context, e *env, args []string) error {
	return runGroup(ctx, e, "hosts", "Agents, their capacity, and enrolment.", []*subcommand{
		{"list", "", "Every host, with health and free capacity", hostsList},
		{"cordon", "<host-id>", "Keep its runners, accept no new ones", hostsCordon},
		{"drain", "<host-id>", "Cordon it, then drain every runner on it", hostsDrain},
		{"uncordon", "<host-id>", "Let it accept new runners again", hostsUncordon},
		{"delete", "<host-id>", "Forget a host; refused while it still has runners", hostsDelete},
		{"join-token", "create", "Mint a token that enrols a new host", hostsJoinToken},
	}, args)
}

func hostsList(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies hosts list", "List the hosts that have joined this controller.")
	cf := registerClientFlags(fs, true)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}

	var out listResponse[hostItem]
	raw, err := client.get(ctx, "/hosts", nil, &out)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}
	if len(out.Items) == 0 {
		p.note("No hosts. Add one with: zoomies hosts join-token create")
		return nil
	}

	rows := make([][]string, 0, len(out.Items))
	for _, h := range out.Items {
		health := p.paint(colourGreen, "healthy")
		if !h.Healthy {
			health = p.paint(colourRed, "unreachable")
		}
		if h.Cordoned {
			health = p.paint(colourYellow, "cordoned")
		}
		used := 0.0
		if h.Capacity > 0 {
			used = float64(h.ActiveRunners) / float64(h.Capacity)
		}
		rows = append(rows, []string{
			h.Name,
			h.ID,
			health,
			fmt.Sprintf("%d/%d", h.ActiveRunners, h.Capacity),
			p.bar(used, 10),
			dash(strings.Join(h.Backends, ",")),
			dash(hostPlatform(h)),
			dash(hostSize(h)),
			p.relTime(h.LastHeartbeat),
		})
	}
	p.table([]string{"name", "id", "state", "runners", "used", "backends", "platform", "size", "last seen"}, rows)

	// A host with no usable backend is connected, healthy and completely
	// useless: no pool matches it, so its jobs queue with nothing to say why.
	// The agent already explained it -- repeat that here rather than leaving a
	// dash in the backends column.
	for _, h := range out.Items {
		if len(h.Backends) > 0 {
			continue
		}
		p.note("%s can run nothing, so no pool will be scheduled on it.", h.Name)
		for _, b := range h.BackendInfo {
			if !b.Available && b.Detail != "" {
				p.note("  %s: %s", b.Kind, b.Detail)
			}
		}
	}
	return nil
}

// hostPlatform is what this machine is, in the terms a pool asks in. The
// controller sends the sentence already rendered; the kernel and architecture
// are the fallback for an agent too old to report a distribution.
func hostPlatform(h hostItem) string {
	if h.PlatformLabel != "" {
		return h.PlatformLabel
	}
	return strings.Trim(h.OS+"/"+h.Arch, "/")
}

// hostSize is how much machine it is, which is the other half of the answer to
// "why is this host full".
func hostSize(h hostItem) string {
	if h.CPUs <= 0 {
		return ""
	}
	out := fmt.Sprintf("%d vCPU", h.CPUs)
	if h.MemoryMB > 0 {
		out += fmt.Sprintf(", %d GB", h.MemoryMB/1024)
	}
	return out
}

func hostsCordon(ctx context.Context, e *env, args []string) error {
	return hostsSetCordon(ctx, e, args, true)
}

func hostsUncordon(ctx context.Context, e *env, args []string) error {
	return hostsSetCordon(ctx, e, args, false)
}

func hostsSetCordon(ctx context.Context, e *env, args []string, cordoned bool) error {
	verb, summary := "cordon", "Stop scheduling new runners onto a host. The ones it already has keep running."
	if !cordoned {
		verb, summary = "uncordon", "Let a host accept new runners again."
	}
	fs := newFlagSet(e, "zoomies hosts "+verb+" <host-id>", summary)
	cf := registerClientFlags(fs, false)
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a host ID, as shown by `zoomies hosts list`")
	if err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	var h hostItem
	body := map[string]any{"cordoned": cordoned}
	if _, err := client.post(ctx, "/hosts/"+url.PathEscape(id)+"/cordon", nil, body, &h); err != nil {
		return err
	}
	if cordoned {
		fmt.Fprintf(e.out, "Cordoned %s; its %d existing runner(s) are untouched.\n", h.Name, h.ActiveRunners)
	} else {
		fmt.Fprintf(e.out, "Uncordoned %s; it can take %d more runner(s).\n", h.Name, h.Free)
	}
	return nil
}

// hostsDrain empties a host: cordon it, then drain what it is already running.
//
// It exists because the two halves are always done together and doing them in
// the wrong order does not work. Draining first leaves the host uncordoned, so
// the scheduler puts a fresh runner on it while the old ones are still
// finishing, and an operator watching the count go down and back up concludes
// the drain failed. This is the composition, in the order that empties a host:
// stop new work arriving, then let what is here finish.
//
// It does not force, but nor is it free: a drained runner is given five minutes
// to finish the job it is on and is then stopped, which is what makes the host
// actually empty rather than waiting on the longest job somebody happened to
// start. An operator who wants the machine gone immediately has
// `runners delete --force`, and having to type that separately is still the
// point -- the difference is now five minutes rather than for ever.
func hostsDrain(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies hosts drain <host-id>",
		"Cordon a host and drain every runner on it, so it empties as its jobs finish.")
	cf := registerClientFlags(fs, false)
	fs.example("zoomies hosts drain hst_k3f9qz2m")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a host ID, as shown by `zoomies hosts list`")
	if err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	// Cordon first, and separately, so that a failure here stops the whole
	// thing: draining a host that will immediately be given new runners is
	// worse than doing nothing, because it looks like it worked.
	var h hostItem
	if _, err := client.post(ctx, "/hosts/"+url.PathEscape(id)+"/cordon", nil,
		map[string]any{"cordoned": true}, &h); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Cordoned %s.\n", dash(h.Name))

	q := url.Values{}
	q.Set("host_id", id)
	q.Set("limit", "500")
	var runners listResponse[runnerItem]
	if _, err := client.get(ctx, "/runners", q, &runners); err != nil {
		return err
	}
	var ids []string
	for _, r := range runners.Items {
		ids = append(ids, r.ID)
	}
	if len(ids) == 0 {
		fmt.Fprintf(e.out, "It had no runners, so it is already empty.\n")
		return nil
	}
	if runners.Total > len(ids) {
		// The page is 500 and the API's ceiling is the same, so a host with
		// more than that needs a second pass. Saying so beats draining most
		// of them and reporting success.
		fmt.Fprintf(e.out, "It has %d runners and this drains the first %d; run it again for the rest.\n",
			runners.Total, len(ids))
	}
	fmt.Fprintf(e.out, "Draining %s:\n", countOf(len(ids), "runner"))
	return bulkRunners(ctx, e, client, "drain", ids, false)
}

func hostsDelete(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies hosts delete <host-id> [--force]",
		"Forget a host. Refused while it still has live runners, unless forced.")
	cf := registerClientFlags(fs, false)
	force := fs.Bool("force", false, "delete even though runners are still on it; their registrations become orphans")
	if err := fs.parse(args); err != nil {
		return err
	}
	id, err := fs.oneArg("a host ID")
	if err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	q := url.Values{}
	if *force {
		q.Set("force", "true")
	}
	if _, err := client.del(ctx, "/hosts/"+url.PathEscape(id), q, nil); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "Deleted host %s.\n", id)
	return nil
}

// hostsJoinToken is `zoomies hosts join-token create`. It is nested one deeper
// than the rest because minting a credential deserves to be typed out in full.
func hostsJoinToken(ctx context.Context, e *env, args []string) error {
	return runGroup(ctx, e, "hosts join-token", "Tokens that enrol a new host.", []*subcommand{
		{"create", "", "Mint a single-use join token and print the command to run on the new host", joinTokenCreate},
	}, args)
}

func joinTokenCreate(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies hosts join-token create [--ttl 15m]",
		"Mint a single-use join token. The token is shown once; only its hash is stored.")
	cf := registerClientFlags(fs, true)
	ttl := fs.Duration("ttl", 15*time.Minute, "how long the token may be redeemed for")
	capacity := fs.Int("capacity", 2, "the capacity the new host starts with; 0 lets the agent decide from its CPU count")
	labels := kvValue{}
	fs.Var(labels, "labels", "labels for the new host, e.g. arch=arm64")
	connection := fs.String("connection", "direct", "how the host connects: direct or tailcat (private encrypted connection)")
	controllerURL := fs.String("controller", "", "the address the new host should join on, when it is not server.external_url")
	fs.example(
		"zoomies hosts join-token create",
		"zoomies hosts join-token create --ttl 1h --capacity 8 --labels arch=arm64",
		"zoomies hosts join-token create --controller https://zoomies.internal:8443",
		"zoomies hosts join-token create --connection tailcat",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}

	body := map[string]any{"ttl": ttl.String(), "capacity": *capacity, "connection": *connection}
	if len(labels) > 0 {
		body["labels"] = map[string]string(labels)
	}
	if u := strings.TrimSpace(*controllerURL); u != "" {
		body["controller_url"] = u
	}
	var token joinTokenItem
	raw, err := client.post(ctx, "/join-tokens", nil, body, &token)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}

	fmt.Fprintf(e.out, "Run this on the new host, within %s:\n\n  %s\n\n",
		compactDuration(time.Until(token.ExpiresAt)), token.Command)
	fmt.Fprintf(e.out, "token     %s\n", token.Token)
	fmt.Fprintf(e.out, "expires   %s\n", token.ExpiresAt.Local().Format(time.RFC3339))
	fmt.Fprintf(e.out, "capacity  %s\n", strconv.Itoa(token.Capacity))
	fmt.Fprintln(e.out, "\nThis is the only time the token is shown. It may be redeemed once.")
	return nil
}

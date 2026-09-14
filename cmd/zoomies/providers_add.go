package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/eyupio/zoomies/internal/config"
)

// providerSpec holds the flags `providers add` and `providers edit` share.
// Keeping them in one place is what makes `edit` accept exactly the settings
// `add` does, as the pool commands do.
//
// The wizard asks its questions over five steps. Here they are one line, and
// the line is the same shape as `hosts join-token create`: what to connect to,
// what to build, what it may spend. The answers the wizard discovers from the
// cluster -- nodes, storages, bridges, templates -- have to be typed, because
// discovery needs a saved credential and this command has not saved one yet.
// `zoomies providers check` says afterwards which of them the cluster refuses.
type providerSpec struct {
	name        *string
	endpoint    *string
	tokenFile   *string
	caFile      *string
	insecure    *bool
	gateway     *string
	nodes       *listValue
	template    *string
	storage     *string
	bridge      *string
	vmidRange   *string
	settings    kvValue
	labels      kvValue
	capacity    *int
	backend     *string
	cpus        *float64
	memoryMB    *int64
	diskMB      *int64
	pools       kvValue
	maxMachines *int
	maxInFlight *int
	idleTimeout *string
	costPerHour *float64
	enabled     *bool
}

// registerProviderFlags declares them, with the API's own defaults so that a
// provider created here is the same as one the wizard made.
func registerProviderFlags(fs *flagSet) *providerSpec {
	spec := &providerSpec{
		nodes:    &listValue{},
		settings: kvValue{},
		labels:   kvValue{},
		pools:    kvValue{},
	}
	spec.name = fs.String("name", "", "the provider's name, as the machines page and the audit log will call it, e.g. proxmox-lab")
	spec.endpoint = fs.String("endpoint", "", "where the controller reaches it, e.g. https://pve.example.com:8006")
	spec.tokenFile = fs.String("credential-file", "", "a file holding the API token (user@realm!tokenid=secret); otherwise it is asked for, or read from standard input when that is not a terminal")
	spec.caFile = fs.String("endpoint-ca-file", "", "the CA certificate to verify the endpoint against, e.g. pve-root-ca.pem, instead of turning verification off")
	spec.insecure = fs.Bool("endpoint-insecure", false, "skip certificate verification of the endpoint; the token then crosses to anybody in the middle, and the provider warns for as long as this is on")
	spec.gateway = fs.String("gateway", "", "the address `zoomies gateway` printed, for a provider on a network the controller cannot reach")
	fs.Var(spec.nodes, "nodes", "the cluster nodes machines may be built on (repeatable, or comma-separated)")
	spec.template = fs.String("template", "", "the VMID of the prepared template every machine is cloned from")
	spec.storage = fs.String("storage", "", "the storage a clone's disk lands on")
	spec.bridge = fs.String("bridge", "", "the network bridge a machine attaches to (default vmbr0)")
	spec.vmidRange = fs.String("vmid-range", "", "the block of VM identifiers only Zoomies allocates from, e.g. 9000-9099 (the default)")
	fs.Var(spec.settings, "setting", "any other driver setting, as key=value; `zoomies providers kinds` lists them")
	fs.Var(spec.labels, "labels", "labels every machine reports as a host, e.g. arch=amd64")
	spec.capacity = fs.Int("capacity", 2, "how many runners one machine may run at once")
	spec.backend = fs.String("backend", "docker", "the runner backend the machines offer: docker, podman or process")
	spec.cpus = fs.Float64("cpus", 0, "CPUs per machine; 0 keeps the template's")
	spec.memoryMB = fs.Int64("memory-mb", 0, "memory per machine, in MiB; 0 keeps the template's")
	spec.diskMB = fs.Int64("disk-mb", 0, "disk per machine, in MiB; 0 keeps the template's")
	fs.Var(spec.pools, "pool-selector", "only buy for pools whose labels match, e.g. tier=large; empty means every pool")
	spec.maxMachines = fs.Int("max-machines", 0, "the most machines this provider may own at once; 0 rents nothing")
	spec.maxInFlight = fs.Int("max-in-flight", 1, "how many machines may be being built at once")
	spec.idleTimeout = fs.String("idle-timeout", "15m", "how long a machine may sit idle before it is drained and deleted")
	spec.costPerHour = fs.Float64("cost-per-hour", 0, "what one machine costs an hour, for the page to add up")
	spec.enabled = fs.Bool("enabled", true, "whether the provider may buy machines at all")
	return spec
}

// body renders the flags as the request the API takes. On a create every flag
// is sent so the row carries the documented defaults; on an edit only the
// flags the operator typed are, because a PATCH that resent the defaults would
// quietly reset the settings they did not name.
//
// The driver's settings come back separately: a create sends them as they
// are, and an edit merges them over the row's, because the API replaces the
// whole map and an edit that named one key would otherwise drop the rest.
func (spec *providerSpec) body(e *env, fs *flagSet, create bool) (body map[string]any, settings map[string]string, err error) {
	body = map[string]any{}
	want := func(flag string) bool { return create || fs.changed(flag) }

	if want("name") {
		body["name"] = strings.TrimSpace(*spec.name)
	}
	if want("endpoint") {
		body["endpoint"] = strings.TrimSpace(*spec.endpoint)
	}
	if fs.changed("endpoint-ca-file") {
		pem, err := os.ReadFile(*spec.caFile)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the CA certificate: %w", err)
		}
		body["ca_pem"] = string(pem)
	}
	if want("endpoint-insecure") {
		body["insecure_skip_verify"] = *spec.insecure
	}
	if fs.changed("gateway") {
		if addr := strings.TrimSpace(*spec.gateway); addr != "" {
			body["connection"] = "tailcat"
			body["tailcat_address"] = addr
		} else {
			body["connection"] = "direct"
		}
	} else if create {
		body["connection"] = "direct"
	}

	// The driver's settings. The named flags are the Proxmox form's required
	// questions spelled as flags; --setting reaches everything else, and a
	// --setting naming one of these keys is the same answer given twice.
	settings = map[string]string{}
	for k, v := range spec.settings {
		settings[k] = v
	}
	if len(*spec.nodes) > 0 {
		settings["nodes"] = strings.Join(*spec.nodes, ",")
	}
	if fs.changed("template") {
		settings["template_id"] = strings.TrimSpace(*spec.template)
	}
	if fs.changed("storage") {
		settings["storage"] = strings.TrimSpace(*spec.storage)
	}
	if fs.changed("bridge") {
		settings["bridge"] = strings.TrimSpace(*spec.bridge)
	} else if create {
		settings["bridge"] = "vmbr0"
	}
	if fs.changed("vmid-range") || create {
		lo, hi, err := parseVMIDRange(*spec.vmidRange)
		if err != nil {
			return nil, nil, err
		}
		settings["vmid_min"], settings["vmid_max"] = lo, hi
	}
	if create {
		body["settings"] = settings
	}

	if create || len(spec.labels) > 0 {
		body["machine_labels"] = map[string]string(spec.labels)
	}
	if want("capacity") {
		body["machine_capacity"] = *spec.capacity
	}
	if want("backend") {
		body["machine_backend"] = strings.TrimSpace(*spec.backend)
	}
	if want("cpus") {
		body["machine_cpus"] = *spec.cpus
	}
	if want("memory-mb") {
		body["machine_memory_mb"] = *spec.memoryMB
	}
	if want("disk-mb") {
		body["machine_disk_mb"] = *spec.diskMB
	}
	if create || len(spec.pools) > 0 {
		body["pool_selector"] = map[string]string(spec.pools)
	}
	if want("max-machines") {
		body["max_machines"] = *spec.maxMachines
	}
	if want("max-in-flight") {
		body["max_creates_in_flight"] = *spec.maxInFlight
	}
	if want("idle-timeout") {
		body["idle_timeout"] = strings.TrimSpace(*spec.idleTimeout)
	}
	if want("cost-per-hour") {
		body["cost_per_machine_hour"] = *spec.costPerHour
	}
	if want("enabled") {
		body["enabled"] = *spec.enabled
	}

	// The credential last, and only when asked for: an edit that names no
	// token keeps the one that is sealed.
	if fs.changed("credential-file") {
		raw, err := os.ReadFile(*spec.tokenFile)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the API token: %w", err)
		}
		body["credential"] = strings.TrimSpace(string(raw))
	} else if create {
		token, err := readSecret(e, "API token (user@realm!tokenid=secret): ")
		if err != nil {
			return nil, nil, err
		}
		if token == "" {
			return nil, nil, fmt.Errorf("a provider needs an API token; paste it when asked, pipe it on standard input, or give --credential-file")
		}
		body["credential"] = token
	}
	return body, settings, nil
}

// parseVMIDRange reads "9000-9099" into the two settings the driver keeps,
// because one flag for a range is what people type and two numbers that can
// disagree is what the row stores.
func parseVMIDRange(s string) (lo, hi string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "9000", "9099", nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return "", "", fmt.Errorf("--vmid-range %q is not a range; write it as 9000-9099", s)
	}
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	lo64, errA := strconv.Atoi(a)
	hi64, errB := strconv.Atoi(b)
	switch {
	case errA != nil || errB != nil:
		return "", "", fmt.Errorf("--vmid-range %q is not two numbers; write it as 9000-9099", s)
	case lo64 > hi64:
		return "", "", fmt.Errorf("--vmid-range %q runs backwards; the lower VMID comes first", s)
	}
	return a, b, nil
}

// providersAdd is `zoomies providers add <kind>`: the wizard in one command.
//
// It runs the same dry run the wizard's review step does before it writes
// anything, so a typo is refused with a field name and no row; then it
// creates the provider; then it runs the live preflight and prints what the
// cluster would refuse, in the same words the page uses. A provider is
// created renting nothing unless --max-machines says otherwise, for the
// reason the API gives: a connection test should not start an invoice.
func providersAdd(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies providers add proxmox --name <n> --endpoint <url> --nodes <n> --template <vmid> --storage <s> [flags]",
		"Add a provider from the terminal: validate the answers, save them, and ask the cluster what it would refuse.")
	cf := registerClientFlags(fs, true)
	spec := registerProviderFlags(fs)
	noCheck := fs.Bool("no-check", false, "save the provider without asking the cluster anything; `zoomies providers check` does that later")
	fs.example(
		"zoomies providers add proxmox --name proxmox-lab --endpoint https://pve.example.com:8006 \\\n"+
			"      --nodes pve1 --template 9000 --storage local-lvm --bridge vmbr0 \\\n"+
			"      --endpoint-ca-file pve-root-ca.pem --max-machines 4",
		"zoomies providers add proxmox --name lab --endpoint https://pve.home:8006 --nodes pve1,pve2 \\\n"+
			"      --template 9000 --storage ceph --vmid-range 9100-9199 --credential-file token.txt \\\n"+
			"      --gateway <address zoomies gateway printed>",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	kind, err := fs.oneArg("a provider kind, e.g. proxmox; `zoomies providers kinds` lists them")
	if err != nil {
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

	body, _, err := spec.body(e, fs, true)
	if err != nil {
		return err
	}
	body["kind"] = strings.ToLower(strings.TrimSpace(kind))

	// The dry run first. It is what refuses a bad answer before a row exists
	// and, unlike the POST, it lists every field that is wrong at once.
	var verdict providerValidation
	if _, err := client.post(ctx, "/providers/validate", nil, body, &verdict); err != nil {
		return err
	}
	if !verdict.Valid {
		for _, f := range verdict.Errors {
			fmt.Fprintf(e.err, "  %s: %s\n", f.Field, f.Message)
		}
		return fmt.Errorf("this provider cannot be created as described; nothing was saved. Fix what is above and run it again")
	}

	var created providerItem
	raw, err := client.post(ctx, "/providers", nil, body, &created)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}
	p.note("Created %s (%s), a %s provider at %s.", created.Name, created.ID, created.Kind, dash(created.Endpoint))
	printFindings(e.out, verdict.Warnings)

	if !*noCheck {
		p.note("")
		p.note("Asking %s what it would refuse...", created.Name)
		if err := checkProvider(ctx, e, p, client, created); err != nil {
			return err
		}
	}

	p.note("")
	if created.MaxMachines == 0 {
		p.note("It rents nothing yet: raise the ceiling when you are ready with\n  zoomies providers edit %s --max-machines 4", created.Name)
	} else {
		p.note("It may own up to %d machines at once. `zoomies providers pause %s` stops it buying more.", created.MaxMachines, created.Name)
	}
	return nil
}

// providersEdit is `zoomies providers edit <name|id>`: change the settings you
// name and nothing else. It exists so that the ceiling `add` left at zero can
// be raised from the same terminal, rather than the one step that spends money
// being the one that needs a browser.
func providersEdit(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies providers edit <name|id> [flags]",
		"Change the settings you name on a provider and leave the rest alone.")
	cf := registerClientFlags(fs, true)
	spec := registerProviderFlags(fs)
	fs.example(
		"zoomies providers edit proxmox-lab --max-machines 8",
		"zoomies providers edit proxmox-lab --credential-file token.txt",
		"zoomies providers edit proxmox-lab --endpoint-ca-file pve-root-ca.pem --endpoint-insecure=false",
	)
	if err := fs.parse(args); err != nil {
		return err
	}
	ref, err := fs.oneArg("a provider name or ID")
	if err != nil {
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
	body, settings, err := spec.body(e, fs, false)
	if err != nil {
		return err
	}
	if len(body) == 0 && len(settings) == 0 {
		return usagef(fs.Name(), "nothing to change; name at least one setting, e.g. --max-machines 4")
	}
	provider, err := resolveProvider(ctx, client, ref)
	if err != nil {
		return err
	}
	if len(settings) > 0 {
		merged := map[string]string{}
		for k, v := range provider.Settings {
			merged[k] = v
		}
		for k, v := range settings {
			merged[k] = v
		}
		body["settings"] = merged
	}

	var out providerItem
	raw, err := client.patch(ctx, "/providers/"+url.PathEscape(provider.ID), nil, body, &out)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}
	p.note("Updated %s.", out.Name)
	if out.Held != "" {
		p.note("It is buying nothing right now: %s", out.Held)
	} else if out.MaxMachines > 0 {
		p.note("It may own up to %d machines at once.", out.MaxMachines)
	}
	return nil
}

// checkProvider runs the live preflight and prints it with printFindings, so
// the terminal and the wizard say the same words about the same provider. It
// returns an error when something found would stop a machine being created.
func checkProvider(ctx context.Context, e *env, p *printer, client *apiClient, provider providerItem) error {
	var report providerCheckItem
	if _, err := client.post(ctx, "/providers/"+url.PathEscape(provider.ID)+"/check", nil, nil, &report); err != nil {
		return err
	}
	if !report.Reachable {
		p.note("%s could not be reached at %s.", provider.Name, dash(provider.Endpoint))
	} else if report.Version != "" {
		p.note("%s answered: %s.", provider.Name, report.Version)
	}
	printFindings(e.out, report.Findings)
	if !report.OK {
		return fmt.Errorf("%s would refuse to create a machine as configured; fix what is above and run `zoomies providers check %s`", provider.Name, provider.Name)
	}
	if n := len(config.Findings(report.Findings).Warnings()); n > 0 {
		p.note("%s is usable, with %s above.", provider.Name, pluralWarnings(n))
		return nil
	}
	p.note("%s is usable and nothing above weakens the defaults.", provider.Name)
	return nil
}

// providersKinds is `zoomies providers kinds`: what this build can rent from,
// and the settings each driver asks for, so that --setting has a list to
// choose from without opening the wizard.
func providersKinds(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies providers kinds", "List the kinds of provider this build can add, and the settings each one asks for.")
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
	var out listResponse[providerKindItem]
	raw, err := client.get(ctx, "/providers/kinds", nil, &out)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}
	for i, k := range out.Items {
		if i > 0 {
			p.note("")
		}
		p.note("%s  (%s)", k.Kind, k.Label)
		rows := make([][]string, 0, len(k.Settings))
		for _, s := range k.Settings {
			need := "optional"
			if s.Required {
				need = "required"
			}
			rows = append(rows, []string{s.Key, s.Kind, need, dash(s.Default), s.Help})
		}
		p.table([]string{"setting", "type", "", "default", "what it is"}, rows)
	}
	return nil
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runDiagnostics fetches the support bundle and, by default, writes it to a
// file.
//
// It is deliberately thin. The document is the server's bytes written through
// unchanged, so a section added to the bundle tomorrow reaches a support case
// today without this being rebuilt; what the CLI adds is the file and a summary
// of what is in it, because the one question an operator has before attaching a
// bundle to a public issue is what they are about to hand over.
func runDiagnostics(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies diagnostics [--file zoomies-bundle.json]",
		"Collect a support bundle: this instance, its fleet and what is wrong, in one JSON document.")
	cf := registerClientFlags(fs, true)
	file := fs.String("file", "", "write the document here; \"-\" means stdout. The default names a file in the working directory")
	stdout := fs.Bool("stdout", false, "write the document to stdout instead of a file, the same as --file -")
	fs.example("zoomies diagnostics", "zoomies diagnostics --file /tmp/bundle.json", "zoomies diagnostics --stdout")
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

	var bundle bundleResponse
	raw, err := client.get(ctx, "/diagnostics/bundle", nil, &bundle)
	if err != nil {
		return err
	}

	// --output json or yaml is a pipe, and a pipe wants the document rather
	// than a file it would then have to find.
	if p.structured() {
		return p.emit(raw)
	}
	if *stdout || *file == "-" {
		_, err := e.out.Write(append(raw, '\n'))
		return err
	}

	path := *file
	if path == "" {
		path = bundleFileName(bundle.GeneratedAt)
	}
	// 0600 because a bundle carries the whole configuration and the fleet's
	// shape: it is not a secret, and it is nobody else's on a shared host.
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("writing the bundle to %s: %w", path, err)
	}
	if abs, aerr := filepath.Abs(path); aerr == nil {
		path = abs
	}
	printBundleSummary(p, &bundle, path, len(raw))
	return nil
}

// bundleFileName names the file after the instant the controller took the
// bundle, so two bundles from one afternoon are told apart by the thing that
// distinguishes them.
func bundleFileName(at time.Time) string {
	if at.IsZero() {
		return "zoomies-bundle.json"
	}
	return "zoomies-bundle-" + at.UTC().Format("20060102-150405") + ".json"
}

// printBundleSummary says what went into the file.
func printBundleSummary(p *printer, b *bundleResponse, path string, size int) {
	p.note("Wrote %s (%s).", path, humanBytes(size))
	p.note("")
	p.keyValues([][2]string{
		{"Instance", strings.TrimSpace(b.Instance.Version + " " + b.Instance.OS + "/" + b.Instance.Arch)},
		{"Taken", b.GeneratedAt.UTC().Format(time.RFC3339)},
		{"Problems", fmt.Sprintf("%d", len(b.Problems.Items))},
		{"Config findings", fmt.Sprintf("%d", len(b.Findings))},
		{"Installations", fmt.Sprintf("%d", len(b.Installations))},
		{"Pools", fmt.Sprintf("%d", len(b.Pools))},
		{"Hosts", fmt.Sprintf("%d", len(b.Hosts))},
		{"Runners", fmt.Sprintf("%d", len(b.Runners))},
		{"Unfinished jobs", fmt.Sprintf("%d, %d explained", len(b.Jobs), len(b.Explanations))},
		{"Scaling decisions", fmt.Sprintf("%d", len(b.ScalingEvents))},
		{"Log pointers", fmt.Sprintf("%d", len(b.Logs.Runners))},
	})

	// The two things a reader of the bundle would otherwise have to discover
	// by finding a section short and guessing why.
	if len(b.Errors) > 0 {
		p.note("")
		p.note("Some sections could not be gathered, and the bundle says so in its errors array:")
		for _, e := range b.Errors {
			p.note("  %s: %s", e.Section, e.Error)
		}
	}
	for _, t := range b.Truncated {
		p.note("")
		p.note("%s was shortened: %s", t.Section, t.Reason)
	}

	p.note("")
	p.note("No workflow log is in it. Attach the ones a support case needs from the runners the")
	p.note("bundle's logs section names, so you choose what leaves this fleet.")
}

// humanBytes renders a size the way an operator about to attach a file thinks
// about one.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// `zoomies pools export` and `zoomies pools import`: the Pools page's Export
// and Import, over the same two routes, for the operator who keeps the fleet's
// pools in a repository and applies them from a pipeline.

// poolsImportPlan is the part of POST /pools/import the CLI prints.
type poolsImportPlan struct {
	Applied bool `json:"applied"`
	Changes []struct {
		Pool   string `json:"pool"`
		Action string `json:"action"`
		Fields []struct {
			Field    string `json:"field"`
			Current  any    `json:"current"`
			Incoming any    `json:"incoming"`
		} `json:"fields"`
		Reason   string   `json:"reason"`
		Warnings []string `json:"warnings"`
		EnvKeys  []string `json:"env_keys"`
	} `json:"changes"`
	Summary struct {
		Create    int `json:"create"`
		Change    int `json:"change"`
		Unchanged int `json:"unchanged"`
		Refused   int `json:"refused"`
		Skipped   int `json:"skipped"`
	} `json:"summary"`
}

func poolsExport(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies pools export [--file pools.yaml]",
		"Write every pool to a file another instance can import. Environment values are never in it.")
	cf := registerClientFlags(fs, false)
	file := fs.String("file", "", "write the document here; \"-\" means stdout. The default names a file in the working directory")
	format := fs.String("format", "yaml", "yaml, for a repository, or json, which says when and where it was taken")
	fs.example("zoomies pools export --file pools.yaml", "zoomies pools export --format json --file -")
	if err := fs.parse(args); err != nil {
		return err
	}
	if err := fs.noMoreArgs(); err != nil {
		return err
	}
	if *format != "yaml" && *format != "json" {
		return usagef("pools export", "--format is yaml or json")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("format", *format)
	raw, err := client.get(ctx, "/pools/export", q, nil)
	if err != nil {
		return err
	}
	if *file == "-" {
		_, err := e.out.Write(raw)
		return err
	}
	path := *file
	if path == "" {
		path = "zoomies-pools-" + time.Now().UTC().Format("20060102-150405") + "." + *format
	}
	// Readable by others: the document carries no secret, and it is meant to
	// be committed.
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("writing the pools to %s: %w", path, err)
	}
	if abs, aerr := filepath.Abs(path); aerr == nil {
		path = abs
	}
	fmt.Fprintf(e.out, "Wrote %s. Environment values are not in it; set them by hand after importing.\n", path)
	return nil
}

func poolsImport(ctx context.Context, e *env, args []string) error {
	fs := newFlagSet(e, "zoomies pools import <file> [--dry-run] [--skip <pool>,...]",
		"Create and change pools to match a pools export. Nothing is written while any pool is refused.")
	cf := registerClientFlags(fs, true)
	dryRun := fs.Bool("dry-run", false, "show what would change and write nothing")
	skip := fs.String("skip", "", "comma-separated names of pools in the document to leave out")
	fs.example("zoomies pools import pools.yaml --dry-run", "zoomies pools import pools.yaml",
		"zoomies pools import pools.yaml --skip zoomies-legacy", "cat pools.yaml | zoomies pools import -")
	if err := fs.parse(args); err != nil {
		return err
	}
	path, err := fs.oneArg("a pools export, or - for stdin")
	if err != nil {
		return err
	}
	var document []byte
	if path == "-" {
		document, err = io.ReadAll(io.LimitReader(e.in, 8<<20))
	} else {
		document, err = os.ReadFile(path)
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	client, err := cf.client()
	if err != nil {
		return err
	}
	p, err := cf.printer(e)
	if err != nil {
		return err
	}
	body := map[string]any{"document": string(document), "dry_run": *dryRun, "skip": splitNames(*skip)}
	var plan poolsImportPlan
	raw, err := client.post(ctx, "/pools/import", nil, body, &plan)
	if err != nil {
		return err
	}
	if p.structured() {
		return p.emit(raw)
	}
	printPoolsPlan(p, &plan)
	return nil
}

func splitNames(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// printPoolsPlan says, pool by pool, what the import would do or did, and
// ends on the sentence that says which of the two it was.
func printPoolsPlan(p *printer, plan *poolsImportPlan) {
	for _, ch := range plan.Changes {
		switch ch.Action {
		case "create":
			p.note("%s %s", p.paint(colourGreen, "create   "), ch.Pool)
		case "change":
			p.note("%s %s", p.paint(colourYellow, "change   "), ch.Pool)
			for _, f := range ch.Fields {
				p.note("            %s: %s -> %s", f.Field, planValue(f.Current), planValue(f.Incoming))
			}
		case "refused":
			p.note("%s %s", p.paint(colourRed, "refused  "), ch.Pool)
			p.note("            %s", ch.Reason)
		default:
			p.note("%s %s", p.paint(colourDim, "unchanged"), ch.Pool)
		}
		for _, w := range ch.Warnings {
			p.note("            warning: %s", w)
		}
		if len(ch.EnvKeys) > 0 && ch.Action != "refused" {
			p.note("            set by hand: %s", strings.Join(ch.EnvKeys, ", "))
		}
	}
	s := plan.Summary
	p.note("")
	counts := fmt.Sprintf("%d to create, %d to change, %d already so, %d refused, %d skipped",
		s.Create, s.Change, s.Unchanged, s.Refused, s.Skipped)
	switch {
	case plan.Applied:
		p.note("Applied: %s.", strings.NewReplacer("to create", "created", "to change", "changed").Replace(counts))
	case s.Refused > 0:
		p.note("Dry run: %s. Fix the refused pools or --skip them before applying.", counts)
	default:
		p.note("Dry run: %s. Nothing was written.", counts)
	}
}

func planValue(v any) string {
	if v == nil {
		return "not set"
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return `""`
		}
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

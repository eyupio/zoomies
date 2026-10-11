package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// The catalog is the one place every problem code and every Kennel Club check
// is explained, and docs/ai-context.md tells an agent to read it before it
// explains a problem. An agent that reaches the fleet only over MCP had no way
// to: an OAuth token is good on /mcp alone. It is a few hundred kilobytes, far
// more than a tool answer should put in front of a model, so without arguments
// the tool is an index, and an entry comes by its code. The same two readings
// are the catalog's resources (resources.go).

// catalogCategories are the catalog's own, in the order an agent should look.
var catalogCategories = []string{"capacity", "reliability", "security", "configuration", "cost"}

// catalogEntry is the part of an entry the index and a category list keep.
type catalogEntry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Severity string `json:"severity"`
}

// catalog is GET /catalog taken apart: the entries as the controller wrote
// them, and the slice of each the index keeps.
type catalog struct {
	version int
	raw     []json.RawMessage
	entries []catalogEntry
}

// readCatalog fetches the catalog with the caller's token and takes it apart,
// refusing a shape it cannot read rather than passing it on.
func readCatalog(ctx context.Context, c API) (*catalog, error) {
	body, err := c.Call(ctx, "GET", "/catalog", nil)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Version int               `json:"version"`
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errCatalogShape
	}
	var entries []catalogEntry
	if err := json.Unmarshal(body, &struct {
		Entries *[]catalogEntry `json:"entries"`
	}{&entries}); err != nil || len(entries) != len(doc.Entries) {
		return nil, errCatalogShape
	}
	return &catalog{version: doc.Version, raw: doc.Entries, entries: entries}, nil
}

// index is every code under its category, which is small enough to hand a
// model whole and enough to choose an entry from.
func (c *catalog) index() ([]byte, error) {
	categories := map[string][]string{}
	for _, e := range c.entries {
		categories[e.Category] = append(categories[e.Category], e.ID)
	}
	for _, ids := range categories {
		sort.Strings(ids)
	}
	return json.Marshal(map[string]any{"version": c.version, "categories": categories})
}

// category is one category's codes with their titles and severity.
func (c *catalog) category(name string) ([]byte, error) {
	out := make([]catalogEntry, 0, len(c.entries))
	for _, e := range c.entries {
		if e.Category == name {
			out = append(out, e)
		}
	}
	return json.Marshal(map[string]any{"version": c.version, "category": name, "entries": out})
}

// entry is one entry in full, as the controller wrote it, or false.
func (c *catalog) entry(code string) (json.RawMessage, bool) {
	for i, e := range c.entries {
		if e.ID == code {
			return c.raw[i], true
		}
	}
	return nil, false
}

// family is every code that shares a code's prefix before its first dot, in
// order: what to offer in place of a code that is not there.
func (c *catalog) family(code string) []string {
	prefix, _, ok := strings.Cut(code, ".")
	if !ok || prefix == "" {
		return nil
	}
	var out []string
	for _, e := range c.entries {
		if strings.HasPrefix(e.ID, prefix+".") {
			out = append(out, e.ID)
		}
	}
	sort.Strings(out)
	return out
}

func catalogTools() []*tool {
	return []*tool{
		{
			Name:  "get_catalog",
			Title: "Catalog of problem codes and checks",
			Description: "Every problem code the controller raises and every check Kennel Club makes, with what each means, what to change and how to see that it worked: " +
				"the catalog at GET /catalog, which is Zoomies' own fixed text and names no fleet, repository or person. " +
				"Without arguments it is an index, each code under its category (capacity, reliability, security, configuration, cost). " +
				"With code it is that one entry in full, which is what to read before explaining a problem's code, a finding's check or a job explanation's problem_code. " +
				"With category it is that category's codes with their titles and severity. " +
				"A code that is not in the catalog is said to be unknown; it is not an empty entry.",
			InputSchema: object(nil, map[string]any{
				"code":     str("a problem code or check id, as list_problems, get_job or kennel_findings show it, such as pool.no_host or ci.no_timeout"),
				"category": enum("list the codes of one category, with titles", catalogCategories...),
			}),
			Annotations: readOnly,
			call:        getCatalog,
		},
	}
}

func getCatalog(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
	var a struct {
		Code     string `json:"code"`
		Category string `json:"category"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	cat, err := readCatalog(ctx, c)
	if err != nil {
		return nil, err
	}
	switch {
	case a.Code != "":
		entry, ok := cat.entry(a.Code)
		if !ok {
			// A code one letter off is answered with its family's codes, so the
			// next call is the right one; a model shown "no such code" alone
			// guessed again, and again.
			if family := cat.family(a.Code); len(family) > 0 {
				return nil, fmt.Errorf("there is no %s in the catalog; the codes that start the same way are %s", a.Code, strings.Join(family, ", "))
			}
			return nil, fmt.Errorf("there is no %s in the catalog; get_catalog without arguments lists every code", a.Code)
		}
		return jsonContent(entry), nil
	case a.Category != "":
		body, err := cat.category(a.Category)
		if err != nil {
			return nil, err
		}
		return jsonContent(body), nil
	}
	body, err := cat.index()
	if err != nil {
		return nil, err
	}
	return jsonContent(body), nil
}

// errCatalogShape is a catalog the tool cannot take apart, refused rather than
// passed on, as the Kennel tools refuse an answer they cannot read.
var errCatalogShape = errors.New("the controller's catalog is not in the shape this tool expects; upgrade the controller and the CLI together")

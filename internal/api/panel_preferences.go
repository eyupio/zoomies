package api

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

/*
The pages whose panels an operator can arrange, and the panels each has. This
is the UI's list, not a domain fact: a panel is a component in
web/src/lib/overview/panels.ts and nothing in the database knows about it. It
lives here so a document naming a page or a panel this build cannot draw is
refused with a sentence the client can act on, rather than stored and
discovered later as a layout nobody can repair in the interface.
*/
var panelPages = map[string][]string{
	"overview": {"attention", "pools", "failures", "jobs", "events", "history", "diagnostics"},
}

// panelLayout is how one page is arranged: the order its panels are drawn in
// and the ones not drawn. Both are optional, because absent is a choice never
// made and the page keeps its default; the UI appends any panel the order
// leaves out, so a short order is not a broken one.
type panelLayout struct {
	Order  []string `json:"order,omitempty"`
	Hidden []string `json:"hidden,omitempty"`
}

// panelPreferences is the `panels` object of an account's private
// preferences, keyed by page.
type panelPreferences map[string]panelLayout

// validate says which part of a panel document this build does not have, or
// nothing. A page is checked before its panels, since a page the UI cannot
// arrange has no list to check the panels against.
func (in panelPreferences) validate(field string) []fieldError {
	var errs []fieldError
	for _, page := range slices.Sorted(maps.Keys(in)) {
		known, ok := panelPages[page]
		if !ok {
			errs = append(errs, fieldError{field + "." + page, "not a page with panels to arrange"})
			continue
		}
		layout := in[page]
		for _, list := range []struct {
			name string
			ids  []string
		}{{"order", layout.Order}, {"hidden", layout.Hidden}} {
			if len(list.ids) > len(known) {
				errs = append(errs, fieldError{field + "." + page + "." + list.name, "at most " + strconv.Itoa(len(known)) + " panels"})
				continue
			}
			for _, id := range list.ids {
				if !slices.Contains(known, id) {
					errs = append(errs, fieldError{field + "." + page + "." + list.name, "one of " + strings.Join(known, ", ")})
					break
				}
			}
		}
	}
	return errs
}

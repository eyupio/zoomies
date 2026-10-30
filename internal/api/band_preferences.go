package api

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

/*
The pages that open on a band. This is the UI's list, not a domain fact: a band
page is an address in web/src/lib/bands/pages.ts and nothing in the database
knows about it. It lives here so a document naming a page this build cannot draw
is refused with a sentence the client can act on, rather than stored and
discovered later as a band that never changes.
*/
var bandPages = []string{
	"overview", "runners", "pools", "queue", "jobs", "workflows", "usage",
	"hosts", "providers", "installations", "migrate", "kennel",
}

var bandCuts = []string{"slim", "compact", "standard", "expanded"}

var bandLogoSizes = []string{"auto", "small", "medium", "large"}

// bandPreferences is the `bands` object of an account's private preferences:
// where each band stands, which are switched off, and whether the dog is drawn
// and at what size. Every field is optional, because an absent one is a choice
// never made and the page keeps its default.
type bandPreferences struct {
	Hidden    []string          `json:"hidden,omitempty"`
	Sizes     map[string]string `json:"sizes,omitempty"`
	Logo      *bool             `json:"logo,omitempty"`
	LogoSize  string            `json:"logo_size,omitempty"`
	LogoPages map[string]bool   `json:"logo_pages,omitempty"`
	LogoSizes map[string]string `json:"logo_sizes,omitempty"`
}

// validate says which part of a band document this build does not have, or
// nothing. Keys are checked as well as values: a page the UI cannot open would
// otherwise be carried around forever by a document nobody can repair in the
// interface.
func (in *bandPreferences) validate(field string) []fieldError {
	if in == nil {
		return nil
	}
	var errs []fieldError
	if len(in.Hidden) > len(bandPages) {
		errs = append(errs, fieldError{field + ".hidden", "at most " + strconv.Itoa(len(bandPages)) + " pages"})
	}
	for _, page := range in.Hidden {
		if !slices.Contains(bandPages, page) {
			errs = append(errs, fieldError{field + ".hidden", "one of " + strings.Join(bandPages, ", ")})
			break
		}
	}
	for _, page := range slices.Sorted(maps.Keys(in.Sizes)) {
		if !slices.Contains(bandPages, page) {
			errs = append(errs, fieldError{field + ".sizes." + page, "not a page with a band"})
			continue
		}
		if cut := in.Sizes[page]; !slices.Contains(bandCuts, cut) {
			errs = append(errs, fieldError{field + ".sizes." + page, "one of " + strings.Join(bandCuts, ", ")})
		}
	}
	if in.LogoSize != "" && !slices.Contains(bandLogoSizes, in.LogoSize) {
		errs = append(errs, fieldError{field + ".logo_size", "one of " + strings.Join(bandLogoSizes, ", ")})
	}
	for _, page := range slices.Sorted(maps.Keys(in.LogoPages)) {
		if !slices.Contains(bandPages, page) {
			errs = append(errs, fieldError{field + ".logo_pages." + page, "not a page with a band"})
		}
	}
	for _, page := range slices.Sorted(maps.Keys(in.LogoSizes)) {
		if !slices.Contains(bandPages, page) {
			errs = append(errs, fieldError{field + ".logo_sizes." + page, "not a page with a band"})
			continue
		}
		if size := in.LogoSizes[page]; !slices.Contains(bandLogoSizes, size) {
			errs = append(errs, fieldError{field + ".logo_sizes." + page, "one of " + strings.Join(bandLogoSizes, ", ")})
		}
	}
	return errs
}

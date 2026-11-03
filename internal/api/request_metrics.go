package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// The request metrics are labelled by the route that served a request, never by
// the path that was asked for.
//
// A path carries ids, and a scanner walking made-up paths carries whatever it
// likes, so a label made from one would grow with the traffic and not with the
// code. A route pattern is written in the source: a controller has a few
// hundred, and the group is only the first part of one, so the label has a few
// dozen values however much traffic there is.

// routePattern is the pattern of the route that served r, or "" when none did.
// It is only complete once the handler has run, which is why it is read after
// it and not before.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}

// routeGroup is the metrics' "group" label for a matched route pattern: the part
// after /api/v1/ for the API, the first part of the path for everything else,
// and one of two fixed names for a request no route matched.
//
// A request that matched nothing is the UI asking for a file, which the catch-all
// serves, or an API path that does not exist. They are told apart by the prefix
// of the path, which is a choice between two names and so adds nothing.
func routeGroup(pattern, path string) string {
	if pattern == "" {
		if strings.HasPrefix(path, "/api/") {
			return "unmatched"
		}
		return "ui"
	}
	p := strings.TrimPrefix(pattern, "/")
	if rest, ok := strings.CutPrefix(p, "api/v1/"); ok {
		// An API path with no route is reported as the pattern of the router
		// that refused it, "/api/v1/*". It is a miss, so it is counted as one;
		// the star would otherwise be read below as the UI's catch-all.
		if rest == "*" || rest == "" {
			return "unmatched"
		}
		p = rest
	}
	first, _, _ := strings.Cut(p, "/")
	switch first {
	case "", "*":
		return "ui"
	case "healthz", "readyz":
		return "health"
	}
	return sanitiseGroup(first)
}

// sanitiseGroup keeps a group to the characters a metric label reads well in.
// A pattern is source, so this changes nothing today; it is what makes a route
// somebody adds with an odd first segment a readable label and not a broken one.
func sanitiseGroup(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '_'
	}, s)
}

// longLivedRoutes are the routes that hold a connection open for their purpose:
// the event stream, a log tail, an agent's task poll, a runner's whole output
// streamed in, an assistant's answer as it is written, and the MCP transport.
//
// They are counted and not timed. How long one lasted is how long its client
// stayed, and one of them lasting forty minutes would put every later request in
// its group into the top bucket of a histogram that is there to say which
// requests are slow.
//
// The patterns are the router's own, and a test holds each to a route that exists.
var longLivedRoutes = map[string]bool{
	"/api/v1/events":                  true,
	"/api/v1/jobs/{id}/events":        true,
	"/api/v1/runners/{id}/logs":       true,
	"/api/v1/agent/tasks":             true,
	"/api/v1/agent/logs/{stream_id}":  true,
	"/api/v1/assistant/chat":          true,
	"/api/v1/assistant/personal/chat": true,
	"/mcp":                            true,
}

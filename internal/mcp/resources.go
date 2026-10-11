package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Resources are what an agent loads to learn the vocabulary before it works,
// without spending a tool call: the catalog's index at one URI, and any entry
// by its code through a template. They read the same route as get_catalog,
// with the same token, so a resource is never a way round a tool's limits.

const (
	catalogURI      = "zoomies://catalog"
	catalogTemplate = "zoomies://catalog/{code}"
	catalogMIME     = "application/json"
	// rpcResourceNotFound is the code the specification reserves for a URI
	// the server does not serve, so a client can tell it from a bad request.
	rpcResourceNotFound = -32002
)

func (s *Server) listResources() any {
	return map[string]any{"resources": []map[string]any{{
		"uri":      catalogURI,
		"name":     "catalog",
		"title":    "Catalog of problem codes and checks",
		"mimeType": catalogMIME,
		"description": "Every problem code and Kennel Club check this fleet can report, by category. " +
			"Read " + catalogTemplate + " for one entry in full: what it means, what to change and how to see that it worked.",
	}}}
}

func (s *Server) listResourceTemplates() any {
	return map[string]any{"resourceTemplates": []map[string]any{{
		"uriTemplate": catalogTemplate,
		"name":        "catalog-entry",
		"title":       "One catalog entry",
		"mimeType":    catalogMIME,
		"description": "One problem code or check in full, by the code list_problems, get_job or kennel_findings show, such as pool.no_host or ci.no_timeout.",
	}}}
}

func (s *Server) readResource(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		return nil, &RPCError{rpcInvalidParams, "resources/read wants a uri"}
	}
	code, isEntry := strings.CutPrefix(p.URI, catalogURI+"/")
	if p.URI != catalogURI && (!isEntry || code == "") {
		return nil, &RPCError{rpcResourceNotFound, "no resource at " + strconv.Quote(p.URI) + "; this server serves " + catalogURI + " and " + catalogTemplate}
	}
	cat, err := readCatalog(ctx, s.api)
	if err != nil {
		return nil, &RPCError{rpcInternalError, err.Error()}
	}
	var text []byte
	if isEntry {
		entry, ok := cat.entry(code)
		if !ok {
			return nil, &RPCError{rpcResourceNotFound, "there is no " + code + " in the catalog; " + catalogURI + " lists every code"}
		}
		text = entry
	} else if text, err = cat.index(); err != nil {
		return nil, &RPCError{rpcInternalError, err.Error()}
	}
	return map[string]any{"contents": []map[string]any{{"uri": p.URI, "mimeType": catalogMIME, "text": string(text)}}}, nil
}

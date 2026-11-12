package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/auth"
	"github.com/eyupio/zoomies/internal/controller"
	"github.com/eyupio/zoomies/internal/mcp"
)

// assistantToolbox is the fleet's read tools for the assistant, as the person
// chatting may use them. It is the MCP server over the in-process API with that
// person's identity and no leave to offer a tool that changes anything, so the
// tools, their refusals and their limits are the ones an agent of theirs meets,
// and a viewer's Eli sees what a viewer's token would.
type assistantToolbox struct {
	server *mcp.Server
}

// assistantMaxWait is the cap on one of Eli's waits for a change. It starts at
// the controller's value and is a variable only so a test of the cap need not
// hold a request for the full ten seconds.
var assistantMaxWait = controller.AssistantMaxWait

func (s *Server) assistantToolbox(r *http.Request, as *auth.Identity) *assistantToolbox {
	return &assistantToolbox{server: mcp.New(inProcessAPI{s: s, from: r, as: as, direct: true}, mcp.Options{
		WindowBlock:   controller.AssistantWindowBlock,
		MaxWait:       assistantMaxWait,
		WithheldKinds: controller.AssistantWithheldKinds,
	})}
}

func (b *assistantToolbox) Tools() []assistant.Tool {
	defs := b.server.Definitions()
	out := make([]assistant.Tool, 0, len(defs))
	for _, d := range defs {
		schema, err := json.Marshal(d.InputSchema)
		if err != nil {
			continue
		}
		out = append(out, assistant.Tool{Name: d.Name, Description: d.Description, Parameters: schema, Live: d.Live})
	}
	return out
}

func (b *assistantToolbox) Call(ctx context.Context, name string, args json.RawMessage) ([]string, bool, error) {
	return b.server.CallTool(ctx, name, args)
}

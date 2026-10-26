package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// windowTestTool is a tool the real surface does not have yet: one that answers
// a block of text and the same facts as a value, and is live.
func windowTestTool() *tool {
	return &tool{
		Name:        "window_probe",
		Title:       "Window probe",
		Description: "A test tool that answers structured content.",
		InputSchema: object(nil, map[string]any{}),
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"queued": map[string]any{"type": "integer"}},
		},
		Annotations: readOnly,
		live:        true,
		structured: func(context.Context, API, json.RawMessage) ([]Content, any, error) {
			return []Content{{Type: "text", Text: `{"queued":3}`}}, map[string]any{"queued": 3}, nil
		},
	}
}

func probeServer() *Server {
	return newServer(fakeAPI{}, Options{}, append(tools(), windowTestTool()))
}

// Through the protocol, as a client sees it: the structured value rides beside
// the text a client older than the 2025-06-18 revision still reads, and the
// tool's list entry carries the schema that value answers to.
func TestAStructuredToolAnswersBothBlocksAndStructuredContent(t *testing.T) {
	s := probeServer()

	result, rpcErr := s.handle(context.Background(), &rpcRequest{
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"window_probe","arguments":{}}`),
	})
	if rpcErr != nil {
		t.Fatalf("tools/call: %v", rpcErr)
	}
	var call struct {
		Content           []Content      `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	}
	roundTrip(t, result, &call)
	if len(call.Content) != 1 || call.Content[0].Text != `{"queued":3}` {
		t.Errorf("the text block was lost: %+v", call.Content)
	}
	if !reflect.DeepEqual(call.StructuredContent, map[string]any{"queued": float64(3)}) {
		t.Errorf("structuredContent = %v, want the queued count beside the text", call.StructuredContent)
	}

	listed, rpcErr := s.handle(context.Background(), &rpcRequest{Method: "tools/list"})
	if rpcErr != nil {
		t.Fatalf("tools/list: %v", rpcErr)
	}
	var list struct {
		Tools []map[string]any `json:"tools"`
	}
	roundTrip(t, listed, &list)
	var found, others int
	for _, tl := range list.Tools {
		if tl["name"] == "window_probe" {
			found++
			if tl["outputSchema"] == nil {
				t.Error("tools/list does not carry the probe's outputSchema")
			}
			if _, has := tl["live"]; has {
				t.Error("tools/list carries live, which the protocol has no hint for")
			}
			continue
		}
		others++
		_, has := tl["outputSchema"]
		structured := []any{"get_retained_comparison", "get_checkout_preview", "get_cache_preview", "get_readiness_preview", "get_kennel_fix", "get_sharding_preview", "recent_events", "wait_for_change", "queue_wait_breakdown", "compare_periods", "host_pressure", "startup_failures"}
		if want := slices.Contains(structured, tl["name"]); has != want {
			t.Errorf("%v: outputSchema present = %v, want %v (only the tools that answer structured content have one)", tl["name"], has, want)
		}
	}
	if found != 1 || others == 0 {
		t.Errorf("tools/list held the probe %d times and %d other tools", found, others)
	}
}

// Eli's toolbox fences each block of text for a model and has no use for a
// value beside it; the tool must not need a second code path to serve it.
func TestAStructuredToolsTextIsAllEliSees(t *testing.T) {
	blocks, failed, err := probeServer().CallTool(context.Background(), "window_probe", nil)
	if err != nil || failed {
		t.Fatalf("CallTool: failed=%v err=%v", failed, err)
	}
	if want := []string{`{"queued":3}`}; !reflect.DeepEqual(blocks, want) {
		t.Errorf("blocks = %q, want %q", blocks, want)
	}
}

// wait_for_change and recent_events are the real live tools: the window
// follows them as the fleet changes, so the same cursor asked again later has
// more to say and a caller that remembers answers must ask again. Any other
// tool marked live is a claim that its answer goes stale, which should be made
// on purpose and added here.
func TestALiveToolIsMarkedOnItsDefinition(t *testing.T) {
	live := map[string]bool{}
	for _, d := range probeServer().Definitions() {
		live[d.Name] = d.Live
	}
	realLive := []string{"window_probe", "wait_for_change", "recent_events"}
	for _, name := range realLive {
		if !live[name] {
			t.Errorf("%s is not marked live", name)
		}
	}
	for name, l := range live {
		if l && !slices.Contains(realLive, name) {
			t.Errorf("%s is marked live, and only %v are meant to be", name, realLive)
		}
	}
	if _, ok := live["fleet_status"]; !ok {
		t.Error("the real tools are missing from the definitions")
	}
}

// A tool is written one way or the other, never both and never neither, and an
// output schema promises structured content: a schema on a text-only tool would
// be a promise a client validates against nothing.
func TestEveryToolHasExactlyOneWayToAnswerAndASchemaOnlyWhenStructured(t *testing.T) {
	for _, tl := range append(tools(), windowTestTool()) {
		if (tl.call == nil) == (tl.structured == nil) {
			t.Errorf("%s must set exactly one of call and structured", tl.Name)
		}
		if (tl.OutputSchema != nil) != (tl.structured != nil) {
			t.Errorf("%s: OutputSchema must be set if and only if structured is", tl.Name)
		}
	}
}

func roundTrip(t *testing.T, in any, out any) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

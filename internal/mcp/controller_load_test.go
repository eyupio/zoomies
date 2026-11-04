package mcp

import (
	"strings"
	"testing"
)

// The tool is a plain read of one route and nothing else: it can never do what
// the caller's token could not, and it takes no arguments, so one that names an
// argument it does not have is refused rather than ignored.
func TestControllerLoadReadsItsOneRouteAndTakesNoArguments(t *testing.T) {
	tl := kennelTool(t, "controller_load")
	if !tl.Annotations.ReadOnly || tl.action {
		t.Fatalf("controller_load is not a plain read: %+v", tl.Annotations)
	}
	api := &kennelAPI{body: "{\n  \"uptime_seconds\": 12.5,\n  \"steps\": []\n}"}
	blocks, err := kennelCall(t, tl, api, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if api.got != "GET /controller/load" || len(api.query) != 0 {
		t.Errorf("read %q with %v, want GET /controller/load with no query", api.got, api.query)
	}
	// The model reads the answer as the contract documents it, compacted: the
	// whitespace in it is tokens the model pays for.
	if len(blocks) != 1 || blocks[0].Text != `{"uptime_seconds":12.5,"steps":[]}` {
		t.Errorf("answer = %+v, want the body compacted", blocks)
	}
	if _, err := kennelCall(t, tl, &kennelAPI{}, `{"window":"1h"}`); err == nil {
		t.Error("controller_load accepted an argument it does not name")
	}
}

// What a model does with the answer depends on the description saying how to read
// it, and the likeliest wrong turns are to take elapsed time for CPU and to add up
// steps that contain each other, so the description has to warn of both.
func TestControllerLoadDescribesHowToReadItsTimesAndWhereTheCPUShareIs(t *testing.T) {
	d := kennelTool(t, "controller_load").Description
	for _, want := range []string{
		"process_cpu_percent", "steal_percent", "elapsed time and not CPU time", "whole", "uptime_seconds", "never the function",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("description does not say %q:\n%s", want, d)
		}
	}
}

package mcp

import (
	"context"
	"encoding/json"
)

// controllerLoadTools is the one tool that looks at the controller itself and not
// at the fleet it runs. It is a plain read of one route, kept apart from the
// window tools because it is about a process and not about a window of time.
func controllerLoadTools() []*tool {
	return []*tool{
		{
			Name:  "controller_load",
			Title: "Controller load",
			Description: "Where the controller's own time goes, counted since it started (uptime_seconds): its background loops step by step, longest first, its busiest groups of routes, how many scheduling passes ran and what started them, and the machine it runs on. " +
				"Use it when the machine the controller runs on is busy and the question is whether that is Zoomies or something else on it, and which loop or route it is if it is Zoomies. " +
				"Read machine.usage.process_cpu_percent, the share of the machine's CPU the controller process itself used, against cpu_percent (everything on the machine, steal and I/O wait counted as busy) and steal_percent (what the hypervisor kept); what is left of cpu_percent is not Zoomies. " +
				"The step and route times are elapsed time and not CPU time: a step waiting on the database or GitHub counts for as long as it waits, so use them to name the loop and process_cpu_percent to say how much CPU there is. " +
				"A step marked whole times its loop's entire pass and already contains the loop's other steps, so do not add them. slowest_at_most_seconds bounds the slowest run from above, and slowest_over_seconds says a run was slower than the last timing bucket. " +
				"It names the loop and never the function: that is a CPU profile, which an administrator takes from the API. A controller that has just restarted has little in it.",
			InputSchema: object(nil, map[string]any{}),
			Annotations: readOnly,
			call: func(ctx context.Context, c API, raw json.RawMessage) ([]Content, error) {
				if err := decodeArgs(raw, &struct{}{}); err != nil {
					return nil, err
				}
				return getJSON(ctx, c, "/controller/load", nil)
			},
		},
	}
}

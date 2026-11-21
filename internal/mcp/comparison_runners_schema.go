package mcp

import (
	"slices"

	"github.com/eyupio/zoomies/internal/analysis"
)

func comparisonRunnerSchema() map[string]any {
	closed := func(props map[string]any) map[string]any {
		required := []string{}
		for name := range props {
			required = append(required, name)
		}
		slices.Sort(required)
		return object(required, props)
	}
	text := func() map[string]any { return map[string]any{"type": "string"} }
	boolean := func() map[string]any { return map[string]any{"type": "boolean"} }
	number := func() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
	nullable := func(s map[string]any) map[string]any {
		return map[string]any{"oneOf": []any{s, map[string]any{"type": "null"}}}
	}
	size := func(kind string) map[string]any { return nullable(map[string]any{"type": kind, "exclusiveMinimum": 0}) }
	grant := closed(map[string]any{"basis": text(), "source": text(), "cpus": size("number"), "memory_mb": size("integer")})
	request := closed(map[string]any{"backend": text(), "image_reference_sha256": text(), "cpus": size("number"), "memory_mb": size("integer"), "resource_source": text(), "docker_mode": text(), "cpu_typed": boolean(), "memory_typed": boolean()})
	observation := closed(map[string]any{"sha256": text(), "issued_at": text(), "received_at": text(), "requested": request, "image_digest": nullable(text()), "digest_coverage": text(), "agent_completed_at": nullable(text()), "agent_container_started_at": nullable(text())})
	job := closed(map[string]any{"run_id": text(), "attempt": map[string]any{"type": "integer", "minimum": 1}, "github_job_id": text(), "fleet_matched": boolean(), "recorded_grant": nullable(grant), "observation": nullable(observation)})
	arm := closed(map[string]any{"selected_jobs": number(), "matched_jobs": number(), "recorded_grants": number(), "runner_observations": number(), "rows_omitted": boolean(), "shown": number(), "jobs": map[string]any{"type": "array", "maxItems": 100, "items": job}})
	evidence := closed(map[string]any{"version": map[string]any{"type": "string", "enum": []string{analysis.RunnerEvidenceVersion}}, "baseline": arm, "candidate": arm})
	evidence["description"] = "Descriptive controller grants, dispatched requests and unverified agent claims, not applied limits or controlled equivalence. Full enumeration counts include omitted rows; shown and rows_omitted describe this answer. Null means an older controller supplied no runner evidence."
	return nullable(evidence)
}

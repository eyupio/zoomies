package mcp

import (
	"slices"

	"github.com/eyupio/zoomies/internal/analysis"
)

func comparisonWorkflowSchema() map[string]any {
	closed := func(props map[string]any) map[string]any {
		required := []string{}
		for key := range props {
			required = append(required, key)
		}
		slices.Sort(required)
		return object(required, props)
	}
	nullable := func(s map[string]any) map[string]any {
		return map[string]any{"oneOf": []any{s, map[string]any{"type": "null"}}}
	}
	hash := func(n string) map[string]any {
		return map[string]any{"type": "string", "pattern": "^[a-f0-9]{" + n + "}$"}
	}
	count := func() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
	source := closed(map[string]any{"basis": map[string]any{"type": "string", "enum": []string{analysis.RootSourceBasis}}, "commit_sha": hash("40"), "blob_sha": hash("40"), "content_sha256": hash("64"), "bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": 131072}, "dependencies": nullable(comparisonDependencySchema())})
	attempt := closed(map[string]any{"run_id": map[string]any{"type": "string", "pattern": "^[1-9][0-9]*$"}, "attempt": map[string]any{"type": "integer", "minimum": 1}, "event": nullable(map[string]any{"type": "string", "maxLength": 64, "pattern": "^[a-z0-9_]+$"}), "workflow_path_sha256": nullable(hash("64")), "root_source": nullable(source)})
	arm := closed(map[string]any{"selected_attempts": count(), "root_sources": count(), "rows_omitted": map[string]any{"type": "boolean"}, "shown": count(), "attempts": map[string]any{"type": "array", "maxItems": 100, "items": attempt}})
	evidence := closed(map[string]any{"version": map[string]any{"type": "string", "enum": []string{analysis.WorkflowEvidenceVersion}}, "baseline": arm, "candidate": arm})
	evidence["description"] = "Retained triggers, path fingerprints and root-file digests only. Collection does not attest executed bytes or cover reusable workflows/actions, inputs or test scope. Full counts include omitted rows; shown describes this answer. Null means an older controller supplied no view."
	return nullable(evidence)
}

func comparisonDependencySchema() map[string]any {
	props := map[string]any{}
	for _, n := range []string{"version", "basis", "source_sha256", "coverage"} {
		props[n] = map[string]any{"type": "string"}
	}
	counters := map[string]any{}
	names := []string{"declarations", "commit_pins", "digest_pins", "floating", "local", "expressions", "unknown"}
	for _, n := range names {
		counters[n] = map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}
	}
	props["counts"] = map[string]any{"oneOf": []any{object(names, counters), map[string]any{"type": "null"}}}
	props["rows_omitted"] = map[string]any{"type": "boolean"}
	props["references"] = map[string]any{"type": "array", "maxItems": 0, "items": map[string]any{"type": "object"}}
	return object([]string{"version", "basis", "source_sha256", "coverage", "counts", "rows_omitted", "references"}, props)
}

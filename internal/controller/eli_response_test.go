package controller

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eyupio/zoomies/internal/assistant"
	"github.com/eyupio/zoomies/internal/prrepair"
	"github.com/eyupio/zoomies/internal/store"
)

func TestRepairRetainsRedactedResponsesEvenWhenTheCallFails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		events    []assistant.Event
		wantError bool
		wantText  string
	}{
		{"text", []assistant.Event{{Delta: `{"read":["a.go"]}`}, {Done: true}}, false, `{"read":["a.go"]}`},
		{"tool arguments", []assistant.Event{{ToolCall: &assistant.ToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"a.go"`)}}, {Done: true}}, false, "Tool call: read"},
		{"cut", []assistant.Event{{Delta: "partial", Done: true, Cut: true}}, true, "partial"},
		{"stream error", []assistant.Event{{Delta: "partial", Err: errors.New("provider error ghp_abcdefghijklmnop")}}, true, "partial"},
		{"unfinished", []assistant.Event{{Delta: "partial"}}, true, "partial"},
		{"empty", []assistant.Event{{Done: true}}, false, ""},
		{"redacted", []assistant.Event{{Delta: "ghp_abcdefghijklmnop person@example.org -----BEGIN PRIVATE KEY-----\nPRIVATE_CONTENT\n-----END PRIVATE KEY-----"}, {Done: true}}, false, "[redacted credential]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []store.EliRepairStep
			m := &repairModel{provider: repairEventProvider{events: tc.events}, record: func(s store.EliRepairStep) { steps = append(steps, s) }}
			_, _, err := m.complete(t.Context(), nil, 12000)
			if (err != nil) != tc.wantError || len(steps) != 1 {
				t.Fatalf("steps %d, error %v", len(steps), err)
			}
			step := steps[0]
			if !step.ResponseRecorded || !strings.Contains(step.Response, tc.wantText) || (step.Error != "") != tc.wantError {
				t.Fatalf("response recording incomplete: %+v", step)
			}
			if strings.Contains(step.Error, "ghp_") || strings.Contains(step.Response, "ghp_") || strings.Contains(step.Response, "person@example.org") || strings.Contains(step.Response, "PRIVATE_CONTENT") {
				t.Fatal("retained credentials or email in the response")
			}
		})
	}
}

func TestRepairDiagnosticRedactionPrecedesTheByteLimitAndPreservesUTF8(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat("é", prrepair.MaxReplyBytes),
		strings.Repeat("x", prrepair.MaxReplyBytes-64) + "-----BEGIN PRIVATE KEY-----\n" + strings.Repeat("PRIVATE_CONTENT", 20),
	} {
		safe, truncated := repairDiagnosticText(raw)
		if len(safe) > prrepair.MaxReplyBytes || !utf8.ValidString(safe) || strings.Contains(safe, "PRIVATE_CONTENT") {
			t.Fatal("the retained response exceeded its bound or leaked a cut private key")
		}
		if truncated != (len(raw) > prrepair.MaxReplyBytes && !strings.Contains(raw, "PRIVATE_CONTENT")) {
			t.Fatalf("truncation flag does not describe the redacted response: %v", truncated)
		}
	}
}

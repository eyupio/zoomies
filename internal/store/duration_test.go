package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// A bare number used to mean nanoseconds from JSON and seconds from YAML, so
// the same "300" was a third of a millisecond on one path and five minutes on
// the other. Both now insist on a unit, and say so in the same words.
func TestDurationsNeedAUnitFromJSONAndYAMLAlike(t *testing.T) {
	var fromJSON struct {
		D Duration `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"d":"5m"}`), &fromJSON); err != nil || fromJSON.D.Duration() != 5*time.Minute {
		t.Fatalf(`json "5m" = %v, %v`, fromJSON.D, err)
	}
	if err := json.Unmarshal([]byte(`{"d":300}`), &fromJSON); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("json 300 decoded as %v with error %v, want a refusal that asks for a unit", fromJSON.D, err)
	}

	var fromYAML struct {
		D Duration `yaml:"d"`
	}
	if err := yaml.Unmarshal([]byte("d: 5m"), &fromYAML); err != nil || fromYAML.D.Duration() != 5*time.Minute {
		t.Fatalf("yaml 5m = %v, %v", fromYAML.D, err)
	}
	if err := yaml.Unmarshal([]byte("d: 300"), &fromYAML); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("yaml 300 decoded as %v with error %v, want a refusal that asks for a unit", fromYAML.D, err)
	}
	if err := yaml.Unmarshal([]byte("d: soon"), &fromYAML); err == nil || !strings.Contains(err.Error(), `"soon"`) {
		t.Fatalf("yaml soon: %v, want an error naming the value", err)
	}
}

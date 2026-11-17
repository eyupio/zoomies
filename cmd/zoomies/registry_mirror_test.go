package main

import (
	"testing"

	"github.com/eyupio/zoomies/internal/config"
)

// The backends are built from the configuration once, when the agent starts,
// which is why the two settings say they apply on a restart. This holds that
// what an operator set is what the backend is built with, since the backend
// cannot say: a setting that never arrived leaves the mirror off with nothing
// to show for it.
func TestTheRegistryMirrorSettingsReachTheContainerBackends(t *testing.T) {
	cfg := config.Default()
	if got := dockerOptions(cfg, discardLogger()); got.RegistryMirror || got.RegistryMirrorMaxBytes != 0 {
		t.Fatalf("the default configuration builds a backend with the mirror on (%v, %d)", got.RegistryMirror, got.RegistryMirrorMaxBytes)
	}
	cfg.Agent.RegistryMirror.Enabled = true
	cfg.Agent.RegistryMirror.MaxBytes = 20 << 30
	got := dockerOptions(cfg, discardLogger())
	if !got.RegistryMirror || got.RegistryMirrorMaxBytes != 20<<30 {
		t.Fatalf("options = (%v, %d), want the mirror on with the size the operator set", got.RegistryMirror, got.RegistryMirrorMaxBytes)
	}
}

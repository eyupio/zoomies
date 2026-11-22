package controller

import (
	"errors"
	"testing"

	"github.com/eyupio/zoomies/internal/store"
)

func TestJITDoesNotRegisterNativeLinuxAsMacOSWhenTheDistroIsUnknown(t *testing.T) {
	pool := &store.Pool{
		Backend:  store.BackendProcess,
		Platform: store.Platform{OS: "macos", Arch: "amd64"},
		Labels:   []string{"ci"},
	}
	host := &store.Host{OS: "linux", Arch: "amd64"}

	if labels, err := jitLabels(pool, host); !errors.Is(err, errJITPlatformMismatch) {
		t.Fatalf("labels = %v, error = %v; want a platform mismatch", labels, err)
	}
}

package backend

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"golang.org/x/sys/unix"
)

// Exercise Remove itself with a real unlinkat permission failure, followed by
// a failed helper and a successful retry. Even root CI can run this: a child
// test drops DAC override capabilities on its locked OS thread. Other tests
// and the parent process keep their original credentials.
func TestToolCleanupRetainsRunnerUntilRetrySucceeds(t *testing.T) {
	if os.Geteuid() == 0 {
		if os.Getenv("ZOOMIES_TEST_CLEANUP_UNPRIVILEGED") != "1" {
			cmd := exec.Command(os.Args[0], "-test.run=^TestToolCleanupRetainsRunnerUntilRetrySucceeds$", "-test.v")
			cmd.Env = append(os.Environ(), "ZOOMIES_TEST_CLEANUP_UNPRIVILEGED=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("unprivileged cleanup regression: %v\n%s", err, out)
			} else {
				t.Logf("%s", out)
			}
			return
		}
		runtime.LockOSThread()
		// Do not unlock: Go retires this thread when the test goroutine ends.
		caps := [2]unix.CapUserData{}
		if err := unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &caps[0]); err != nil {
			t.Fatalf("dropping test process capabilities: %v", err)
		}
	}
	shared := t.TempDir()
	runner := cleanupRunner(t, shared)
	farm := runner.Config.Labels[LabelToolFarm]
	apiDir := filepath.Join(farm, "go/1.27.1/x64/api")
	if err := os.WriteFile(filepath.Join(apiDir, "go1.txt"), []byte("cached tool"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(apiDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(apiDir, 0755) })
	var stopped, recover bool
	var deleted []string
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("id") == runner.ID {
				writeJSON(w, 200, runner)
				return
			}
			w.WriteHeader(404)
		},
		"POST " + v + "/containers/runner-id/stop": func(w http.ResponseWriter, r *http.Request) {
			stopped = true
			w.WriteHeader(204)
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			if !stopped {
				t.Error("cleanup began before the runner was stopped")
			}
			writeJSON(w, 201, map[string]string{"Id": "helper"})
		},
		"POST " + v + "/containers/helper/start": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
		"POST " + v + "/containers/helper/wait": func(w http.ResponseWriter, r *http.Request) {
			if !recover {
				writeJSON(w, 200, map[string]int{"StatusCode": 1})
				return
			}
			// Model the daemon clearing the cache under its own credentials.
			if err := os.Chmod(apiDir, 0755); err != nil {
				t.Error(err)
			}
			if err := os.RemoveAll(filepath.Join(farm, "go")); err != nil {
				t.Error(err)
			}
			writeJSON(w, 200, map[string]int{"StatusCode": 0})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
			deleted = append(deleted, r.PathValue("id"))
			w.WriteHeader(204)
		},
	})
	b := dockerBackendFor(t, f, DockerOptions{SharedDir: shared})
	if err := b.Remove(context.Background(), Handle(runner.ID)); err == nil {
		t.Fatal("permission failure disappeared")
	}
	if slices.Contains(deleted, runner.ID) {
		t.Fatal("failed cleanup lost the runner's retry labels")
	}
	if !slices.Contains(deleted, "helper") {
		t.Fatal("real permission error did not trigger helper cleanup")
	}
	recover = true
	if err := b.Remove(context.Background(), Handle(runner.ID)); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(deleted, runner.ID) {
		t.Fatal("successful retry retained the runner")
	}
	if _, err := os.Stat(farm); !os.IsNotExist(err) {
		t.Fatalf("farm survived successful retry: %v", err)
	}
}

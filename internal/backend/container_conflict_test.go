package backend

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eyupio/zoomies/internal/store"
)

func TestContainerConflictRecovery(t *testing.T) {
	for _, scenario := range []string{"late sidecar", "late runner", "foreign", "missing labels", "wrong role", "active parent", "active runner", "gone", "repeated", "inspect error", "delete error", "cancel", "non conflict"} {
		t.Run(scenario, func(t *testing.T) {
			spec := jitSpec()
			sidecar := scenario != "late runner" && scenario != "active runner"
			cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{})
			name := containerName(spec.Name)
			if sidecar {
				cfg = buildDinDConfig(spec, dockerFlavor(), containerOptions{})
				name = dindName(name)
			}
			labels := cfg.Labels
			if scenario == "foreign" {
				labels[LabelRunnerID] = "somebody-else"
			}
			if scenario == "missing labels" {
				labels = nil
			}
			if scenario == "wrong role" {
				labels[LabelRole] = "other"
			}
			calls, deletes, starts := 0, 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newFakeEngine(t, map[string]http.HandlerFunc{
				"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
					calls++
					if scenario == "non conflict" {
						w.WriteHeader(500)
						return
					}
					if calls == 1 || scenario == "repeated" {
						writeJSON(w, 409, map[string]string{"message": "name already in use"})
						return
					}
					writeJSON(w, 201, map[string]string{"Id": "fresh"})
				},
				"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
					if scenario == "inspect error" {
						w.WriteHeader(500)
						return
					}
					if scenario == "gone" {
						w.WriteHeader(404)
						return
					}
					if r.PathValue("id") != name {
						if scenario == "active parent" {
							writeJSON(w, 200, ContainerInspect{ID: "parent", State: &ContainerState{Running: true}})
						} else {
							w.WriteHeader(404)
						}
						return
					}
					writeJSON(w, 200, ContainerInspect{ID: "stale-id", Config: &ContainerConfig{Labels: labels}, State: &ContainerState{Running: sidecar || scenario == "active runner", Status: "created"}})
				},
				"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
					deletes++
					if r.PathValue("id") != "stale-id" {
						t.Errorf("deleted by mutable name: %s", r.PathValue("id"))
					}
					if scenario == "delete error" {
						w.WriteHeader(500)
						return
					}
					if scenario == "cancel" {
						cancel()
					}
					w.WriteHeader(204)
				},
				"POST " + v + "/containers/{id}/start": func(w http.ResponseWriter, r *http.Request) { starts++; w.WriteHeader(204) },
			})
			b := dockerBackendFor(t, f, DockerOptions{})
			id, err := b.createWithConflictRecovery(ctx, spec, cfg, sidecar)
			switch scenario {
			case "late sidecar", "late runner", "gone":
				if err != nil || id != "fresh" || calls != 2 {
					t.Fatalf("result %s, %v, calls %d", id, err, calls)
				}
				want := 1
				if scenario == "gone" {
					want = 0
				}
				if deletes != want {
					t.Fatalf("deletes %d want %d", deletes, want)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("cancel: %v calls=%d", err, calls)
				}
			case "non conflict":
				if err == nil || calls != 1 || deletes != 0 {
					t.Fatalf("unexpected retry: %v calls=%d deletes=%d", err, calls, deletes)
				}
			default:
				if !errors.Is(err, ErrContainerConflict) || Fault(err) != store.FaultContainerConflict {
					t.Fatalf("wrong fault: %v", err)
				}
				if scenario == "repeated" {
					if calls != 3 || deletes != 2 {
						t.Fatalf("unbounded attempts: %d/%d", calls, deletes)
					}
				} else if scenario != "delete error" && deletes != 0 {
					t.Fatalf("unsafe deletion: %d", deletes)
				}
			}
			if starts != 0 {
				t.Fatal("recovery started an existing container")
			}
		})
	}
}

func TestForeignConflictSurvivesFailedCreateCleanup(t *testing.T) {
	spec := jitSpec()
	spec.DockerMode = store.DockerDinD
	creates, deletes := 0, 0
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/images/{ref...}": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"Id": "sha256:cached"})
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			if creates == 0 || !strings.HasSuffix(r.PathValue("id"), "-dind") {
				w.WriteHeader(404)
				return
			}
			writeJSON(w, 200, ContainerInspect{ID: "foreign", Config: &ContainerConfig{Labels: map[string]string{LabelManaged: "true", LabelRunnerID: "another-runner"}}})
		},
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			creates++
			writeJSON(w, 409, map[string]string{"message": "name already in use"})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) { deletes++; w.WriteHeader(204) },
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	_, err := b.CreateWithResult(context.Background(), spec)
	if !errors.Is(err, ErrContainerConflict) || creates != 1 || deletes != 0 {
		t.Fatalf("unsafe cleanup: %v, creates=%d deletes=%d", err, creates, deletes)
	}
}

func TestDinDConflictRecoveryStillWaitsForHealth(t *testing.T) {
	spec := jitSpec()
	cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
	calls, probes := 0, 0
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(409)
			} else {
				writeJSON(w, 201, map[string]string{"Id": "fresh"})
			}
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			switch r.PathValue("id") {
			case dindName(containerName(spec.Name)):
				writeJSON(w, 200, ContainerInspect{ID: "stale", Config: &ContainerConfig{Labels: cfg.Labels}})
			case "fresh":
				probes++
				writeJSON(w, 200, ContainerInspect{State: &ContainerState{Running: true, Health: &ContainerHealth{Status: "healthy"}}})
			default:
				w.WriteHeader(404)
			}
		},
		"DELETE " + v + "/containers/stale":     func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
		"POST " + v + "/containers/fresh/start": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	id, err := b.startDinD(context.Background(), spec, containerOptions{})
	if err != nil || id != "fresh" || probes != 1 {
		t.Fatalf("readiness skipped: %s %v probes=%d", id, err, probes)
	}
}

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
			// "gone" is the one scenario here that actually waits out this budget --
			// every other outcome returns before the deadline is ever read. 50ms cut
			// it close enough that a loaded race-detector run could spend that on the
			// create-then-inspect round trip alone and give up before the retry that
			// was meant to succeed, failing the fleet's own duplicate-agent advice on
			// a name that was never contested by one.
			b.nameRelease = 2 * time.Second
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
					// A name that is taken again by a container we own after every
					// removal is somebody else creating it, so the removals are
					// bounded even though waiting for a release is not.
					if calls != maxOwnedRemovals || deletes != maxOwnedRemovals {
						t.Fatalf("unbounded attempts: %d/%d", calls, deletes)
					}
					if !strings.Contains(err.Error(), "duplicate agents") {
						t.Fatalf("no duplicate-agent advice: %v", err)
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

// A container the daemon has been asked to remove keeps its name until the last
// of its filesystem has gone, which for a docker-in-docker sidecar is seconds
// rather than milliseconds. The create that follows must wait that out: failing
// the runner here costs a job over a name that was already on its way free.
func TestConflictRecoveryWaitsForADaemonToReleaseAName(t *testing.T) {
	spec := jitSpec()
	cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
	calls, deletes := 0, 0
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls <= 3 {
				writeJSON(w, 409, map[string]string{"message": `Conflict. The container name "/` + dindName(containerName(spec.Name)) + `" is already in use by container "9a499d77123e49cb3e03b58a15f2b6756cfbeb310233288e14082077b771501c".`})
				return
			}
			writeJSON(w, 201, map[string]string{"Id": "fresh"})
		},
		// The container is already gone: only its name is still indexed.
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) },
		"DELETE " + v + "/containers/{id}":   func(w http.ResponseWriter, r *http.Request) { deletes++; w.WriteHeader(204) },
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	b.nameRelease = 2 * time.Second

	id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, true)
	if err != nil || id != "fresh" {
		t.Fatalf("gave up on a name still being released: %s %v", id, err)
	}
	if calls != 4 || deletes != 0 {
		t.Fatalf("calls=%d deletes=%d", calls, deletes)
	}
}

// The name and the container behind it can disagree, and when they do an
// inspect by name reports nothing while the daemon still refuses the name. The
// conflict reply names the container holding it, and that ID is the only handle
// left to prove ownership with -- removal is still by inspected ID and still
// only of a container whose labels say it is ours.
func TestConflictRecoveryRemovesTheOccupantTheDaemonNamed(t *testing.T) {
	const occupant = "9a499d77123e49cb3e03b58a15f2b6756cfbeb310233288e14082077b771501c"
	for _, foreign := range []bool{false, true} {
		name := "owned"
		if foreign {
			name = "foreign"
		}
		t.Run(name, func(t *testing.T) {
			spec := jitSpec()
			cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
			labels := cfg.Labels
			if foreign {
				labels[LabelRunnerID] = "somebody-else"
			}
			dind := dindName(containerName(spec.Name))
			calls, deleted := 0, ""
			f := newFakeEngine(t, map[string]http.HandlerFunc{
				"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls == 1 {
						writeJSON(w, 409, map[string]string{"message": `Conflict. The container name "/` + dind + `" is already in use by container "` + occupant + `". You have to remove (or rename) that container to be able to reuse that name.`})
						return
					}
					writeJSON(w, 201, map[string]string{"Id": "fresh"})
				},
				"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
					if r.PathValue("id") != occupant {
						// Including the name itself: it resolves to nothing.
						w.WriteHeader(404)
						return
					}
					writeJSON(w, 200, ContainerInspect{ID: occupant, Name: "/" + dind, Config: &ContainerConfig{Labels: labels}, State: &ContainerState{Running: true}})
				},
				"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
					deleted = r.PathValue("id")
					w.WriteHeader(204)
				},
			})
			b := dockerBackendFor(t, f, DockerOptions{})
			b.nameRelease = 50 * time.Millisecond

			id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, true)
			if foreign {
				if !errors.Is(err, ErrContainerConflict) || deleted != "" {
					t.Fatalf("removed another workload's container: %v deleted=%q", err, deleted)
				}
				return
			}
			if err != nil || id != "fresh" || deleted != occupant {
				t.Fatalf("result %s, %v, deleted %q", id, err, deleted)
			}
		})
	}
}

// The two exhausted conflicts read differently on purpose: an operator sent to
// look for a duplicate agent finds none when the truth is that the daemon is
// still unlinking a container this host removed itself.
func TestExhaustedConflictSaysWhichConflictItIs(t *testing.T) {
	spec := jitSpec()
	cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
	inspects := 0
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 409, map[string]string{"message": "name already in use"})
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			// Ours on the first look, then gone but for its name.
			if inspects++; inspects > 1 {
				w.WriteHeader(404)
				return
			}
			writeJSON(w, 200, ContainerInspect{ID: "stale-id", Config: &ContainerConfig{Labels: cfg.Labels}, State: &ContainerState{Running: true}})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	b.nameRelease = 10 * time.Millisecond

	_, err := b.createWithConflictRecovery(context.Background(), spec, cfg, true)
	if !errors.Is(err, ErrContainerConflict) || Fault(err) != store.FaultContainerConflict {
		t.Fatalf("wrong fault: %v", err)
	}
	if !strings.Contains(err.Error(), "has not finished releasing the name") {
		t.Fatalf("does not say the name is still being released: %v", err)
	}
	if strings.Contains(err.Error(), "duplicate agents") {
		t.Fatalf("sends the operator after a duplicate agent that is not there: %v", err)
	}
}

func TestConflictOccupantReadsBothDaemonsWordings(t *testing.T) {
	for _, tc := range []struct{ message, want string }{
		{`Conflict. The container name "/zoomies-linux-x64-rascal-dind" is already in use by container "9a499d77123e49cb3e03b58a15f2b6756cfbeb310233288e14082077b771501c". You have to remove (or rename) that container to be able to reuse that name.`, "9a499d77123e49cb3e03b58a15f2b6756cfbeb310233288e14082077b771501c"},
		{`creating container storage: the container name "zoomies-linux-x64-rascal-dind" is already in use by 4b2a91c7de10. You have to remove that container to be able to reuse that name`, "4b2a91c7de10"},
		// A name is not an ID, and an unrecognised wording names nothing.
		{`the container name "zoomies-runner" is already in use by zoomies-runner`, ""},
		{"name already in use", ""},
		{"", ""},
	} {
		if got := conflictOccupant(tc.message); got != tc.want {
			t.Errorf("conflictOccupant(%q) = %q, want %q", tc.message, got, tc.want)
		}
	}
}

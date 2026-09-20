package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
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

// The tests from here down script a daemon by attempt counts, never by time.
// Every decision -- which create attempt is left unanswered, from which one the
// name is refused, how many inspects of the occupant answer 404 before the
// daemon has registered it -- is taken under one lock on the request that
// arrives, so nothing depends on when a handler the client hung up on wakes,
// and the assertions are about outcomes and request counts, never elapsed time.

// occupantID is the container a 409 names in these tests: a full ID, as Docker
// prints it.
const occupantID = "446231582fb9a5d3c2e1f0b9a8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9"

// shortenHeaderTimeout stands in for a daemon that takes longer than
// responseHeaderTimeout to answer: the transport gives up on an unanswered
// request after d. It has to run before the first request, which is when the
// transport reads it.
func shortenHeaderTimeout(t *testing.T, b *DockerBackend, d time.Duration) {
	t.Helper()
	tr, ok := b.api.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the client's transport is %T, not the *http.Transport the timeout lives on", b.api.http.Transport)
	}
	tr.ResponseHeaderTimeout = d
}

// conflict409 is the daemon's refusal of a name, in Docker's words.
func conflict409(name, occupant string) map[string]string {
	return map[string]string{"message": `Conflict. The container name "/` + name + `" is already in use by container "` + occupant + `". You have to remove (or rename) that container to be able to reuse that name.`}
}

// echoInspect is what a daemon answers for a container it created from body
// and never started: the image, labels and network mode it was asked for, with
// an empty mode stored as the daemon spells it.
func echoInspect(id, name string, body ContainerCreateRequest, mode string) ContainerInspect {
	if mode == "" {
		mode = "default"
	}
	return ContainerInspect{
		ID:         id,
		Name:       "/" + name,
		Config:     &ContainerConfig{Image: body.Image, Labels: body.Labels},
		HostConfig: &HostConfig{NetworkMode: mode},
		State:      &ContainerState{Status: "created"},
	}
}

// slowCreateDaemon is a fake daemon for the timed-out-create scenarios. It
// echoes back what it was sent, as a real one would: the occupant it registers
// carries the image, labels and network mode of the create body it decoded.
type slowCreateDaemon struct {
	name     string // the contested container name
	occupant string // the ID its 409 names
	// unansweredCreates is how many create attempts, from the first, it never
	// answers; the client's header timeout ends each of them.
	unansweredCreates int
	// unansweredInspects is how many inspects of the occupant, from the first,
	// it never answers.
	unansweredInspects int
	// unregistered is how many answered inspects of the occupant say 404 before
	// the daemon has registered it.
	unregistered int
	// mode is the network mode the registered occupant reports; empty is what a
	// daemon stores for an empty request.
	mode string
	// shape, when set, adjusts the registered occupant before it is answered.
	shape func(*ContainerInspect)
	// neverLanded makes every answered create a 201: the create the client gave
	// up on never reached the daemon.
	neverLanded bool
	// freshAfterRemoval makes a create answer 201 once the occupant has been
	// removed, as a daemon whose name is free again does.
	freshAfterRemoval bool
	// onInspect runs under the lock on each inspect of the occupant.
	onInspect func(attempt int)

	mu       sync.Mutex
	creates  int
	refused  int
	inspects int
	deletes  []string
	starts   []string
	body     ContainerCreateRequest
}

func (d *slowCreateDaemon) routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
			// The body is drained before anything else, and in particular
			// before a handler blocks: net/http only starts the read that
			// notices the client hanging up once the body has been consumed,
			// and a handler blocked with the body unread never wakes, which
			// hangs the fake's Close and the test with it.
			var body ContainerCreateRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			// A create under any other name is a fallback name, which this
			// backend must never invent; answering it 500 fails the call.
			if got := r.Form.Get("name"); got != d.name {
				w.WriteHeader(500)
				return
			}
			d.mu.Lock()
			d.creates++
			attempt := d.creates
			d.body = body
			freed := d.neverLanded || (d.freshAfterRemoval && slices.Contains(d.deletes, d.occupant))
			refuse := attempt > d.unansweredCreates && !freed
			if refuse {
				d.refused++
			}
			d.mu.Unlock()
			switch {
			case attempt <= d.unansweredCreates:
				<-r.Context().Done()
			case refuse:
				writeJSON(w, 409, conflict409(d.name, d.occupant))
			default:
				writeJSON(w, 201, map[string]string{"Id": "fresh"})
			}
		},
		"GET " + v + "/containers/{id}/json": func(w http.ResponseWriter, r *http.Request) {
			if ref := r.PathValue("id"); ref != d.occupant && ref != d.name {
				// Everything else, a sidecar's parent runner included.
				w.WriteHeader(404)
				return
			}
			d.mu.Lock()
			d.inspects++
			attempt := d.inspects
			if d.onInspect != nil {
				d.onInspect(attempt)
			}
			body := d.body
			removed := slices.Contains(d.deletes, d.occupant)
			started := slices.Contains(d.starts, d.occupant)
			d.mu.Unlock()
			switch {
			case attempt <= d.unansweredInspects:
				<-r.Context().Done()
			case removed, attempt-d.unansweredInspects <= d.unregistered:
				w.WriteHeader(404)
			default:
				insp := echoInspect(d.occupant, d.name, body, d.mode)
				if started {
					insp.State = &ContainerState{Status: "running", Running: true, Health: &ContainerHealth{Status: "healthy"}}
				}
				if d.shape != nil {
					d.shape(&insp)
				}
				writeJSON(w, 200, insp)
			}
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) {
			d.mu.Lock()
			d.deletes = append(d.deletes, r.PathValue("id"))
			d.mu.Unlock()
			w.WriteHeader(204)
		},
		"POST " + v + "/containers/{id}/start": func(w http.ResponseWriter, r *http.Request) {
			d.mu.Lock()
			d.starts = append(d.starts, r.PathValue("id"))
			d.mu.Unlock()
			w.WriteHeader(204)
		},
	}
}

// counts reads the tallies once the call under test has returned.
func (d *slowCreateDaemon) counts() (creates, refused, inspects int, deletes []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.creates, d.refused, d.inspects, slices.Clone(d.deletes)
}

// The incident this file now designs out began with a create the daemon did
// not answer inside the header timeout. The daemon does not stop a create
// because the client stopped waiting, so the only thing that says whether it
// landed is asking again. A 409 naming a container that inspects as absent is
// that create still being registered, and once it can be inspected it is ours
// to start rather than to remove and ask for a second time -- whatever the
// daemon calls the network an empty request was stored with.
func TestATimedOutCreateIsAskedAgainAndTheContainerItLeftIsAdopted(t *testing.T) {
	for _, tc := range []struct {
		name, mode  string
		neverLanded bool
	}{
		{"docker stores an empty mode as default", "default", false},
		{"older docker stores it as bridge", "bridge", false},
		{"podman stores it as pasta", "pasta", false},
		{"the first create never landed", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := jitSpec()
			cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
			name := containerName(spec.Name)
			d := &slowCreateDaemon{name: name, occupant: occupantID, unansweredCreates: 1, unregistered: 2, mode: tc.mode, neverLanded: tc.neverLanded}
			f := newFakeEngine(t, d.routes())
			b := dockerBackendFor(t, f, DockerOptions{})
			shortenHeaderTimeout(t, b, 300*time.Millisecond)
			// Short enough that the release budget alone would have failed
			// this: the settle window is what carries it.
			b.nameRelease = 10 * time.Millisecond
			b.nameSettle = 10 * time.Second

			id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, false)
			if err != nil {
				t.Fatalf("a create the daemon was slow to finish failed the runner: %v", err)
			}
			creates, refused, _, deletes := d.counts()
			if len(deletes) != 0 {
				t.Fatalf("removed %v instead of adopting the container the daemon finished", deletes)
			}
			if _, live := b.abandonedCreateAt(name); live {
				t.Fatal("the abandoned-create record outlived the create it was about")
			}
			if tc.neverLanded {
				if id != "fresh" || creates != 2 || refused != 0 {
					t.Fatalf("id=%s creates=%d refused=%d; want the re-ask's 201 after exactly one unanswered create", id, creates, refused)
				}
				return
			}
			if id != occupantID {
				t.Fatalf("id=%s, want the occupant %s adopted", id, shortID(occupantID))
			}
			if creates != 4 || refused != 3 {
				t.Fatalf("creates=%d refused=%d; want one unanswered create, then a 409 for each of the two 404 inspects and one for the inspect that adopted", creates, refused)
			}
		})
	}
}

// How long a name nobody can inspect is waited on, and how the wait fails,
// comes from the evidence. A create of ours on record gets the settle window
// measured from the record and fails as busy, which the agent retries and the
// scheduler steers away from; no record gets the release budget and the
// conflict finding it always was. A record whose window has passed is waited
// on for nothing: the agent's retries share one window rather than each
// opening a fresh one.
func TestASettlingNameIsWaitedOnForTheWindowTheEvidenceAllows(t *testing.T) {
	const never = 1 << 20
	for _, tc := range []struct {
		name         string
		record       bool
		recordAge    time.Duration
		settle       time.Duration
		unregistered int
		adopted      bool
		fault        store.FaultKind
	}{
		{"a live record is waited out and the container adopted", true, 0, 10 * time.Second, 3, true, ""},
		{"a live record the daemon never honours is busy", true, 0, 300 * time.Millisecond, never, false, store.FaultBackendBusy},
		{"no record is the conflict it always was", false, 0, 10 * time.Second, never, false, store.FaultContainerConflict},
		{"a record whose window has passed is not waited on again", true, 2 * time.Second, time.Second, never, false, store.FaultBackendBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := jitSpec()
			cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
			name := containerName(spec.Name)
			d := &slowCreateDaemon{name: name, occupant: occupantID, unregistered: tc.unregistered}
			f := newFakeEngine(t, d.routes())
			b := dockerBackendFor(t, f, DockerOptions{})
			b.nameRelease = 10 * time.Millisecond
			b.nameSettle = tc.settle
			if tc.record {
				b.recordAbandonedCreate(name, time.Now().Add(-tc.recordAge))
			}

			id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, false)
			creates, _, _, deletes := d.counts()
			if len(deletes) != 0 {
				t.Fatalf("removed %v while nothing could be inspected", deletes)
			}
			if tc.adopted {
				if err != nil || id != occupantID {
					t.Fatalf("id=%s err=%v; want the occupant adopted once it became inspectable", id, err)
				}
				if creates != tc.unregistered+1 {
					t.Fatalf("creates=%d, want one per 404 inspect and one more to adopt", creates)
				}
				return
			}
			if got := Fault(err); got != tc.fault {
				t.Fatalf("fault %q, want %q: %v", got, tc.fault, err)
			}
			if creates < 1 {
				t.Fatal("no create was attempted")
			}
			if tc.fault == store.FaultContainerConflict {
				if !strings.Contains(err.Error(), "duplicate agents") {
					t.Fatalf("no duplicate-agent advice for a name with no create of ours behind it: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrDaemonBusy) || errors.Is(err, ErrContainerConflict) || errors.Is(err, ErrUnavailable) {
				t.Fatalf("a slow daemon must be busy and nothing else: %v", err)
			}
			if msg := err.Error(); !strings.Contains(msg, "too slow to create containers") || !strings.Contains(msg, name) || strings.Contains(msg, "duplicate agents") {
				t.Fatalf("the operator is not told it is the host's load: %v", err)
			}
			if !strings.Contains(err.Error(), "is already in use by container") {
				t.Fatalf("the daemon's own words were lost: %v", err)
			}
			if tc.recordAge > tc.settle && creates != 1 {
				t.Fatalf("creates=%d; a window that has already passed must not be waited on again", creates)
			}
		})
	}
}

// Adoption is for the container this call would otherwise make again, and for
// nothing else. Whatever fails the test is removed and recreated under the same
// name as before -- never retained, or the loop would spin on our own
// container. A running sidecar is the one running thing that is removable: a
// running runner is retained, as it always has been.
func TestOnlyAMatchingNeverStartedContainerIsAdopted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sidecar  bool
		wantMode string // the network mode this call asks for
		shape    func(*ContainerInspect)
		adopted  bool
	}{
		{"a runner bound to a sidecar that has gone", false, "container:new", func(i *ContainerInspect) { i.HostConfig.NetworkMode = "container:old" }, false},
		{"a different image", false, "", func(i *ContainerInspect) { i.Config.Image = "ghcr.io/acme/runner:1" }, false},
		{"an empty request mode stored as default", false, "", func(i *ContainerInspect) { i.HostConfig.NetworkMode = "default" }, true},
		{"an empty request mode stored as bridge", false, "", func(i *ContainerInspect) { i.HostConfig.NetworkMode = "bridge" }, true},
		{"no host config at all", false, "", func(i *ContainerInspect) { i.HostConfig = nil }, false},
		{"a container that has exited", false, "", func(i *ContainerInspect) { i.State = &ContainerState{Status: "exited", ExitCode: 1} }, false},
		{"a sidecar that is running", true, "", func(i *ContainerInspect) { i.State = &ContainerState{Status: "running", Running: true} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := jitSpec()
			name := containerName(spec.Name)
			var cfg ContainerCreateRequest
			if tc.sidecar {
				name = dindName(name)
				cfg = buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage})
			} else {
				cfg = buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), NetworkMode: tc.wantMode})
			}
			d := &slowCreateDaemon{name: name, occupant: occupantID, shape: tc.shape, freshAfterRemoval: true}
			f := newFakeEngine(t, d.routes())
			b := dockerBackendFor(t, f, DockerOptions{})
			b.nameRelease = 10 * time.Millisecond

			id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, tc.sidecar)
			if err != nil {
				t.Fatalf("a container of our own was neither adopted nor replaced: %v", err)
			}
			creates, _, _, deletes := d.counts()
			if tc.adopted {
				if id != occupantID || len(deletes) != 0 || creates != 1 {
					t.Fatalf("id=%s deletes=%v creates=%d; want the occupant adopted untouched", id, deletes, creates)
				}
				return
			}
			if id != "fresh" || !slices.Equal(deletes, []string{occupantID}) || creates != 2 {
				t.Fatalf("id=%s deletes=%v creates=%d; want the occupant removed by ID and a fresh create under the same name", id, deletes, creates)
			}
		})
	}
}

// An inspect the daemon did not answer says nothing about who holds the name,
// so it is not the "could not safely recover" conflict an unverifiable answer
// is: the loop keeps waiting under the same budget, and a daemon that never
// answers fails the runner as busy -- the host is the problem, and nothing was
// removed on the strength of a guess.
func TestAnInspectTheDaemonDidNotAnswerIsWaitedOnNotCalledAConflict(t *testing.T) {
	for _, tc := range []struct {
		name       string
		unanswered int
		release    time.Duration
		adopted    bool
	}{
		{"the second inspect answers and the container is adopted", 1, 5 * time.Second, true},
		{"no inspect ever answers", 1 << 20, 500 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := jitSpec()
			cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
			d := &slowCreateDaemon{name: containerName(spec.Name), occupant: occupantID, unansweredInspects: tc.unanswered}
			f := newFakeEngine(t, d.routes())
			b := dockerBackendFor(t, f, DockerOptions{})
			shortenHeaderTimeout(t, b, 250*time.Millisecond)
			b.nameRelease = tc.release

			id, err := b.createWithConflictRecovery(context.Background(), spec, cfg, false)
			_, _, inspects, deletes := d.counts()
			if len(deletes) != 0 {
				t.Fatalf("removed %v on the strength of an inspect that never answered", deletes)
			}
			if inspects < 1 {
				t.Fatal("the occupant was never inspected")
			}
			if tc.adopted {
				if err != nil || id != occupantID {
					t.Fatalf("id=%s err=%v; want the occupant adopted once an inspect answered", id, err)
				}
				return
			}
			if Fault(err) != store.FaultBackendBusy || errors.Is(err, ErrContainerConflict) {
				t.Fatalf("an unanswered inspect was reported as something other than a busy daemon: %v", err)
			}
			if !strings.Contains(err.Error(), "could not establish who holds container name") {
				t.Fatalf("the message does not say what could not be learned: %v", err)
			}
		})
	}
}

// The caller's context bounds every wait. A deadline that lands inside the
// settle wait was spent on the daemon, so it is busy and says so in the
// slow-daemon wording; a cancellation is the caller giving up and is handed
// back as itself; and a start-before or credential expiry already behind us is
// waited on for nothing, since waiting can only produce a runner the agent
// will refuse to start.
func TestTheCallersContextAndDeadlinesEndTheSettleWait(t *testing.T) {
	settling := func(t *testing.T, ctx context.Context, spec Spec, hook func(int)) (*slowCreateDaemon, error) {
		t.Helper()
		cfg := buildRunnerConfig(spec, dockerFlavor(), containerOptions{Now: time.Now()})
		name := containerName(spec.Name)
		d := &slowCreateDaemon{name: name, occupant: occupantID, unregistered: 1 << 20, onInspect: hook}
		f := newFakeEngine(t, d.routes())
		b := dockerBackendFor(t, f, DockerOptions{})
		b.nameSettle = time.Minute
		b.recordAbandonedCreate(name, time.Now())
		_, err := b.createWithConflictRecovery(ctx, spec, cfg, false)
		return d, err
	}

	t.Run("a deadline is spent on the daemon", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		d, err := settling(t, ctx, jitSpec(), nil)
		if Fault(err) != store.FaultBackendBusy || !errors.Is(err, ErrDaemonBusy) || errors.Is(err, ErrContainerConflict) || errors.Is(err, ErrUnavailable) {
			t.Fatalf("a deadline spent waiting on the daemon must read as busy and nothing else: %v", err)
		}
		if !strings.Contains(err.Error(), "too slow to create containers") || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the exit does not carry the slow-daemon wording and the deadline: %v", err)
		}
		if _, _, _, deletes := d.counts(); len(deletes) != 0 {
			t.Fatalf("removed %v", deletes)
		}
	})

	t.Run("a cancellation is the caller's own", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d, err := settling(t, ctx, jitSpec(), func(int) { cancel() })
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrContainerConflict) || errors.Is(err, ErrDaemonBusy) {
			t.Fatalf("a cancelled wait must come back as the cancellation, untagged: %v", err)
		}
		if _, _, _, deletes := d.counts(); len(deletes) != 0 {
			t.Fatalf("removed %v", deletes)
		}
	})

	t.Run("a start-before or credential expiry already passed", func(t *testing.T) {
		for _, which := range []string{"start before", "credential expiry"} {
			spec := jitSpec()
			if which == "start before" {
				spec.StartBefore = time.Now().Add(-time.Second)
			} else {
				spec.Credentials.ExpiresAt = time.Now().Add(-time.Second)
			}
			d, err := settling(t, context.Background(), spec, nil)
			creates, _, _, deletes := d.counts()
			if Fault(err) != store.FaultBackendBusy || errors.Is(err, ErrContainerConflict) {
				t.Fatalf("%s: fault %q: %v", which, Fault(err), err)
			}
			if creates != 1 || len(deletes) != 0 {
				t.Fatalf("%s: creates=%d deletes=%v; want one 409 and no wait", which, creates, deletes)
			}
		}
	})
}

// After this agent has removed its own container, a name still held is the
// daemon unlinking, and that stays the conflict it has always been -- even when
// a create of the name was on record before the removal, because the removal
// is what finished that create. The slow-daemon wording is never given for a
// create that did in fact finish.
func TestAStillHeldNameAfterOurOwnRemovalIsAConflictEvenWithACreateOnRecord(t *testing.T) {
	spec := jitSpec()
	cfg := buildDinDConfig(spec, dockerFlavor(), containerOptions{Now: time.Now(), DinDImage: DefaultDinDImage})
	name := dindName(containerName(spec.Name))
	d := &slowCreateDaemon{name: name, occupant: occupantID, shape: func(i *ContainerInspect) {
		i.State = &ContainerState{Status: "running", Running: true}
	}}
	f := newFakeEngine(t, d.routes())
	b := dockerBackendFor(t, f, DockerOptions{})
	b.nameRelease = 10 * time.Millisecond
	b.nameSettle = time.Minute
	b.recordAbandonedCreate(name, time.Now())

	_, err := b.createWithConflictRecovery(context.Background(), spec, cfg, true)
	if !errors.Is(err, ErrContainerConflict) || Fault(err) != store.FaultContainerConflict || errors.Is(err, ErrDaemonBusy) {
		t.Fatalf("wrong fault: %v", err)
	}
	if !strings.Contains(err.Error(), "has not finished releasing the name") {
		t.Fatalf("does not say the name is still being released: %v", err)
	}
	if strings.Contains(err.Error(), "too slow to create containers") || strings.Contains(err.Error(), "duplicate agents") {
		t.Fatalf("sends the operator after the wrong thing: %v", err)
	}
	if _, _, _, deletes := d.counts(); !slices.Equal(deletes, []string{occupantID}) {
		t.Fatalf("deletes=%v, want our own container removed once by ID", deletes)
	}
	if _, live := b.abandonedCreateAt(name); live {
		t.Fatal("the record survived the removal that finished the create")
	}
}

// The record is what turns a 409 for an uninspectable name into a busy exit,
// so it has to go when the create it is about has been seen through by any
// path -- including a Remove of the runner, which takes the sidecar and its
// name with it -- and it must not go for anyone else's name.
func TestRemoveForgetsTheAbandonedCreatesOfTheRunnerAndItsSidecar(t *testing.T) {
	f := newFakeEngine(t, map[string]http.HandlerFunc{
		"GET " + v + "/containers/c1/json": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, ContainerInspect{ID: "c1", Config: &ContainerConfig{Labels: map[string]string{LabelName: "runner-1"}}})
		},
		"DELETE " + v + "/containers/{id}": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
	})
	b := dockerBackendFor(t, f, DockerOptions{})
	now := time.Now()
	for _, n := range []string{"runner-1", "runner-1-dind", "runner-2"} {
		b.recordAbandonedCreate(n, now)
	}
	if err := b.Remove(context.Background(), "c1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, n := range []string{"runner-1", "runner-1-dind"} {
		if _, live := b.abandonedCreateAt(n); live {
			t.Errorf("%s: the record survived Remove", n)
		}
	}
	if _, live := b.abandonedCreateAt("runner-2"); !live {
		t.Error("Remove forgot another runner's record")
	}
}

// A second timeout on the same name must not move the window the settle wait
// is measured from, or a daemon that keeps outlasting the header timeout would
// be waited on for ever; a record older than the memory is neither live nor
// kept; and a record that is forgotten stays forgotten.
func TestAnAbandonedCreateIsRememberedOnceAndForgottenWhenSeenThrough(t *testing.T) {
	b := &DockerBackend{}
	first := time.Now().Add(-time.Minute)
	b.recordAbandonedCreate("held", first)
	b.recordAbandonedCreate("held", time.Now())
	if at, ok := b.abandonedCreateAt("held"); !ok || !at.Equal(first) {
		t.Fatalf("a second timeout moved the record to %v; want %v kept", at, first)
	}
	b.recordAbandonedCreate("stale", time.Now().Add(-abandonedCreateMemory))
	if _, ok := b.abandonedCreateAt("stale"); ok {
		t.Fatal("a record older than the memory reads as live")
	}
	b.recordAbandonedCreate("other", time.Now())
	b.abandonedMu.Lock()
	_, kept := b.abandoned["stale"]
	b.abandonedMu.Unlock()
	if kept {
		t.Fatal("a stale record was not pruned by the next write")
	}
	b.forgetAbandonedCreate("held", "other")
	for _, n := range []string{"held", "other"} {
		if _, ok := b.abandonedCreateAt(n); ok {
			t.Fatalf("%s: a forgotten record is still live", n)
		}
	}
}

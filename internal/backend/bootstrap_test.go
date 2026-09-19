package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDinDWaitsForHealthyDaemonBeforeReleasingStartup(t *testing.T) {
	func() {
		probes := 0
		f := newFakeEngine(t, map[string]http.HandlerFunc{
			"POST " + v + "/containers/create": func(w http.ResponseWriter, r *http.Request) {
				var cfg ContainerCreateRequest
				if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
					t.Error(err)
				}
				if cfg.Healthcheck == nil || !slices.Equal(cfg.Healthcheck.Test, []string{"CMD", "docker", "--host=tcp://127.0.0.1:2375", "info"}) || cfg.Healthcheck.Timeout != 5*time.Second {
					t.Errorf("healthcheck = %+v", cfg.Healthcheck)
				}
				writeJSON(w, 201, map[string]string{"Id": "d1"})
			},
			"POST " + v + "/containers/d1/start": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
			"GET " + v + "/containers/d1/json": func(w http.ResponseWriter, r *http.Request) {
				probes++
				health := "starting"
				if probes == 2 {
					health = "unhealthy"
				} // A slow boot may recover within its budget.
				if probes >= 3 {
					health = "healthy"
				}
				writeJSON(w, 200, ContainerInspect{State: &ContainerState{Running: true, Health: &ContainerHealth{Status: health}}})
			},
		})
		b := dockerBackendFor(t, f, DockerOptions{})
		start := time.Now()
		id, err := b.startDinD(context.Background(), jitSpec(), containerOptions{})
		if err != nil || id != "d1" || probes != 3 || time.Since(start) < 2*time.Second {
			t.Fatalf("id=%s err=%v probes=%d elapsed=%s", id, err, probes, time.Since(start))
		}
	}()
}

func TestDinDReadinessIsBoundedAndReportsEarlyExit(t *testing.T) {
	for _, scenario := range []string{"timeout", "cancel", "oom", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f := newFakeEngine(t, map[string]http.HandlerFunc{
					"POST " + v + "/containers/create":   func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 201, map[string]string{"Id": "d1"}) },
					"POST " + v + "/containers/d1/start": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) },
					"GET " + v + "/containers/d1/json": func(w http.ResponseWriter, r *http.Request) {
						if scenario == "cancel" {
							cancel()
						}
						if scenario == "unavailable" {
							writeJSON(w, 500, map[string]string{"message": "busy"})
							return
						}
						state := &ContainerState{Running: true}
						if scenario == "oom" {
							state = &ContainerState{Status: "exited", OOMKilled: true, ExitCode: 137}
						}
						writeJSON(w, 200, ContainerInspect{State: state})
					},
				})
				b := dockerBackendFor(t, f, DockerOptions{})
				spec := jitSpec()
				spec.Env["ZOOMIES_DOCKER_WAIT"] = "2"
				start := time.Now()
				_, err := b.startDinD(ctx, spec, containerOptions{})
				if err == nil {
					t.Fatal("unready daemon accepted")
				}
				switch scenario {
				case "oom":
					if !strings.Contains(err.Error(), "OOM killed: true") {
						t.Fatal(err)
					}
				case "cancel":
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				default:
					if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
						t.Fatalf("unbounded readiness: %v, %s", err, time.Since(start))
					}
				}
			}()
		})
	}
}

func TestDinDRejectsInvalidReadinessBudgetBeforeCreating(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "3601", "2m", "+2"} {
		spec := jitSpec()
		spec.Env["ZOOMIES_DOCKER_WAIT"] = value
		b := &DockerBackend{}
		if _, err := b.startDinD(context.Background(), spec, containerOptions{}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

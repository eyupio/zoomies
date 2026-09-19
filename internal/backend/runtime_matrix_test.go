package backend

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Protocol fixtures test reported capabilities, not real-engine certification.
func TestRuntimeCapabilityMatrix(t *testing.T) {
	for _, runtime := range []string{"docker", "podman"} {
		for _, rootless := range []bool{false, true} {
			for _, cgroup := range []string{"1", "2"} {
				t.Run(fmt.Sprintf("%s/rootless=%t/cgroup=%s", runtime, rootless, cgroup), func(t *testing.T) {
					cpu := !rootless || cgroup == "2"
					f := newFakeEngine(t, map[string]http.HandlerFunc{
						"GET " + v + "/_ping":   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
						"GET " + v + "/version": func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, VersionInfo{Version: "fixture"}) },
						"GET " + v + "/info": func(w http.ResponseWriter, r *http.Request) {
							security := []string{}
							if rootless {
								security = append(security, "name=rootless")
							}
							writeJSON(w, 200, map[string]any{"SecurityOptions": security, "CgroupVersion": cgroup, "CpuCfsQuota": cpu, "MemoryLimit": true, "PidsLimit": true})
						},
					})
					var info Info
					if runtime == "docker" {
						info = dockerBackendFor(t, f, DockerOptions{}).Probe(context.Background())
					} else {
						b, err := NewPodman(DockerOptions{Host: "tcp://" + f.Listener.Addr().String(), Logger: quietLogger()})
						if err != nil {
							t.Fatal(err)
						}
						info = b.Probe(context.Background())
					}
					if !info.Available || info.Rootless != rootless || !info.Limits.Known || info.Limits.CPU != cpu || !info.Limits.Memory {
						t.Fatalf("capabilities: %+v", info)
					}
					if info.SupportsDinD != (runtime == "docker") {
						t.Fatalf("DinD: %+v", info)
					}
				})
			}
		}
	}
}

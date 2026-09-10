package installer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerUpgradeRestoresTheOldContainerWhenTheReplacementCannotStart(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed start"}[fail], func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				calls = append(calls, r.Method+" "+path+"?"+r.URL.RawQuery)
				switch {
				case strings.HasSuffix(path, "/zoomies/json"):
					_ = json.NewEncoder(w).Encode(map[string]any{"Id": "old", "Name": "/zoomies", "Config": map[string]any{"Image": stockAgentRepository + ":old"}, "HostConfig": map[string]any{"AutoRemove": false}, "State": map[string]any{"Running": true}})
				case strings.HasSuffix(path, "/zoomies-before-upgrade/json"):
					w.WriteHeader(404)
				case strings.HasSuffix(path, "/containers/create"):
					w.WriteHeader(201)
					_, _ = w.Write([]byte(`{"Id":"new"}`))
				case strings.HasSuffix(path, "/new/start") && fail:
					w.WriteHeader(500)
					_, _ = w.Write([]byte(`{"message":"cannot start"}`))
				case strings.HasSuffix(path, "/new/json"):
					_, _ = w.Write([]byte(`{"Id":"new","State":{"Running":true}}`))
				default:
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			opts, rec := upgradeFixture(t, DeploymentDocker)
			opts.DockerHost = server.URL
			opts.run = func(context.Context, string, ...string) (string, error) { return "", nil }
			err := Upgrade(context.Background(), opts)
			if (err != nil) != fail {
				t.Fatalf("fail=%v: %v", fail, err)
			}
			all := strings.Join(calls, "\n")
			if !strings.Contains(all, "/old/stop?t=600") || !strings.Contains(all, "name=zoomies-before-upgrade") {
				t.Fatalf("missing graceful replacement: %s", all)
			}
			if fail {
				if !strings.Contains(all, "/old/rename?name=zoomies") || !strings.Contains(all, "/old/start?") || !strings.Contains(all, "/new?force=1&v=0") {
					t.Fatalf("rollback missing: %s", all)
				}
				for _, call := range calls {
					if strings.HasPrefix(call, "DELETE ") && strings.Contains(call, "/containers/old?") {
						t.Fatal("removed rollback container")
					}
				}
			} else if !strings.Contains(all, "/old?v=0") {
				t.Fatalf("old container not cleaned up safely: %s", all)
			}
			stored, _ := ReadDeploymentRecord(opts.ConfigDir)
			if fail && stored.Image != rec.Image {
				t.Fatal("failed upgrade recorded as successful")
			}
		})
	}
}

package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestContainerReplacementKeepsCredentialsPortsVolumesAndIsolation(t *testing.T) {
	var created map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/json") {
			_, _ = w.Write([]byte(`{"Id":"0123456789abcdef","Name":"/zoomies","Config":{"Image":"agent:old","Env":["TOKEN=keep-this"],"User":"65532","Healthcheck":{"Test":["NONE"]}},"HostConfig":{"AutoRemove":false,"Binds":["/socket:/socket"],"PortBindings":{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"8081"}]},"SecurityOpt":["no-new-privileges:true"],"CustomFutureOption":true},"Mounts":[{"Type":"volume","Name":"existing-data","Destination":"/var/lib/zoomies","RW":true}],"State":{"Running":true},"NetworkSettings":{"Networks":{"zoomies":{"Aliases":["zoomies","0123456789ab"],"IPAddress":"172.18.0.4","EndpointID":"old-endpoint","IPAMConfig":null}}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/create") {
			if r.URL.Query().Get("name") != "zoomies" {
				t.Error("wrong name")
			}
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"new"}`))
			return
		}
		if r.Method == http.MethodDelete {
			if r.URL.Query().Get("v") != "0" {
				t.Error("removed data volumes")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected request %s", r.URL)
		w.WriteHeader(500)
	}))
	defer server.Close()
	client, err := NewAPIClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.PrepareReplacement(context.Background(), "zoomies", "agent:new")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateReplacement(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if created["Image"] != "agent:new" || created["User"] != "65532" || !reflect.DeepEqual(created["Env"], []any{"TOKEN=keep-this"}) {
		t.Fatalf("lost configuration: %v", created)
	}
	host := created["HostConfig"].(map[string]any)
	if host["CustomFutureOption"] != true || host["PortBindings"] == nil || host["SecurityOpt"] == nil {
		t.Fatalf("lost runtime options: %v", host)
	}
	if !reflect.DeepEqual(host["Binds"], []any{"/socket:/socket", "existing-data:/var/lib/zoomies:rw"}) {
		t.Fatalf("lost data volume: %v", host)
	}
	endpoints := created["NetworkingConfig"].(map[string]any)["EndpointsConfig"].(map[string]any)
	network := endpoints["zoomies"].(map[string]any)
	if network["IPAddress"] != nil || network["EndpointID"] != nil || !reflect.DeepEqual(network["Aliases"], []any{"zoomies"}) {
		t.Fatalf("copied runtime network identity: %v", network)
	}
	if err := client.RemoveContainerKeepingVolumes(context.Background(), snapshot.ID, false); err != nil {
		t.Fatal(err)
	}
}

func TestAnAutomaticallyRemovedContainerIsRefusedBeforeAnUpgradeCanStopIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("preflight mutated a container")
		}
		_, _ = w.Write([]byte(`{"Id":"old","Config":{"Image":"old"},"HostConfig":{"AutoRemove":true}}`))
	}))
	defer server.Close()
	client, _ := NewAPIClient(server.URL)
	if _, err := client.PrepareReplacement(context.Background(), "old", "new"); err == nil {
		t.Fatal("accepted a container that would destroy the rollback copy when stopped")
	}
}

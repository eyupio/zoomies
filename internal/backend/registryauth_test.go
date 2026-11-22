package backend

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func credFor(server string) string {
	return base64.URLEncoding.EncodeToString([]byte(`{"username":"bot","password":"x","serveraddress":"` + server + `"}`))
}

func TestAuthForSendsACredentialOnlyToTheRegistryItNames(t *testing.T) {
	ghcr := credFor("ghcr.io")
	hub := credFor("https://index.docker.io/v1/")
	noServer := base64.StdEncoding.EncodeToString([]byte(`{"username":"bot","password":"x"}`))

	tests := []struct {
		name, auth, image string
		sent              bool
	}{
		{"private image on the credential's registry", ghcr, "ghcr.io/org/runner:1", true},
		{"registry name in a different case", credFor("GHCR.io"), "ghcr.io/org/runner:1", true},
		{"default dind sidecar goes anonymous", ghcr, "docker:27-dind", false},
		{"explicit Docker Hub name goes anonymous", ghcr, "registry-1.docker.io/library/docker:27-dind", false},
		{"another registry with a port goes anonymous", ghcr, "registry.example.com:5000/img:1", false},
		{"digest reference on the credential's registry", ghcr, "ghcr.io/org/runner@sha256:abc", true},
		{"Docker Hub credential reaches a Hub image", hub, "myorg/runner:1", true},
		{"Docker Hub credential reaches a library image", hub, "docker:27-dind", true},
		{"Docker Hub credential stays off ghcr.io", hub, "ghcr.io/org/runner:1", false},
		{"credential without serveraddress is sent as before", noServer, "docker:27-dind", true},
		{"unreadable credential is sent as before", "not base64 json", "docker:27-dind", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &DockerBackend{creds: newRegistryCredentials(tt.auth)}
			got := b.authFor(tt.image)
			if tt.sent && got != tt.auth {
				t.Fatalf("authFor(%q) = %q, want the credential", tt.image, got)
			}
			if !tt.sent && got != "" {
				t.Fatalf("authFor(%q) sent a credential, want an anonymous pull", tt.image)
			}
		})
	}
}

func TestAuthForWithNoCredentialIsAnonymous(t *testing.T) {
	if got := (&DockerBackend{}).authFor("ghcr.io/org/x"); got != "" {
		t.Fatalf("authFor = %q, want empty", got)
	}
}

func dockerConfig(auths string) string {
	return base64.StdEncoding.EncodeToString([]byte(`{"auths":` + auths + `}`))
}

func headerFields(t *testing.T, header string) map[string]string {
	t.Helper()
	var f map[string]string
	if err := json.Unmarshal(decodeBase64(header), &f); err != nil {
		t.Fatalf("header %q is not base64 JSON: %v", header, err)
	}
	return f
}

func TestEachRegistryGetsItsOwnCredentialFromADockerConfig(t *testing.T) {
	userPass := base64.StdEncoding.EncodeToString([]byte("bot:ghcr-token"))
	b := &DockerBackend{creds: newRegistryCredentials(dockerConfig(`{
		"ghcr.io": {"auth": "` + userPass + `"},
		"https://index.docker.io/v1/": {"username": "hubuser", "password": "hubpass"},
		"registry.example.com:5000": {"identitytoken": "tok"},
		"empty.example.com": {}
	}`))}

	ghcr := headerFields(t, b.authFor("ghcr.io/org/runner:1"))
	if ghcr["username"] != "bot" || ghcr["password"] != "ghcr-token" || ghcr["serveraddress"] != "ghcr.io" {
		t.Fatalf("ghcr.io credential = %v", ghcr)
	}
	hub := headerFields(t, b.authFor("docker:27-dind"))
	if hub["username"] != "hubuser" || hub["serveraddress"] != "docker.io" {
		t.Fatalf("Docker Hub credential = %v", hub)
	}
	if tok := headerFields(t, b.authFor("registry.example.com:5000/img:1")); tok["identitytoken"] != "tok" {
		t.Fatalf("identity token credential = %v", tok)
	}
	for _, image := range []string{"quay.io/org/img:1", "empty.example.com/img:1"} {
		if got := b.authFor(image); got != "" {
			t.Fatalf("authFor(%q) sent a credential, want an anonymous pull", image)
		}
	}
}

func TestAnEmptyAuthsMapPullsEverythingAnonymously(t *testing.T) {
	b := &DockerBackend{creds: newRegistryCredentials(dockerConfig(`{}`))}
	if got := b.authFor("ghcr.io/org/runner:1"); got != "" {
		t.Fatalf("authFor sent %q, want an anonymous pull", got)
	}
}

package backend

import (
	"encoding/base64"
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
			b := &DockerBackend{auth: tt.auth, authServer: credentialServer(tt.auth)}
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

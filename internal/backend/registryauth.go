package backend

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// registryHost is the registry an image reference is pulled from, lower-cased,
// with Docker Hub's several names folded into "docker.io". A reference whose
// first path element has no dot or colon (and is not "localhost") is a Docker
// Hub repository, which is how the daemon reads it too.
func registryHost(ref string) string {
	first, _, hasPath := strings.Cut(ref, "/")
	if !hasPath || !(strings.ContainsAny(first, ".:") || first == "localhost") {
		return "docker.io"
	}
	return normaliseRegistryHost(first)
}

func normaliseRegistryHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com":
		return "docker.io"
	}
	return host
}

// credentialServer reads the registry a base64 X-Registry-Auth value says it is
// for, from its "serveraddress". It returns "" when the value does not say, or
// cannot be read, so a credential written without one keeps being sent with
// every pull as it always was.
func credentialServer(auth string) string {
	auth = strings.TrimSpace(auth)
	var raw []byte
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(auth); err == nil {
			raw = b
			break
		}
	}
	var c struct {
		ServerAddress string `json:"serveraddress"`
	}
	if raw == nil || json.Unmarshal(raw, &c) != nil {
		return ""
	}
	addr := strings.TrimSpace(c.ServerAddress)
	if _, rest, ok := strings.Cut(addr, "://"); ok {
		addr = rest
	}
	addr, _, _ = strings.Cut(addr, "/")
	return normaliseRegistryHost(addr)
}

// authFor is the credential to send when pulling image. A credential that names
// its registry goes only to that registry: sent anywhere else it would be
// refused (Docker Hub answers 401 to a ghcr.io token rather than pulling
// anonymously, which stopped every dind runner from starting) and it would
// hand the token to a registry the operator never configured it for.
func (b *DockerBackend) authFor(image string) string {
	if b.auth == "" || b.authServer == "" || registryHost(image) == b.authServer {
		return b.auth
	}
	return ""
}

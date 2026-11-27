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

// registryCredentials is what agent.registry_auth holds, ready to answer "which
// credential does this image get?".
//
// The setting is one of two shapes, told apart by what it decodes to. The
// original is a single X-Registry-Auth value, {"username":...,"password":...},
// and it keeps working unchanged. The other is the "auths" map of
// ~/.docker/config.json, so an operator with a private runner image on one
// registry and a private sidecar on another can give each its own credential.
type registryCredentials struct {
	// single is the original value, sent as it is, and singleServer the registry
	// its serveraddress names ("" when it names none, so it goes with every
	// pull as it always did).
	single, singleServer string
	// byHost holds one ready X-Registry-Auth header value per registry from the
	// "auths" form; an image whose registry is not in it pulls anonymously.
	byHost map[string]string
}

func newRegistryCredentials(auth string) registryCredentials {
	auth = strings.TrimSpace(auth)
	if auth == "" {
		return registryCredentials{}
	}
	var doc struct {
		ServerAddress string `json:"serveraddress"`
		Auths         map[string]struct {
			Auth          string `json:"auth"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			IdentityToken string `json:"identitytoken"`
		} `json:"auths"`
	}
	raw := decodeBase64(auth)
	if raw == nil || json.Unmarshal(raw, &doc) != nil {
		return registryCredentials{single: auth}
	}
	if doc.Auths == nil {
		return registryCredentials{single: auth, singleServer: normaliseServerAddress(doc.ServerAddress)}
	}
	c := registryCredentials{byHost: map[string]string{}}
	for server, e := range doc.Auths {
		host := normaliseServerAddress(server)
		user, pass := e.Username, e.Password
		if e.Auth != "" {
			// The "auth" field is base64 of "user:password", as docker login writes it.
			if b := decodeBase64(e.Auth); b != nil {
				user, pass, _ = strings.Cut(string(b), ":")
			}
		}
		entry := map[string]string{"serveraddress": host}
		switch {
		case e.IdentityToken != "":
			entry["identitytoken"] = e.IdentityToken
		case user != "" || pass != "":
			entry["username"], entry["password"] = user, pass
		default:
			continue
		}
		b, _ := json.Marshal(entry)
		c.byHost[host] = base64.URLEncoding.EncodeToString(b)
	}
	return c
}

// forImage is the credential to send when pulling image. A credential that names
// its registry goes only to that registry: sent anywhere else it would be
// refused (Docker Hub answers 401 to a ghcr.io token rather than pulling
// anonymously, which stopped every dind runner from starting) and it would
// hand the token to a registry the operator never configured it for.
func (c registryCredentials) forImage(image string) string {
	if c.byHost != nil {
		return c.byHost[registryHost(image)]
	}
	if c.single == "" || c.singleServer == "" || registryHost(image) == c.singleServer {
		return c.single
	}
	return ""
}

func decodeBase64(s string) []byte {
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}

// normaliseServerAddress reduces a serveraddress or an "auths" key, which may
// carry a scheme and a path ("https://index.docker.io/v1/"), to a registry host.
func normaliseServerAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if _, rest, ok := strings.Cut(addr, "://"); ok {
		addr = rest
	}
	addr, _, _ = strings.Cut(addr, "/")
	return normaliseRegistryHost(addr)
}

func (b *DockerBackend) authFor(image string) string { return b.creds.forImage(image) }

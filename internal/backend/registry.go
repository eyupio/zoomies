package backend

import "strings"

// DockerHubRegistry is the registry an image reference with no registry part
// resolves to. It is named as docker.io rather than the index host the daemon
// actually dials, because that is what an operator allowing egress, or
// logging in, writes.
const DockerHubRegistry = "docker.io"

// RegistryHost is the registry an image reference is pulled from, by the
// daemon's own rule: the first path component is a registry only when it
// looks like a host -- it has a dot or a port, or is localhost -- and
// otherwise the image is on Docker Hub. "ubuntu", "library/ubuntu" and
// "eyupio/runner" are all docker.io; "ghcr.io/eyupio/runner" is ghcr.io and
// "registry.internal:5000/runner" keeps its port, because the port is part of
// what a firewall rule has to allow.
//
// It is what turns "the image would not pull" into "this host cannot reach
// ghcr.io", which is the sentence an operator can act on.
func RegistryHost(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	first, _, found := strings.Cut(image, "/")
	if !found {
		return DockerHubRegistry
	}
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		first = strings.ToLower(first)
		// Docker Hub's own hostnames are the same registry as a bare name.
		if first == "index.docker.io" || first == "registry-1.docker.io" {
			return DockerHubRegistry
		}
		return first
	}
	return DockerHubRegistry
}

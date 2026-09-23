package backend

import "testing"

// The registry is the thing a blocked egress rule or a missing login is
// about, so naming the wrong one sends an operator to fix a firewall for a
// host that was never the problem.
func TestRegistryHostNamesWhereAnImageIsPulledFrom(t *testing.T) {
	for _, tc := range []struct{ image, want string }{
		{"ubuntu", "docker.io"},
		{"ubuntu:24.04", "docker.io"},
		{"library/ubuntu", "docker.io"},
		{"eyupio/runner:latest", "docker.io"},
		{"docker.io/library/ubuntu", "docker.io"},
		{"index.docker.io/eyupio/runner", "docker.io"},
		{"ghcr.io/eyupio/zoomies-runner:ubuntu-24.04", "ghcr.io"},
		{"GHCR.IO/eyupio/runner", "ghcr.io"},
		{"ghcr.io/eyupio/runner@sha256:0123abcd", "ghcr.io"},
		{"registry.internal:5000/runner", "registry.internal:5000"},
		{"10.0.0.4:5000/team/runner:1", "10.0.0.4:5000"},
		{"localhost/runner", "localhost"},
		{"localhost:5000/runner", "localhost:5000"},
		{"", ""},
		{"  ", ""},
	} {
		if got := RegistryHost(tc.image); got != tc.want {
			t.Errorf("RegistryHost(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

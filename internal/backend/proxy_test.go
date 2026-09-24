package backend

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func proxyLookup(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

// A host behind a proxy configures its agent with one; a runner that did not
// inherit it would fail its first checkout with nothing pointing at why.
func TestContainerRunnersInheritTheAgentsProxy(t *testing.T) {
	spec := jitSpec()
	agent := map[string]string{
		"HTTPS_PROXY": "http://proxy.corp:3128",
		"no_proxy":    ".corp",
		"UNRELATED":   "must not leak",
	}
	o := containerOptions{Now: time.Now(), ProxyEnv: inheritedProxyEnv(proxyLookup(agent), spec.Env)}
	env := buildRunnerConfig(spec, dockerFlavor(), o).Env
	for _, want := range []string{"HTTPS_PROXY=http://proxy.corp:3128", "no_proxy=.corp"} {
		if !slices.Contains(env, want) {
			t.Errorf("runner env is missing %q: %v", want, env)
		}
	}
	for _, e := range env {
		if e == "UNRELATED=must not leak" {
			t.Errorf("runner inherited a variable that is not a proxy setting: %v", env)
		}
	}
}

// The pool is the more specific statement, and an empty value is how a pool
// says its runners must go direct although the host needs a proxy.
func TestAPoolsOwnProxyWinsOverTheAgents(t *testing.T) {
	spec := jitSpec()
	spec.Env = map[string]string{"HTTPS_PROXY": "", "HTTP_PROXY": "http://pool:8080"}
	agent := map[string]string{"HTTPS_PROXY": "http://agent:3128", "HTTP_PROXY": "http://agent:3128", "NO_PROXY": "localhost"}
	o := containerOptions{Now: time.Now(), ProxyEnv: inheritedProxyEnv(proxyLookup(agent), spec.Env)}

	for name, env := range map[string][]string{
		"runner":  buildRunnerConfig(spec, dockerFlavor(), o).Env,
		"sidecar": buildDinDConfig(spec, dockerFlavor(), o).Env,
	} {
		for _, e := range env {
			if e == "HTTPS_PROXY=http://agent:3128" || e == "HTTP_PROXY=http://agent:3128" {
				t.Errorf("%s: the agent's proxy overrode the pool's: %v", name, env)
			}
		}
		for _, want := range []string{"HTTPS_PROXY=", "HTTP_PROXY=http://pool:8080", "NO_PROXY=localhost"} {
			if !slices.Contains(env, want) {
				t.Errorf("%s env is missing %q: %v", name, want, env)
			}
		}
	}
}

// The docker-in-docker daemon pulls the images a job's builds name, so it needs
// the proxy too -- but none of the job's other variables.
func TestTheDockerSidecarGetsTheProxyAndNothingElseFromThePool(t *testing.T) {
	spec := jitSpec()
	o := containerOptions{Now: time.Now(), ProxyEnv: inheritedProxyEnv(proxyLookup(map[string]string{"http_proxy": "http://p:1"}), spec.Env)}
	env := buildDinDConfig(spec, dockerFlavor(), o).Env
	if !slices.Contains(env, "http_proxy=http://p:1") || !slices.Contains(env, "DOCKER_TLS_CERTDIR=") {
		t.Fatalf("sidecar env = %v, want the proxy and DOCKER_TLS_CERTDIR", env)
	}
	for _, e := range env {
		if e == "ZULU=last" || e == "ALPHA=first" {
			t.Errorf("sidecar received a pool variable meant for the job: %v", env)
		}
	}
}

func TestNoProxyInheritedWhenTheAgentHasNone(t *testing.T) {
	if got := inheritedProxyEnv(proxyLookup(nil), nil); len(got) != 0 {
		t.Fatalf("inheritedProxyEnv = %v, want nothing", got)
	}
}

// A runner behind a proxy is handed these variables, and the stock image gives
// it passwordless sudo because real workflows `sudo apt-get install`. sudo's
// default env_reset dropped every one of them, so the install timed out on a
// host whose runners could otherwise reach the internet. The image's sudoers
// entry keeps them; this holds it to the list the agent actually passes on.
func TestTheRunnerImageKeepsTheProxyThroughSudo(t *testing.T) {
	dockerfile := readFile(t, filepath.Join("..", "..", "deploy", "Dockerfile.runner"))
	var keep string
	for _, line := range strings.Split(dockerfile, "\n") {
		if _, rest, ok := strings.Cut(line, "Defaults:runner env_keep += \""); ok {
			keep, _, _ = strings.Cut(rest, "\"")
			break
		}
	}
	if keep == "" {
		t.Fatal("deploy/Dockerfile.runner has no Defaults:runner env_keep line, so sudo drops the runner's proxy")
	}
	kept := strings.Fields(keep)
	for _, k := range proxyEnvKeys {
		if !slices.Contains(kept, k) {
			t.Errorf("deploy/Dockerfile.runner's env_keep does not carry %s, which the agent passes to every runner", k)
		}
	}
}

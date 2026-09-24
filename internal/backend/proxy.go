package backend

// proxyEnvKeys are the proxy variables a runner inherits from the agent that
// starts it. Both spellings are carried because tools disagree about which one
// they read: curl honours only the lowercase http_proxy, Go and most package
// managers read either, and a runner behind a proxy that half its tools ignore
// fails in ways that look like flaky networking rather than configuration.
var proxyEnvKeys = []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

// inheritedProxyEnv returns the agent's proxy variables as KEY=value pairs, in
// a stable order, leaving out any the pool sets itself.
//
// A host that can only reach GitHub through a proxy has its agent configured
// with one already -- the agent could not long-poll the controller or pull an
// image otherwise -- so a runner that did not inherit it would fail on its
// first checkout. The pool's own value wins, including an empty one, which is
// how a pool opts its runners out of a proxy the host needs.
func inheritedProxyEnv(lookup func(string) (string, bool), pool map[string]string) []string {
	var env []string
	for _, k := range proxyEnvKeys {
		if _, set := pool[k]; set {
			continue
		}
		if v, ok := lookup(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

# Sentinel journal

Short notes on security findings and the fixes made for them. One entry per
pull request, newest first.

## 2026-11-26: Agent's HTTP client refuses redirects

The ordinary agent HTTP client (`internal/agent/httptransport.go`) left
`CheckRedirect` unset, so Go's default policy followed up to ten redirects on
any response. The tailcat variant has always refused them with
`http.ErrUseLastResponse`, because a redirect from inside the tunnel could
reroute credentials, and the reasoning applies to the ordinary client too: the
controller's REST API never answers with a redirect, so one from it is either
a misconfiguration or an attempt to reroute the agent to somewhere the operator
did not name.

Fix: set the same `CheckRedirect` on the main client, and add a test that
fails if it is unset so the pattern does not drift back.

## 2026-10-08: MCP discovery responses no-store without an external URL

When `security.mcp_oauth` is on and `server.external_url` is empty, the OAuth
discovery documents served at `/.well-known/oauth-protected-resource[/mcp]`
and `/.well-known/oauth-authorization-server` fill their issuer and endpoint
addresses from each request's `Host` header. The responses carried
`Cache-Control: public, max-age=300`. A shared HTTP cache keyed on URL alone
could then hand one caller's Host-derived answer back to another, sending a
later client to a forged authorisation server.

The config validator refuses this combination at startup when the controller
binds publicly, but allows it on loopback where the operator may be running
behind a reverse proxy that forwards whatever Host the client sent.

Fix: switch those three responses to `Cache-Control: no-store` whenever
`server.external_url` is empty. The cacheable path is unchanged when it is
set, so the issuer and endpoints are then a fixed address and sharing the
cached answer is safe.

<!-- Copy: served verbatim at https://zoomies.sh/auth.md by hooks/seo.py. It
     describes what a person, or an agent acting for one, can pass at a Zoomies
     deployment. Keep it true to internal/api and internal/auth, and check both
     when something here names a scope or an endpoint. -->
---
agent_auth:
  skill: https://zoomies.sh/.well-known/agent-skills/zoomies/SKILL.md
  register_uri: https://demo.zoomies.sh/oauth/register
  registration_methods:
    oauth_dynamic_registration:
      issuer: https://demo.zoomies.sh
      steps:
        - Register a client with its redirect URIs at the demo's /oauth/register.
        - Send the person to /oauth/authorize, where they sign in at the demo and approve the client.
        - Exchange the code at /oauth/token for a bearer token bound to /mcp.
    operator_issued_api_token:
      steps:
        - The person who runs the deployment mints an API token in the controller's settings, at the scopes the agent needs.
        - Present it on every call as `Authorization: Bearer <token>`.
---

# auth.md

How an agent authenticates against a Zoomies deployment, and what needs an
account to begin with.

## What needs no credentials

This site is public documentation. Every page, the sitemap, llms.txt,
llms-full.txt, the Markdown copy beside every page and the problem-code
catalog at catalog.json answer any request, and the robots.txt content
signals say yes to all three uses.

## What does

A fleet is each operator's own deployment of the single `zoomies` binary, so
the interfaces an agent drives live at the deployment's own address, not here.
There are two of them, and both are documented in full:

- The REST interface, under `/api/v1`, in
  [the API surface](https://zoomies.sh/api-surface/), generated from
  `api/openapi.yaml` in the repository.
- The MCP interface, at `/mcp` on every controller, over Streamable HTTP: the
  tools an agent calls to ask about and, where the person confirms, act on the
  fleet. For details see [connecting an agent](https://zoomies.sh/connect-claude/).

### Credentials for the REST interface

An operator creates long-lived API tokens in the controller's settings. Each
token carries an explicit scope list in `resource.verb` form (what a viewer
may read, what an operator may change), and every request presents it as a
bearer token:

    Authorization: Bearer <token>

The token answers as the role its scopes give it, never more; a route the
scope does not cover is refused, and the refusal says which one is missing.

### Credentials for the MCP interface

The controller is its own OAuth 2.0 authorization server and resource server,
so an MCP client's flow is the standard one against the instance itself:

- Protected-resource metadata (RFC 9728) at
  `/.well-known/oauth-protected-resource` (the same body answers at the
  `/mcp` variant), and a 401 from the interface carries
  `WWW-Authenticate` naming where that metadata is.
- Authorization-server metadata, RFC 8414, at
  `/.well-known/oauth-authorization-server`. Authorisation is a code flow
  with PKCE (S256) only. Dynamic client registration appears in that
  metadata only while the deployment's `security.mcp_open_registration`
  setting is on; the default is off, and then the operator adds the MCP
  client by hand first (the walkthrough shows where). There is no
  identity-provider discovery document, because no ID token is issued.
- Scopes: `mcp:read` reads the fleet the way a viewer may, and `mcp:operate`
  adds what an operator may change and implies `mcp:read`. Tokens are bearer
  tokens in headers.

### A worked example

The [public demo](https://demo.zoomies.sh/) is a real deployment serving the
whole chain, so the endpoints can be read before anything is built:

```
curl -s https://demo.zoomies.sh/.well-known/oauth-protected-resource
curl -s https://demo.zoomies.sh/.well-known/oauth-authorization-server
```

Its `resource_documentation` points back at
[the connection walkthrough](https://zoomies.sh/connect-claude/), which walks
an MCP client through the flow by hand.

## Where the identity provider is

Beside the deployment interfaces, the hosted service runs its own OpenID
Provider at the portal, `https://portal.zoomies.sh`, which signs the
accounts on that surface and answers discovery for itself with an `issuer`
that names it, `RS256` ID tokens and a `jwks_uri` holding its keys:

```
curl -s https://portal.zoomies.sh/.well-known/openid-configuration
```

This site mirrors that document, unchanged, at
`/.well-known/openid-configuration` and `/.well-known/oauth-authorization-server`,
so a reader of the site finds the real issuer without leaving it; every
endpoint and key it names belongs to the portal, refetched at each request,
so a rotation or an added endpoint can never leave a stale copy here. When
the portal cannot be reached for a moment, the mirror answers with the copy
each site build took from the same address, so discovery is one build stale
rather than dark.

The reachable example's protected-resource metadata is mirrored the same
way, at `/.well-known/oauth-protected-resource`: it describes the demo's own
MCP resource and the authorization server it trusts, names this site's
connection walkthrough as its documentation, and carries the demo's
`mcp:read` and `mcp:operate` scopes. Everything it points at belongs to the
demo.

## Registration instead of accounts

There is no agent account to create. A token is granted by the person who
runs the instance (operator or admin), or issued to an MCP client by the
instance's own authorization server; the instance never learns anything about
an agent beyond what its credentials may do, and revoking a token or an MCP
connection disconnects the agent at once.

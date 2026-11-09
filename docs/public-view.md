---
icon: material/eye-outline
title: Public view
description: >-
  Let anyone read a Zoomies fleet without signing in, for a public demo or an open CI fleet. Every change still needs an account; no operator details are shown.
---

# Public view

Some fleets are meant to be looked at. A project's demo, an open-source
organisation that wants contributors to see why their job is queued, a lab
that puts its runners on a screen in the corridor. `security.public_view` lets
anyone who can reach the controller read its fleet without an account, and
keeps everything else exactly where it was.

It is off by default, and a controller with it on says so in its problems list
as `auth.public_view`, as every setting that widens who may look does.

## Turn it on

In **Settings → Security**, turn on **Public read-only view**, then restart the
controller. Or set it in `zoomies.yaml`:

```yaml
security:
  public_view: true
```

`ZOOMIES_PUBLIC_VIEW=true` overrides both, as every `ZOOMIES_*` variable does.
Make sure an administrator account exists first: the first-run form is what a
controller with no account shows, public or not.

## What a visitor sees

A request with no credential at all is a **visitor**. In the browser that is
the whole product, read-only, with a line under the top bar saying so and a
**Sign in** link beside it:

| A visitor reads | A visitor is not shown |
| --- | --- |
| The Overview, pools, runners, the queue, workflows and jobs | The audit log, which names people and the addresses they came from |
| Runner logs, live and finished | Accounts, API tokens, sessions and MCP connections |
| Usage and what it cost | Settings, and every page under them |
| Hosts, providers and the machines rented from them | AI context and the source files it reads |
| Installations, webhook deliveries and the update status | Eli, its conversations and its repairs |
| Kennel Club's findings and the problems list | Migrations |

The live event stream follows the same rule: a frame is sent only to somebody
who could fetch what it carries, so a visitor never receives an audit row.

Every change answers **403** with *this is a read-only public view; sign in to
do this*, whether it is asked for from the UI, the API or MCP. The UI does not
offer the pages a visitor cannot open, so they are rarely told.

## Signing in

Whoever runs the fleet signs in as usual, from the link under the top bar or
the **Visitor** menu, and has their own role back. Signing out returns them to
the sign-in page, where **Continue without signing in** makes the browser a
visitor again. A session that runs out does the same on its own: on a public
view a browser whose session has ended is a visitor, and its cookie is cleared.

An API token is never softened into a visitor. A token that is wrong, revoked
or expired is refused with 401, so automation holding a dead token hears about
it rather than carrying on as a stranger.

## Assistants

`/mcp` answers a client with no credential as a visitor, so anybody can point
their own assistant at a public fleet:

```sh
claude mcp add --transport http zoomies https://zoomies.example.org/mcp
```

The read-only tools work on the fleet. The tools that change it are not
offered, and the AI context tools answer that the public view does not show
them. Somebody who needs more connects with an API token in the
`Authorization` header instead. [MCP](mcp.md) has the rest.

## What it costs to be public

Every open tab holds a live event stream. Visitors' streams have a ceiling of
their own, `limits.visitor_event_subscribers` (200 unless you change it), and
are not counted against `limits.event_subscribers`, so however many people are
looking, whoever runs the fleet still gets theirs. A visitor past the ceiling
still reads every page, without live updates, and is told to reload in a
minute.

Watching a running job's log asks that job's agent for a stream as well. Put a
reverse proxy in front that limits requests and open connections per address,
and keep secrets out of the workflows the fleet runs: anyone can read their
logs.

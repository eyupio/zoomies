# improve.md: the advisor's handoff

Written 2026-10-19 against commit `a0f21dba` (the drift check every plan
runs). This is the context the plans were cut from: what was audited, what
was found, what was rejected, and what each plan owes the executor. The plans
themselves, under `plans/`, are the self-contained units; this file is the
map.

## What this repository is

Zoomies: a self-hosted GitHub Actions runner fleet controller. One Go binary
(controller, agent, gateway, installer, CLI by subcommand), Svelte 5 UI
embedded with `go:embed`, SQLite (`modernc`, pure Go, one writer) in
`internal/store`, no Kubernetes, no database server. Build and verify:

```
make build-nogui   # once in a fresh checkout: writes the UI placeholder
                   # internal/api embeds; plain go build/vet/test fail without it
make test          # go test -race -count=1 ./...
make lint          # go vet, gofmt, staticcheck when present, UI lint (npm)
```

Conventions the plans assume throughout (each plan restates what it needs):
British spelling in prose; no em dash characters and no spaced double
hyphen used as a dash (tests in `internal/docs` fail the build on both);
comments explain why, not what; test names are sentences about behaviour;
commit messages are imperative sentences in plain prose, sentence case, no
prefix; package boundaries are load-bearing and held by tests (see the root
`AGENTS.md`, which is the contributor's map too).

## How the audit was run

Four parallel read-only sweeps (correctness, security, performance and
test coverage, tech debt and dependencies and docs), every candidate finding
then verified by the advisor against the cited code before it reached the
findings table. The repo's own intent documents were read first
(`AGENTS.md` and the scoped ones, `CONTRIBUTING.md`, `ROADMAP.md` and
`roadmap/`, `docs/architecture.md`, `docs/dependencies.md`), so that
documented tradeoffs were treated as settled, not re-reported.

## The findings that became plans

Ordered by leverage (impact over effort, weighted by confidence); each maps
to one plan file.

| # | Finding | Category | Evidence | Plan |
|---|---------|----------|----------|------|
| 1 | The agent heartbeat's nil-response guard sits after three uses of the response: dead guard, latent process-killing panic | bug | `internal/agent/daemon.go:1050-1072` | [001](001-heartbeat-nil-guard.md) |
| 2 | The bulk drain/delete endpoint answers with raw `err.Error()` per row, the one route that bypasses the error funnel that hides driver internals | security | `internal/api/handlers_runners.go:327` vs `internal/api/errors.go:328-347` | [002](002-bulk-error-funnel.md) |
| 3 | The process backend's log-bounding housekeeping (`PruneLogs`, `nativeLogLoop`) has zero test coverage anywhere in the tree | tests | `internal/backend/process_logs.go:15-79`, `internal/agent/native_logs.go:12-36` | [003](003-process-log-pruning-tests.md) |
| 4 | `GET /runners/{id}` re-reads the whole fleet to render one row (renderer listings, a duplicate host-count GROUP BY, a duplicate pool read) | perf | `internal/api/handlers_runners.go:74-106` | [004](004-runner-detail-fleet-reads.md) |
| 5 | The log relay's edges mislead: late follow viewers get no tail, drops are unmarked (unlike the agent's own buffer), a silent download returns an empty 200, and a narrow subscribe/teardown race closes a new viewer's channel | bug | `internal/controller/logs.go`, `internal/api/sse.go:540-597` | [005](005-log-relay-edges.md) |
| 6 | The MCP and assistant tool paths buffer whole answers (32 MiB each) with no per-caller budget, unlike every agent route | security | `internal/api/mcp.go:44,174-285` | [006](006-mcp-response-budgets.md) |
| 7 | `docs/architecture.md`, the doc every structural change is routed through, stops at the fleet core: the newer subsystems have no architectural rationale anywhere | docs | `docs/architecture.md` (headings end at line 673) | [007](007-architecture-docs-subsystems.md) |
| 8 | `tailscale.com v1.105.0-pre` is a direct dependency on an unstable channel dragging about 130 indirect modules, with no recorded channel policy | dependencies | `go.mod:25,28-159` | [008](008-tailscale-channel-policy.md) |
| 9 | `internal/controller` is a quarter of the Go tree and holds whole subsystems far beyond its documented "wiring" role | tech-debt | `internal/controller/` inventory vs the root `AGENTS.md` package table | [009](009-controller-package-split.md) |
| 10 | `frameFor` resolves the subscriber's identity (one SQLite read) per frame, per subscriber; the stream already has the cheaper per-heartbeat shape for `reader` | perf | `internal/api/sse.go:291-405` | [010](010-sse-identity-tick.md) |

Direction was deliberately not audited: this repository governs its own
through `ROADMAP.md` (v3.13, the sole active source of truth) and
`roadmap/decisions/`; a competing list from an advisor would be noise.

## Execution order and dependencies

The table in [plans/README.md](README.md) is the working index
(executors update their rows there). In short: 001-003 are small and
independent; 004 then 005 share handlers; 006-008 are independent; 010 after
005 (same file, different regions); 009 last (it moves files across
`internal/controller`, and its diff should not fight 004/005's).

## What was rejected, and why

Recorded so the next audit does not re-report them (the list lives in
[plans/README.md](README.md) too):

- `__Host-` cookie prefix: attributes otherwise right; exposure needs a
  hostile sibling subdomain on a shared parent domain, atypical for
  self-hosted dedicated hosts, and the rollout cost (CLI constant, live
  sessions) is real. Revisit only for shared-domain deployments.
- Rate-limiter wholesale reset on a heartbeat-interval settings change: a
  one-off budget refresh an operator's own edit triggers.
- Splitting the four "god files" (`daemon.go`, `problems.go`,
  `installer.go`, `models.go`): likely deliberate style; only alongside 009
  if merge friction actually bites.
- Audit-row retention, migration squashing, views aliasing (verified clean),
  `make dev` auth-off: all by-design.

## What was not audited

The holes in the coverage, so the next run knows where to look:
`web/src` component internals, `site-worker/`, `hooks/`, `deploy/`,
`test/e2e`, `internal/gateway`, `internal/installer` internals,
`internal/proxmox` and `internal/proxmoxsetup`, `internal/wsl`,
`internal/hosttune` internals, the assistant provider adapters,
`internal/aicontext`'s write paths, and `internal/auth/mcp_oauth.go`'s
consent flow beyond the spot-checked PKCE and redirect handling.

## For the operator

The plans are written to be executed one at a time by a model with no
context from this session: each carries its own excerpts, commands,
boundaries, done criteria and STOP conditions. Branch names and commit
message styles are specified per plan; none of them push, open pull requests
or merge, which stays with you. When a plan's done criteria all hold and its
diff stays inside its scope, it is green; `plans/README.md`'s status row is
the record.

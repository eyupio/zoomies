# Agent guidance

Shared guidance for coding agents working in this repository.

## What this is

Zoomies is a self-hosted GitHub Actions runner fleet controller: one Go binary
that watches for queued jobs, starts an ephemeral runner container per job, and
destroys it when the job finishes. SQLite for state, a Svelte 5 UI embedded with
`go:embed`, no Kubernetes and no database server.

`cmd/zoomies` is the only binary. It is the controller (`zoomies controller`),
the agent (`zoomies agent`), the installer (`zoomies init`) and the CLI, chosen
by subcommand. On a single VM the controller runs an agent inside itself, so the
whole system is one process and one SQLite file.

Read [docs/architecture.md](docs/architecture.md) before changing anything
structural; it explains the shape and the reasons for it.

## Commands

```sh
make build-nogui   # build without rebuilding the UI (the fast inner loop)
make build         # build the UI and embed it (needs npm)
make test          # go test -race -count=1 ./...
make bench         # the benchmarks, six runs each; compare two runs with benchstat (BENCH, BENCH_PKGS, BENCH_COUNT narrow it)
make lint          # go vet, gofmt check, staticcheck if present, UI lint
make fmt           # gofmt + prettier
make dev           # controller with auth off, on :8080, for UI work
make ui-dev        # Vite dev server proxying /api to that controller
make test-unit     # the UI's unit tests: no browser, a few seconds
make test-ui       # Playwright against a real built binary
make openapi       # regenerate both copies of api/openapi.yaml: the embedded Go source and the UI's TypeScript client
make screenshots   # recapture docs/screenshots from the real UI, both themes (needs Pillow)
```

`go build ./...` fails on a clean checkout: `internal/api` embeds
`internal/api/webdist`, which is a build product. Run `make build-nogui` once,
it writes a placeholder, before any plain Go command (`go build`, `go vet`,
`go test`) that compiles that package. The `make` targets that run Go (`test`,
`test-short`, `bench`, `cover`, `lint`, `generate`) write the placeholder
themselves, and never over a real UI build. CI does the same thing as its first
step.

Go 1.27.1 or later (`go.mod` sets the floor; CI builds with 1.27.2), Node 22 or
later. Node is a build-time dependency only; the shipped binary is
self-contained and static (`CGO_ENABLED=0`, pure-Go SQLite).

Run a single test with `go test -run TestName ./internal/pkg/`. `make test-e2e`
needs real GitHub credentials and skips itself without them.

A change made for speed is held to a number: run `make bench` before and after
(on an otherwise idle machine, once per package, `tee`d to files) and put the
`benchstat` comparison in the commit message. `CONTRIBUTING.md` has the
commands. A benchmark checks that its scenario is the one its name describes
before it times anything; keep that when adding one.

## Package boundaries

These are load-bearing. Code that crosses them is the main thing to catch in
review.

| Package | Rule |
| --- | --- |
| `internal/store` | The **only** place SQL is written. Domain types, embedded migrations, every query. No other package imports `database/sql`. |
| `internal/scheduler` | **Pure.** `Decide` takes a snapshot and returns a `Plan`. No clock reads, no database, no network; that is what makes scaling behaviour testable, and it is where every decision's operator-facing *reason string* comes from. |
| `internal/updates` | **Pure.** `Choose` takes a list of releases, the mode, the soak and the instant, and picks the release the mode would take and says why. It imports the standard library and `internal/version` and reads no clock; `purity_test.go` enforces both. `Decide`, the planner for automatic updates, has joined it: it takes a snapshot and returns the actions and the sentence the status shows, under the same rules. |
| `internal/updates/channel` | The impure half of `internal/updates`: finds the update folder, writes a request into it and reads what the helper left. It is the only place the service touches that folder, and it never creates it (the installer does, with the service account's ownership). |
| `internal/api` | Transport only. A handler reads a request, asks the controller / auth / store, and renders the shape `api/openapi.yaml` promises. It has no opinions about the fleet. The resource views themselves (`HostView`, `PoolView`, …) are `internal/controller/views.go` types the handlers alias, because the event stream renders the same JSON and is fed from the controller. |
| `internal/provider` | The infrastructure-provider contract, a fake that obeys it, and `RunContractTests`, the conformance suite every provider passes. It may import `internal/store`'s domain types and `config.Finding` and **nothing else**; two tests enforce that, and that it names nothing provider-specific. Renting a machine is not placing a runner: this is separate from `internal/backend` on purpose. |
| `internal/provider/proxmox` | The first provider. The Proxmox VE API is hand-rolled `net/http` for the same reason the Docker one is. |
| `internal/controller` | Wiring: the reconcile loop, the machine loop, webhook ingest, the agent task queue, the log relay, and every payload the event stream carries (`views.go`, `derived.go`). The machine loop is **not** part of `Reconcile`: a clone takes minutes and `reconcileMu` is held for a whole scheduling pass. |
| `internal/config` | `zoomies.yaml` + `ZOOMIES_*` overrides, and the validator. |
| `internal/github` | App auth, JIT configs, webhook validation, the fallback poller, and `fake.go`, a fake GitHub used by tests. |
| `internal/backend` | Docker, Podman, bare process. The Docker API is hand-rolled `net/http` against the Engine API on purpose (see below). |
| `internal/agent` | The runner-executing half and its outbound transport. |
| `internal/mcp` | The MCP tools and both transports (stdio for `zoomies mcp`, Streamable HTTP for `/mcp`). A tool calls the REST API through the `mcp.API` interface and nothing else (no store, no controller), so it can never do what the caller's token could not. |

The rules that can be stated as an import are held by a test, not by review
alone: `internal/scheduler`, `internal/updates` and `internal/provider` each have
a purity or boundary test, `internal/mcp` may import only what its test lists,
and a test in `internal/store` fails if any other package's shipped code imports
`database/sql` or the SQLite driver. A test that fails on one of these names the
rule; when the import is a pure helper that belongs, add it to that test's
allowlist with a reason rather than loosening the rule.

Other invariants worth knowing before you edit:

* **The controller never dials an agent.** Agents connect outbound only (long-poll
  for tasks, POST results), so a host behind NAT needs no inbound rule. That is
  why log streaming is inverted: the controller queues a `stream_logs` task and
  the agent opens a chunked POST that gets relayed to the browser's SSE stream.
* **Webhook deliveries are at-least-once and can arrive out of order.** The jobs
  upsert refuses to move a job backwards through its lifecycle. Keep it that way.
* **Every `*.updated` event is the resource's `GET` shape.** The UI drops a frame
  straight into its cache, so a `host.updated` carrying a bare store row (no
  `healthy`, no `free`) repaints the host wrong. Publish through the
  controller's `publish*`/`Publish*` helpers, which render the view; never put a
  store row on the bus. `stats`, `problems.updated` and each host's view are
  computed after every pass and sent only when they change, so a new kind of
  problem (or a host whose slots or heartbeat moved with no row written)
  needs no publish call of its own.
Read the scoped guidance when working in these directories:

* [`internal/store/AGENTS.md`](internal/store/AGENTS.md): one writer, the runner state machine, sentinel errors, IDs.
* [`internal/config/AGENTS.md`](internal/config/AGENTS.md): configuration: settings live in the database.
* [`web/AGENTS.md`](web/AGENTS.md): the UI: tokens, status colours, no new libraries.
* [`internal/hosttune/AGENTS.md`](internal/hosttune/AGENTS.md): host health and tuning: consent, never tune from an upgrade, never restart Docker under work.
* [`.github/AGENTS.md`](.github/AGENTS.md): "Things CI will fail you on": the files CI diffs against their sources.

## Dependencies

Every dependency carries a one-line justification in
[docs/dependencies.md](docs/dependencies.md), and that is enforced by review.
Before adding one: could the standard library or fifty lines of our own do it?
Is it maintained? What does it pull in transitively? What does it cost against
the shell budget if it ships to the browser? Then add the row, in the same
voice, *why*, not *what*.

Some omissions are deliberate and documented there, notably
`github.com/docker/docker` (the hand-rolled Engine API client in
`internal/backend/dockerapi.go` is a few hundred lines and gets Podman almost
free, because `podman.sock` speaks the same protocol).

## Testing

Table-driven and standard-library `testing` throughout; no assertion framework.
Tests use `:memory:` SQLite stores, injected clocks (`store.Options.Now`), and
the fake GitHub in `internal/github/fake.go` rather than network calls. Helper
constructors take `t` and call `t.Helper()` / `t.Cleanup`. Coverage is broad
(roughly one test file for every source file) so a new behaviour is expected to
arrive with one.

Test names are sentences about the behaviour, not the method
(`TestOpenCreatesTheDatabaseDirectory`), and a test often carries a comment
explaining *why the behaviour matters*, not what the code does.

## Voice

The prose in this repository (comments, docs, error messages, commit messages)
has a consistent voice, and matching it is part of a change looking finished.

* **British spelling** in prose: *behaviour*, *authorisation*, *organisation*,
  *utilisation*, *licence*. (JSON field names mirroring GitHub's API stay
  American: `organization` in a webhook payload is correct as-is.)
* **Comments explain why, not what.** The interesting ones name the failure mode
  being designed out, or the trade-off taken. Follow that; do not narrate code.
* **Error and warning messages are written for a person to act on.** An operator
  who gets a 403 should be told which role they are missing. Findings say what
  to change; API errors carry a human message and a stable code.
* **No em dashes.** Do not write `—` anywhere in this repository: prose,
  docs, comments, error messages, UI strings, commit messages or pull request
  text. Use a comma, a colon, a semicolon or parentheses, or split the sentence.
  Do not use `--` as a stand-in either. `internal/docs/no_em_dash_test.go`
  fails the build on a spaced em dash, and `internal/docs/double_hyphen_test.go`
  on a file that gains a spaced `--`. Older code comments still carry about six
  thousand of the latter, and they go as their lines are rewritten, not in one
  sweep that would bury every file's history. The one exception is the UI's
  placeholder for an absent value, which is two hyphens: write it as `NO_VALUE`
  from `web/src/lib/format.ts`, never as a dash of your own;
  `web/unit/absent-value.test.ts` guards it.
* **Diagrams are Mermaid**, in a `mermaid` fenced block beside the prose they
  explain, never ASCII art. The site renders them (`mkdocs.yml` registers the
  fence) and so does GitHub, so a diagram lives in the Markdown it belongs to and
  there is no exported image to go stale. Do not colour one by hand: Material
  themes it for light and dark, and a hard-coded fill is wrong in one of them.
* **Commit messages are imperative sentences in plain prose**, sentence case, no
  Conventional Commits prefix: *"Say why a pool has no host, and let a host say
  it can run again"*, *"Make both halves of 'no capacity' something an operator
  can do"*.

## Layout

```text
cmd/zoomies         the binary: controller, agent, gateway, init, CLI
internal/store      domain model, SQLite schema, every query
internal/config     zoomies.yaml + env, and the validator that warns
internal/scheduler  pure scaling decisions and label matching
internal/updates    which release an update mode would take, and why; pure
internal/updates/channel  the update folder: the service's side of the helper's files
internal/github     App auth, JIT configs, webhooks, the fallback poller
internal/backend    Docker, Podman and bare-process runner backends
internal/auth       identity, RBAC, tokens, audit, OIDC
internal/api        REST, SSE, metrics, and the embedded UI
internal/provider   the infrastructure-provider contract, its fake, and Proxmox
internal/registry   what a container registry says a tag points at, for the digest-pinning generators
internal/controller the reconcile loop, the machine loop and the agent task queue
internal/agent      the runner-executing half
internal/gateway    zoomies gateway: a private provider's end of the tunnel
internal/installer  zoomies init / uninstall / agent join, and the unit,
                    compose and env templates they write
internal/backup     one copy of the database: taking, listing, verifying, archiving,
                    encrypting, shipping it to an S3-compatible remote and restoring it,
                    shared by the CLI, the scheduler and the API
internal/cryptox    AES-256-GCM at rest, argon2id, token hashing
internal/events     in-process pub/sub that the SSE endpoint fans out
internal/migrate    rewriting workflows' runs-on lines
internal/mcp        the fleet over MCP: tools, stdio and Streamable HTTP
internal/aicontext  commit-pinned repository context, independent of the fleet
internal/agentguidance  inspects a repository's agent instruction files without running them
internal/analysis   evidence-backed findings computed from explicit snapshots
internal/assistant  the model-provider contract behind the assistant, and its adapters
internal/catalog    the one machine-readable list of problem codes and Kennel Club checks
internal/cliconfig  ~/.config/zoomies/cli.yaml: where the CLI finds its controller
internal/docs       tests that hold the documentation to the code it describes; no source of its own
internal/hosttune   host health and tuning: detection that never writes, and approved changes
internal/kennel     pure evaluation of what is wrong with a repository's workflows
internal/logging    the process-wide structured logger
internal/machine    which host this process runs on: distribution, release, capacity
internal/naming     the grammar for pool, host and runner names
internal/optimisation  reviewable proposals computed from evidence handed in; reads no source and calls no model
internal/provenance keeps third-party names out of the tree
internal/proxmoxsetup  connecting a Proxmox host: its own principal, binary, service and state
internal/prrepair   bounds what the model may edit when it proposes a pull request
internal/qr         the smallest QR encoder that serves the two-step verification picture
internal/redact     strips credentials and commit emails from text bound for a model
internal/releasekey the release signing key: Ed25519 verification of a release's checksums
internal/toolscan   reads workflows for the toolchains their jobs install
internal/terminalui terminal presentation: the CLI home, paw indicator and command progress
internal/version    build metadata stamped in at link time
internal/wsl        keeps an agent alive inside a WSL2 distro
web/                the Svelte 5 UI
test/e2e            the Docker end-to-end test, behind the `e2e` build tag
api/openapi.yaml    the API contract both clients are generated from
deploy/             the controller and runner images, and the runner entrypoint
design/             the Mission Control style review: concept images and their validation
docker-compose.yml  the compose deployment, at the root so it is the one people find
docs/               the zoomies.sh site, built by mkdocs.yml
overrides/          the site's theme overrides: sharing tags, structured data, sitemap,
                    the header's repository facts
hooks/              the site's build-time metadata: git dates, llms.txt, and the
                    latest release and star count the header shows
site-worker/        the edge that serves zoomies.sh: the built site as the
                    worker's static assets, content negotiation for agents,
                    the discovery mirrors and every response header the
                    assets pipeline cannot set
weekly-seo/         the weekly search check for zoomies.sh: its config, notes and
                    the report and memory files the .claude/agents/weekly-seo.md
                    agent writes
ROADMAP.md          the follow-on roadmap, and the decisions it asks the owner to take
roadmap/            what supports it: the work-package record, decision records,
                    gate evidence, dated plans, the model guidance, the source
                    document and an archive of finished ones; its README says which
                    document answers which question
skills/             what a coding agent installs: the zoomies skill, with a command
                    reference generated from the binary
plans/              the improve advisor's output: prioritised, self-contained
                    implementation plans and their index, each stamped with the
                    commit it was written against; executors update the status
                    table in plans/README.md
install.sh          the one-line installer, served from the site root
nixpacks.toml       the build a Nixpacks-based PaaS runs; see docs/paas.md
```

## Instruction hierarchy

`AGENTS.md` contains shared engineering guidance. Claude imports it through
`CLAUDE.md`. Scoped `AGENTS.md` files add detail beside the code; their Claude
wrappers import the same rules. Fix contradictions rather than relying on load
order. Keep entry points concise and put retrieval detail in the context guide.

## Host health implementation

Follow the consent rules in `internal/hosttune/AGENTS.md` and
`docs/host-health.md`. Upgrades are doctor-only. Container health comes from
the native host binary; no privileged container mounts. All host-tuning tests
use injected filesystems and commands. Preserve reversal records on uninstall.

<!-- zoomies-ai-context:start -->

## Zoomies AI Context

Before using prepared repository context, read `.zoomies/AI_CONTEXT.md`. It explains available access routes, revision checks and selective retrieval.

<!-- zoomies-ai-context:end -->

## Agent skills

### Issue tracker

Issues live in GitHub Issues for eyupio/zoomies, driven by the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: one `GLOSSARY.md` at the repo root, and the numbered decision records in `roadmap/decisions/`. See `docs/agents/domain.md`.

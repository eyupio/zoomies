# Contributing to Zoomies

Thank you for helping Zoomies give self-hosted GitHub Actions runners a better
run. Contributions of code, tests, documentation, bug reports, design feedback,
and real-world deployment experience are all welcome.

By participating, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Search the [issues](https://github.com/eyupio/zoomies/issues) before opening a
  new one.
- Small, focused fixes can go straight to a pull request.
- For a substantial feature, architectural change, new dependency, or breaking
  configuration change, open an issue first so the approach can be agreed
  before a lot of work is done.
- Report vulnerabilities privately as described in
  [SECURITY.md](SECURITY.md). Do not open a public issue for a security problem.

Zoomies is early-beta software that creates runners on other people's machines.
Changes to authentication, authorisation, secrets, runner isolation, container
backends, networking, upgrades, or deletion paths deserve an explicit threat
and failure-mode review.

## Development requirements

The core binary is written in Go. The web UI is built with Svelte and embedded
in that binary.

- Go 1.27.1 or later (the floor in `go.mod`; CI builds and releases with 1.27.2)
- A C compiler (`gcc` or `clang`), because `make test` runs under the race
  detector and Go refuses `-race` without cgo
- Node.js 22 or later and npm, when changing or rebuilding the UI. `make lint`
  also runs `npm ci` in `web/`, so it needs them too
- Python 3.12 (`python3`), only for building the documentation site
- Git
- Docker or Podman for backend and image work
- A GitHub App only for tests or development flows that actually talk to GitHub

Start by forking the repository, then clone your fork:

```sh
git clone https://github.com/YOUR-USERNAME/zoomies.git
cd zoomies
git remote add -t main upstream https://github.com/eyupio/zoomies.git
```

`-t main` keeps `git fetch upstream` to the one branch you need. The repository
also carries a generated `zoomies-ai-context` branch whose history is large, and
a plain fetch would download all of it.

Create a branch from the latest `main`:

```sh
git fetch upstream
git switch -c fix/short-description upstream/main
```

Use a short, descriptive branch name such as `fix/agent-reconnect`,
`feat/pool-draining`, or `docs/runner-security`.

## Build and run

Run `make help` for the maintained list of targets.

A fresh checkout does not compile on its own. `internal/api` embeds the built
UI, which is a build product that is not committed, so a plain `go vet` or
`go test` fails with `pattern all:webdist: no matching files found` until it
exists. Run this once first:

```sh
make build-nogui    # Go-only build; writes a placeholder UI, needs no Node
```

`make build` does the same with the real UI (it needs Node), and either is
enough for every Go command afterwards.

```sh
make build          # build the Go binary with the web UI embedded
make build-nogui    # fast Go-only inner loop
make dev            # local controller with debug logging and auth disabled
make ui-dev         # Vite UI against a controller on localhost:8080
```

`make ui-dev` needs the UI's dependencies, so run `npm ci` in `web/` first.

The development mode disables authentication and is for a local workstation
only. Do not expose it to a network.

## Make a focused change

- Keep pull requests small enough to review and explain the reason for the
  change, not only the implementation.
- Follow the surrounding code and UI patterns. Go code must be formatted with
  `gofmt`; web code is formatted with Prettier. `make fmt` does both.
- Read [AGENTS.md](AGENTS.md) before changing anything structural. Written for
  coding agents, it is also the contributor's map: which package owns what, the
  boundaries between packages that review is meant to catch, and the house
  voice (British spelling, comments that explain why, no em dashes). The
  reasoning behind the shape is in [docs/architecture.md](docs/architecture.md).
- A new documentation page goes under `docs/` and into the `nav` in
  `mkdocs.yml`; a page that is in neither is easy to miss and is checked by
  `mkdocs build --strict`.
- Add or update tests for changed behaviour.
- Update user documentation, configuration examples, API definitions, and
  screenshots when the public behaviour changes.
- Preserve safe defaults. If a change weakens isolation or exposes a new
  capability, make the trade-off explicit in both the UI and documentation.
- Never commit credentials, GitHub App keys, tokens, local databases, generated
  logs, or build output.

Generated files have a single source of truth:

- After changing `api/openapi.yaml`, run `make openapi` and commit both files it
  writes: the Go copy the API serves (`internal/api/openapi_spec.go`) and the
  TypeScript client (`web/src/lib/api/schema.d.ts`). A test fails on either one
  when it is stale.
- After changing the runner image catalogue in `internal/naming`, run
  `make generate` and commit everything it updates. Do not hand-edit generated
  catalogue sections.

## Test your change

Run the narrowest relevant tests while developing, then the standard checks
before opening a pull request:

```sh
make test-short     # the quick Go tests, a few minutes
make fmt            # gofmt and Prettier; make lint fails on what this would change
make lint           # vet, gofmt, staticcheck and the UI lint
make test           # every Go test with the race detector
```

`make test` can take an hour with few CPUs (CI splits it across six runners), so
`make test-short` is the loop to work in. `make lint` runs staticcheck only if it is installed and says
so when it is not; CI always runs it, so installing it
(`go install honnef.co/go/tools/cmd/staticcheck@latest`) saves a round trip.

A change made for speed is held to a number. Run the benchmarks of the package
it touches before the change and after it, and put the comparison in the commit
message:

```sh
make bench BENCH_PKGS=./internal/scheduler | tee before.txt
# make the change, then
make bench BENCH_PKGS=./internal/scheduler | tee after.txt
benchstat before.txt after.txt     # go install golang.org/x/perf/cmd/benchstat@latest
```

`make bench` runs every benchmark six times, so that `benchstat` can say how
far apart the runs were; `BENCH=Decide/quiet` narrows it by name. Leave the
machine alone while it runs, because benchmarks that share the CPU contaminate
each other, and compare only runs made on the same machine.

For documentation changes, install the pinned dependencies and run the same
strict build as the site workflow:

```sh
python3 -m pip install --require-hashes -r docs/requirements.txt
mkdocs build --strict
```

The build must produce `site/sitemap.xml`, `site/llms.txt` and `site/badge.svg`,
and the header must name the latest release, `hooks/source.py` asks the GitHub
API for it at build time, so set `GITHUB_TOKEN` if the anonymous rate limit is
in the way. These checks catch broken links and missing published assets before
a PR is merged.

The showcase and article layouts live in `overrides/partials/`. The existing
Markdown files keep their URLs, metadata and machine-readable copies;
`hooks/showcase.py` adds reading times and the field-guide cards. Add an
existing page to `extra.field_guides` in `mkdocs.yml` to include it in the guide
library. Its title and description remain the page's search metadata.

The public site serves Fraunces and IBM Plex locally. The original Zoomies
artwork stays monochrome; the display type and blue accents belong to the
surrounding layout. Update `showcase.css` for that visual system and
`showcase-material.css` for its integration with Material. The build gives
stylesheets and scripts content-versioned filenames so returning browsers
cannot combine new markup with stale styles.

After the strict build, check the rendered site and its browser behaviour:

```sh
cp install.sh install.ps1 connect-proxmox.sh site/
python3 hooks/tests/check_site.py
npm ci --prefix web --ignore-scripts --no-audit --no-fund
cd web
npx --no-install playwright install chromium
cd ..
node hooks/tests/browser.mjs
```

`check_site.py --baseline /path/to/previous/site` also checks that an existing
build's URLs, titles, descriptions, robots directives, sharing metadata and
content anchors survive. Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to use an
installed Chromium, and `DOCS_SCREENSHOTS` to capture the responsive layouts.
Mermaid stays in the Markdown; its pinned browser bundle is served locally
only when a page needs it. Its source and licence are in `docs/vendor/mermaid/`.

For web UI changes, also run:

```sh
cd web
npm ci --no-audit --no-fund
npm run check
npm run test:unit
npx playwright install chromium   # once, for make test-ui
cd ..
make test-ui
```

Some suites intentionally require more setup:

- `make test-drill` runs a real controller and agent against fake GitHub.
- `make test-e2e` exercises Docker-backed end-to-end behaviour and skips when
  required GitHub credentials are absent.
- `make screenshots` captures documentation screenshots from the real UI.
- `make vet-tags` vets the suites behind the `e2e`, `drill` and `load` build
  tags. A plain `go vet` never compiles them; `make lint` and CI both run this
  target, and it is the quick check after editing a file under one of those tags.

You do not need to run an unrelated privileged or credential-dependent suite.
State exactly what you ran and what you could not run in the pull request.

CI runs what a change's files can affect, decided by the `changes` job at the
top of `.github/workflows/ci.yml`, on a pull request and on `main` alike:

| A change to | Runs |
| --- | --- |
| Markdown, `docs/`, the site, the workflows | the `internal/docs` and `internal/naming` checks, which hold the prose to the code, about a minute |
| `install.sh` | the `install.sh` checks and the upgrade test; no binaries or images |
| `web/` | the UI and Playwright jobs, and the docs checks |
| `deploy/` | the image builds and the tests that read those files |
| Go, or anything not listed | the whole set |
| `ci.yml`, the `Makefile`, `go.mod` | everything |

The Go suite is split by package across six runners (the controller and API
packages are two shards each, the store is one, and everything else shares the
last); `make test TEST_PKGS=./internal/store/` runs one the way CI does. Coverage is
measured on `main`, when the whole Go suite ran. `main` publishes the dev
channel's binaries and images only from a commit that changes what ships (Go,
the UI or `deploy/`) so a documentation merge leaves `--version dev` where it
was. A manual dispatch runs everything. CodeQL, govulncheck, the fuzz targets
and Scorecard run only when their own inputs change, and weekly regardless.

A pull request's checks stop when the pull request does. Pushing to it replaces
the run in flight, and closing or merging it starts a run that takes over the
pull request's concurrency group, cancels the one still going and skips every
job, so a merged pull request does not keep its fleet slots while the commit
on `main` queues behind them. Only a pull request is ever cancelled: runs on
`main` queue instead, because they publish the dev channel. A new workflow that
runs on `pull_request` needs the same three parts, and `internal/docs` fails
without them: the `closed` type, the shared group, and a
`github.event.action != 'closed'` guard leading every job's condition.

## Commit and open a pull request

Use clear, imperative commit subjects:

```sh
git add path/to/changed-file
git commit -m "Add runner reconnect backoff"
git push -u origin fix/agent-reconnect
```

Open a pull request against `eyupio/zoomies:main`. Include:

- the problem and why it matters;
- the chosen approach and important trade-offs;
- the tests and manual checks performed;
- screenshots or a short recording for visible UI changes;
- documentation, configuration, migration, or compatibility impact; and
- any follow-up work deliberately left out.

Keep the branch current with `main`, respond to review comments, and avoid
mixing unrelated cleanup into the same pull request. Maintainers may edit,
squash, or reword commits when merging.

## Coding and design principles

- Prefer straightforward code and explicit failure modes over cleverness.
- Errors should tell an operator what is true, why it matters, and what to do
  next.
- Keep the controller usable as one Go binary with SQLite; do not introduce a
  required external database or orchestration platform without prior agreement.
- The CLI and web UI are clients of the same API. Keep their behaviour and
  terminology aligned.
- Preserve accessibility, responsive layouts, keyboard operation, light and
  dark themes, and the guidance in
  [docs/ui-guidelines.md](docs/ui-guidelines.md).
- Treat logs, metrics, audit records, and documentation as part of the feature,
  not an afterthought.

## Releases

Maintainers cut releases. The shape a tag must have, the signing key and what to
do when a release build fails are in [RELEASING.md](RELEASING.md).

## Licence

By submitting a contribution, you agree that it may be distributed under the
repository's [GNU Affero General Public License v3.0](LICENSE).

Thank you for contributing to Zoomies.

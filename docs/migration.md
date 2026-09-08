# Moving repositories onto your runners

You have a fleet. Your workflows still say `runs-on: ubuntu-latest`, in every
repository, in every job. The migration wizard changes those lines and opens a
pull request on each repository, and it shows you the exact diff before it opens
anything.

Open it at **Migrate** in the navigation, or `g` then `m`.

## What it does

1. **Reads.** It lists the repositories your GitHub App installation can see and
   reads the workflow files at the top of each one's `.github/workflows`.
2. **Proposes.** For every GitHub-hosted label it found — `ubuntu-latest`,
   `ubuntu-24.04-arm`, `macos-14` — it proposes the pool that promises the same
   operating system and architecture.
3. **Lets you make exceptions.** One answer per label is the default and usually
   the whole answer. Where it is not, any single job can be pointed somewhere
   else — see [one job at a time](#one-job-at-a-time).
4. **Shows you.** A unified diff of every file it would change, and every job it
   would not, with the reason.
5. **Opens.** One pull request per repository, each on its own branch, changing
   only the `runs-on` lines you reviewed.

Nothing before step five writes anything.

## What it changes, and what it will not

It rewrites `runs-on` and nothing else. Comments, indentation, quoting, key
order, blank lines and line endings all survive byte for byte — it is a
line-level edit, not a YAML round trip, because a pull request that reformats
the whole file hides the one line that actually changed.

```diff
 jobs:
   build:
-    runs-on: ubuntu-latest      # the cheap one
+    runs-on: zoomies-linux-x64      # the cheap one
     steps:
       - uses: actions/checkout@v4
```

Four things it leaves alone, and says so:

| It sees | It does | Because |
| --- | --- | --- |
| `runs-on: ${{ matrix.os }}` | Skips it | What it resolves to is decided elsewhere in the file, or in a reusable workflow, or by a repository variable. |
| `runs-on: [self-hosted, linux]` | Skips it | Somebody already pointed this job somewhere deliberate. |
| `runs-on: buildjet-4vcpu-ubuntu-2204` | Skips it | Not one of GitHub's labels, so it is another vendor's or your own. |
| A hosted label you did not map | Skips it | You chose to leave it on GitHub's runners. |

Each skip is listed in the review step and again in the pull request body, so
whoever reviews the change can see which jobs are still running on GitHub after
they merge it.

## The labels it writes

A pool's branded label, on its own:

```yaml
jobs:
  build:
    runs-on: zoomies-linux-x64
```

One label is enough to reach a pool, and it is what a workflow should write. The
older habit — `runs-on: [self-hosted, linux, x64]` — is longer, says nothing
about *which* fleet, and breaks as soon as two pools share an architecture.

Every pool also answers to `zoomies`, so `runs-on: zoomies` means "any runner
this fleet has". That is the line to write in a repository nobody has decided a
pool for yet.

## One job at a time

The **Labels** step answers "where does `ubuntu-latest` go" once, for every
repository at once. That is what a fleet nearly always wants, and it is the
default every job starts on.

"Nearly always" is not always. The **Exceptions** step lists every job the scan
found, with the answer the label mapping already gave it, and changing one
changes only that job:

| You want | You do |
| --- | --- |
| The integration suite on the big host | Point `acme/widgets` → `ci.yml` → `integration` at that pool. |
| One job left on GitHub while everything else moves | Point it at **Leave this job on GitHub**. |
| A repository moved a job at a time | Leave the mapping empty and point the jobs you are ready for. |
| One Windows job moved, with no Windows pool proposed | Point it at the pool you know can take it. |

An exception names exactly one place — a job, in a workflow file, in a
repository. There are no patterns and no wildcards, because a pattern would
match jobs you did not read in the review step, and reading the exact change is
what this wizard is for. Two repositories with a job called `build` are two
jobs.

Doing nothing here is the normal outcome. Every row starts on **Use the label
mapping**, and the review step marks the ones that do not, so a diff that
disagrees with the mapping always says why.

The jobs in the table above are listed too, greyed, with the same reason: a
`runs-on: ${{ matrix.os }}` is not a decision an exception can make either, so it
is not offered as one. So is a `runs-on` the scan could not attribute to a job
name — there would be nothing stable to pin the exception to when the pull
request step re-reads the file.

## Permissions

This is the only thing in Zoomies that writes to a repository, and it needs
three App permissions the rest of Zoomies deliberately does not ask for:

| Permission | Level | Why |
| --- | --- | --- |
| Contents | read and write | To create a branch and commit the file. |
| Pull requests | read and write | To open the pull request. |
| Workflows | write | GitHub requires it specifically to change files under `.github/workflows`. |

They are not in the App manifest the installer builds, because an App that
manages a fleet's runners is a high-value credential and most fleets never
migrate anything. Add them yourself, once, when you want to:

1. Open the App's settings — the review step links straight to the page.
2. **Permissions & events**, set the three above.
3. Accept the change on the installation. GitHub asks the account's owner.

The wizard checks before it tries. If they are missing it says so at the review
step, names each one the way GitHub's settings page names it, and refuses to go
further — rather than discovering it halfway through a batch with three of your
eight repositories done.

## What it will not do to you

* **It never force-pushes.** Each run commits to a branch carrying its own
  timestamp, so running the wizard twice opens a second pull request beside the
  first rather than rewriting one somebody is reviewing.
* **It never clobbers a concurrent push.** Every file is committed against the
  blob SHA it was read at. A push that lands while you are reviewing is a
  conflict, and that repository fails with a message rather than silently
  reverting somebody's change.
* **It never opens more than 25 pull requests in one call**, and it refuses an
  empty repository list rather than defaulting to everything the App can see. A
  mistake should be 25 pull requests to close, not an organisation-wide one.
* **It never merges anything.** The pull request is somebody's to review.
* **A failure is contained.** Each repository is its own branch and its own pull
  request, so one failing leaves it exactly as it was and the others alone.

## Doing it from the API

The wizard is two endpoints, and they are as usable from a script as from a
browser. See [the API surface](api-surface.md#migrations).

```sh
# What would change. Writes nothing.
curl -sS -X POST https://zoomies.example.com/api/v1/migrations/plan \
  -H "Authorization: Bearer $ZOOMIES_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"installation_id": "ins_...", "repos": ["acme/widgets"]}'

# Open them.
curl -sS -X POST https://zoomies.example.com/api/v1/migrations/pull-requests \
  -H "Authorization: Bearer $ZOOMIES_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
        "installation_id": "ins_...",
        "repos": ["acme/widgets"],
        "mapping": {"ubuntu-latest": "zoomies-linux-x64"},
        "overrides": [
          {"repo": "acme/widgets", "path": ".github/workflows/ci.yml",
           "job": "integration", "to": "zoomies-linux-arm64"},
          {"repo": "acme/widgets", "path": ".github/workflows/ci.yml",
           "job": "flaky", "to": ""}
        ]
      }'
```

`overrides` is the Exceptions step, and it is optional on both endpoints. Each
entry needs `repo`, `path` and `job`; a `to` of `""` leaves that one job on
GitHub's runners. A request that maps no label at all is accepted as long as one
override carries a `to` — pointing three jobs at a pool by name is a migration
too. An override that names a job the plan could not attribute, or a path that
is not a workflow GitHub runs, is refused rather than dropped.

Both need the operator role. `plan` costs a burst of the installation's GitHub
quota — the same quota the scheduler uses — which is why a viewer cannot spend
it.

The apply call re-reads and re-plans from each repository's current contents
rather than trusting a plan a client hands back. The workflows may have moved
since the review, and an endpoint that committed file contents supplied by its
caller would be a way to write arbitrary files into any repository the App can
reach.

## After it merges

Nothing else to do. The next `workflow_job` webhook for that repository arrives
with your pool's label on it, the scheduler matches it, and a runner starts. If
a job queues and nothing happens, the **Jobs** page has an "unmatched" filter
that finds jobs no pool claims and says why — usually a label typo, or a pool
that is disabled.

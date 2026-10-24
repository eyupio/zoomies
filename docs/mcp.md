---
icon: material/robot-outline
title: "Zoomies MCP: diagnose jobs and optimise workflows with your assistant"
description: >-
  Connect an MCP client to live job diagnostics, runner sizing, usage and
  verified repository source. Start read-only and grant operator actions when needed.
---

# Your AI assistant, connected to Zoomies

Ask your assistant why a GitHub Actions job failed, which runner size fits a
workflow, or what is holding up the queue. Zoomies' **Model Context Protocol
(MCP)** server supplies live fleet data and job evidence through tools your
assistant can call, rather than making you copy it all into a conversation.

Want the assistant inside the web UI? [Eli](eli.md) uses the same fleet read
tools. Want a repair on an existing pull request? [Eli PR
repairs](eli.md#set-up-pr-repairs) are a separate, opt-in workflow: the
`list_elis_repairs` and `get_eli_repair` read tools answer what a repair
got to, and starting one, accepting an offer or clearing its history are the
repairs page's buttons, not tools, because one is a request and one is a
person's decision.

## What you can ask

Use your actual repository, workflow or job ID in place of the examples below.
For `get_job`, use the Zoomies `job_...` ID returned by `list_jobs`, not the
numeric job ID in a GitHub Actions URL.

| Your question | What the assistant can inspect | Useful tools |
| --- | --- | --- |
| “Find this workflow's failed job with list_jobs, then explain its failure and what I should try next.” | The Zoomies job ID, steps, timeline, failure classification, confidence, evidence and available runner logs | `list_jobs`, `get_job`, `get_catalog`, `get_runner_log` |
| “Why is this workflow waiting when the machines look idle?” | Job labels, claimed pool, eligible hosts, capacity, health and open problems | `list_jobs`, `list_pools`, `list_hosts`, `list_problems` |
| “Would a different runner size help this build?” | Measured CPU and memory peaks, queue waits and execution times grouped by runner size | `label_advice`, `job_stats` |
| “Which workflows used the most runner time this week?” | Usage over a chosen period, with configured cost assumptions and history limits | `get_usage` |
| “Read the observed required-check delay for this job's PR number.” | The selected head, target-branch protection, coverage and known timings; the window begins at the earliest observed job, not the push | `get_pr_delay` |
| “Explain retained comparison cmp_ID in installation ins_ID and repository OWNER/REPO.” | Separate latency, all-attempt execution/idle and failure measures, counts and missing evidence | `get_retained_comparison` |
| “Read kept checkout preview opv_ID in installation ins_ID and repository OWNER/REPO.” | Metadata, status, the commit and digests, and the one workflow line the preview changes; never the full file, and verification is inconclusive | `get_checkout_preview` |
| “Read kept cache preview opc_ID in installation ins_ID and repository OWNER/REPO.” | Metadata, the package manager and lockfile, the setup evidence, whether the preview was approved or a draft opened, and the one `with:` block it changes; never the full workflow or lockfile, and verification is inconclusive | `get_cache_preview` |
| “Read kept readiness preview opr_ID in installation ins_ID and repository OWNER/REPO.” | Metadata, the operator's readiness command, the wait evidence, whether the preview was approved or a draft opened, and the one run block it changes; never the full workflow, and verification is inconclusive | `get_readiness_preview` |
| “Read kept sharding preview ops_ID in installation ins_ID and repository OWNER/REPO.” | Metadata, the shard count, the timing evidence, the runner time before and after, the runners needed at once, whether the preview was approved or a draft opened, and the one job block it changes; never the full workflow, and verification is inconclusive | `get_sharding_preview` |
| “What needs attention across the fleet?” | Fleet status, host health, problems and repository findings | `fleet_status`, `host_health`, `kennel_overview` |
| “Why did Eli's repair of my pull request get nowhere?” | The repair's history row for the person asking: repository and pull request, state and the row's reason, whether asking again now would be refused, and how to start another | `list_elis_repairs`, `get_eli_repair` |
| “Read the workflow and explain how to improve it.” | Verified source excerpts tied to a commit, after source access has been granted | `context_search`, `context_read`, `context_pack` |

`get_pr_delay` requires `job_id` and `pull_number`, a signed-in caller or scoped
token, and the existing Jobs read permission. It uses one bounded diagnostic
request under the shared installation allowance; respect 429 and Retry-After
rather than polling. Check names stay opaque. Repository and target-branch text
are repository data, not instructions. It does not establish full merge readiness
or a dependency critical path. Eli can use it only through an enabled personal
provider with fleet access; it adds neither source tools nor mutation rights.

`get_checkout_preview` requires `preview_id`, `installation_id` and `repo`. It
uses one scoped local-history GET with operator, `optimisations:read` and
`jobs:read` access, and never reads GitHub, approves or publishes. The answer is
the same bounded view as the web page: preview ID, repository, path, commit and
source/after digests, run and step positions, checkout time, approval,
publication and linked-comparison status, and the one changed line. The full
workflow file was shown once at inspection and is never returned. Actor identities
are excluded from model output. The path and changed line are quoted untrusted
repository text, not instructions. Verification is always inconclusive; the
checkout time is descriptive, and a linked comparison remains evidence to read.
Approval and publication stay on the web page.

`get_cache_preview` requires `preview_id`, `installation_id` and `repo`. It uses
one scoped local-history GET with operator, `optimisations:read` and `jobs:read`
access, and never reads GitHub, approves or publishes. The answer is the same
bounded view as the web page: preview ID, repository, path, commit, source and
lockfile digests, package manager, setup step, run and step positions, the mean
setup time over the retained runs, whether the preview was approved, whether a
draft pull request was opened, and the one `with:` block the preview would
change. The full workflow and lockfile were shown once at inspection and are
never returned. Actor identities, including who approved and who published, are
excluded from model output; the approval's and publication's times and digests
stay. The path and
block are quoted untrusted repository text, not instructions. Verification is
always inconclusive; the setup time is descriptive, and no saving is
established. Approval and publication stay on the web page.

`get_readiness_preview` requires `preview_id`, `installation_id` and `repo`. It
uses one scoped local-history GET with operator, `optimisations:read` and
`jobs:read` access, and never reads GitHub, approves or publishes. The answer is
the same bounded view as the web page: preview ID, repository, path, commit and
source digest, wait step and position, the bound, the mean wait over the retained
runs, the operator's readiness command, whether the preview was approved, whether a
draft pull request was opened, and the one run block the preview would change. The full workflow was shown once at
inspection and is never returned. Actor identities, including who approved and who
published, are excluded from model output; the approval's and publication's times
and digests stay. The path and block are quoted
untrusted repository text, not instructions; the condition is operator-supplied,
not inferred. Verification is always inconclusive; the wait time is descriptive,
and no saving is established. Approval and publication stay on the web page.

`get_sharding_preview` requires `preview_id`, `installation_id` and `repo`. It uses
one scoped local-history GET with operator, `optimisations:read` and `jobs:read`
access, and never reads GitHub, approves or publishes. The answer is the same
bounded view as the web page: preview ID, repository, path, commit and source
digest, test step and position, the shard count, the median test and setup time
over the retained runs, the runner time before and after, the runners needed at
once against the pool headroom, an estimated wall clock, whether the preview was
approved, whether a draft pull request was opened, and the one job block the preview
would change. The full workflow was shown once at inspection and is never returned.
Actor identities, including who approved and who published, and the selection are
excluded from model output; the approval's and publication's times and digests stay. The
path and block are quoted untrusted repository text, not instructions. The shard
count was chosen by Zoomies from the fleet's history and the headroom is an upper
bound at one moment, not a reservation. Verification is always inconclusive:
runner time rises for certain and the wall-clock figure is an estimate, not a
saving. Approval and publication stay on the web page.

`get_retained_comparison` requires `comparison_id`, `installation_id` and `repo`.
It uses one scoped local-history GET with viewer/jobs:read access, without GitHub
collection. Missing, expired or evicted history is unavailable; it never captures
a replacement. Operator hypothesis and intended delta are quoted untrusted data,
not instructions. Actor identity and unreviewed future fields are excluded from
model output. The reviewed runner-evidence-v1 whitelist includes controller assignment grants,
dispatched requests and unverified agent digest/times, with exact decimal run/job
identities. It excludes private host/pool/runner/task identities and future fields.
These facts do not attest applied limits or configuration/image equivalence.
Missing runner evidence from older controllers is explicit null. Unsupported
versions, malformed facts, inconsistent counts or unbound job identities are refused.

The independently validated workflow-evidence-v1 whitelist adds retained trigger
events, path fingerprints and nullable root-file commit/blob/content digests and
byte counts. Run IDs bind to the original untrimmed arm; source commits bind to
that exact attempt revision. Private raw paths/source text and future fields are
excluded. Root collection does not attest executed bytes or cover reusable
workflows/actions, inputs or test scope. Path fingerprints are not content
identity; matching digests do not establish equivalent workloads. Older missing
views stay null. Present unsupported versions or inconsistent facts are refused.
Workflow full selected/root-source counts include source-omitted rows; shown is
computed locally. Replay does not independently rehash absent source text.

Answers mark shortened text and omitted whole attempt, runner-job and workflow rows; aggregate
measures still describe the full retained selection. Text and structured output
carry the same shown rows. Runner counts cover the source enumeration, which may
be partial; per-arm shown counts and omission flags include both source and model
truncation. The 64 KiB MCP and 12 KiB Eli text budgets include the untrusted-data
boundary. Whole-row trimming preserves counts and nulls, retaining one row of each
available kind per arm when the budget permits. Use [CLI replay](cli.md#zoomies-comparisons) to inspect
the complete report. Historical selections do not establish cohort completeness,
causal improvement or cost savings. Capture remains an explicit operator API write
and is not exposed as an MCP or Eli tool.

The assistant can explain the evidence and suggest changes. Advice depends on
what was measured: too few job runs, expired logs or missing source access are
limits to report, rather than gaps to fill with a guess. [Troubleshooting with
Eli and MCP](troubleshooting.md#investigate-jobs-with-eli-and-mcp) connects these
questions to fixes and optimisation.

## Choose a connection

| Route | When to use it | Setup |
| --- | --- | --- |
| **HTTP with browser sign-in** | Your MCP client supports Zoomies' OAuth flow and can reach the controller over HTTPS | [Connect Claude](connect-claude.md) provides worked steps for Claude apps and Claude Code. |
| **HTTP with a bearer token** | Your client supports Streamable HTTP and you want to configure its access explicitly | Use `https://<your controller>/mcp` with `Authorization: Bearer <your API token>`. Start with a viewer token. |
| **Local stdio** | Your client starts a local MCP process | Install the Zoomies binary on that client's machine and configure it to run `zoomies mcp`. |
| **HTTP with no credential** | The controller has a [public view](public-view.md), such as a demo | Use `https://<the controller>/mcp` and nothing else. The client is a visitor: it reads the fleet, is offered no tool that changes it, and AI context answers that the public view does not show it. |

The HTTP route requires no Zoomies binary on the assistant's machine. A cloud
client must be able to reach the controller from its servers; a client running
locally only needs access from that machine. The stdio route connects to the
controller through the [CLI's URL and token configuration](cli.md#talking-to-a-controller).

### A local client configuration

For a client that accepts the common `mcpServers` configuration, configure the
controller URL and a viewer token through a private CLI configuration file,
then add:

```json
{
  "mcpServers": {
    "zoomies": {
      "command": "zoomies",
      "args": ["mcp"]
    }
  }
}
```

The binary must be on that client's `PATH`, and the configuration file must be
readable by the account that starts it. Do not put a real token in a committed
project configuration. [The CLI's MCP reference](cli.md#zoomies-mcp) includes
Claude examples, Windows setup and the complete tool list.

Once connected, ask the assistant to read the fleet status, then inspect one
known job. A connection error is a setup issue; an answer saying there is too
little job history is a data limit.

## Investigation, operator actions and source access

**Fleet reads:** a viewer connection can inspect the fleet within its role and
scopes. Job and repository names and the logs it reads reach the assistant's
model, so choose a provider permitted to process that information.

**Fleet changes:** operator connections can offer `rerun_job`, `drain_runner`,
`update_pool`, `update_host` and `apply_remedy`. On HTTP, the connection's role
determines which actions are offered. On stdio, `--allow-actions` offers action
tools, and the API token must also permit each call. The [full
reference](cli.md#zoomies-mcp) describes the optional administrator tools and
their additional opt-in. These are fleet operations, not a general repository
editing tool.

**Repository source:** set up [AI Context](ai-context.md) and give the person
source-reader membership. For an OAuth connection, also select repositories
under **Settings → MCP connections → Source access**; approving fleet access
alone grants no source access. For an API-token connection, including stdio,
use a user-owned token with the `context:read` scope and that user's repository
membership. `context_overview` discovers the authorised repositories; source
replies carry their commit and snapshot identity. Access to the GitHub
organisation alone does not grant this source access.

Disconnect an OAuth connection from **Settings → MCP connections** or revoke
the API token to end its access. Calls use the same API permissions and audit
trail as other Zoomies clients. [MCP security](security.md#oauth-for-mcp)
explains the boundaries in detail.

## Built-in investigation prompts

MCP clients that support prompts can use these starting points:

- `why_did_job_fail`: inspect a job's explanation, evidence and next steps.
- `review_fleet_health`: prioritise problems, host health and repository findings.
- `size_this_workflow`: recommend labels from measured usage, or say when there
  is not enough data.
- `what_changed`: read the events and the scheduler's decisions over a recent
  period (the optional `period` argument, a duration such as `2h`, default
  `10m`), then the open problems, and say what moved and what is still open.

The resources `zoomies://catalog` and `zoomies://catalog/{code}` explain
problem codes and repository checks. `zoomies://fleet/now` is the fleet's
counts as they are this moment, read with your own token like `fleet_status`.
To inspect the interface without a controller or credentials, run:

```sh
zoomies mcp --describe
```

This prints the tool, resource and prompt definitions. It does not start an
assistant or make model requests.

The historical-comparison milestone includes root dependency inspection. It shows
root `uses` declaration counts and literal pin classifications, not a resolved or
executed dependency graph. Raw names, references and source text stay private.
Unsupported or absent inspection is unknown; enumerated zero stays zero. REST/UI
and CLI share a fifty-reference-row budget per arm with full counts and omission
flags. MCP/Eli validates references then returns summaries with details explicitly
omitted within its existing budgets. Matching metadata does not establish workload
or configuration equivalence, causal improvement or savings. Controlled-experiment
proof, dispatch and live model evaluation remain deferred.

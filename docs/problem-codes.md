---
description: >-
  Every problem code Zoomies can raise, with its severity and what to do about
  it: the startup validator's findings and the running controller's, in one
  list.
---

# Problem codes

Zoomies never reports a problem without a code. The code is stable across
releases and across languages of complaint — it is what you search for, alert
on, and quote in a bug report — while the sentence beside it is written for the
person reading it and may improve over time.

Problems come from two places and behave differently:

* **The startup validator** reads `zoomies.yaml` and the `ZOOMIES_*`
  environment before anything else happens. Its findings are printed at startup
  and shown in the UI's problems panel for as long as the setting stands.
* **The controller** raises the rest while running, after every reconcile pass.
  These come and go with the fleet: a host that starts answering again clears
  `host.unhealthy` by itself.

Both use the same three severities:

| Severity | What it means |
| --- | --- |
| **Error** | Startup stops, or the fleet cannot do the thing being asked of it. A configuration error is fatal; a runtime error is work that is not happening. |
| **Warning** | Nothing stops. Something is weaker or slower than the default, and the entry says what that costs. |
| **Info** | Neither wrong nor risky, but worth knowing — usually a default that surprises people. |

A few codes change severity with the circumstances, and each says so below.
`security.disable_auth` is the clearest case: on loopback it is a warning, and
on a public address the same setting is an error.

## Configuration: the listener

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `bind.empty` | error | `server.bind` | Give it a `host:port`. Nothing can start without one. |
| `bind.malformed` | error | `server.bind` | It is not a `host:port` address. A bare port needs the colon: `:8080`. |
| `bind.public_no_tls` | warning | `server.bind` | Sessions and API tokens cross the network in the clear. Put TLS in front of it, or bind to loopback and tunnel. |
| `proxy.untrusted` | info | `server.trusted_proxies` | Client IPs in the audit log are the socket's, not the header's. Correct behind nothing; wrong behind a proxy, which is what the setting is for. |
| `proxy.bad_cidr` | error | `server.trusted_proxies` | An entry is not an IP address or a CIDR block. |
| `proxy.trust_everyone` | warning | `server.trusted_proxies` | Any client can claim any address, so the audit log's IPs mean nothing. Name your proxy's range instead. |
| `origins.any` | warning | `server.allowed_origins` | Any website can act with a signed-in operator's session. Name the origins you serve from. |
| `origins.insecure` | warning | `server.allowed_origins` | A plaintext origin is allowed to act on this controller. |
| `indexing.allowed` | warning | `server.allow_indexing` | Search engines are invited to index a fleet controller. Deliberate for a demo, rarely otherwise. |
| `tls.mode_unknown` | error | `server.tls.mode` | Not a TLS mode. The modes are `off`, `self_signed` and `files`. |
| `tls.files_missing` | error | `server.tls` | `mode: files` needs both a certificate and a key. |
| `tls.file_unreadable` | error | `server.tls.*` | The file is named but cannot be read. Usually ownership after an install as another user. |
| `tls.self_signed` | info | `server.tls.mode` | Browsers and agents will not trust it without being told to. Fine for a private network, not for anything else. |
| `external_url.missing` | warning | `server.external_url` | GitHub cannot be told where to deliver webhooks, so scaling falls back to the poller and reacts in tens of seconds. |
| `external_url.malformed` | error | `server.external_url` | Not an absolute URL. |
| `external_url.insecure` | warning | `server.external_url` | GitHub will deliver webhooks over plaintext, so the payloads and their signatures cross the internet unencrypted. |

## Configuration: GitHub

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `github.api_base_missing` | error | `github.api_base_url` | Empty. Leave it unset for github.com, or give GitHub Enterprise Server's API base. |
| `github.api_base_malformed` | error | `github.api_base_url` | Not an absolute URL. |
| `poll.disabled` | warning | `github.poll_fallback` | If a webhook delivery is lost or misconfigured, jobs queue for ever with nothing to notice. Leave it on unless you monitor delivery yourself. |
| `poll.too_fast` | warning | `github.poll_interval` | Polling this often will consume the App's API rate limit. The poller is a safety net, not the primary path. |

## Configuration: storage and secrets

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `db.path_missing` | error | `database.path` | Empty. |
| `db.parent_not_dir` | error | `database.path` | The parent exists and is not a directory. |
| `crypto.no_key` | warning | `security.encryption_key_file` | One will be generated on first start, and it is the only copy. Back it up: without it the stored GitHub App private key and webhook secrets cannot be decrypted. |
| `crypto.key_in_config` | warning | `security.encryption_key` | The key is in the config file, so anything that reads the file — a backup, a support bundle — reads every stored secret. Point at a file instead. |

## Configuration: authentication

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `auth.disabled` | **error on a public bind, warning on loopback** | `security.disable_auth` | Every request is treated as an administrator. Acceptable for local development; never on a host others can reach. |
| `auth.no_login_limit` | warning | `security.rate_limit_logins` | Password guessing is not rate limited. |
| `auth.session_ttl` | error | `security.session_ttl` | Must be positive. |
| `auth.session_ttl_long` | warning | `security.session_ttl` | A stolen session cookie stays useful for this long. |
| `oidc.incomplete` | error | `oidc` | Single sign-on is enabled with no issuer or no client id. |
| `oidc.no_redirect` | error | `oidc.redirect_url` | Needed, and it must be an address the identity provider can reach. |
| `oidc.insecure_issuer` | warning | `oidc.issuer` | The token exchange happens in the clear. |
| `oidc.link_by_username` | warning | `oidc.link_by_username` | A first single sign-on login can take over an existing password account with the same name. |
| `oidc.open_signup` | warning | `oidc.allow_signup` | Anyone your identity provider authenticates gets an account here. Narrow it at the provider, or turn signup off and create accounts yourself. |

## Configuration: the agent

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `agent.backend_missing` | error | `agent.backend` | No runner backend selected. |
| `agent.backend_unknown` | error | `agent.backend` | Not a backend. They are `docker`, `podman` and `process`. |
| `agent.capacity` | error | `agent.capacity` | Must be at least 1, or the host can never take a runner. |
| `agent.workdir` | error | `agent.work_dir` | Empty. |
| `agent.process_backend` | warning | `agent.backend` | The process backend gives jobs no container isolation: a job can read and write anything the agent user can. |
| `agent.process_root` | warning | `agent.backend` | The process backend is running as root, so every job is root on the host. |
| `agent.root` | warning | `agent` | The agent process is running as root. Raised only where an agent actually runs. |
| `agent.insecure_tls` | warning | `agent.insecure_skip_verify` | The agent does not verify the controller's certificate, so anything on the path can impersonate it. |
| `agent.unverified_runner_download` | warning | `agent.allow_unverified_runner_download` | The process backend may install a runner archive whose checksum it could not confirm. |
| `agent.finished_retention` | error | `agent.finished_retention` | Cannot be negative. |
| `agent.finished_retention_long` | warning | `agent.finished_retention` | Finished runners stay on the host this long, holding disk and, for a non-ephemeral pool, whatever the job left behind. |
| `agent.heartbeat_interval_long` | warning | `agent.heartbeat_interval` | Hosts heartbeat less often than the controller's timeout, so a healthy host will be counted lost. |
| `agent.none` | info | `agent.embedded` | This controller hosts no runners itself, so at least one standalone agent has to join it. |

## Configuration: the scheduler and the rest

| Code | Severity | Setting | What to do |
| --- | --- | --- | --- |
| `scheduler.interval` | error | `scheduler.interval` | Must be positive. |
| `scheduler.burst` | error | `scheduler.max_creates_per_tick` | Must be at least 1. |
| `scheduler.lifetime_short` | warning | `scheduler.max_runner_lifetime` | Idle runners are recycled sooner than a long job takes, so work may be interrupted. |
| `capacity_demand.url` | error | `capacity_demand.destination_url` | Not an absolute HTTP URL. |
| `capacity_demand.secret` | error | `capacity_demand.signing_secret` | Empty, so deliveries could not be signed and a receiver could not tell them from anyone else's. |
| `capacity_demand.cooldown` | error | `capacity_demand.cooldown` | Must be positive. |
| `capacity_demand.timeout` | error | `capacity_demand.timeout` | Must be positive. |
| `metrics.public` | warning | `metrics.public` | The metrics endpoint answers without authentication, so anyone who can reach it learns the shape of the fleet. |
| `log.level` | error | `log.level` | Not a level. They are `debug`, `info`, `warn` and `error`. |
| `log.format` | error | `log.format` | Not a format. They are `text` and `json`. |
| `log.debug` | info | `log.level` | Debug logging is on, which is loud and includes request detail. |

## Runtime: hosts and installations

| Code | Severity | What it means |
| --- | --- | --- |
| `host.unhealthy` | **error with runners on it, warning without** | The host has stopped heartbeating. With runners recorded on it their state is unknown, which is worse than a spare host being down. |
| `host.cordoned_with_work` | warning | A cordoned host could run jobs that are queued. Cordoning is deliberate, so this is a reminder rather than a fault. |
| `installation.unhealthy` | error | The GitHub App installation is not usable — the App was uninstalled, its key was rotated, or its permissions were changed. Nothing can register until it is fixed. |
| `webhook.rejected` | warning | Deliveries arrived and were refused, almost always a signing-secret mismatch. |
| `webhook.never_received` | warning | No webhook has ever arrived, so scaling is running entirely on the poller. |

## Runtime: pools, jobs and runners

| Code | Severity | What it means |
| --- | --- | --- |
| `pool.no_capacity` | **error with jobs waiting, warning without** | The pool cannot start the runners it wants. The entry carries the scheduler's own reason: no host matches its selector, every host is full, or every host is cordoned. |
| `pool.runners_failing` | **error with jobs waiting, warning without** | Runners are being created and dying before they register. The usual causes are an image that cannot be pulled and a backend that cannot start a container. |
| `pool.repository_scale_up_deferred` | warning | The per-repository creation throttle held runners back. Expected under a burst; standing means the throttle is too tight. |
| `jobs.unmatched` | warning | Jobs are queued whose labels no enabled pool here claims. They may belong to another runner provider, or a pool may be missing a label. |
| `jobs.runner_lost` | warning | A job's runner stopped under it, so the failure is the fleet's rather than the workflow's. |
| `runners.failed` | warning | Runners are in the failed state with their reasons recorded. |
| `runners.not_progressing` | warning | Runners have sat in `provisioning` or `registering` for over half the provision timeout, so the fleet says so while there is still time to look rather than only when it fails them. The entry splits the two shapes, because they are not fixed in the same place: a runner still waiting for a container is a backend or image problem on the host, and one whose container started without registering is the runner process failing to reach GitHub. |
| `capacity_demand.delivery_failed` | warning | An external capacity provisioner did not accept the latest event, after its retries. |
| `controller.loop_panicked` | error | A background loop panicked and was restarted. The fleet keeps running, but this is a bug: the stack is in the log, and it is worth reporting. |

## Keeping this list honest

Every code above is checked against the source by a test
(`TestEveryProblemCodeIsDocumented`), so a code added to the validator or the
controller without a row here fails the build. The severities and the sentences
are prose and are not checked — if one reads wrong, it is wrong.

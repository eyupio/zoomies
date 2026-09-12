# Private runner hosts through Tailcat

Implemented on `agent/tailcat-private-hosts`, based on main commit
`8287ffdae0193f76b8b9fdaf587c2771f7be542d`.

## Scope delivered

- Built-in Tailcat v0.6.0 transport for outbound agents, with no separate binary.
- Persistent controller identity encrypted in the existing SQLite settings store.
- Agent capability kept in mode-0600 credentials, with a non-secret config alias.
- Agent-only tunnel surface; existing join and agent authentication remains required.
- Add a host connection choices, generated installer and existing-binary commands,
  waiting and error states, observed connection badges and fleet summary.
- CLI `hosts join-token create --connection tailcat`, OpenAPI and generated clients.
- README and site feature promotion plus setup, security, architecture and API docs.
- Go 1.27.1 build floor and CI toolchain updates required by upstream Tailcat.

## Validation completed locally

- Agent, API, store, configuration, installer, controller, CLI and documentation
  Go package tests passed, excluding the real Tailcat relay test below.
- Targeted private-connection tests passed under the race detector.
- `go vet` passed for the changed backend packages and CLI.
- Svelte/TypeScript checks and targeted ESLint passed.
- Production UI build passed its bundle-size checks.
- `mkdocs build --strict` passed.

## Remaining validation before merge

- Run `TestPrivateHostJoinsHeartbeatsAndReconnectsAfterControllerRestart` in CI.
  It uses a real local DERP/STUN relay and verifies encrypted enrolment, heartbeat,
  identity reuse after controller restart, transport observation and token replay
  rejection. This workspace cannot initialise Tailcat because its sandbox denies
  netlink network-interface discovery (`operation not permitted`). The test is
  included in the ordinary Go test suite; it is not skipped in source or CI.
- Run the existing host browser suite and new `private-hosts.spec.ts` on desktop
  and mobile. Browser execution was unavailable locally; the cloud browser also
  refused access to the localhost preview. No browser test is claimed as passed.
- Run `make screenshots` and commit the refreshed full set. Its new `private-host`
  capture photographs the selection step before credentials are generated. Existing
  screenshots have not been altered or substituted with mockups.
- Perform one real home-lab enrolment, restart both ends, interrupt and restore
  connectivity, run a GitHub job and inspect its live log stream.

## Review notes

The badge identifies Tailcat transport, not a measured direct-versus-relayed
path or latency. Custom relay configuration and tunnel identity rotation are
not exposed in this first implementation; disabling and restarting stops the
listener without rotating its saved identity. Deleting a drained host revokes
its agent access. Hosted relay metadata and throughput limitations are documented.

Publishing as a draft pull request has been explicitly authorised. The remaining
validation gates above must be completed before merging.

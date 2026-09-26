# Codex subscription bridge: isolated milestone 1

**Runtime pin note (2026-09-26).** Desk's managed Codex pin has moved from
`0.145.0` to `0.156.0`. The protocol results, the configuration qualification,
the fixture and the pinned references below now describe `0.156.0`. The rest
of this record, including its verification lists and milestone updates, was
recorded against `0.145.0`; the `0.145.0` fixture remains in the repository
history. The probe now counts every tool list in each model request, including
`additional_tools` input items. `0.156.0` is the newest stable release whose
tool inventory passes. From `0.156.1`, `gpt-6-sol` requests have no top-level
`tools` member; an `additional_tools` item advertises code-mode `exec`/`wait`,
`request_user_input_async`, `clock.sleep` and `collaboration.*` agent tools,
although the profile disables code mode, multi-agent work and the experimental
user-input tool. This is a local scripted-model check, not a certification.

Date: 2026-09-25. Status: local protocol proof and Go foundation; **not a usable
or certified subscription feature**.

## What was developed

The initial prototype was developed in
`/tmp/jps-desk-subscription-4mgfp4ys/desk`, a separate copy of the
Desk's tracked and nonignored working files, including its existing uncommitted
changes. Its Git baseline records that snapshot. During those proof runs,
the live Desk checkout, processes, configuration, account stores, and active
conversations were not modified or restarted. Copied dependencies are independent files, not links
back into the live tree.

- `scripts/codex-subscription-probe.py` runs the real installed Codex client
  against a scripted model listening only on loopback. Each scenario has an
  empty temporary home, private Codex profile, and a separate empty Git root.
- `internal/codexbridge` provides a bounded stdio client, pinned handshake,
  Linux process-group cleanup, and initial closed run/authentication contracts.
  Native errors and stderr are not returned to callers. It rejects API-key and
  externally supplied token account types and login methods.
- No routes, configuration decoders, frontend components, engine registry,
  production credential stores, or JPS runtime behavior have been changed.

## Local source merge

On 2026-09-25, at the user's request, the eleven-file milestone patch was
applied to `/home/onword/repo/judgment-pack/judgment-pack-desk` as working-tree
changes. It applied without conflicts. The other existing source files were
verified unchanged, and both pre-existing Desk processes retained their process
IDs and start times. No running server was rebuilt or restarted.

Validation in the merged local tree passed: `go test ./...`, the bridge's race
tests including the credential-free native handshake, `go vet` for the bridge,
patch whitespace, and Python probe syntax. The earlier 340 frontend regression
tests were run in the isolated copy; no frontend files changed in this merge.
Login lifecycle, engine integration, and setup UI remain unfinished.

## Local protocol results

The binary reports `codex-cli 0.156.0`; its SHA-256 is
`78a11f06e0a2dda42d13fba1d50dc62e8cbdb2d5f69789722f4d4d99b5cdbe30`.
The generated experimental schema and matching upstream release source were
used to interpret its behavior. Newer clients require a fresh certification.

All eight scenarios passed under each of two model metadata selections
(`gpt-5.5` and `gpt-6-sol`). These names select native metadata for a scripted
local response; **they are not evidence of account entitlement or live model
availability**. Every model request advertised exactly `jps_probe` in its
`tools` member and `skills.list`/`skills.read` in a `skills` namespace there;
no other tool list appeared.

| Scenario | Observed result |
| --- | --- |
| Host tool | `item/tool/call` received; supplied result returned to model; turn completed |
| Private image | `view_image` unregistered; synthetic private image did not reach model |
| File write | `apply_patch` unregistered; synthetic canary absent |
| Shell | `exec_command` call rejected as unsupported |
| Web | No advertised web tool; synthetic `web.run` call rejected |
| Subagent | `spawn_agent` call rejected as unsupported |
| Skills listing | Empty enabled orchestrator catalog, with a null `next_cursor` |
| Forged skill read | Unavailable package refused (`skill package is not available`) |

The negative control retains a local execution environment. The same probe
correctly fails because `apply_patch` and `view_image` reappear. This catches the
mistake of treating read-only permissions as a tool allow-list.

A separate diagnostic used the installed bubblewrap executable and the named
permissions profile. An attempted synthetic private-image read returned
`Permission denied (os error 13)`. Its overall inventory check intentionally
fails because the diagnostic enables the local environment. This is distinct
from the earlier missing-bubblewrap error caused by the minimal test PATH.

Machine-readable results are in
[fixtures/codex-subscription-proof.json](fixtures/codex-subscription-proof.json).
They contain no real account details, credentials, prompts, or project files.
The private image's temporary path is replaced by a placeholder, and the
client's user agent and stderr diagnostics are omitted.

## Configuration finding and qualification

Use `environments: []` on **both** `thread/start` and `turn/start`. Omission or
null selects or inherits an environment. Keep a named restricted permissions
profile, no escalation, no ambient MCP servers, no apps/plugins/hooks, no
browser/computer/image-generation tools, and no subagents. The host constructs
these fields; they must never come from a browser's raw RPC request.

The strict “only host tools exist” wording in the initial plan needs a narrow
qualification: the no-environment mode also advertises `skills.list` and
`skills.read`. At `0.145.0`, `update_plan` was advertised as well. At `0.156.0`
it is registered only when `tools.update_plan.enabled` is set, which defaults
to false, and the probe now refuses it.
There is no supported general built-in-tool allow-list in this tested version.
The fixed utilities were audited and explicitly tested, rather than silently
ignored by the probe. Planning is internal progress; the skills catalog has no
authority with the selected integrations disabled. The eventual adapter must
count/bound native work and refuse configuration that makes the catalog live.

This is a local candidate boundary, not proof for every OS, future model
catalog, managed host configuration, or authenticated subscription. No real
secret was placed in the fixture; no claim is made about arbitrary filesystem
access beyond the exercised paths and the inspected registration conditions.

## Verification

The new Go tests cover normal/concurrent replies, incompatible versions,
redacted upstream errors, malformed and oversized frames, event-queue overflow,
cancellation/reaping, non-null empty environment arrays, duplicate/native tool
names, subscription-only account status, and constrained login URLs. The opt-in
native handshake test reads only absent account status in a new private profile.
It does not call login or inference.

Validation completed in the isolated copy:

- Native scripted-model checks: 16 passed across the two metadata selections;
  negative control failed as expected.
- `JPS_CODEX_TEST_BINARY=... go test -race ./internal/codexbridge`: passed,
  including the actual native handshake and absent-account check.
- `go vet ./internal/codexbridge`: passed.
- `go test ./...`: passed.
- Existing assistant ToolGate, enforcement, conformance, and endpoint-form
  tests: 340 passed across four files. The SDK emitted existing compatibility
  and deprecation warnings for its scripted models.

These initial checks tested the prototype and existing engine, not certification
of a new Desk engine. No frontend implementation was added in that initial
stage. Subsequent account/run implementation is recorded below.

Run the proof from this copy:

```sh
python3 scripts/codex-subscription-probe.py --codex /absolute/path/to/codex
JPS_CODEX_TEST_BINARY=/absolute/path/to/codex go test -race ./internal/codexbridge
```

The negative control must return a nonzero exit status:

```sh
python3 scripts/codex-subscription-probe.py --codex /absolute/path/to/codex --negative-control --scenario host-tool
```

## Remaining release validation

The profile/account lifecycle, run transport, model discovery, configuration and
setup UI are implemented locally. The milestone updates record their evidence.
Real ChatGPT sign-in and inference, cancellation, restart and disconnect still
need an explicitly initiated release smoke test. The source UI is opt-in through
the trusted installation flag.

Local integration used only the milestone patch, preserving the unrelated
working-tree changes. The full isolated snapshot must not replace the local
checkout. The active Desk remains running with its existing behavior.

## Milestone 2 local update — 2026-09-25

The follow-up fixed numeric rounding in tool schemas: Desk now validates the
root schema type while forwarding a copied raw JSON schema, preserving large
integer and decimal literals. Regression cases include `9007199254740993`.

The private profile manager, native account lifecycle and session-guarded
account routes are now implemented behind the explicit `--codex` flag. The
[account lifecycle record](../design/codex-account-lifecycle.md) describes the
ownership, API contract, timeout, cancellation and durable cleanup behavior.

Validation passed in the local working tree:

- `go test ./...` and `go vet ./...`.
- Bridge race tests with the installed native binary, including a fresh private
  profile, absent-account status and official logout of that empty profile.
- Account HTTP guard/lifetime tests under the race detector.
- Scripted auth scenarios covering success, failure, concurrent admission,
  expiry, session/policy revocation, restart recovery, refresh failure, failed
  logout, and completion racing cancellation.
- Formatting and tracked patch whitespace checks.

No real account login or inference was performed. Both existing Desk processes
were preserved. No frontend or engine registry changes were made in this stage,
and these source changes have not been committed or installed into the live
processes.

## Milestone 3 local candidate update — 2026-09-25

The bounded Go WebSocket transport and browser Codex adapter now connect native
host-tool requests to the existing ToolGate, host tools, proposal parser and
runtime-backed critique recorder. The [run bridge record](../design/codex-run-bridge.md)
describes the exact contracts, budgets, cancellation and verification.

Full Go tests and vet, native/HTTP bridge race tests, targeted browser adapter
and transport checks, existing engine/lifecycle regressions, TypeScript and the
production build passed. A temporary fixture also passed the real Vite WebSocket
upgrade with the required Origin and session-subprotocol forwarding. The recorded
JPS conformance scenario verifies schema preservation and forced rehearsal at the
real ToolGate; its runtime replies are scripted from the recorded corpus.

These are candidate integration tests, not a live subscription certification.
No account login or paid inference was performed. The candidate remains outside
the engine registry and has no setup UI. Existing Desk backend processes were
preserved; source changes remain uncommitted.

## Milestone 4 local setup update — 2026-09-25

The [setup implementation](../design/codex-subscription-setup.md) adds shared
Go/TypeScript configuration, account-model discovery and revalidation, Admin/chat
controls and a common selected-target/readiness path for all assistant consumers.
API configuration remains stored but inactive while Codex is selected. The user
can explicitly run a ToolGate-backed schema connection check; no account or model
query starts inference automatically.

The registry now includes the opt-in agent, with separate protocol matrices and
the shared recorded JPS scenario. Real browser checks used synthetic responses
at desktop and narrow widths. No real login or paid inference was performed;
backend processes and unrelated working-tree changes were preserved.

## Primary references

- [Official App Server protocol](https://learn.chatgpt.com/docs/app-server)
- [Official Codex authentication](https://learn.chatgpt.com/docs/auth)
- [Pinned native tool registration](https://github.com/openai/codex/blob/rust-v0.156.0/codex-rs/core/src/tools/spec_plan.rs)
- [Pinned orchestrator skill authority checks](https://github.com/openai/codex/blob/rust-v0.156.0/codex-rs/ext/skills/src/tools/read.rs)
- [Pinned configuration schema](https://github.com/openai/codex/blob/rust-v0.156.0/codex-rs/core/config.schema.json)
- [Pinned official browser login implementation](https://github.com/openai/codex/blob/rust-v0.156.0/codex-rs/login/src/server.rs)

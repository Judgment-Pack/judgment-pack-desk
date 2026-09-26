# Private Codex account lifecycle

Implemented locally on 2026-09-25. This completes the account backend stage of
[the subscription plan](chatgpt-subscription-plan.md). The subsequent
[run bridge](codex-run-bridge.md) and [setup UI](codex-subscription-setup.md) use
this same account manager. Both API and subscription engines are now
selectable in the source build. No real account was signed in,
and no paid inference was performed during verification.

## Ownership and activation

Desk owns the account bridge in `internal/codexbridge` and the session guards in
`internal/desk/model_providers.go`. Runtime, the gateway, Jobs, and project files
do not receive provider credentials.

Subscription access is enabled by default on supported Linux x86-64 hosts.
The first account operation acquires the private profile; catalog reads do not.
Only explicit login prepares a missing pinned runtime, then starts official
sign-in without restarting Desk. The [setup record](codex-subscription-setup.md)
documents download bounds, integrity and cancellation. Advanced owners may
supply `--codex /absolute/path/to/codex` or disable access with `--codex off`.
No executable path or download source is accepted from a project or HTTP input.
The native initialize response must match `codex-cli 0.156.0`.

The managed profile lives under `<DeskConfigDir>/codex`, outside the open
project. A directory lease permits one Desk process to own it at a time and is
released after its child is reaped. A second process reports unavailable until
restarted after the owner releases the profile.

The owner-controlled Desk directory contains private 0700 subdirectories for
the profile, empty working directory, home and temporary files. Managed files
are 0600. Desk refuses symlinks, a foreign/unmarked profile, altered managed
configuration, unsafe credential-file metadata, and a profile inside the open
project. It never repairs permissions or adopts a terminal profile.

Codex alone reads, writes, and refreshes its credentials. Desk checks metadata
of `auth.json` without reading its contents. The subprocess has an explicit
environment rather than inheriting terminal credentials, API keys, proxy
overrides or provider endpoints. The restricted native configuration follows
the isolation proof. Account methods never start a model turn. The separate
session-bound run method re-verifies subscription authentication before starting
a constrained turn; neither path grants native permission requests. Ordinary chat backups remain allowlisted
and do not include this profile.

## Closed HTTP contract

All routes require a minted Desk browser session and the existing origin
guard. Mutations additionally require the recognized local browser Origin.
The launch/setup secret by itself does not authorize account operations.
Responses are `no-store`; native errors, email addresses, account IDs, and
tokens are not returned.

| Method and path | Body | Behavior |
| --- | --- | --- |
| GET `/api/model-providers` | none | Static provider metadata, configured/enabled flags, required CLI version, `engineReady` for the enabled run implementation |
| GET `/api/model-providers/openai/models` | none | Closed native catalog after subscription-account verification |
| GET `/api/model-providers/openai/status` | none | Native account presence; pending/last login state only for its initiating session |
| POST `/api/model-providers/openai/login` | `{"method":"browser"}` or `{"method":"device"}` | Prepare a missing managed runtime, then official native sign-in; returns one transient challenge and a Desk-generated attempt ID |
| POST `/api/model-providers/openai/cancel` | `{"id":"<attempt-id>"}` | Cancel the initiating session's current attempt |
| POST `/api/model-providers/openai/refresh` | `{}` | Native account read requesting refresh |
| POST `/api/model-providers/openai/logout` | `{}` | Local sign-out of this Desk-owned profile |

Mutations accept at most 1 KiB of JSON with no unknown fields or trailing
values. No route forwards arbitrary native RPC. Busy account operations return
409 rather than queueing another login or credential writer.

Status identifies OpenAI, subscription authentication and the Codex agent.
`connected` means a ChatGPT account is present; it does not certify entitlement,
remaining limits, or access to a particular model. `lastVerified` is the time
of the native account read, not an inference/access check.

A successful login start returns only method, opaque Desk attempt ID, validated
official URL, optional device code and expiry. Challenges are not stored by
Desk or repeated in status responses. The future UI must keep them in transient
memory. A lost challenge can be canceled using the initiating session's pending
attempt ID from status, then restarted.

## Cancellation and durable recovery

A login expires after ten minutes and belongs to both its initiating session
and the sign-in policy epoch. Ending the HTTP request after challenge delivery
does not end the login. Session revocation, policy change, expiry, native process
failure, explicit cancellation, and Desk shutdown stop unfinished work.

Desk records a durable cleanup bit before beginning login or logout. Completion
is accepted only for the current native process and login ID, while the owner
and deadline remain valid, and after a native account read confirms ChatGPT auth.

Cancellation asks native Codex to cancel, stops and reaps that writer, then
performs official logout in a fresh private process. This prevents a completion
racing with cancellation from restoring credentials after logout. The cleanup
bit is removed only after signed-out status is verified and the cleanup process
has been reaped.

If cleanup fails or Desk is interrupted, the bit remains. The next account
operation must complete cleanup before it can report a connected account or
start another login. Shutdown preserves a completed account; a successfully
completed login remains available after restart. A refresh/outage failure does
not erase a completed account or switch to API billing. Local logout is not
represented as account-wide token revocation.

## Verification and remaining work

Scripted native-protocol tests cover browser/device challenges, completion
before response handling finishes, restart persistence, cancellation with a
late credential write, stale/cross-session IDs, expiry, session/policy
revocation, blocked-login interruption, concurrent admission, process failure,
unexpected native requests, wrong account types, bad challenge origins,
refresh failures, logout failures and durable recovery.

HTTP tests cover session/origin enforcement, request bounds, unknown fields,
method restrictions, error redaction, session lifetime and shutdown. Profile
tests cover lease contention, environment isolation, unsafe paths/permissions,
configuration changes and symlink/hardlink refusal.

The installed native CLI passed initialization and signed-out account status
and official logout using the actual managed configuration in an isolated,
empty test profile.
This is credential-free evidence, not a successful real subscription sign-in.

The run transport, configuration, model discovery and setup UI are implemented;
see the linked records above. A real subscription sign-in and release smoke test
remain unperformed. No account presence check claims entitlement or successful
inference.

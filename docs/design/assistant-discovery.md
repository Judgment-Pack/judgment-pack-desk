# Saved connection model metadata

`POST /api/assistant/models` is a fixed authenticated metadata action. It accepts
no body or query. Declared nonempty, chunked and unknown body forms are refused
without reading the input, before the bounded relay transport is entered. The
saved API connection selects its family, destination and
first model-list page. A supplied connection requires its current saved revision;
missing, unknown or stale identities are refused without choosing a default.
Legacy configuration remains supported only while no connection registry exists.

The action internally invokes the existing relay handler with a host-selected
GET suffix. It retains the normal session gate (including the relay's second
session check), connection/key custody, binding check, queue, timeout, cancellation,
response header filtering and one MiB JSON/credential scanner. It supplies only
session/connection headers plus fixed JSON accept and Anthropic version headers;
caller provider headers do not travel. It introduces no provider loop, generation,
retry, pagination or permissions. A refusal keeps the existing status vocabulary;
provider refusal bodies are never rendered by the browser reader.

The browser's existing single family parser retains exact model IDs, display
labels and Gemini's declared `generateContent` filter. A valid empty array is
empty; a missing/wrong array or unreadable JSON is unavailable. Server scanner
refusals stay refusals. Endpoint form changes and saved connection revisions drop
observations and cancel pending requests; changing the form back cannot restore
an old observation. Desk preference discovery also cancels on revision changes
and unmount. Neither result changes the saved connection or authorizes a write.
The existing fixed reachability/keybinding probe remains a separate action.

## Remaining diagnostic execution

Released Agent Go v0.9.0 exposes `relay.Call(context.Context, suffix, relay.Request)`;
`Request` contains method, headers and body, with no destination or credential.
Its source inventory has no dedicated model discovery/probe/connection package.
Metadata therefore reuses the existing Desk transport/custody boundary instead of
creating a second provider implementation or waiting for an unrelated release.

The API capability generation probe is a separate remaining cutover. Agent #186
blocks the required one-request canonical execution budget in v0.9.0; the released
`context.ExecutionController.LimitTo` contract must be fixed and accepted with a
real request/checkpoint fixture before this diagnostic can activate. Do not use
a larger allowance with a one-use host fence, a substituted package, or a fake
execution. The legacy capability probe cannot pass the relay's canonical
non-listing dispatch gate. This metadata action makes no capability-generation
claims and does not activate that path.

Native Test connection uses the closed server `connection-diagnostic` purpose.
The browser requests a model-free reference with only an attempt ID and explicit
saved connection/revision, then starts that exact reference through the canonical
assistant ledger. The host selects the current saved effective model/profile,
fixed instruction and schema-only capability. No prompt, suffix, model, tools,
checkpoint or provider transport can be supplied by that action.

The source digest binds the signed-in session/policy, project, exact connection
and profile, rooted Runtime selection and private native credential-file custody.
The managed profile is private and exclusively leased. Its opaque custody digest
binds manager lifetime/account-operation epoch and rooted file identity/change
metadata; no credential contents, account labels, file identifiers or paths are
returned. External replacement or renewal conservatively invalidates a capture.
This is credential-file custody, not a provider account identity claim. Native
sign-in and catalog/effort admission remain the existing bridge's responsibility,
with source checks at physical native admission/generation and schema dispatch.

Execution uses the released native adapter and managed context/checkpoint. Agent
v0.9.1 reserves `LimitTo` and `requestCap` for API executions and refuses them for
native sessions. The native fixture retains provider-managed usage `0`, the
released native default limit `1` and no API request cap; that limit does not
count the provider’s internal requests. The host admits one schema-only native generation,
verified with one actual schema receipt and no replay; API #186 does not block
this native path. Only `get_schema` is offered to the native peer. Artifact proposals, host tools, writes, acquisition,
evaluation, delegation and compute grants are unavailable. Success requires a
complete current checkpoint, one successful actual Runtime schema receipt and
one matching tool-result observation. Provider confirmation text is insufficient.

A lost start acknowledgement reconciles that exact admission through a read-only
lookup once, without resending or allocating a replacement. A missing uncertain
admission stays unresolved. Explicit Stop persists canonical cancellation before
provider-context cancellation; view detachment only drops browser observations.
Diagnostic reads retain their exact original session/policy owner; replacement
windows cannot inherit their private observations. Restart and restored records
cannot replay. Restored records remain historical
and never establish a current successful connection check. Form, configuration,
profile, account-custody, project and session changes withdraw displayed status;
reverting edits cannot revive an earlier success. Saving remains a separate action.

Public additions are the `connection-diagnostic` owner/purpose, fixed diagnostic
reference/lookup/result routes, and optional opaque `accountRevision` on native
provider status. Private storage adds only the owner enum, using the existing
strict run format, backup/move and historical restore handling. Handwritten owner
vectors and the actual native/schema/checkpoint/restore producer are read by both
Go and the browser; `scripts/dev/assistant-diagnostic.sh` runs that proof.

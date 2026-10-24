# Saved connection model metadata

`POST /api/assistant/models` is a fixed authenticated metadata action. It accepts
no body or query. The saved API connection selects its family, destination and
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

Native Test connection is also separate: `AssistantSettings.tsx` currently calls
`useAssistantRun` with purpose `test-design`, only the saved `get_schema` tool,
and a fixed no-proposal instruction. Its replacement needs the canonical native
run/checkpoint/context path, current saved account/model binding, one schema-read
observation, schema refusal/cancel and no-proposal browser evidence. Reuse the
account/catalog bindings from #636. This requires the shared run/hooks activation
lane and is outside this metadata-only change; it is not blocked merely because
API generation has the #186 budget defect.

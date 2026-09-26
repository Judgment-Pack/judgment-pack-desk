# Bounded Codex run bridge

Implemented locally on 2026-09-25 as the milestone 3 candidate in the
[subscription plan](chatgpt-subscription-plan.md). The Go transport and browser
adapter are implemented and tested. The subsequent
[setup integration](codex-subscription-setup.md) registers the subscription engine and
adds configuration, model discovery and all assistant consumers. The provider
catalog reports readiness for the enabled run implementation. A real signed-in
subscription smoke test remains outstanding.

## Ownership and execution

Desk lazily acquires the private-profile account manager and its run interface.
The managed runtime is prepared on Connect; `--codex` is an advanced installation
override, with `off` available to disable subscription access. Account endpoints
and run transport share one manager and one profile lease. Only Linux and the
pinned `codex-cli 0.157.1` initialization contract are accepted at this stage.
See [account lifecycle](codex-account-lifecycle.md) for credential ownership and
restart cleanup.

`internal/desk/agent_run.go` exposes one authenticated WebSocket at
`/api/agent/run`. It requires a minted Desk browser session, the recognized local
browser Origin, and the existing session subprotocol. A launch secret, query
parameter or arbitrary bearer is insufficient. Vite proxies this specific route
as a WebSocket before its general HTTP `/api` prefix. Agent sockets participate
in server shutdown but receive no MCP/file-watcher broadcasts.

Each socket owns one run and receives an opaque Desk run ID. Native thread,
turn, RPC and item IDs never become browser control capabilities. The closed
client messages are `start`, `tool-result`, and `cancel`; the closed server
messages are `started`, `event`, `tool-call`, and `end`. Unknown request fields,
extra starts, unsolicited/duplicate replies and mismatched run/call IDs end the
socket. There is no raw native-RPC forwarding, reconnect or automatic replay.

The browser binds the selected model, session and fixed route outside the
engine in `assistant/agentTransport.ts`. The candidate adapter receives only
this bounded capability. It cannot select a socket URL, obtain credentials or
switch to another engine. The shared configuration/readiness resolver now constructs this binding. Native
model discovery is rechecked at run admission; stale or unsupported model/effort
selections are refused without substitution.

Before starting a turn, the manager requests native account refresh and requires
ChatGPT authentication. It admits one active run and refuses competing account
operations/runs as busy. It creates a new ephemeral thread with the fixed OpenAI
provider and requested model, no provider/model fallback, private working
directory, `approvalPolicy: never`, and the restricted `jps` permissions profile.
It explicitly disables environments on both thread and turn. The returned
thread metadata must match the requested model, provider, directory and policy.

The empty-skill utilities remain the qualification in the
[isolation proof](../reviews/codex-subscription-proof.md). They are not exposed as
JPS tools. The models a run may name are those of Desk's closed model catalog,
which the private profile supplies and every launched process must list; the
[setup record](codex-subscription-setup.md) explains why. Unknown native RPC
requests, including permission approvals, fail the run; Desk never grants them.

## Tools and presentation

The adapter forwards the served runtime and host schemas without rebuilding
schema properties. It assigns separate `desk_runtime_*` and `desk_host_*` aliases
and retains the original tool name in each description. Duplicate original names
are refused. The backend only accepts declared aliases, matching native
thread/turn/call IDs and object arguments.

Runtime callbacks return through the existing browser `callTool` and ToolGate.
The recorded conformance scenario verifies that an explicit `rehearsal: false`
request for `experimental_evaluate` is rewritten to `true` at the MCP boundary,
with tool-call, guardrail and tool-result events in the existing order. Codex
has no direct MCP connection. Host callbacks use their existing execution
capabilities and cancellation signals. Structured host results remain structured
in Desk's event stream and are included in a text JSON envelope for Codex.

Only assistant text deltas and completed assistant messages leave the native
bridge, with opaque Desk item IDs and normalized commentary/final phases. Raw
reasoning, token accounting, native errors and protocol metadata are omitted.
Delta and completed text must agree. The browser drains queued final messages
before processing a terminal close and bounds its own event queue.

The adapter reuses the existing chat, pack, brief and test-design instructions,
language instructions, streaming prose filter and strict proposal parser. A
plain chat reply creates no proposal. Proposal JSON is separated from prose;
saving/acceptance remains owned by Desk's existing workflow.

Explicit adversarial review opens a second bounded run with the shared critic
prompt. Its verdict uses the shared recorder of actual runtime results. Model
claims and host-source results do not substitute for runtime validation. A
critic that performs no runtime checks cannot attach a verified critique.

## Limits and cancellation

| Boundary | Limit |
| --- | --- |
| Concurrent runs per private profile | 1 |
| Native run deadline / socket lifetime | 10 minutes / 12 minutes |
| Initial browser start / socket write | 10 seconds / 5 seconds |
| Outstanding host callbacks / callback deadline | 1 / 60 seconds |
| Author / critic tool calls | 20 / 4 |
| Author / critic reasoning-item starts | 20 / 4 |
| Native notifications and requests / native item IDs | 10,000 / 256 |
| Native frame / browser socket frame | 4 MiB / 2 MiB |
| Public text, counting deltas and completions | 2 MiB |
| Tool arguments / tool reply | 256 KiB / 1 MiB |
| Browser queued messages / aggregate received text | 64 / 8,388,608 UTF-16 code units |
| Declared tools / aggregate schema and description bytes | 128 / 1 MiB |

The native counts are separate from Vercel's model-step counter: a Codex turn
contains its own loop. Reasoning effort is independent of these budgets. The
candidate accepts the pinned effort enum through `max` and refuses `ultra`;
model-supported effort choices now come from the native catalog in the setup
stage, and are rechecked at admission.

Cancellation, session expiry, sign-in policy changes, socket loss, native
failure, logout and Desk shutdown stop pending work. The manager kills the
managed process group immediately on cancellation and reaps it before admitting
another run. This candidate uses process teardown rather than depending on a
graceful native turn-interrupt acknowledgement, including when native stdin is
blocked. Successful runs also reap their child; credentials remain in the
private profile for a later explicitly started run.

Logout first blocks new admission, cancels and waits for the run, then performs
the account manager's official logout. Profile ownership is released only after
child cleanup. An abandoned browser iterator cancels its run before any pending
tool can proceed; socket loss also aborts an in-flight host callback. Existing
MCP connection ownership remains with the browser assistant lifecycle.

Failures use fixed public codes and messages. Native provider bodies are not
returned or logged. This stage does not yet distinguish all provider quota,
entitlement and transient-outage causes for setup UI. No failure switches to API
billing or automatically retries inference.

## Verification

Local checks passed:

- Full Go suite and `go vet ./...`.
- Bridge race suite, including scripted native run/cancellation/logout scenarios
  and the actual installed CLI's signed-out status/logout in an empty profile.
- Real HTTP WebSocket guard/round-trip tests under the race detector: session,
  Origin, policy revocation, disconnect, duplicate/foreign replies and sanitized
  terminal errors.
- Ten candidate adapter tests, including the recorded real JPS schema/result
  corpus through the actual browser ToolGate and a scripted MCP server. All
  network globals are sealed during that adapter scenario. This verifies wire
  rehearsal enforcement; it is not a new live native-to-JPS subscription run.
- Nine browser transport tests, including late terminal frames, callback abort,
  malformed/unoffered work, session expiry and no automatic reconnect.
- Two overlapping frontend verification batches: 244 tests in five files and
  190 tests in six files, including shared contract/enforcement/conformance,
  existing Vercel behavior and assistant lifecycle regressions. These counts
  must not be summed as unique tests.
- TypeScript checking and production build. The build retains its existing
  large-chunk advisory; scripted SDK tests retain compatibility warnings.
- A temporary local Vite/HTTP fixture verified a real WebSocket upgrade, original
  Origin and subprotocol forwarding, and backend Host rewriting. No credentials
  or model service were used.

No real subscription account was connected and no paid inference was performed.
Existing Desk backend processes were preserved. The source remains uncommitted.
No UI controls or engine registry entries were added in the initial run-bridge
stage; the subsequent setup record describes their implementation.

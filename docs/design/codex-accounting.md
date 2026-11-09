# Private Codex accounting observations

An installation-owned Go host may create `codexbridge.NewAccountingSink()` and
pass it as `desk.Config.CodexAccounting`. Root and shared Desk runs, and separately
managed named accounts, use that sink. The default is nil. It is excluded from
JSON configuration and has no HTTP, project-file, environment or browser mapping.
This is an accounting-only host observation surface, not a provider capability.

The sink holds at most 16 value reports, each with bounded scalar fields and eight
fixed reroute slots. Retained identifiers are copied, with existing 256-byte native
ID bounds and 128-byte ASCII model-name bounds. Each report's retained representation
is below 16 KiB, excluding existing protocol buffers and transient parsing. Publication
is a nonblocking channel send; a full sink increments `Dropped()` and preserves the
original Run result. The sink has no consumer callbacks, workers, IO, locks or close
operation. The consumer owns any subsequent private evidence storage.

Each instrumented `Manager.Run` receives a sink-local monotonically increasing
invocation sequence, including refused/busy calls. Sequence exhaustion produces
sequence zero with an explicit incomplete flag; identities never wrap. A report
publishes after existing cleanup and `ProviderReleased`. Admission and exact owned
client reap are separate flags. A refusal before admission cannot claim another
invocation's release. Publication is observation, not a release acknowledgement.
Failures before a lazy Manager can be constructed, or a process crash before
publication, leave a missing report for the host to identify.

The existing Run loop observes pinned native `thread/tokenUsage/updated` and
`model/rerouted` notifications, scoped to the acknowledged thread and started turn.
It also retains bounded thread/start configuration acknowledgement and scoped
terminal status. A separate strict metadata reader rejects duplicate allowed
members and traverses unknown data under a 512-token, 12-level budget. Budget,
malformed and scope failures affect evidence only; normal dispatch, tool calls,
errors, process ownership, cleanup and admission remain unchanged. Raw frames,
prompts, answers, reasoning, tool arguments/results, account data, credentials,
paths, stderr and error prose never enter reports. Native IDs remain private and
are not added to RunEvent, browser state, shared stores or canonical receipts.

Usage retains the latest valid observed snapshot, without summing updates or
computing differences. If the first update is incomplete, valid members remain
available with explicit missingness. A later malformed update preserves the prior
snapshot and marks the report incomplete. Snapshot observation order and total
update count remain separate. `last` has wire scope `provider_last_breakdown`;
`total` has wire scope `provider_thread_total`. Input, cached input, output,
reasoning output, total and optional cache-write input are independent provider
counters. Reasoning is never added to output. Only `state: "value"` is an observed
nonnegative int64, including explicit zero. Absent, null and invalid states are
unavailable; the optional cache-write schema default is not an observation.
Context-window absence, null and value remain distinct.

All counters are **last observed** and `terminal_usage_finality` is always
`unproven`. The hook sees only notifications the existing loop processes before
returning; it adds no reader, completion drain, delay or protocol request. Even a
completed turn and valid usage snapshot do not prove final or billable usage.
`Incomplete` records missing/invalid observations and unsuccessful runs; unproven
finality is separately explicit and is not itself an added release gate.
Thread/start model and effort are configuration acknowledgements, not proof of
per-turn execution. Any reroute, malformed reroute or acknowledged configuration
disagreement makes a fixed-model comparison ineligible without changing dispatch.

A host must correlate its expected application invocations with reports, sequence
boundaries and dropped counts; missing evidence is never zero usage. Fixed native
IDs alone are not canonical application run identities. Report unexpected runs,
gaps, drops, missing usage, cancellation and incomplete application output explicitly.
Provider-reported tokens do not establish account quota consumption or an enforceable
token ceiling. Monetary cost is unavailable without attributable billing evidence.

Tests use handwritten JSON and directly started test executables with synthetic
private profiles. They cover presence, latest counters, scope, duplicate keys,
traversal/reroute/sink bounds, private-field exclusion, exact reap/publication order,
unchanged requests/events/tools/errors with observation disabled/enabled, and normal
host constructor/factory injection. These are offline observations. A future release
comparison must separately identify each instrumented source, overlay and executable;
an instrumented baseline is not the unchanged published binary. This hook supplies
no launcher, browser orchestrator, baseline overlay or live account operation.

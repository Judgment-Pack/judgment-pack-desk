# Shared delegated tasks

Status: implemented for conversation runs through the Vercel and Codex adapters.

## Decision

Delegation belongs to Desk's shared assistant layer, not Inputs or a specific
provider SDK. `withDelegation` composes the existing `Engine` contract. Profiles
supply instructions and explicit capability classes; workflow adapters mark
eligible host tools. Unmarked tools and all interaction tools are parent-only.
Children never inherit runtime tools, full conversation history, credentials,
proposal actions, workflow execution, approval controls, or delegation itself.

The first profiles are analyze (supplied context), review (read-only context),
and research (read-only context and permitted source acquisition). Inputs and
Library expose their read tools through the same capability metadata. Main chat
and attached Inputs/pack/graph chat use the same coordinator. A future Jobs
consumer can bind the same engine capabilities and checkpoint callback; this
change does not add a Jobs chat or alter the durable Inputs research worker.

## Execution and limits

One response can delegate one batch of up to three independent tasks. API
connections execute at most two children concurrently. The Codex account is
exclusive, so the parent yields first, children run serially, and the parent
then continues. This avoids recursively opening an account already held by its
parent. The SDK's streamText adapter remains responsible for model/tool loops;
there is no second provider client or credential path in the coordinator.

Parent and children share a 20-tool-call ceiling. Each child has at most six
tool calls, six API model requests, a two-minute deadline, and a 12,000-character
retained result. API model requests share the existing 20-request ceiling across
the parent, children and continuation, reserving one for synthesis. Subscription
providers manage their internal model calls; Desk does not pretend to count
those. The Codex path permits at most five provider-run invocations (initial
parent, three children, continuation), with the existing native run bounds.
No budget is replenished by creating a child.

Only an explicit provider `busy` refusal is retried, at most twice with bounded
backoff. It proves that execution was not admitted. Uncertain executions,
authentication failures, invalid responses, and child tool errors are not
blindly replayed. Provider-owned transport recovery retains its existing policy.

Stop or leaving the response cancels the whole batch. A failed child does not
throw away successful siblings; the parent gets the actual completed/failed
results. Children cannot recursively delegate. Outputs remain generated working
material, never verified evidence or authorization. Workflow validators and
human review still own writes and decisions.

## Modes and approvals

Delegation is an execution technique, not a composer mode. Chat/Research controls
source access. Ask me/Auto remains the existing workflow approval policy. Both
can delegate already-authorized read-only work inside the shared budget. A child
that encounters a research choice returns control to the parent; it cannot
approve it. No approval, write, evaluation, workflow-start or budget-increase
capability is handed to a child. Auto grants no additional child permissions.

## Progress and persistence

One task appears as one row in the existing Work disclosure. Its details show
the objective, actual tool operations, result and failure, without adding large
cards above the composer. Running uses the existing spinner in place of the
chevron. Child prose stays in task details; the parent's answer remains the main
conversation.

Transitions are emitted before further work and a supplied checkpoint callback
must finish before the next operation. Chat binds it to the existing project
conversation store and waits through an in-flight debounced save. Failure to
save stops further delegation. Results and bounded operation summaries are saved
with the owning response. Reload preserves completed findings and marks queued
or running tasks interrupted; it never restarts them. Execution retains chat's
existing window-owned lifetime, not a detached background-job lifetime.

## Validation

Scripted tests cover the actual Vercel and Codex adapters, isolated context,
capability restrictions, shared ceilings, concurrency, failed siblings, Stop,
checkpoint failure, typed busy-only retry, parent continuation and restoration.
The browser fixture uses an exclusive fake provider and temporary project to
exercise saved task details, narrow layout, cancellation and reload. No paid
provider calls or user project data are needed for these checks.

References: [Vercel subagents](https://ai-sdk.dev/docs/agents/subagents),
[Desk engine boundary](0008-codex-subscription-agent.md).

## Durable workflow delegation

Inputs research also binds the provider-independent Go `internal/delegation`
coordinator. It owns bounded dispatch, state transitions and cancellation; the
workflow adapter owns scoped context, permitted actions, budget reservations,
source verification and record publication. Other workflows can bind the same
hooks without importing Inputs types. Interactive chat keeps its existing
window-owned execution lifetime; this change does not turn chat into a job.

Research recipes optionally save `execution: {approach, concurrency}`. Missing
settings preserve Standard behavior. Delegated enables independent discovery and
candidate tasks. Concurrency zero means Auto, one or two means an explicit
maximum. The reviewed run stores both the requested and effective concurrency.
The current Codex adapter caps AI execution at one; API connections support up
to two research workers. Changing settings never starts or reconfigures a run.

The coordinator can delegate up to three distinct discovery objectives in a
batch. Discovery children search permitted sources and return candidate names
and retained references. Each claimed candidate receives a separate preparation
task. Selected-record reprocessing creates one task per immutable selected row,
with only that row's retained evidence and no source-acquisition capability.
Children cannot delegate, change providers, expand budgets, approve, evaluate or
write records themselves. They return host-validated actions. Each has at most
six model requests, six source attempts and five minutes, bounded further by the
parent's approved run limits and 30-minute lifetime. Completed findings flow
through the existing guarded publication path as they arrive.

All model and source admissions, including waiting requests, reserve the shared
allowance atomically. Ordinary discovery preserves two coordinator requests for
final reconciliation. Source retries have independent attempt-owned schedules.
A duplicate acquisition waits for an in-flight request or reuses its retained
response; unknown outcomes are never replayed. API capacity is shared by model
relay and internal research across the owning Desk process and managed Desks:
four total in flight, at most two background research calls, leaving room for
interactive work. A 429 temporarily lowers admission to one without retrying the
model request. Existing Gateway source admission still bounds HTTP acquisition.

Task assignment and every operation are checkpointed before dispatch. Fatal
storage, policy or verification errors cancel and join the batch. Ordinary
child failures retain successful siblings. Stop cancels all children; process
recovery preserves sources/results, marks unfinished tasks interrupted and does
not automatically restart paid work. Record updates retain existing recipe and
record conflict checks. Applied record actions carry publication order separately
from request start order, so evaluation replay also handles parallel completion.

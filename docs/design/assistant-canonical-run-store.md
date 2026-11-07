# Canonical assistant persistence

New production assistant admissions select Agent's published host RunStore through
Desk's existing private assistant-run journal. This is the persistence integration
for ADR 0014 and the existing host execution boundary. It adds no storage registry,
permission source, provider loop or automatic replay. The integration candidate is
not release-certified: native delegated lifecycle correctness remains blocked by
[Agent #217](https://github.com/Judgment-Pack/judgment-pack-agent/issues/217), and
known-not-started native busy release disposition remains blocked by
[Agent #216](https://github.com/Judgment-Pack/judgment-pack-agent/issues/216).

## Admission and migration

Only package-private production executor construction selects the canonical
profile. Requests cannot choose or downgrade it. Explicit admission initializes
the exact prepared checkpoint under its retained execution ID. The outer
`CanonicalRequired` custody flag requires an active canonical journal, even before
the first Agent write adds its DurableBook. Missing journals, cleared activation,
invalid heads and divergent public checkpoint projections refuse. Opening uses
`DurableRunInput` with the real store and `Resume: true`; it never supplies both a
RunStore and a legacy checkpoint callback.

Existing records retain their callback profile and lifecycle. There is no bulk
rewrite or historical evidence migration. A durable checkpoint cannot resume
through callbacks, including an inactive journal containing durable state. Domain
receipts are not converted into observations. The initial checkpoint is prepared
through the shared session, and Agent's supported writer creates its own empty
book and subsequent genuine observations. GET, load, export/import and startup
recovery do not initialize a book or launch execution.

## One private transaction

The resource-scoped port implements Load, Commit and ReadEvents over the admitted
run's existing fsync/rename/directory-sync/readback transaction. A commit atomically
replaces the supplied exact checkpoint bytes, appends exact ordered scoped event
bytes and retains its idempotency receipt. Canonical revisions/cursors remain
separate from the outer host revision. Exact receipt reconciliation and successful
commits synchronize the outer writer; host intent/result/output/action receipts
continue to own physical effects and their exact bytes.

Commit identity uses SHA-256 over length-framed run ID, expected revision,
checkpoint presence and bytes, event count and ordered exact event bytes. Parsed
JSON equivalence is not transaction identity. Redelivery returns the original
receipt; changed contents refuse. Cursor identities are run/store scoped. The
port remains readable after a conflict or uncertain acknowledgement; Agent's
execution writer latches returned uncertainty before dependent dispatch.

Published bounds remain 4 MiB per record, 8 MiB per transaction, 256 events per
commit and pages of 1–100 events. The private run retains at most 1,024 canonical
events and 4,096 commit identities, within the existing 16 MiB assistant-run file
limit. Capacity refuses before publication. No checkpoint, event, receipt or replay
barrier is pruned to fit.

## Exact-byte validation and independent snapshots

Under the existing private-data custody, a Server retains one exact-byte parsing
fact shared by run transactions and read-only authoring capture lookup. Every
lookup still checks the current rooted private path, project scope, file ownership,
permissions and size, and freshly reads the file. Reuse requires the same root
inode, storage path/revision, project, filename and every byte. Unseen bytes use
the authoritative full decoder. The retained copy has exact capacity and is at
most the 16 MiB file bound; temporary decoding allocations are additional. It
retains no decoded ledger, live root, permissions or current authorization.
Unlocked snapshots keep their full independent decoding path.

Changed transactions validate the complete callback ledger, marshal it, and run
all original raw-storage custody and checkpoint checks into a separate graph.
Only a purity eligibility latch taken before validation and structural equality
with that decoded graph allow reuse of the callback's typed validation. Otherwise
the complete decoded ledger is validated again. The only reachable dynamic field
is citation `excerptId`: nil and exact built-in strings are eligible; other types
use the fallback without adding a type-based refusal. A handwritten reflection
inventory guards reachable interfaces and concrete JSON/text/zero hooks, including
map keys and generic fields. Component updates require a renewed hook audit. The
callback must relinquish concurrent mutation while synchronous validation and
marshaling run; this contract does not make concurrent alias writes race-safe.

An output fact is published only after the existing verified write succeeds. A
failure or uncertain acknowledgement returns no snapshot and publishes no output
fact; the next lookup rereads actual bytes. There is no callback or operation
retry. Exact canonical checkpoint, event, request and result bytes remain intact.

A successful transaction always returns an independently decoded snapshot. In
particular, `changed=false` returns the captured valid durable baseline (or a fresh
empty ledger for missing input), excluding transient callback edits. This is an
explicit return-value isolation fix. Closure side effects still occur exactly
once under custody. Audited production return consumers are duplicate admission,
duplicate canonical commit and writer revision lookup; none requires transient
false-return edits. Caller-retained and returned slices, maps and pointers cannot
mutate disk, the retained parsing fact or subsequent snapshots.

## Canonical and public ownership

For an active journal, Commit validates the canonical checkpoint and updates its
public typed projection in that same replacement. Original bytes, lifecycle,
digests, retained valueJSON and the private DurableBook remain authoritative.
Independent callback checkpoint changes refuse. Public semantic event rows retain
their canonical cursor identity and exact typed event, and validation requires a
matching root envelope in the private journal.

Child-scope raw events remain private; the coordinator publishes the child's task
result. A root delegation request is coordinator-internal. Root checkpoint/budget
events update the checkpoint projection instead of entering the semantic list.
Intermediate invocation ends are withheld; host response finalization delivers the
latest acknowledged root end once. Live display prefixes use the existing bounded
progress/preview path; raw canonical envelopes are retained unchanged as supplied
by Agent. Adapter Emit only delivers acknowledged state, without a second append
or reconstructing Agent's private one-use Plan delivery identity. Gate/host-origin
notices use an explicit canonical ingress.

Desk installs its counter decorator immediately inside the released delegation
adapter. The coordinator has installed its own decorator by the first inner Run;
a sync.Once composes it before session opening. The composed decorator preserves
figures/handoff and reads the shared gate TestCalls before Agent guards, hashes and
publishes the checkpoint. All children share that stable root writer. It does not
change a published checkpoint or overwrite the coordinator's decorator.

## Stop, recovery and explicit continuation

Stop, reacquisition and historical import update outer host state and physical
uncertain receipts. They preserve exact acknowledged canonical head/log/receipts
and the corresponding typed checkpoint, including its saved lifecycle. Outer
stopped/interrupted/historical state closes dispatch independently of that saved
checkpoint status. Imported runs remain nonexecutable. A read of a subsequently
published result is not permission to repeat an effect.

Question answers and allowance extensions retain existing current-session,
source, phase, safe-operation, question/grant and revision checks. The explicit
host action publishes its new checkpoint through the canonical CAS helper in the
same private transaction as its exact action receipt. The canonical commit ID is
bound to that host action; prior journal/receipt entries remain intact. Redelivery
returns the receipt and never launches another continuation.

A closed creation workflow also has two finite status transitions after its
adapter has returned:

| Current host/checkpoint state | Allowed new checkpoint | Existing authorization |
| --- | --- | --- |
| admitted/running host, running checkpoint | complete | Admitted model-free creation checks have finished |
| admitted/running host, complete checkpoint | running | The admitted independent creation review returned; current model-free validation remains |

Both require the exact admitted creation source/action, no pending question,
unresolved approval or handoff, and only completed/refused physical operations.
Reopening is restricted to the two independently reviewed creation actions; final
completion additionally requires the retained creation receipt. Stopped, terminal,
awaiting-input and paused responses cannot enter this helper. Each transition
publishes a new validated CAS head, preserves all other fields and retainUntil,
and clears the returned adapter's binding. Later host notices use the current
head; a subsequent adapter must explicitly reopen the actual retained head. This
helper grants neither compute nor permission and cannot revive canceled work.

## Provider and release evidence

API execution uses Agent's strict prepared-request, transport terminal and result
acknowledgement gates over Desk's actual relay. The raw journal stays bounded and
within the private owner scope. The existing authenticated control view retains
the exact typed checkpoint, including its private DurableBook. There is no uncertain-outcome retry. The actual native bridge acknowledges
the exact live ProviderRelease capability only from invocation-owned cleanup after
reaping its process and clearing ownership; saved state and socket closure are not
release proof. Pre-admission busy/invalid invocations do not close another owner or
invent acknowledgement.

Actual native simple Work/Plan admissions and diagnostics use canonical storage.
The delegated parent/child/parent path remains a release prerequisite: published
child saves can select the completed parent snapshot and overwrite the active root
status. Desk retains the child intent and refuses physical dispatch. It does not
normalize that canonical status in its decorator or weaken the running guard.

The strict writer also guards retained prepared request strings on each canonical
publication. A causal probe found ordinary two-request execution still running
inside the published OutputGuard matcher at ten seconds on a retained string of
about 63 KiB. Activation-specific tests allow a bounded 60-second fixture wait;
production work/request limits and guards are unchanged. This performance
characteristic remains visible for upstream optimization and release assessment.

The full-offer API accepted-Plan completion fixture separately allows a bounded
180-second Work observation. The native current-Plan fixture performs an update,
a real read and evidence-backed completion within one supplied native Work turn,
also observed for at most 180 seconds. Proposal and other native fixture bounds
retain 60 seconds. Recorded diagnostics found repeated strict journal/envelope
and checkpoint validation, including 55.57 sampled CPU seconds decoding canonical
envelopes in a 181.55-second profiled two-run observation. These overlapping CPU
samples do not establish a product latency target or speedup. The fixture retains
the actual production offer, acknowledged operation receipts and exact active-task
completion assertions. Its trace requires exactly one proposal generation followed
by the actual Work checkpoint marker, one Work generation, and the acknowledged
read/completion witnesses in order. Model operation IDs are local to each run;
the marker binds the following generation to the actual Work execution. The caller
validates the complete marker checkpoint and accepted lineage before the trace
helper projects only its execution identity and phase for this association.
Production limits and all validation/authority guards are
unchanged. Functional fixture completion does not resolve the measured host
latency concern or claim a performance pass; release assessment retains that concern.

## Saved display/export review item

The authenticated owner-control API retains the exact typed checkpoint. The
existing terminal chat projector also copies it into saved `state.execution` and
clears approval/handoff; the standalone chat exporter serializes that selected
chat unchanged. Thus a full private chat export can contain Durable prepared
request descriptors, results and child internals, alongside existing retained
Context and operation data. Review this included data explicitly when releasing;
this is distinct from the public canonical report's exclusion allowlist.

API canonical descriptors contain suffix, method and body, excluding headers.
Desk attaches the bound session bearer and real provider key later at the relay
boundary. Native descriptors contain only the published AgentRequest fields:
prompt, instructions, tools, effort, phase and model, without transport credentials
or account/session tokens. User/source text can itself contain sensitive content;
full chat export remains an owner-selected private history operation. Imported
run records are historical and cannot recreate approval, provider-release,
continuation or dispatch authority. This integration adds no display format and
does not strip fields or alter the canonical checkpoint digest for export.


## External adapter intent association (E2, inactive)

A new optional private `assistantHostIntent.sourceBinding` records version,
original own tool, canonical wire name, canonical source-record JSON and digest,
reviewed approval id/revision, configuration digest, projection version and the
observed native callback alias (empty for API calls). The record is closed:
missing, null, duplicate, case-aliased and unknown members refuse. Its source
record carries exactly the original custom adapter declaration and identity;
result receipts are forbidden in an intent. Each adapter owns its full singleton
list; filtering another adapter never changes that list's digest.

The explicit source-tool constructor freezes this tuple before physical intent
hashing and wraps the actual owner Execute closure. It requires a current
host-owned authorization function and rechecks it at source identity observation
and dispatch. No saved approval, imported binding or historical result installs
this wrapper. Production factories do not call the constructor in E2. Source
activation remains dependent on E3 exact proof association, E4 honest errors and
controls, E5 allowance policy and E6 integration.

For root canonical calls, physical acquisition/operation names stay the original
own names. The retained dispatch must use the exact canonical source-record
digest and wire name, with the same original request bytes, canonical request
digest, execution/author scope, dispatch id and recomputed fingerprint. The
canonical parent call must carry the exact source record, wire name and arguments.
An intent without sourceBinding retains the original source=host/original-name
reader and serialized bytes. Mixed old/new custody refuses. Historical association
never checks or grants a current approval.

At the native bridge, the actual offered callback alias enters a private context
alongside its durable callback id. After Agent routes that callback, the trusted
source Execute closure captures the alias with its own fixed source and own tool.
The historical reader matches the retained alias, callback, owning invocation,
exact offered alias and original arguments. It never derives a source mapping
from descriptions or strips arbitrary name prefixes. The earlier absent-binding
native reader remains for historical compatibility.

Owner read classification uses declared external-read taxonomy before Agent
derives legacy Effect. Agent ReadingTool also classifies sourced act results as
retrieved data; that classification does not make the workflow eligible for Plan,
research-choice continuation or read delegation. Mapping alone cannot grant
successful sourced Plan custody: E3 must add the independently verified exact
association and the final accepted SourceGate-decorated tool_result.

Non-root reads retain their existing physical path. E2 does not claim canonical
sourced child qualification. Released Agent011 exports DurableDispatchScope at
native provider invocation but its generic ExecuteDurableHost callback does not
carry the actual durable claim id/scope. Matching concurrent equal-argument child
claims by guess would not establish exact custody. E3/E6 must resolve and test
that contract before activation while preserving supported child/read domains.

No source input domain, raw argument spelling, output projection or existing
record limit is reduced here. Complete new serialized record positive/boundary/
one-over evidence, actual API/native producer paths and integrated historical
reload/child qualification remain pending; E2 source staging runs no checks.

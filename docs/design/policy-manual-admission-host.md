# Private manual admission host integration

This source implements the optional experimental `local-admin-manual-pack/1`
profile with explicit offline activation and local-session registration. Its
reviewed Desk and Runner boundaries are enabled uniformly for actual isolated
acceptance; release requires that real gate to pass. Runner advertises the closed
v3 capability and exact profile. Unconfigured workspaces retain their behavior.
There is no test environment override. This private host contract does not adopt
or change the retained portable Policy/adoption/check contract pins.

The [core](policy-manual-admission-core.md) owns all permission data in the same
anchored Policy transaction as lifecycle. The registry holds custody demand and
transition state, never resource eligibility or start grants.

## Explicit offline transition

The explicit command is `policy-authority enable-execution --root <absolute>
--installation <expected> --runner <absolute> <project>`. Flags precede the
project. The explicit Runner selector is ephemeral; there is no executable
identity discovery from a copied Runner store. Existing managed-home entries
retain their Runner workspace and installation owner. Verified legacy layouts
retain their existing startup/child namespace. Ambiguous layouts refuse.

The workspace must already be Policy-enrolled and executor-enrolled, with a
current schema 2 head and complete retained closure. Ordinary startup and lookup
never migrate it. Stop older or unconfigured Desk/Runner/Runtime processes before
initial enrollment: the machine-use lock excludes cooperating configured Desk
processes and migration journals, not arbitrary preexisting children. Subsequent
actual executor lease ownership supplies process/Runtime quiescence.

Enablement holds exclusive machine-use, the exact pinned external executor lease
continuously, then ledger → registry → anchor locks. It:

1. Pins the configured Runner source and creates one private executable snapshot.
   Closed complete v3/profile preflight runs with EOF only, before helper inputs.
2. Persists registry v3 pending state with stable executor identity, independent
   machine/Runner identities, prepare nonce and old exact ledger digest.
3. Creates and syncs the immutable sibling demand
   `executors/policy-execution-v1/<machineWorkspace>`. That fixed directory is not
   a possible workspace ID, so ownership-era Desk rejects it after registry-only
   rollback. Every registry demand and external file must match bidirectionally.
4. Calls the [nonexecuting Runner store helper](policy-manual-admission-store-prepare-shape.md)
   on the same snapshot, passing only a duplicate of the continuously held lease
   at fd3. Runner owns SQLite schema 3→4, marker ordering and idle checks. Desk
   never opens SQLite. Only a fresh exact `committed` reply advances enablement;
   recovered observations, noise, refusal, loss and uncertainty hold.
5. Advances the existing Policy anchor with exact schema 3 bytes, then persists
   final enabled registry metadata. No helper or callback is replayed.

Any uncertain transition poisons the original machine object. Pending, partial,
missing, extra or mismatched custody refuses after restart without repair,
rollback, deletion or reconciliation. If the final registry write was fully
synced but its response was lost, a fresh process may observe the complete exact
registry/ledger/fence closure. This observation cannot replay queued execution or
manufacture a registration/start permit. Another acknowledgement file would only
move the final-response ambiguity.

Startup verifies the registry, full external demand closure, existing anchor and
schema 3 identity. Status is diagnostic and never repairs. Removing execution
metadata, restoring registry v2 alone, or introducing an orphan demand refuses.
Schema 3 archive/restore stripping is explicitly unsupported. Whole-trust-plane
rollback or an operator stripping every trusted demand and supplying standalone
old binaries remains outside the same-OS-owner trust boundary.

## Captured channel and launch demand

Every enabled Jobs start—including fallback and child Desk—requires exact v3
capabilities/profile on the same pinned private snapshot as execution. A known
complete v3 is also acceptable for an ownership-only workspace, while capability
support alone never enables governance. Unknown/incomplete profiles refuse.

The host creates one Unix socketpair with both raw descriptors marked CLOEXEC
under Go's fork lock. Only the child endpoint is passed at fd3; host originals
close immediately. Runtime receives no authority endpoint. A fresh channel has
private random session/challenge and retained scope. It carries no browser bearer.
Runner acquires the external lease, fixes actual signing and activates before
recovery/workers or its Jobs handshake. A key-specific preworker fallback retains
all demands and creates another fresh channel. There is no downgrade fallback.

The [closed wire](policy-manual-admission-wire-shape.md) uses seq1 activation
exactly once, then strictly increasing uint64 sequence strings, one exchange at
a time, 64KiB including LF and five-second exchange deadlines. Idle activated
channels can wait for work. Protocol faults, EOF, cancellation and unexpected
owned exit permanently hold Jobs, without claiming an orphan Runtime stopped.
Domain refusals have finite safe codes. Recovered/observed results grant no launch.

## Exact registration and current local ingress

A trusted direct owner client reads Runner's exact `execution-binding` endpoint
with captured loopback endpoint/token, no proxy/redirect, bounded output and closed
profile/base64/digest/workspace validation. That route is absent from the browser
Jobs allowlist. Registration selects the exact approved adoption/review; the core
resolves its complete retained dependency closure. It pins the immutable release,
Pack publication/bytes and Runtime identity, independently of mutable draft basis.

Enabled manual POST ingress requires a current local session (launch-only and
OIDC credentials refuse), one original Idempotency-Key, exact canonical path and
no browser private-intent header. The installation-admin caller identity is
stable across session rotation. The host reads the original body once, persists
its intent and exact facts/evidence references, rechecks the session, then forwards
the same bytes and key with the host-created private selector. Same key with a
changed raw body—including outer whitespace—conflicts. No normalization or retry
can change the admitted request.

Enabled Jobs authoring can create pure previews/jobs. Triggers, mappings,
Preparation, upgrades and workflow execution have no first-profile admission
path. Runner independently enforces its full service boundary. Desk refuses
operational prepared-input workflow POSTs and resumed input evaluation/research
workers. The generic MCP relay consults retained demand even when Jobs is absent:
only host-reviewed read/check utilities and the known evaluator with exact
`rehearsal:true` pass; unknown/ambiguous operational tools refuse. Agent bridge
Runtime calls return through that relay. Graph checks use a literal rehearsal
argument. Direct Desk read/check/rehearsal utilities remain nonoperational and
are outside Runner's inherited executor lease chain; no competing lease or
second ownership transport is introduced.

The minimal Jobs UI shows exact adoption/review registration and current
eligibility. It disables unsupported operational controls. Its key fingerprint
uses the existing exact `canonicalJobsRequest` sender, preserving RetainedJSON
numeric lexemes. A session/Desk/job-scoped reminder retains only key and request
digest before dispatch, survives refresh, and never retries. An unresolved
submission is held for history inspection; choosing a new intent is explicit.
No facts, evidence or session credential are stored in that reminder.

## Required acceptance profile

`TestPolicyManualRealDeskRunnerRuntime` requires absolute
`JPACK_MANUAL_RUNNER_TEST_BIN` and `JPACK_BIN`; supplied incompatible candidates
fail instead of skipping. The fixture uses the same actual compatible Runtime
for Policy/coverage/adoption checks and execution, real helper/store enablement,
owner export and exact registration. It covers actual numeric input, rotated
local session, withdrawal-first, withheld fresh commit reply followed by
withdrawal and delayed actual spawn, dropped reserve/start replies and restart
without replay. On Linux the live audit-blocked Runtime must hold its exact
executor lease and neither authority socket. The transport proxy only delays or
drops real replies after durable core transactions; there is no Runtime test hook.

The actual ownership-era CLI rollback fixture uses
`JPACK_MANUAL_OLD_DESK_TEST_BIN`. Existing ownership/death fixtures and Runner's
actual old-store, journal-failure, one-use proof and final-material fixtures remain
required. Focused custody/channel/session/relay/UI checks complement these real
fixtures; synthetic helper/checker tests are not full-chain acceptance evidence.
Published component pins remain unchanged.

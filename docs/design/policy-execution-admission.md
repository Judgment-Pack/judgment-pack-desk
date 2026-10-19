# Local Policy execution admission

Status: implementation design, 2026-10-18. Desk adoption review is merged in
[#487](https://github.com/Judgment-Pack/judgment-pack-desk/pull/487).
The optional anchored repository, explicit executor enrollment and Runner
ownership integration are foundations. Ownership binds every Jobs start in an
executor-enrolled workspace to an independently retained external lease and
requires capability negotiation on the same pinned executable snapshot before
boot. There is no governed execution capability, current-run admission API, or
withdrawal enforcement in this tranche.
[Desk #455](https://github.com/Judgment-Pack/judgment-pack-desk/issues/455),
[Runner #147](https://github.com/Judgment-Pack/judgment-pack-runner/issues/147)
and [Gateway #295](https://github.com/Judgment-Pack/judgment-pack-gateway/issues/295)
remain open. Candidate RFC 0023 remains in Review.

## Preserve the existing boundaries

Runtime checks supplied material offline and executes the Pack's decision rules.
Desk owns the current Policy lifecycle, exact reviewed adoption and authorization
to start governed work. Runner must enforce that authorization immediately before
operational evaluation. Gateway needs a separate check immediately before an
external action; admission for an earlier run cannot authorize a later action.
An exact digest is an identity, not permission.

Keep existing jobs explicitly ungoverned until an administrator reviews their
registration. Never infer governance from citations, a passing test, a Pack's
title, a chat message or membership in a Library collection. Once registered as
governed, a resource cannot become ungoverned by omitting a field, restoring an
old job record, choosing an old companion or falling back after an error.

## Foundations being prepared

The private Runner profile `judgment-pack-runner/execution-payload/1` commits to
the existing release digest, pinned Runtime, exact facts and evidence bytes,
retained Preparation and SourceInput, and operational signing public key. It
preserves absent versus supplied evidence and numeric representations. Facts do
not enter the Policy candidate's safe-integer canonical domain. The profile is
private experimental machinery, not an adopted portable Spec contract. Creating
the descriptor does not start a process or check eligibility.

Desk's private authority anchor commits to the exact Policy ledger head outside
the portable store. Enrollment is explicit; ordinary lookup never creates or
resets an anchor. A mismatch, missing anchor or pending write intent prevents a
current-authority claim. All live Policy access now shares an optionally bound repository factory.
A private trusted resolver supplies each pinned workspace binding once at
startup. The optional trusted local CLI profile explicitly initializes a separate
machine root, classifies legacy workspaces and enrolls exact existing ledgers.
There is no default root or automatic enrollment. Legacy installations remain
unbound. The integration does not protect jobs. Its [storage contract](policy-authority-anchor.md)
describes durability and failure behavior.

The fixed transaction order is Policy ledger lock, then anchor lock. Read the
exact ledger while holding its lock; retain both locks across current-head
verification, domain mutation, durable intent, durable ledger replacement and
anchor finalization. Source-worker serialization, where needed, precedes both.
No code holding the anchor may acquire the ledger lock afterward. Bound current
reads also take the bounded-wait exclusive ledger lock before the anchor and
retain both through the read callback; they do not write. Explicit history-only reads remain separate from current
authority and expose no Update/current View interface. A domain closure is never
replayed to recover a failed write.

## Required authority integration

A machine-local root must be resolved independently of movable Desk home,
project folders and Runner state. Existing `s.configDir`, assistant custody and
the home registry can move with the home, so they cannot serve as this root.
The local enrollment profile validates containment and exclusion from archive and
home migration, and retains an installation identity and trusted stable workspace
mapping there. Paths supplied by a request or a restored project cannot create
that mapping. Missing state for an enrolled workspace holds current authority;
this is not an execution gate. The operator must preserve root and expected ID
in trusted launcher configuration. Pre-marker snapshots and full trust-plane
loss cannot establish that this configuration once existed without an external
witness. The shared lifetime lease excludes cooperating configured Servers only.

All live Policy writes must pass through one repository factory, including
browser/chat services, source polling, disconnect handling and lifecycle changes.
A hook in the withdrawal handler alone leaves other authority changes untracked.
History-only archive restore stays inactive and cannot enroll itself. Read-only
history remains distinguishable from a current-authority read.

A successful durable intent must precede ledger replacement. Any pending intent
holds the store; matching either its old or new ledger is diagnostic information,
not automatic reconciliation. A final-marker write can fail after rename. Return
an uncertain outcome and do not attempt to rewind through another fallible
write. On reopen a stable new anchor and matching ledger can establish current
storage, but cannot authorize replay of the earlier operation. Operation receipts
and logical-operation barriers must enforce that separate requirement.

## Adoption and current eligibility are different checks

Register the exact Pack release, immutable reviewed adoption and complete selected
dependency closure together. Resolve dependencies from registered state, never
from a caller-selected subset. Resource revisions identify this registration;
the entire Policy ledger head is not the resource revision.

The adoption candidate's complete mutable basis must remain current before its
approval. That check is not an every-run rule. A new unrelated draft or review
must not revoke an already registered exact publication. Execution needs its own
check over the retained publication, recovery state, current authority,
withdrawal, effective dates and expiry. Supersession, source updates and freshness
follow explicitly registered lifecycle requirements. Existing adoption helpers
that reject pending source updates or superseded publications cannot silently
define those ongoing rules.

## Executor ownership before dispatch

The first governed profile should be one local OS owner and a supported local
filesystem, with manual single-Pack runs first. Other governed trigger types
must refuse until their replay identity and restore fixtures are implemented;
existing ungoverned triggers retain their behavior.

Before starting a governed Runner, verify the exact binary supports the required
boot protocol, or use a new paused-activation protocol. Today's Runner starts
workers before its `jobs/1` handshake and ignores unknown boot fields. Sending
extra fields and checking the response afterward is insufficient.

A per-workspace executor lease must live outside copied Runner folders. Its
ownership must survive a Runner crash for as long as an admitted Runtime child
can execute. File-descriptor inheritance is a candidate mechanism, requiring a
process fixture that kills Desk and Runner while the child retains the lease.
A PID, timeout, boot nonce or per-copy file lock does not prove old work stopped.
Only activate a new epoch after proven old-owner quiescence. Unsupported platform
or child-lifetime behavior must refuse governed activation.

Service credentials and executor epochs are distinct from the Runner owner API
bearer and Runtime audit-signing key. Signing-key fallback must never disable
governance. Trusted registration must override missing or stale governance fields
in restored Runner state before any queued work can resume.

## Start ordering and recovery

The final start commitment binds resource/revision, exact release and payload,
logical operation, attempt, authenticated executor and current epoch. Desk must
serialize this commitment with withdrawal in the same anchored transaction.
Withdrawal first blocks the start; an already committed exact start may finish.
Queue insertion and an eligibility preview are not start commitments.

Retain both attempt idempotency and a barrier across attempts of the same logical
operation. An old queue can otherwise mint a new attempt and run twice. Manual
operation identity must derive from its original trusted caller/idempotency
domain. Event and schedule occurrences need their own stable identity. Current
file-observation timestamps can be regenerated after restore; governed file
triggers need authority-held cursor reconciliation before support is claimed.

Only the fresh committed result for the still-owned exact attempt may dispatch.
A recovered receipt is history. Lost responses, malformed replies, failed
persistence, revoked identity and interrupted ownership close dispatch and
require reconciliation. A new nonce or new attempt does not prove nondispatch.
Never automatically replay a Runtime launch or external action. Retention limits
must refuse capacity rather than delete replay barriers.

## Integration acceptance sequence

1. Test the inactive exact-byte payload and durable-anchor primitives with fixed
   vectors, all storage failure edges, stale restoration and concurrent access.
2. Add explicit machine enrollment and the central anchored repository factory;
   prove every live writer participates and history-only restore stays inactive.
3. Add capability negotiation and executor ownership with process-death fixtures.
4. Register exact resources and immutable adoption closures; add durable logical
   operation/attempt receipts and the bounded authenticated authority transport.
5. Wire one non-bypassable Runner operational gate. Cover the private workflow
   path as well as dispatch; close unsupported governed paths explicitly.
6. Prove both withdrawal/start orderings, lost responses, copied stores, earlier
   queued snapshots, changed payloads, stale epochs and downgrade attempts using
   a real Desk authority and Runner process. Then extend supported triggers.
7. Integrate Gateway's actual action boundaries separately, including its private
   storage-commit path. Run admission does not confer action permission.

Targeted local tests establish each slice; repository CI supplies broad checks.
Development fixture binaries do not imply a published release or an installed
upgrade. Runtime release publication and restarting the user's Desk remain
separate explicit decisions.

## Limits

A machine anchor detects rollback of portable Desk or Runner state while that
anchor remains trustworthy and newer. It cannot detect an undetected whole-host
snapshot that restores both the ledger and anchor. Known whole-host recovery
must hold governed work; stronger guarantees require a separately reviewed
external monotonic witness or authority. This design does not claim distributed
ownership or protection against a hostile process running as the same OS user.

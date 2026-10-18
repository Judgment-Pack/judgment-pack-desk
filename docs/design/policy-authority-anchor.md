# Private Policy authority high water

Status: optional trusted local machine profile, 2026-10-18. This module has no default
machine root, executor or execution integration. It cannot authorize execution,
prove an operation's outcome, or establish a replay-safe start. See the
[admission integration design](policy-execution-admission.md) for those separate
requirements.

The trusted host supplies an absolute machine-local directory, a stable
installation identity and a stable workspace identity. The directory must be
outside portable Desk home/project/Runner storage, backups and migration roots.
The primitive validates private ownership through `openPrivateDataRoot`; the
host must establish the path's independent custody and containment before any
future enrollment. A request or restored record cannot choose the mapping.
There is no default or automatic enrollment when a record is missing.

`newPolicyAuthorityAnchor(path, identity)` validates configuration without I/O.
`Enroll(ctx, exactCurrentLedgerBytes)` is a distinct, explicitly authorized
initial action. It creates generation 1 only when the anchor record is absent.
An existing record, including a corrupt or pending one, is never overwritten or
reset by enrollment replay. An uncertain enrollment poisons that object.

`WithCurrent(ctx, exactCurrentLedgerBytes, func(held) error)` is the sole update
boundary. The total order is **Policy ledger lock, then anchor lock**. The caller
must acquire the ledger lock before reading the exact supplied bytes and keep it
continuously through return. The primitive takes a nonblocking exclusive anchor
lock, validates its identity and high water against those bytes, then invokes the
callback while retaining that lock. Missing, corrupt, mismatched, pending or busy
state prevents callback admission. A separate read/check/write token does not
exist; `held` cannot be used after callback return or panic unwinding.

Inside the callback, `held.Current()` supplies a bounded observation and
`held.Advance(ctx, exactNextLedgerBytes, durableWrite)` can be attempted once.
The caller validates and constructs the update while both locks remain held.
The trusted writer receives a defensive byte copy and must durably write and
verify exactly those bytes under the already-held ledger lock. Unchanged digest
is a no-op: no intent, generation change or writer invocation. Generation is
bounded to positive JSON-safe integers, with an exact increment on each changed
commit; overflow refuses before writing.

The closed, bounded version 1 record contains installation/workspace identity,
generation, exact ledger SHA-256, and an optional intent containing next
generation, old/new digests and an unpredictable nonce. Unknown, duplicate,
case-variant, missing required and explicit null members are refused, including
inside the intent. An intent must advance exactly one generation, bind the
current old digest, and choose a distinct new digest. State uses the existing
private read/write helpers and directory lock; no alternate storage loop exists.

Changed writes proceed in order: durably retain intent; invoke the writer once;
after confirmed exact ledger success, durably replace high water and clear
intent. Callback or persistence failures are bounded internal codes, with no raw
paths, content or nested error prose. A callback error before a changed commit is
`callback-refused`; future repository wiring may capture a known domain error
and return it only for this no-commit case. A failure after a successful advance
is uncertain even if a later callback action fails. Ignored inner errors cannot
turn the outer result into success. No rollback or repair write is attempted.

Pending intents always hold the store, including when the observed ledger
matches their old or new digest. The pure classifier reports `pending-old`,
`pending-new` or `pending-other` for diagnosis, and resolves none of them.
Any uncertain call poisons its object and yields no success or replay permission.
A final marker can fail after rename or durability/read-back checks: on reopen,
the record may be pending against the new ledger, or a fully stable new high
water matching that exact durable ledger. The latter can be observed as current
storage, but does not report delivery of the earlier operation or authorize its
replay. Intent-write failure before its persistence may leave unchanged old
storage; the ledger writer has not run. Logical operation receipts and dispatch
barriers must enforce outcome reconciliation separately.

The primitive detects a portable ledger restored behind a trustworthy newer
machine anchor. It cannot detect an undetected whole-host rollback of both, or a
hostile process with the same OS owner replacing the anchor. Known whole-host
recovery needs an execution hold and explicit reconciliation; stronger guarantees
require a separately reviewed external monotonic witness. No execution
enforcement is claimed by these helpers.

## Optional current and historical repositories

The private trusted `Config.policyAuthorityForProject(*ProjectRoot)` resolver is
called once for each pinned workspace during `New`, before workers or serving.
Its pinned root is borrowed: the resolver must neither close nor detach it.
The resolved anchor is retained on that Server; request handling never resolves
or replaces it. Children inherit the resolver and resolve their own pinned root,
not their parent's anchor. The resolver owns the independent trusted workspace
mapping, installation identity and duplicate-workspace checks. A nil resolver
preserves legacy mode. A configured resolver may return nil only for a known
never-enrolled legacy workspace; any error aborts startup without fallback.
The optional machine profile below supplies the trusted resolver. There is no
request-selected root or HTTP enrollment action.

Every production live Policy repository constructor uses `s.policyRepository()`:
request services, both source-poll entry points and source disconnection across
open servers. In bound mode, both View and Update acquire the ledger directory
lock exclusively with bounded waiting, then read and validate the exact existing
ledger bytes. This serializes ordinary refreshes without making the anchor's
independent-copy lock blocking. Missing directories or ledger files refuse and
are never replaced with an empty store. `WithCurrent` runs before the domain
callback. Full domain validation, current authorization, size limits and
cancellation checks precede `Advance`; its single writer uses the existing
`writePrivateData` for those exact prepared bytes. Known domain errors are
preserved only after currentness was verified and before any changed commit.
Legacy unbound read/write behavior remains unchanged.

Authority-specific failures, including missing, busy, corrupt or mismatched
current state, return a bounded Policy error with `Uncertain: true`. They prove
neither that an earlier logical operation was never committed nor that a new
attempt may begin. Pending/final persistence failures remain uncertain, and are
never passed through precommit normalization. There is no automatic fallback,
repair, domain closure replay or write replay.

`s.policyHistoryReader()` returns a separate read-only type with `ReadHistory`
and no Update or current View. It skips only anchor currentness: existing ledger
schema, requested immutable blob closure, locking and current authorization still
apply. Bound history also requires an existing directory and ledger; never-enrolled
legacy empty history retains its prior behavior. Explicit GET operation receipt,
exact revision, original-source download and recovery archive reads use this
interface. Ordinary get/list/lifecycle/adoption and new authoring provenance stay
current-bound. An operation receipt retains the originally saved outcome and
returns `delivery: recovered`; it proves retained history, never current approval
or permission to start/replay. Absence from an older historical ledger cannot
prove that an operation never committed. Archives remain historical and restore
inactive; staged restore never enrolls an anchor.

## Explicit local machine profile

The [operator CLI](../policy-authority.md) initializes an unused absolute root,
classifies a pinned workspace as known legacy, then explicitly enrolls its exact
existing validated ledger and immutable blob closure. Serving requires both the
root and its expected random installation ID in trusted launcher configuration.
`Config.PolicyAuthority` is host input, never portable Desk settings. It cannot
be combined with a custom resolver. No default path, missing-root creation,
automatic legacy classification or empty-ledger initialization exists.

Initialization uses non-replacing directory creation and refuses any existing
leaf, including empty or partially initialized state. The owner-only root keeps
a closed, bounded registry (1 MiB, at most 256 workspaces), random installation
and workspace IDs, registry generation and exact root/project device-and-inode
identities. Project path and directory identity must both match. Portable home,
signing, project and Desk IDs do not create mappings. Copies, replacement folders,
unknown mappings, wrong installation IDs and corrupt or missing registries refuse.
An unexpected anchor directory, or an anchor under a legacy entry, also refuses;
rolling back only the registry cannot downgrade enrollment. Missing enrolled
anchors retain the binding and historical access while current access refuses.

Configured Servers each own a shared machine-use lease until all their children,
workers and private stores close. Closing stops admission and drains admitted
Policy metadata transactions before project descriptors or the lease are released.
Mutating CLI commands require a nonblocking
exclusive lease first. This prevents enrollment racing a running configured
legacy Server whose nil binding was captured at startup. Enrollment then locks
the existing ledger, registry and anchor in that order. It retains a registry
intent with exact ledger digest and opaque nonce, claims a demand-only portable
marker, enrolls the anchor, then finalizes the registry. Each new relative anchor
directory is synced through its pinned parent before anchor contents are written.
There is no reset, replay, repair or automatic completion command. Pending
enrollment holds that workspace; other explicitly known stable workspaces may
start. Registry corruption, orphan anchors or root custody failure hold the profile.

After an enrollment persistence error the caller receives an unknown outcome,
never an enrollment success. Confirmed intent remains pending until finalization.
A final persistence error may leave a stable enrolled record: read-only status
can observe it, but repeating enrollment still refuses and history never permits
replay. No fallible rollback or best-effort failure-marker write is attempted.

The portable marker contains only installation/workspace IDs and a demand for
binding. It cannot choose a machine root or grant authority. Startup checks it,
and unbound current transactions recheck it under the ledger lock before domain
access and after completion; a previously running unconfigured Server therefore
refuses after the demand appears. Machine anchors open relative to the pinned
root and recheck root and lease-file identity during current access, including
completion. Configured legacy transactions perform the same custody checks.

The machine root must be separate from configuration, the entire Desk home,
every pinned project, application data, current/previous external chat roots and
Runner state (currently beneath configuration/project storage). Storage openers
and proposed storage/restore destinations apply the same exclusion. Configured
project/home moves and browser Desk creation refuse before writing in this slice;
trusted relocation/provisioning requires a future design. Pending home/project
move journals refuse enrollment/startup; configured launches skip automatic home
migration recovery before classification. Policy restores keep their existing
inactive review and source-consent barriers, and cannot enroll themselves.

This profile requires a configured launcher and stopping all older/unconfigured
Desk processes before enrollment. Its lease cannot stop an older binary. A
pre-marker snapshot cannot demand a missing trust plane, and intentional omission
of all machine configuration plus pre-marker data, whole-host rollback, or a
hostile same-owner process requires an external witness for stronger detection.
No current user data is enrolled or application process restarted by installation.

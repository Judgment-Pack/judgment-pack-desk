# Cooperative storage coordinator (capacity v2 foundation)

The unused exact-reference fanout foundation adds an explicit ticket/manifest
variant (`JPSTB002`, header byte9=1) alongside unchanged flat byte9=0 records.
`Request.Fanout` selects it; no Desk producer or native qualification record does.
It keeps16 physical groups,64 slots and every fixed file/page length. Each group
uses the existing Component physical/capacity operands with zero flat Workspace
fields. Its Attribution/SettlementAttribution and terminal attribution changes
apply independently to every exact member; differing per-member quantities need
disjoint groups. Expanded `(root,category,workspace)` keys cannot overlap.

One immutable manifest contains a sorted unique <=1024 `(ID,generation)` table
and16 fixed128-byte membership bitsets. Every table row must be used; bits beyond
the table, unused group rows and reserved bytes are zero. The maximum is128
envelope +16384 initial ticket +16 variant header +24576 table +2048 membership
=43152 bytes within the unchanged512KiB region. This variant has zero ObjectEntry
rows: its original host MUST retain a separate complete bounded immutable effect
plan and bind that plan to Workflow/Operation, exact token and manifest. Q proves
no such host custody or plan. A missing host plan is not an empty effect set.
The proposed two transfer banks and their dispatcher/bootstrap/control envelopes,
correct owner category/reference partition, new/external target creation and
durable selection reattachment remain unimplemented prerequisites. This core
foundation does not complete transfer behavior or authorize activation.

Ticket pages contain only the variant marker and group rows; live loads verify
and hydrate membership from the exact committed manifest before registration,
accounting, status or settlement. Persisted-applied terminal manifests may already
be overwritten. Their inspection returns a nonnil empty Fanout marker, never an
invented empty live membership. ValidateAppliedRequest checks the separately
retained complete original request against the terminal's manifest commitment.
All returned live dictionaries/membership slices are detached. Repair retains
readable fanout membership and group order exactly, allowing conservative operand
increases; it does not translate flat/fanout obligations. Lost membership refuses
inferred reconstruction without explicit original-owner disposition. A parser
failure never proves a zero workspace charge or licenses replay.

No permanent checkpoint feature or migration is needed for this unused variant.
Old strict loading checks BOTH ticket banks before selecting a page or skipping
retired manifests and rejects byte9=1. Reserve, Advance, Settle, Checkpoint,
Register, policy/census and ordinary inspection all load under the same Q lock.
After a fanout ticket may exist, an old handle cannot admit/release a promise by
falling back to a cheaper bank. An old operation may finish effects within its
already conserved flat promise; its next Q call refuses and retains the promise.
A manifest-only cut before ticket publication returned no token or permission;
old flat pages may remain usable without losing an admitted effect. Even retired
fanout peer pages remain an old-reader refusal until normal exact-applied slot
reuse replaces them. Original-owner recovery still needs complete facts; no
automatic unknown-page repair is allowed. Desk's independent legacy Q decoder
continues to refuse the variant, and its exact host/enrollment/source-pin bridge
must be completed before any production producer selects it.

This package supplies durable, finite, cross-process admission and accounting.
It does not enforce an opaque child's writes or grant permission to act. Every
production owner and writer still needs enrollment. Package tests do not qualify
aggregate Desk storage safety or release completion.

`OpenHeld` borrows the original owner's held private anchor and stable lock
witness. It opens an independent description of that same named lock inode,
checks ownership/private modes/no-follow/single-link/same-file custody, and never
accepts a browser pathname. The owner keeps the anchor and witness alive until
`Close`. Calls per handle serialize; different handles/processes use real flock.
The five-second cancellable wait applies to acquiring short transactions, not an
infallible deadline on an uninterruptible kernel IO call. Original ownership and
admission leases keep their existing nonblocking semantics.

A new original owner explicitly initializes, registers declared roots, object
filesystems, workspaces and exact capacity constraints, then publishes a complete census while maintenance blocks growth.
Policy values must be finite. The coordinator itself requires a conservative
64 MiB/16-file/one-directory control allowance; its exact fixed file lengths sum
to 42 MiB in six files including the empty stable lock. This allowance and real
writes do not establish guaranteed exclusive physical allocation or fsync success.
Physical availability is a fresh observation separate from retained usage and
durable promises. Reserve accepts typed per-constraint CapacityObservation only.
The legacy Linux ObserveHeld helper remains statistics-only and cannot establish
capability, quota membership or authoritative pool identity. Other legacy
measurement platforms currently return unsupported. Unknown required numeric axes refuse growth. Native quota/container discovery
status is separate: unknown does not mean disabled or unlimited. Documented
InodesDynamic omits only a nonexistent independent native finite inode pool;
finite policy units, known applicable quotas and Desk namespace limits remain.
Existing supported flock adapters do not imply macOS measurement/enforcement or
all-filesystem qualification.

`Reserve` checks every component under one Q lock: installation/category,
workspace/category, and every applicable overlapping capacity constraint.
Components bind root custody, filesystem/workspace IDs and generations, logical
peak/settlement byte/file/directory triples, complete immutable per-constraint
peaks and the root capacity binding. Logical physical usage is charged once;
capacity promises fan to each declared policy and known applicable native
restriction. Catalog v3 has one installation advisory availability-policy row
(Kind7), shared by every root, plus independent known native restriction rows.
Its residual is the minimum measured post-headroom availability across enrolled
scopes, not a physical pool or exclusive reservation against other writers. Its
inode axis has a fixed finite cap of 1,048,576 authored policy units even when
all native inode models are dynamic. Native observations and unknown discovery
remain separately labelled; no generic physically Healthy claim follows.
Duplicate component keys refuse. A handle opened with `HeldAnchor.AdvisoryLimits`
(Desk v0.14.0) treats installation and workspace limits as advisory: Reserve
still charges every promise and Snapshot reports usage over a limit, but no
promise is refused with ErrLimit. Headroom, bounds, overflow and authority
refusals are unchanged. Pure attribution carries the complete relation
with zero capacity peaks. Every peak includes the installed versioned authored allocation estimate for
rounding, namespace/metadata and settlement activity. These finite conservative
policy estimates are not measured or universal OS allocation bounds and never
license extra logical effects or retries. KnownResources covers only the
declared numeric policy axes; admission does not certify complete native quotas
or future allocation/fsync success. Overflow, unknown owner,
stale generation, finite table exhaustion and bounds refuse before a ticket
returns; this is not an action authorization.

The original owner enforces each bound before stage growth, persists the returned
token in its own workflow, and uses `Advance` to bind exact stage/dispatch intent.
Original CAS, approvals, phase, Sync/Close/rename/directory Sync and exact readback
remain mandatory. No domain callback or action executes under Q. A failed
persistence request stops dispatch; retrying underlying effects is prohibited.
`Settle` accepts observed owner facts only. Residual stages must remain charged
as retained data or keep a ticket unresolved. Terminal delta persistence is
idempotent by exact token/fact. `Checkpoint` incorporates terminal tuples once;
slots and manifests cannot be reused before that exact checkpoint is durable.
Before persisting another terminal ticket, `Settle` durably incorporates any
prior terminal deltas. A lower-slot deletion can therefore depend on a completed
higher-slot addition without making restart accounting depend on slot order.
Failure of that prerequisite checkpoint leaves the new settlement unwritten and
its reservation outstanding; a complete newer checkpoint may still be readable
after an uncertain persistence result.

There are 64 dual-page ticket slots, two 4 MiB checkpoints, 4,096 roots (768-byte
rows), 1,024 workspaces (512-byte rows), 128 filesystems (256-byte rows), nine
categories, 512 capacity constraints, at most four exact constraints per root/
component and 16 components per ticket. Counts never truncate; graphs exceeding
four independent applicable restrictions require a reviewed contract change.
Constraint headroom is finite and immutable after enrollment in this batch;
UpdatePolicy changes logical limits/observation age only. Each resource sum is
bounded by bytes<=1<<60 and inodes<=1<<40, independently of logical charges. A 512 KiB immutable manifest
region per slot contains its 128-byte envelope, full 16 KiB initial reservation,
2-byte entry count and at most 507,774 encoded entry bytes. Up to 4,096 entries
are allowed, subject also to that cumulative byte bound. Each relative target or
stage locator is at most 1,024 UTF-8 bytes. Unsupported indivisible production
workflows exceeding these bounds remain enrollment constraints; split only if
the original workflow supports independently durable partitions. Do not disable
required atomic workflows to declare completion.

Checksummed generations detect torn/corrupt/ambiguous records. Any invalid page
blocks fresh admission rather than falling back to an older, cheaper record.
The prior readable reservation remains recovery evidence. Checkpoint-applied
terminal identities retire their manifest references before reuse: an interrupted
new manifest cannot be interpreted by an old terminal page. No TTL, PID or
heartbeat releases a promise.

`InspectRecovery` fences healthy admission and returns bounded metadata and an
exact-state fingerprint. Original owners acquire domain leases outside Q and
inspect only exact registered targets/stages/receipts. `Repair` requires that
fingerprint, complete original-owner census/registry, per-slot proved disposition
or conservative uncertain obligation, owner evidence and exact original v2
format evidence tied to that fingerprint. Repair preserves every readable
constraint identity/generation/resources/headroom, filesystem capability relation
and root binding. Each rebuilt peak independently covers every old constraint;
a larger shared-pool peak cannot replace a volume quota peak. Before its first
baseline write Repair validates the complete finite aggregate with promises;
over-policy conservation is allowed, representation overflow is refused. A readable unretired
reservation/manifest cannot shrink or lose its exact target/stage entries without explicit disposition proof. `Manifest` reads only a current live token; retired references refuse. If no
readable envelope or immutable plan exists, reconstruction needs explicit per-slot original-owner
evidence; missing bytes never become a zero charge. Evidence digests are internal
host assertions, not cryptographic proof or public bearer permissions. Maintenance
checkpoints precede overwrite; repair failures leave maintenance/unknown until
fresh fingerprint-bound facts. A full census retires already materialized deltas
without reapplying them. Possible effects remain uncertain, never replayed.
Missing/truncated/oversized/foreign fixed files require original-custody B2
installation recovery; this bounded page-repair interface does not erase or
truncate ambiguous data to make their shapes fit.

B2 must bind one shared machine anchor to original `internal/desk` custody/home
owners, register explicit config/history/private/scratch roots, and enroll exact
writers such as `newDataStage`/`writePrivateData`. B3+ reserve before model/effect
dispatch and preserve token binding through interruption. B6 must join the trusted
updater/helper and solve actual native/Runner/Gateway/connection/Git write bounds.
B8 maps stable internal refusals to existing failure/events/recovery contracts.
These integrations, actual ENOSPC/allocation testing, macOS measurement/enforcement,
required atomic-workflow envelope coverage, full checklist, independent release
review and CI remain pending. No package-only result waives those gates.

Handwritten JSON vectors cover all categories, states, arithmetic and namespace
boundaries. Focused tests include distinct executable helper processes at a pipe
barrier using the test's own binary, real stable flock contention, all-or-none
multi-filesystem admission, torn/short Write/Sync/Close/readback injection,
manifest retirement, idempotent settlement, conservative owner repair, finite
metadata serialization and stale physical observations. Fault injection is not
physical fsync/ENOSPC qualification. The process helper is excluded from the
parent selection and runs only as the contention test's explicit child.


`ConfigurationSnapshot` returns complete detached v2 filesystem capability and
primary-reference metadata, constraint identities/resources/headroom, policy,
revision, epoch and maintenance. `Snapshot.CapacityPromised` is a canonical
per-constraint aggregate separate from its logical Promised field. `InspectHost`
uses exactly one validated load for that configuration, complete immutable root
and workspace definitions, full tickets and actual persisted applied tuples.
Ticket component/change slices are detached; errors return a zero DTO. Replayed
terminal deltas are never reported as already persisted checkpoint incorporation.
No read API grants original custody, write authority or allocation guarantees.

The fixed format is JPSTB002 (including embedded manifest tickets). Recognized
JPSTB001 in any active or inactive checkpoint/ticket/outer or embedded manifest
refuses before mutation and preserves all bytes. Desk's fixed original host
journal uses DSKHINT2/version2: sixteen 400-byte component rows, body 15,663,
encoded 15,791 and zero 593-byte tail in the unchanged 16,384-byte page. Recognized
DSKHINT1 in either bank/inactive slot refuses before a mutating Q join, including
the maintenance fence. No automatic migration, zeroing or reset is supplied;
unpublished v1 data remains original state requiring a separately reviewed,
crash-conserving migration when present.

The staged Desk owner uses only explicit typed original observer facts and
backend-authored finite envelopes; it supplies no production backend or guessed
generation after restart. Census keeps original home/use/domain holds across
measurement, registration and publication. Every short phase reopens the original
journal and inspects complete current definitions and the same maintenance epoch
under the existing real 250ms control child; the overall bound remains 30s.
Registration never makes roots Known or publishes partial measurement, and a
later explicit census reacquires custody and measures afresh after interruption.
The extra prior-format journal guard precedes the fence; same-format idle/use/
domain refusal remains after it. These shorter phases do not make Sync/Close
preemptible and do not prove a runtime timing result.

The original owner's staged Desk anchor is not activated by this package. Its
finite existing-home bootstrap requires an already private config/parent and
existing validated exclusive `.home-use.lock`; it creates an immutable claim,
original identity record, readiness record, bootstrap/use locks, fixed 2 MiB host
intent area, and Q's exact original six-file directory. The allowance is 64 MiB /
16 files / one directory for Q plus a separate 4 MiB / 16 files / four directories
for host control. A complete final readiness record after uncertain final
acknowledgement may be inspected at the same identity, still in maintenance.
Partial state is never initialized again or automatically repaired. This original
bootstrap dependency supplies no old-binary barrier, new-base creation or non-home
activation; staged census/journal/writer code remains separately gated.

The existing unexported deterministic fault seam now covers each initializer
file's create, write/short-write, Sync and actual Close, then final directory
Sync/Close and the final `load` readback boundary. With no fault it preserves the
original initializer IO sequence. Final `load` reads checkpoint/ticket pages;
it does not read empty manifest payloads. A fault at this seam is not a claim
that an additional per-manifest readback occurred or that physical fsync failed.
All new staged tests remain runtime-unverified until the coordinator's reviewed
focused batch. Migration/version exclusion, production qualification/activation of root census
and checked host intents, active recovery/token remap, missing-root/shape recovery,
macOS measurement and child enforcement remain required before any aggregate #685
claim. Synthetic capability/envelope fixtures certify no native platform.

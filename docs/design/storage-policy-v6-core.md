# Configurable storage policy core (Desk #685)

This dependency step supplies detached V6 comparison data and original-Q phase
primitives. It does not activate V6 in startup, native qualification, enrollment,
policy POST/status, compact checkpoint reconstruction, or the UI. Those call sites
must be connected and reviewed before configuration is exposed. Existing formats
and ordinary startup dispatch remain unchanged and refuse an unknown V6 frame.

V6 uses DSKOWN6 NUL, version 6, the existing 64-byte header/checksum, zero padding
and fixed 8 MiB catalog banks. The payload is an ordered closed JSON object:
Version, Sequence, TransitionKind, Phase, Transition, PreviousCatalogDigest,
BeforeEffectiveDigest, AfterEffectiveDigest, Source, EffectivePolicy, Overrides,
PolicyTransition. Source is a complete canonical validated V3, V4 or V5 payload;
its original version and every source/provider/lineage operand are preserved.
TransitionKind is closed: policy (1) requires the 2048-byte inverse; source (2)
requires an empty byte string and forbids abort. All fields are mandatory; null
is not an alternate empty encoding. Policy successors keep Source byte-identical.
Source successors preserve effective policy and the complete sparse table, require
each new workspace admission baseline to equal the current future default, and
reuse the exact existing V3 pair/V4 Adjacent/V5 Adjacent relations. The existing
V3-to-V4 bridge comparison is reused with its original provisional native barriers;
no V5 upgrade is invented. Inner source sequences advance only on source changes;
the outer V6 sequence advances for every policy/source publication.

The new outer frame never masquerades as an old codec. Starts, Follows,
Publication and Classify are comparison relations, not recovery permissions.
Prepared carries proposed effective values; active carries committed values;
aborted carries original values and retains the proposed delta for exact joining.

EffectivePolicy contains the installation budget and future-registration default.
ObservationMaxAge is immutable. Sorted sparse overrides carry exact workspace
ID, generation and complete Limits. Unknown, stale, duplicate, unsorted, invalid
and admission-equal rows refuse. Absence means that workspace's original sticky
admission limit; it never means the latest default. Copy default must be an
explicit one-time edit. Repeated edits replace/remove the current row, with no
history growth. Source definitions and effective Q definitions are separate
helpers with detached slices and distinct V6 effective-definition digest domain.
No physical headroom or source definition is changed by an effective projection.

PolicyTransition is exactly 2048 bytes: DSKPOL6 NUL; installation ID, binding,
original Q revision/epoch/bank, domain-separated selected-checkpoint and both full
ticket-bank digests, live-manifest-vector digest, complete Before/After Policies,
and optional exact workspace ID/generation/BeforeLimits/AfterLimits. The first
1681 bytes have fields; the remaining 367 must be zero. Policies occupy 488 bytes
each; each Limits has 30 little-endian uint64 coordinates. A zero workspace has
zero generation/limits. The selected old peer is deliberately outside commitment.
The live manifest vector has 64 ordered 42-byte rows (slot, live flag, uint64
length, digest); empty/retired entries retain only slot. Strict Q loading validates
actual remaining manifest bytes, including each live manifest's full hash.

Capture is read-only. The original host must first validate the complete prior
catalog/Q definition relation under existing outer exclusion, preflight all
Prepared/active/aborted/closure and five control groups, then durably publish
Prepared before BeginPolicyTransition. Q primitives grant no host/native effect
permission. Each primitive reloads under the existing Q lock, refuses all
terminal-unapplied tickets and raw/replayed differences, and compares the full
original accounting, unused rows, ticket banks and live manifests. Existing
admitted nonterminal promises remain. No mutable-state memoization is introduced.

For original bank b, revision r, epoch e: Q0 is b/r/e/not-maintenance; Q1 is
1-b/r+1/e+1/maintenance with old policy; Q2 is b/r+2/e+1/maintenance with new
policy and at most one current workspace limit; Q3 is 1-b/r+3/e+1/not-maintenance
with new values. Later peers are the exact immediately previous phase. Inversion
restores only the declared policy/one row and generation/epoch/maintenance in the
actual selected checkpoint, then canonical-encodes under the original anchor and
matches Q0. Still-present tickets/manifests supply bytes; hashes cannot recreate
lost data. Abort permits Q1 -> b/r+2/e+1/not-maintenance/old values only. Q2 never
rolls back. A no-fence abort writes no Q and needs later host disposition.

The new sticky atomic path is separate from legacy UpdatePolicy, whose original
equal-default propagation is unchanged. All existing Q pages, checked write,
sync, Close, readback, lock/deadline behavior and error values remain unchanged.
The typed host must retain original authority, lock order, consumed phase grants,
no replay and cancellation/drain guarantees; no Q API can establish them itself.

V6 closure materializes every missing maximum-width pledged C/S/F receipt using
the original version's closure tree, full before-ID lists and widest envelope.
It additionally reserves a maximum-width override for every registered workspace
and maximum legal logical policy widths. Current prefix annotations and the complete
future tree pass the exact V6 scanner within the unchanged 8,388,544-byte payload
cap. Factual source/lineage rows are retained. Overflow refuses before Prepared;
it does not spend pledged receipt room, increase a cap or evict history. JSON
sparse rows use their exact encoded size, not the optional 264-byte binary-row
estimate in the design. New enrollment must redo this closure before admission.

The maximum-workspace sizing shape in tests is a bounded allocation/size vector,
not a valid original source or native claim. Source-valid smaller fixtures test
lossless enrollment, C/S/F and later policy continuity.

Handwritten tests are authored only at this step. They do not qualify native or
application behavior. Exact-source build/tests, complete native/proof sizing and
API/enrollment/startup/cancellation/interruption/application acceptance remain
parent-owned gates. Existing 93 Linux/98 platform mandatory paths, 90 complete
proof bodies, 64 KiB/body, 1 MiB/record and 16-record limits remain unchanged.

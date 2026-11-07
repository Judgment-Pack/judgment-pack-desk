# Enrollment capacity-estimate policy

Issue #685 separates hard logical, temporary and settlement quotas from physical capacity estimates. `CapacityVector` already makes this distinction. Enrollment Scope3 / PolicyKind2 now applies it explicitly to the existing finite control schedule and creation tail. This corrects the later enrollment claim that every native allocation, metadata update and retained snapshot image must fit P. It is a versioned contract correction, not equivalent proof of that former claim.

The numeric B/I estimates, complete schedule sum P, parent/constraint attribution, headroom subtraction, settlement cap, object/file/component caps, evidence extents and reservation lifetimes are unchanged. `2*T+W+M` remains an authored policy model. Filesystem-internal allocation and retention can exceed that estimate without new external consumption. Observers continue to report their actual values and uncertainty; a low, unknown or stale required observation refuses admission. An unexpected short write, ENOSPC or persistence failure retains valid predecessors and uncertain obligations. It cannot increase a limit, reduce P, forgive debt, reclaim a snapshot or replay Q.

## Identities and compatibility

Scope1 and Scope2 keep their original meanings, layouts and digest domains. Scope3 uses the Scope2 original-peer layout, including all immutable founding-peer operands, with PolicyKind2 and digest domain `desk-enrollment-control-capacity-estimate-v1`. All existing evidence/envelope/dispatch bindings still include the complete Scope. A legacy decoder refuses Scope3. The original-peer layout predicate is separate from the new policy predicate: retaining an old structural proof does not authorize new semantics.

Native Record3 additionally requires CapacityPolicy2. Its implementation and profile domains are `desk-enrollment-native-implementation-capacity-estimate-v1` and `desk-enrollment-native-profile-capacity-estimate-v1`. Record1/2 parsing and domains remain unchanged. Record3 applicability selects the supported public syscall/filesystem ABI by exact OS, architecture, toolchain and ordered original-parent backend/type/flags/geometry. Kernel release/version strings remain bounded, hashed evidence provenance and may differ from the executing kernel report. Complete evidence bodies and sorted unique source pins remain necessary, and release review binds them to the actual executable. Parsing alone creates no original qualification member. A provisional Linux development record is present in development installed data; it is not accepted production qualification or evidence that the current source and final package correspond.

The same creation plan operands now use estimate-specific plan domains. New tail-bearing creation pages use DSKHINT6/version6; local creation uses DSKHINT7/version7. Both carry CapacityPolicy2 in header byte13. Old DSKHINT4/5 pages still round-trip byte-for-byte with CapacityPolicy0 in the decoded value. Creation witness tag6 distinguishes new pages from legacy creation witness tag4. Shared host readers retain both formats. Rewriting an old header cannot change its committed plan or create estimate semantics. Tail discharge additionally requires equal policy semantics between the original action and enrollment evidence.

Old obligations are inspectable and retained. Startup reconstructs the complete original Scope/P reservation before returning the existing typed MigrationRequired refusal; it does not enter a control or Q continuation. Original creation recovery similarly refuses old policy obligations. No automatic migration, debt transfer, reset, deletion or conversion is implemented. Existing E1 migration refusals and every old no-replay obligation remain. A separate explicit migration must establish its original contract before continuation.

## All five native subjects remain required

Mask31 and its five independently reviewed evidence entries remain mandatory. Under Record3, bit0 covers old-reader logical/state/death compatibility and fresh headroom before entry; bit1 covers original native capture and custody; bit2 covers checked successful arm/reference persistence before Q; bit3 covers application I/O completion and exclusion before a new bounded pass; bit4 covers actual short/error/ENOSPC behavior preserving original state, reservations and no replay. The allocation portion of bits0/3/4 no longer claims a universal native <=P theorem. Actual native observations corroborate the exact source-derived safety subjects and disclose allocation uncertainty.

Neither a larger estimate nor a complete JSON record qualifies a platform. The original pre-Fence, one-Fence and post-Fence rules remain: no new names/extents/Q before both fences; exact original Q/identity/receipt/source joins; confirmed Q prefixes skip completed calls; a consumed arm with actual pre-state is uncertain and never replayed; failed publication stops and drains. P is not spent by byte writes, renewed by restart or released by a low-space refusal.

## Native implementation and remaining evidence

Linux/amd64 retains its original observations and single-call pwrite. Darwin/arm64 and Darwin/amd64 now have actual read-only kernel/parent observations through existing sysctl/statfs ABIs and single-call pwrite. APFS fragment-size/project fields are absent, encoded zero, rather than invented. The supported ABI and exact parent cohort still need a matching genuine record; complete actual observations are captured and rechecked exactly within each invocation. The legacy FIEMAP density primitive is retained for inspection/tests and is not a Scope3 allocation premise; APFS never inherits it.

Darwin enrollment/creation persistence and the original coordinator Sync sites use one explicit F_FULLFSYNC operation through the retained description. ENOTSUP, EINVAL, EINTR and other failures return through the existing failure/uncertainty path. There is no fallback to fsync or implicit retry. Linux and other coordinator platforms retain os.File.Sync. The original coordinator's single 4MiB WriteAt, close/readback order, Q states and encoding are unchanged; initialization uses the same checked persistence primitive. Native source manifests include the changed persistence leaves.

A successful Go os.File.Sync on Darwin could previously mean F_FULLFSYNC returned ENOTSUP and fsync succeeded. That ambiguity is removed from these leaves. Whether each actual original file, parent directory, filesystem and device honors the explicit command and required crash durability remains a genuine native prerequisite. The scratch test records file/parent refusals explicitly; it cannot turn directory failure into success or establish power-loss safety. Full five-subject qualification, original-object identity across restart, real pre-/one-/post-Fence death/error histories, the actual 4MiB Q route and all three published native architectures remain pending genuine evidence.

## Bounded source manifests and validation

Record1/2 retain Sources<=64. Record3 permits one flat Sources<=98 list, with the original per-path 4096-byte, per-artifact 65536-byte, 16-record and total 1048576-byte input limits, canonical JSON, sorted uniqueness and digest checks unchanged. The two new mandatory Record3 pins are `storage_budget_enrollment_operands.go`, which implements pure decode reuse, and `storage_budget_enrollment_q.go`, which implements actual arm/Q dispatch. Both use their complete `internal/desk/` paths. They expand the required unions from 91/96 to 93 source pins for Linux and 98 for Darwin. This explicit structural correction raises the former 96-pin Record3 maximum to 98; no storage quota, P, control extent, component limit or second manifest lane changes. Every missing required pin refuses even when the remaining list is rehashed.

The maximum increase over the legacy 64-pin limit is 34 additional pins times 24712 serialized bytes, or 840208 bytes. Each 4096-byte valid relative path needs at least three separators, so at most 4093 bytes escape to six JSON bytes; include path quotes, fixed member spelling, a 129-byte worst-case digest array and one separator per pin. The two added pins increase the former 96-pin worst case by 49424 bytes. CapacityPolicy adds 19 bytes separately. These are conservative metadata bounds, not physical allocation allowances or an assurance that a complete record fits. The unchanged 1048576-byte total input ceiling still decides aggregate acceptance after all complete evidence bodies are included. Measure the exact revised canonical record bytes and retain source-list/body digests with the batch; source metadata is not native qualification evidence.

Handwritten tests cover old/new identities, original reservations, creation-page round trips/relabel refusal, all required source pins (including deletion followed by a recomputed Implementation digest), 98/99 and total-input boundaries, retained Record1/2 64/65 boundaries, and modeled observations below headroom after overhead above the estimate. The existing two-process coordinator harness now also exercises logical, Transient and settlement promises, alongside shared capacity. Separate scratch native tests exercise actual4096/65536-byte write, checked sync, close, parent sync and readback and report actual platform/build identity. Existing exact Q-cut fixtures remain unchanged. Authoring, execution, native qualification, independent review and release are separate gates; none is implied by this document.

## Record3 applicability and provisional development status

The public-ABI applicability correction below preceded the first provisional Linux development record. That development record now exists; its historical source pins, derivations and native results cannot be relabeled as evidence for later changed source. The required source-list correction above changes structural coverage and bounds only. Record1/2 validation/domains, Scope1/2 debt and every existing Scope3/creation obligation retain their original meanings; no debt reset, migration or reinterpretation is introduced.

Tightening the mandatory Record3 pins immediately invalidates the existing development record. Keep the source-contract correction in scratch until reviewed current-source semantic derivations, genuine captured native evidence and the matching immutable installed artifact can be integrated together. A hash refresh alone supplies no proof. The prerequisite capture may use an isolated source snapshot with an explicitly empty installed list, preserving all existing native/parser groups and recording that exact input in its execution manifest. This creates no qualification. Keep installed data excluded from the implementation-only identity and pin the actual captured test binaries, complete execution source and later installed artifact separately; never hash the record recursively into its own implementation proof. Final package correspondence remains an additional gate.

Release applicability and original invocation currentness are separate comparisons:

| Fact | Record3 applicability | Original invocation currentness |
|---|---|---|
| OS and architecture | Exact linux/amd64 public syscall/UAPI, or darwin/amd64 and darwin/arm64 libSystem/SDK ABI | Exact captured values |
| Toolchain | Exact reported Go version; release checks separately pin complete build/toolchain/cgo/SDK/source identity | Exact captured value |
| Kernel release/version | Bounded nonempty run provenance, retained in the complete hashed record; no guessed version range | Exact actual captured strings, even when they differ from the release report |
| Parent backend/type | Exact ext4/APFS selector and existing supported validation | Exact captured values in original parent order |
| Mount and project flags | Every raw bit exact; no wildcard, feature-mask guess or inherited-quota relaxation | Exact captured values |
| Block and fragment geometry | Existing validation and exact report-to-parent equality | Exact captured values |
| Actual objects, volume/incarnation, roles and constraints | Existing independent custody, mapping and fresh-headroom checks | Existing exact original checks remain mandatory |

The public Linux syscall interface is documented as stable for userspace. The inspected pinned Go1.26.10 wrappers call that interface; the existing ext4 leaves additionally validate actual filesystem type, public UAPI request/response sizes and error outcomes. An unavailable ioctl/syscall still refuses at its existing leaf. This supports selecting the public ABI rather than an exact kernel build string; it is not a theorem about every kernel configuration, filesystem allocation, storage device or successful durability.

On Darwin, the inspected pinned Go wrappers call libSystem pwrite, the pinned x/sys wrapper calls fcntl, and the existing cgo bridge calls public fgetattrlist. SDK static assertions and strict returned-mask/length/type parsers protect their concrete layouts. Apple's attribute API warns that volumes need not support every requested attribute; the existing parser refuses missing fields rather than accepting filled zeros. Checked F_FULLFSYNC still returns native errors without fallback. API selection and a kernel report match cannot establish that a particular original directory/device supplies the required barrier. Darwin qualification remains pending its own genuine evidence.

Mount/project flags may affect access, quota/inheritance or persistence. Geometry supplies actual capacity conversion inputs. This bounded correction removes neither selector: it permits a different kernel report only for the same exact qualified parent cohort and build. It therefore removes exact-kernel coupling but does not establish coverage of all ordinary mount/geometry combinations or complete desktop deployability. All five evidence subjects, source/build correspondence and checked barriers still require review and genuine native results. No physical estimate becomes a certified maximum.

The original issuer stores the actual complete live facts separately from the release record. Every currentness check requires complete equality with that immutable capture, including both kernel strings and every raw parent bit. It also rechecks release applicability. Creation profile role/order/arity and exact parent comparisons are unchanged. Two Record3 reports differing only in kernel provenance overlap and are refused by the strict record-list parser; selecting the first matching report cannot hide ambiguity. The full record/profile digest still binds its provenance bytes.

Primary contracts and exact source basis:

- [Linux userspace syscall stability](https://docs.kernel.org/process/stable-api-nonsense.html) and [stable userspace ABI documentation](https://docs.kernel.org/admin-guide/abi-stable.html). These describe userspace interfaces, not unstable internal kernel structures.
- [Apple fcntl contract](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/fcntl.2.html) and [attribute API support/packing contract](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getattrlist.2.html). The archived fcntl page is not evidence of current APFS directory/device support; that remains a native gate.
- Pinned Go1.26.10 `src/syscall/syscall_unix.go` and `zsyscall_darwin_amd64.go`/`zsyscall_darwin_arm64.go`; pinned x/sys v0.49.0 `unix/fcntl_darwin.go`; existing architecture-specific native ext4 leaves, Darwin SDK assertions/parsers and checked sync leaves. Exact inspected source hashes are retained in the batch handoff; an ambient different Go source tree is not the release source basis.

New handwritten comparisons cover all three ABI targets, differing kernel provenance, unchanged toolchain/parent/geometry selectors, malformed provenance bounds, complete live-capture drift, empty capture, overlapping reports and retained distinct exact parent cohorts. They are parser/comparison fixtures only; no qualification member, installed record or native success is manufactured.


## Desk-owned ordinary file fanout

The independent enrollment Q reader now preserves the core exact-reference
variant, including its full sorted workspace ID/generation dictionary and exact
per-group bitsets. Live/unapplied accounting requires the complete validated
manifest; a ticket-page marker cannot supply membership. Physical quantities
are charged once per group and attribution once to every selected workspace.
Transition comparisons include membership. Applied terminals retain only the
page marker and never reopen the retired manifest. Scope3 serialized applied
views use the component-count high bit for that marker, with the same8244-byte
per-ticket bound; unhydrated live values refuse. Scope1/2 evidence still refuses
the new marker and ordinary witness tag8. Flat ticket JSON and binary bodies
remain unchanged.

Ordinary single-file writes with16 through1024 retained references use the
closed DSKHINT8/version8 host record. Versions3 through7 already belong to
identity/creation and keep their existing meanings. Writes with at most15
references retain DSKHINT2. The new ordinary variant stores a 32-byte reference
binding and the33-byte original Reserved token in the existing page padding:
15791+65=15856 encoded bytes in16384, leaving528 of the former593 padding bytes.
The fixed64-slot, two-bank host allocation remains2097152 bytes; no new file,
bootstrap object, control floor, Q extent, workload limit or quota is introduced.
Each data operation uses at most two physical groups: the data group with the
full reference set, and the existing unreferenced config Control settlement
promise. A config Control data group merges that promise as before. The fixed
settlement promise remains65536 bytes, with unchanged parent allocation estimate
and native call schedule: the same16384-byte host writes/readbacks and existing Q
calls. Hashing/reconstruction adds no persistent writes or allocation lane.

The dictionary is reconstructed from the original retained owner catalog rather
than duplicated in every host page. Existing root references and workspace
definitions/generations cannot be replaced during catalog retention; root binding
also freezes after census/while an operation is active. The reference digest
binds the exact data root identity/generation/category and full sorted membership.
A separate immutable host-plan digest binds Target, Stage, original CAS/old object
metadata, lengths, every physical/control component, root/filesystem identities,
locators, app workflow/operation, nonce and reference digest into the new Q
workflow/operation. Its fanout manifest has no object-entry lane. This record is
specific to ordinary single-file writes and gives no transfer/creation authority.

Current original validation compares the reconstructed complete request with Q.
An exact-applied terminal reconstructs the original manifest commitment using the
retained Reserved token without reading the retired payload. Later Q slot reuse
continues to use the existing exact persisted retirement proof; a newer ticket's
manifest cannot be interpreted as its predecessor's. Enrollment retains the exact
catalog projection and raw host witness; it rechecks the same reference binding
and current-terminal commitment or the existing later-slot proof. Unknown, torn,
mismatched or nonreusable originals refuse. Old flat live/uncertain/terminal
obligations retain their original categories, byte form and no-replay treatment.
An old host reader refuses DSKHINT8 in either bank, before startup Q mutation.

The shared owner category decision now identifies recognized top-level memory
and attachment files as Memory and Attachments in managed History and legacy
config namespaces. Legacy references remain attributed to every original member.
Retained/provider roots keep Retained; other ownership decisions are unchanged.
Census uses this same decision. Reclassification requires the existing original
home EX acquisition, complete census and admission/epoch fencing. Every unresolved
old Q promise and nonreusable host intent blocks publication; no old debt is
subtracted or relabeled. The running server retains home SH, excluding a stale
startup owner until its whole lifetime ends. Normal startup admission stays closed
until that complete census succeeds.

This is a Desk-owned foundation. Transfer banks, new/external destination
enrollment, reattachment, selection transitions and whole-operation lifetime/route
activation remain dependent work. Child growth is measured and warned about in
v0.14; child enforcement belongs to the later adoption issue. This foundation adds
no installed native record, no native qualification is implied by synthetic vectors, and
Darwin checked directory durability remains a genuine pending evidence gate.


## Confirmed admission results at a coordination deadline

The host control callback previously shared one 250 ms child context across all
admission inspection, original host-page writes, Reserve, confirmation and
Advance. This was a bound on coordination and later journal steps together,
not a proven latency for every supported reference count. Actual diagnostic
observations found a successful Reserve completing after that budget, followed
by an expired-context Busy on confirmation; a successful Advance similarly
preceded a refused Staging record. Neither observed admission had native lock
contention. Those observations include scratch tracing overhead and are not a
latency guarantee.

Core and independent-reader definition lookups now use index-only loops over
their 4096-root, 1024-workspace, 128-filesystem and 512-constraint fixed arrays.
The former value-array range could copy the whole array on each per-reference
lookup, even for an early match. Exact first/last/missing/zero lookup semantics,
full per-reference generation/custody checks and independent attribution remain;
no table, cache, map, quota or reference cap is added.

This explicitly amends ordinary admission's completion budget. The same control
mutex/flock and original home/domain/use/owner borrower remain held throughout.
The original 250 ms coordination context still governs every new Reserve/Advance.
Only a positively successful original Q return may enter a separate 250 ms
metadata budget derived from the original caller context. Reserve confirmation
reads its exact returned Q tuple and writes the existing planned Reserved page;
Advance confirmation writes the existing planned Staging page. Error/unknown Q
results retain their old uncertainty path and do not enter this recording path.
There is no extra Q mutation, file, durable phase, retry or recovery authority.

The metadata budget does not remove caller cancellation. A cancelled original
caller refuses a new metadata write and retains whichever original host/Q debt
was already durable. After recording, caller cancellation or exhausted original
coordination returns an error: no further Q mutation or application-write permit
is issued. Close/drain still waits for this original borrower to finish and then
releases the same descriptions; it does not cancel or forgive the ticket.

Total control residency may therefore include the extra metadata window; this
is not an unchanged end-to-end 250 ms timeout. Nonpreemptible fixed-size native
calls can still exceed a deadline. Every already-planned native call, byte count,
parent reservation, fixed extent and settlement estimate is unchanged. An
unknown or failed confirmation retains debt and never causes Q replay. Tests
force internal deadline expiry after real positive Reserve/Advance returns and
separately cancel the original caller, without sleeps or timing-based success
assertions. Native qualification and full acceptance remain pending.


## Original transfer completion and cancellation

Normal completion is part of the same admitted transfer as its data/serving
callback. After a nil callback result while the original preparation is live,
the lexical owner completes on that preparation's original context, retaining
the caller deadline/cancellation and Server.Close-derived cancellation. This
includes exact settlement/applied-Q proof and the owning successor's admitted
Tail, both Fences, complete controls, reconciliation and final binding install.
A Retired transfer ticket alone does not discharge that Tail or expose selection.

Context selection occurs once. An already failed callback or cancelled original
preparation retains the existing two-second detached cleanup dispatch window.
Cancellation after live normal completion starts does not select a fresh cleanup
context, retry a consumed group, or replay Q/effects. Incomplete reconciliation
retains original debt and refuses ordinary admission. Export serving failures
and checked original Close errors keep their original joined results.

This explicitly changes normal completion's dispatch budget: it has no separate
two-second elapsed ceiling. A caller with no deadline has no new fixed normal
wall-clock ceiling, as in its data phase. The original finite plan, byte/extent/
count and reservation bounds, fresh currentness/native/admission checks, caller
and Close cancellation remain. The two-second cleanup acceptance bound is not
a native-drain guarantee. On either path, entered synchronous work must actually
return and the lexical owner must check every mandatory Close before releasing
original home/use/private/domain custody or permitting another pass. Server.Close
only revokes and joins; it neither executes completion nor forgives debt.

## Native five-subject fixture coverage before record issuance

The existing release archive prerequisite step selects the observation, checked
file/parent persistence and platform identity tests together with the record
comparison/bounds, actual errno, retained-original process-death, Fence-image
process-death and Q admission/death fixtures. Linux/amd64 additionally selects
the build-tagged actual short-write fixture. Each selected top-level test must
run and pass exactly once; any package or subtest failure or skip refuses the
prerequisite. Exact source, toolchain and test-binary evidence remains retained.
The existing test/step time limits remain unchanged. Release execution on all
three native targets is still required; this selection issues no record.

The native test route remains independent of installed records. Safe primitive
fixtures call the actual one-shot write and checked-sync leaves directly; they
never construct a qualification member, invoke an enrollment effect with fake
proof, or register a release record. Pure record/comparator fixtures no longer
require installed release data to remain permanently empty. The original
missing-qualification entry refusal and immutable installed input are unchanged.

| Subject | Reused source/fixture basis | Added bounded native/process coverage | Source derivation / subsequent journey evidence |
|---|---|---|---|
| 0: old-state/death compatibility and fresh headroom | Original-peer/Scope codecs, `TestStorageEnrollmentPeakPrefixOriginalAndArmedJoins`, exact Q vectors and existing capacity/process admission tests | `TestStorageEnrollmentNativeFenceProcessDeathImages` persists exact old/Fence codec images on scratch originals at pre-/one-/post-Fence PID cuts; Q death cuts conserve debt | Derive live Fence exclusion and fresh headroom from the inspected guards; actual qualified entry journeys follow genuine issuance |
| 1: original capture/custody | Existing Linux/Darwin persistent-file tests and original-parent/anchor currentness tests | Actual O_EXCL collision; `TestStorageEnrollmentNativeProcessDeathPreservesOriginal` compares retained/reopened native volume/object/incarnation, name witness and exact bytes across PID death | Complete original parent-role/object capture on each genuine creation/enrollment route |
| 2: checked arm/reference persistence before Q | `nativePermit`, original arm/reference ordering, checked native sync leaves; existing scratch persistence and original 4 MiB checkpoint API test | Actual 4096-byte scratch write/file barrier, and explicit parent barrier before death; a refused Darwin directory full-sync fails this fixture | Successful actual original file/directory/device barriers and arm/reference-before-Q histories; scratch dispatch bytes are not an enrolled arm/reference |
| 3: completed I/O/exclusion before another pass | Original epoch retained holds/cancellation tests, owner Close/drain tests and cross-process writer/anchor tests | Child retains original Q flock until real PID death; peer must refuse while held, then load the same original banks. Native child dies with retained descriptions | Full admitted application I/O/borrower completion on genuine enrollment paths; PID death is not an in-flight syscall interruption proof |
| 4: short/error outcomes conserve originals/debt/no replay | Existing one-shot result vectors and Q write/sync/close/readback fault seams | Actual readonly pwrite EBADF; Linux child-local file-size limit produces a real short write and EFBIG while retaining original tail; injected Q uncertainty survives death, exact manifest remains inspectable, and refused admission leaves all five Q file digests unchanged | Source-derived common error propagation plus required simulated ENOSPC/low-space/quota boundary evidence; profile-specific successful/error barriers and later no-replay journeys |

`TestStorageEnrollmentNativeLinuxActualShortWrite` lowers only its exact child
process's soft RLIMIT_FSIZE to 4096, preserving the hard limit and restoring the
original soft value before exit. The parent limit is never changed. Its one
65536-byte pwrite must stop at 4096 bytes; a separate call beginning at the limit
must report EFBIG. The existing 8192-byte original must retain its identity,
extent and untouched tail. This is actual kernel short/error behavior under a
process file-size restriction, not ENOSPC. The Linux man-pages primary contracts
for [resource limits](https://man7.org/linux/man-pages/man2/getrlimit.2.html) and
[short writes](https://man7.org/linux/man-pages/man2/write.2.html) describe those
outcomes. Failure to obtain them fails the fixture; it does not create a pass.

Q process cuts occur before Reserve, after a successful Reserve, and after the
existing injected ticket write/sync/close/readback refusals. Only the write case
injects a 32-byte torn page. Complete possible reservations remain charged; torn
state refuses ordinary loading and retains the original manifest for inspection.
The child remains blocked on its owned input pipe until killed by its exact PID
and joined. Injected errors are not reported as actual errno, and process death
is not machine power loss or proof of device durability.

The Fence image test uses the existing handwritten codec fixtures, both actual
original scratch descriptions, and at most 128 native writes of 65536 bytes per
8 MiB image. It checks the exact old/old, Fence/old and Fence/Fence surviving
pairs, native identity across reopen, old-reader acceptance/refusal and unchanged
two-file namespace. It never sets a live epoch's fenced flag or creates Q state.
The live barrier additionally requires nativePermit, current original targets,
both readbacks and held original exclusion in persistInitialEnrollmentFences;
those requirements remain source-derived before first issuance.

First record issuance can use genuine primitive runs combined with independently
reviewed source derivations for all five subjects. It does not require an app
path that is itself blocked until a record exists. The complete source/build/
profile-bound derivations and reviewed result artifacts have not been issued by
these tests. After genuine issuance, unchanged actual first-use API/browser and
enrollment journey/cut checks still must run. Required simulated low-space,
quota-exceeded and ENOSPC at write/rename/settlement boundaries remain mandatory.
The result-vector fixture covers injected ENOSPC at the one-shot write boundary;
generic writer boundary errors and pre-stage ENOSPC API fixtures are separate
existing evidence, not a claim of typed ENOSPC coverage at every later boundary.
Lack of forced real host ENOSPC is an evidence limitation, not a new release gate.

These additions do not change Record3 applicability. Exact parent flags,
geometry, toolchain/build/source binding and complete current-invocation facts
remain mandatory. The existing Linux prerequisite evidence does not cover Darwin
or every ordinary desktop mount. Darwin checked directory F_FULLFSYNC remains
an explicit native gate. Current-source five-subject derivation/evidence review,
matching record assembly and final package correspondence remain pending. Retained
focused API/browser or application behavior must keep its exact source provenance;
it does not establish native qualification. Required remaining application journeys
still follow accepted matching evidence; no fixture or parser success bypasses the
original issuer.


## Configurable policy and native currentness

Policy preparation is an explicit original-owned prerequisite for the real
requester. New and policy snapshot reads do not enroll, and policy mutation
compares a fresh complete snapshot without silently rebasing it. Registered
workspace limits are explicit sticky values; the default applies only to future
registrations, and copying it to an existing workspace is a one-time edit.
Immutable source rows and their physical identity/headroom remain unchanged.
A decrease below charges preserves them and all admitted promises while refusing
new growth. The original five policy groups own Prepared, Q begin/apply, final
outcome and Q finish; uncertain writes require original recovery, not retry.

Native stage-prefix semantic version 3 uses bytes 233:249 for the independently
bound nonzero current Q epoch and minimum revision; bytes 249:256 stay zero.
Commit derives E+1/R+3, fenced abort E+1/R+2, and the distinct unfenced abort
retains E/R. P/C/S/F copies that pair through both banks and retained historical
prefix recipes. Only genuine completed E1 rebinds it to its joined Q3. Intermediate
source cuts retain their original before pair. Currentness additionally requires
healthy original Q, full effective definitions, persistence/applied state and
source/owner/native custody; revision or the pair alone grants no authority.
Old prefix version2 cannot activate current policy by synthesizing these values.
Legacy codecs remain separate and unchanged.

These 16 bytes occupy existing reserved space: stage 256, native prefix 800 and
before recipe 736 do not grow; no extra policy/native write or pledged C/S/F room
is introduced. Source-valid joint byte-limit/+1 witnesses, exact-source focused
execution, complete current native/proof correspondence and application gates
remain required. The authored host tests are not execution or qualification.

# Storage budget original-owner startup

Desk #685 connects the actual parent constructor to the original owner-v2,
claim-v2, catalog-v3 and Q state. These formats and the existing Q arithmetic
remain unchanged. The original adopted project, assistant config, home lease,
registered providers and their selected operations seal construction. Serialized
IDs, paths, copied wrappers and observation DTOs cannot construct this origin.

The parent retains the original home descriptor continuously in EX while it
inspects sources, opens the two catalog banks, reconciles actual Q, publishes
Ready and measures existing data. A selected injected provider operation is
acquired before host custody and retained until its borrowed source descriptors
have drained. Prefix order is home EX, installation-use EX, original work EX,
then canonical domain EX; the short control/Q gate comes after these prefixes.
Initial census borrows these same holds. The constructor changes the same home
descriptor to SH, then checks original currentness, Q, logical limits and a fresh
concrete advisory capacity pass before recovery or workers start. The live
source set retains the absolute named home and every original registry project
edge, including projects without private data. Currentness
rechecks these names; child servers borrow the parent's installation only after
matching their adopted root to that exact captured project relationship. They
neither initialize Q nor close the parent's pin.

## Host-authored initial policy

Defaults are finite host policy, not workload measurements or native allocation
proof. Installation total is 24 GiB, 262144 files and 65536 directories. Workspace
total is 4 GiB, 65536 files and 16384 directories. Every category has the same
count ceilings as its corresponding total. Category ceilings intersect shared
totals; they do not create additional entitlement.

| Category | Installation bytes | Workspace bytes |
| --- | ---: | ---: |
| History | 6442450944 | 1073741824 |
| Retained | 12884901888 | 2147483648 |
| Attachments | 6442450944 | 1073741824 |
| Memory | 268435456 | 33554432 |
| Control | 536870912 | 268435456 |
| Disposable | 268435456 | 67108864 |
| Binaries | 8589934592 | 4294967296 |
| Transient | 4294967296 | 2147483648 |
| Settlement | 1073741824 | 4294967296 |

The positive workspace Binaries ceiling completes the schema; installation-owned
managed binaries retain their installation attribution. Workspace Settlement is
an aggregate 4 GiB ceiling, still intersecting the installation's 1 GiB pool and
both totals. The existing 128 MiB ceiling remains per operation. Restart restores
persisted definitions and limits; it does not silently apply new defaults.

## Bounded bootstrap and persistence inventory

`storageOwnerBootstrapEffects` names each single-use group below. B, F, D and M
are bounded logical persistence bytes, new files, new directories and explicit
metadata-only operations. Every group is reserved before its first effect using
the unchanged advisory allocation-v2 estimate. A failed or uncertain group stays
consumed for that launch. The table is authored from the controlled initializer
and publication source, never from saved data, fixture values or free space.
Config and anchor parents are observed separately through normal descriptors;
whole-launch cumulative estimated demand is subtracted even when sharing cannot
be established. An existing target additionally receives its exact regular-file
subject observation. Unsupported or insufficient observations refuse growth.

| Parent/group | B | F | D | M |
| --- | ---: | ---: | ---: | ---: |
| config/claim | 4096 | 1 | 0 | 0 |
| config/record | 4096 | 1 | 0 | 0 |
| config/bootstrap | 0 | 1 | 0 | 0 |
| config/use | 0 | 1 | 0 | 0 |
| config/intent | 2097152 | 1 | 0 | 0 |
| config/catalog-a-create | 8388608 | 1 | 0 | 0 |
| config/catalog-b-create | 8388608 | 1 | 0 | 0 |
| config/data-lock | 0 | 1 | 0 | 0 |
| config/work-lock | 0 | 1 | 0 | 0 |
| config/anchor | 0 | 0 | 1 | 0 |
| config/prepared | 8388608 | 0 | 0 | 1 |
| config/active | 8388608 | 0 | 0 | 1 |
| config/ready | 32 | 1 | 0 | 0 |
| config/ready-complete | 0 | 0 | 0 | 1 |
| anchor/lock | 0 | 1 | 0 | 0 |
| anchor/initialize | 44040192 | 5 | 0 | 0 |
| anchor/register | 4194304 | 0 | 0 | 1 |
| anchor/maintenance | 4194304 | 0 | 0 | 1 |
| anchor/census | 4194304 | 0 | 0 | 1 |
| anchor/complete-state | 44040192 | 0 | 0 | 7 |
| config/claim-complete | 4096 | 0 | 0 | 1 |
| config/record-complete | 4096 | 0 | 0 | 1 |

The unchanged estimate rounds B up to 65536, doubles it, then adds
`65536 * (F + D + M + 4)` bytes and `F + D + 2` policy inode units for
each nonempty group. Summing all 22 possible named groups gives B=136331296,
F=16, D=1, M=15 and estimated demand 281149440 bytes / 61 policy inode units.
This is a finite ceiling across alternatives, not a claim that every group is
spent on one launch. The fresh branch spends 17 groups (excluding the three
completion groups, existing-state completion and maintenance), with estimated
demand 182386688 bytes / 51 policy inode units.

Derivation of effects:

- Exclusive fixed controls use final names, never truncate or replace. Claim and
  record are 4096 bytes each, intent is 2 MiB, catalog banks are 8 MiB each and
  Ready is exactly 32 bytes. Zero-length locks still consume namespace capacity.
  Each creator has at most 128 writes of at most 64 KiB, one file Sync/Close,
  one original parent Sync/Close and bounded full readback/currentness checks.
- Q initialization creates two 4 MiB checkpoints, two 1 MiB ticket banks and
  one 32 MiB manifest store: 42 MiB in five files. The existing controlled
  initializer performs 194 bounded writes, five file Sync/Close operations and
  anchor Sync/Close. The stable Q lock has its separate exclusive group.
- Register, BeginMaintenance and PublishCensus each publish at most one existing
  4 MiB checkpoint. Prepared and Active each overwrite one existing 8 MiB bank
  with one bounded WriteAt and Sync/Close; they create no additional bank.
- Same-inode claim/record/catalog completion Syncs the original full fixed file,
  closes the completion descriptor, Syncs/closes the exact original parent, and
  checks the retained immutable bytes. It does not rewrite an already matching
  page. Existing Q completion reopens only its six explicitly parent-bound retained
  anchor witnesses, then Syncs/closes those files and the anchor
  (seven explicit metadata operations), conservatively includes all 42 MiB,
  and compares the complete actual InspectHost tuple before and after.
- Ready completion validates its exact binding and length, Syncs/closes the same
  inode and original parent, then rechecks bytes and currentness. It never
  changes a malformed Ready or recreates a deleted post-census Ready.

Fresh retained control-file length is 62922784 bytes, including both catalogs
and Ready, with 16 new owner files and one anchor directory. The infrastructure
phase already provisioned the zero-length home lease: including that original
file gives 17 retained fixed files with the work lock (16 without it). This physical source
inventory is distinct from logical census provisioning: the established 68 MiB
Q/config floor remains, the two catalog banks are measured as a further 16 MiB,
and Ready's 32 bytes are charged exactly once. Existing data is measured in
addition; pre-budget bytes never enlarge the authored limits or grants.

## Crash state and remaining qualification

Prepared must be durable before Register. Both banks are checked as a pair;
conflicting equal sequences, non-adjacent transitions or two invalid banks
refuse. A single valid bank can continue only through the exact retained
transaction. Actual Q Before requires its exact saved revision; actual After
finishes publication without repeating Register. A state matching neither
refuses. Unknown or partial Q is preserved and never initialized, reset or
rebased. Fresh initialization requires the exact positively empty namespace,
blank banks, absent Ready and empty original host journal.

Ordinary reopen conserves exact root/workspace/filesystem/constraint IDs,
generations, policy, capacity bindings and every current/retired ticket tuple.
Unretired obligations, unreusable host journal state or an unknown maintenance
fence wait for their original recovery path. Existing over-budget data remains
readable through the private startup read-only routes while growth and workers
pause; this state is launch-local and grants no saved permission.

This batch has handwritten source tests and requires independent source review
before focused compiler/runtime checks. It does not qualify all producers,
original R7/recovery, D2, restriction transition/retention, UI, platform/native
coverage or release. In particular, later identity/history/provider mutations
must carry their obligation-preserving source/catalog transitions; silently
rewriting definitions on restart is forbidden. A standalone existing private
folder without an identity can still receive one from the later startup identity
producer, so that producer transition remains an explicit subsequent dependency.

# Private Policy authority high water

Status: inactive storage primitive, 2026-10-18. This module has no Server, HTTP,
repository, executor or default-path integration. It cannot authorize execution,
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
require a separately reviewed external monotonic witness. No integration or
execution enforcement is claimed by these helpers.

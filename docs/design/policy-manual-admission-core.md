# Private manual admission authority core

The core for `local-admin-manual-pack/1` is integrated with its reviewed
[host adapters](policy-manual-admission-host.md). Explicit offline activation and
local-session HTTP registration are enabled in the uniform acceptance candidate;
unconfigured workspaces keep their existing behavior. The
[wire fixture](policy-manual-admission-wire.json) and
[wire shape](policy-manual-admission-wire-shape.md) define the captured authority channel.
This host contract is separate from proposed portable Policy contracts; retained
adoption/check contract identities are unchanged.

## One authority transaction

Explicit private initialization changes the existing anchored Policy ledger
from schema 2 to schema 3 with a required, closed `execution` version 1 block.
Historical Policy records retain schema 2. Ordinary writes never initialize or
migrate execution state. The initializer requires an anchor whose installation
and workspace identities match the trusted service context. Phase B must first
establish durable external custody demands and actual executor quiescence;
calling this private primitive alone is not a supported enablement procedure.

The execution block contains scoped identity, durable host-time high water,
current epoch, immutable resources, registration operation receipts, manual
intents, epochs and attempts. All maps are required. Limits are 256 resources,
256 epochs, 4,096 registration receipts, 4,096 intents and 4,096 attempts, within
the existing 16 MiB ledger limit. Capacity refuses; it never removes a replay
barrier. Counters use canonical positive uint64 strings; the initial immutable
registration revision is `"1"`.

Current and history reads use the same ledger decoder. It rejects duplicate or
case-variant members, unknown execution fields, missing required members,
explicit nulls, invalid string escapes, malformed identities and contradictory
receipts. Schema 2 cannot contain execution, including `execution:null`.
Schema 3 requires it. Existing schema 2 history remains readable. Current
schema 2 archive and restore formats explicitly refuse schema 3; they cannot
export, restore, or silently strip its execution state.

The existing lock order remains ledger then anchor. No second authority store
or evaluator is introduced. Each checked ledger carries its exact raw digest as
nonserialized transaction metadata. Start receipts refer to that precommit head,
including valid historical whitespace; they do not hash a reserialized ledger
or themselves.

## Trusted registration and eligibility

The host injects authenticated current local-administrator authorization,
stable installation/machine/Runner/executor identity, clock, owning Runner
release resolver and retained adoption resolver. Request bodies cannot replace
these capabilities. The release resolver runs outside the Policy lock and must
authenticate the actual owner, verify the exact current job revision, reject
every trigger (including paused or initial trigger metadata), and return the
existing canonical typed Release bytes. It must check Runner release readiness
and its pinned Runtime. The core verifies the exact byte digest and supported
closed manual release structure. Mappings, dialects, input profiles, mapping
warnings, sample SourceInput and Preparation are unsupported in this profile.

Inside the authority transaction, the retained resolver derives the complete
approved adoption closure from existing immutable artifacts and historical
coverage/check/publication bindings. The core independently rechecks that exact
retained closure and matches the actual Pack bytes, ID and version. One job has
one immutable registration. Only the original registration operation can recover
its outcome, even if Runner has subsequently changed or become unavailable.

Registration records `desk-managed-effective/1`. Ongoing eligibility validates
the retained closure and exact historical publications separately from mutable
adoption-time freshness. Every selected publication must currently be effective
under Desk ownership, beyond ownership/recovery boundaries. Withdrawal, expiry,
future effectiveness, supersession, missing material and later adoption requests
for changes block. Unrelated drafts/reviews and connected-source updates alone
do not withdraw an exact Desk-managed publication.

## Logical operations, time and recovery

Manual intent identity binds installation, both workspaces, job, the stable local
administrator domain and original idempotency key. It excludes release,
registration revision, session, epoch and attempt. Exact original request,
facts and optional evidence bytes are retained as immutable references. Numeric
lexemes and false/zero/null facts survive; omitted evidence differs from supplied
null. Reusing a key with different bytes or binding conflicts. Reserve and start
both repeat the digest of the original raw body, so outer whitespace changes
also conflict even when the facts/evidence descriptor is unchanged.

A trusted session must prove external ownership before epoch activation. Epochs
bind host session/challenge and actual signing configuration. Reserve checks the
entire intent, registration, resource, original request digest and canonical
descriptor against retained input preimages and signing, then creates one
operation/attempt barrier. An eligibility denial still seals that attempt. A
repeated reserve preserves the original receipt and adds a separate durable hold
to an uncommitted attempt; it cannot continue to start. A new session/epoch never
opens another attempt.

`commitStart` repeats every binding and checks current lifecycle inside the same
anchored transaction as withdrawal. Withdrawal first blocks; commitment first
retains a start-committed historical decision even if withdrawal follows. The
core returns decisions, not process-launch objects. Later Runner integration
must require a fresh committed decision and durable local persistence before
minting its one-use launch object. Recovered results and observed history never
permit dispatch.

Authenticated execution commands and enabled-ledger lifecycle commands observe
UTC whole-second host time inside the held transaction. Valid denials retain
time; blocked attempts/start decisions retain their result. Lifecycle denials
discard partial domain changes while retaining time. Malformed/unauthorized
requests do not write. Clock rollback holds eligibility against durably
observed high water; this does not authenticate the external clock. Uncertain
anchor/storage outcomes dominate any domain result and hold, with no replay or
automatic repair.

Phase B still needs the explicit durable downgrade fence and registry migration,
local-only authenticated ingress, trusted binding export client and inherited
private channel. Phase C must gate every operational Runner path and prove actual
Desk/Runner/Runtime ordering, lost-reply, copied-store and downgrade cases before
any governance capability is advertised.

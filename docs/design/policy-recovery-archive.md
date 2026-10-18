# Project Policy recovery archives (#476)

This private, manual recovery format exports one project's complete Policy
history and its exact retained-byte closure. It is separate from **Download chat
backup**, whose v1 all-project chat-store format and restore behavior are
unchanged. A Policy recovery archive does not include conversations, Pack/Graph
drafts, memory, project files, jobs, credentials, installation identities,
connection registries, source consent or browser journals. It is not a complete
project backup or a standardized portable Policy contract.

## Capture and validation

`GET /api/policy-recovery` returns the current project identity, archive limits,
verified counts and export revision. Failed closure verification returns
`available:false`, a reason and no counts; it never substitutes an empty store.
Inspection/restoration can still work with a corrupt or unreadable current
Policy ledger because they create a different, inactive project. Current project
identity, private custody and authentication must remain usable.

`GET /api/policy-recovery/archive` requires `If-Match` with that export revision.
The server captures one validated ledger under the repository read transaction,
then releases the lock before verifying immutable objects and staging the ZIP.
Committed blobs precede their ledger references; there is no artifact GC.
Concurrent writers can continue while the captured cut stays fixed. A complete
archive is prepared before any download bytes are released, with current
authentication rechecked. `X-Policy-Archive-Digest` identifies the exact ZIP.

The closed manifest has format `jpack-desk-policy-recovery`, version 1, private
Policy schema 2, capture time, scope `project-policies`, exact ledger reference
and deduplicated blob references. Entries are only `manifest.json`,
`policy-ledger.json` and `blobs/<SHA256>`. The closure includes every revision and
uploaded original, operation outcome, source original/extraction, retained
external-version/import provenance and supported evidence. Unrelated artifacts
and orphan bytes in the shared retained store are excluded.

The archive is unencrypted. Hashes/lengths detect corruption; they authenticate
neither an author nor approval or current authority. Offline validation reuses
the existing candidate, receipt and strict v1 source-observation invariants.
`gatewayCanonicalJSON` implements the frozen Gateway integer/scalar/code-point
profile and is tested against the vendored complete canonical corpus and exact
producer observation. It makes no Gateway, Runtime or provider call and does
not change online wire verification. Unknown streams/schema, missing/extra
objects, duplicate entries/members, traversal, links/special files, ZIP64 or
multi-disk directories, inconsistent identities and inflated entries refuse
the complete archive. Limits are 512 MiB expanded payload, 2 MiB ZIP overhead,
8,192 entries, 1 MiB manifest, 16 MiB ledger and 32 MiB per retained blob; large
stores beyond these bounds fail explicitly instead of producing partial success.

## Reviewed restore and recovery

`POST /api/policy-recovery/inspect` accepts multipart `settings` with an absolute
`destination`, then `archive`. It validates and reports exact scope/counts,
archive digest/length, ledger digest, capture time and absent destination plus
pinned-parent revision. This operation does not restore a project.

`POST /api/policy-recovery/restore` uploads the same archive with closed settings:
`operationId`, `projectId`, `archiveDigest`, `archiveBytes`, `ledgerDigest`,
`destination` and `destinationRevision`. There is no current-ledger CAS: a
background observation must not invalidate an independently reviewed upload.
The current actor/project, exact archive and destination identity bind the
request digest. Current session and Origin checks apply to every route; no
model write adapter or approval/run grant is introduced.

Only a new absent destination is admitted, outside the current project,
protected settings and known active chat-store trees, under an existing safe
parent. The server writes a complete owner-only sibling staging project on the
destination filesystem, syncs it and publishes with pinned-parent no-replace
rename. Existing targets are never merged or overwritten. The source project,
active chat pointer, startup selection and running Desk remain unchanged.
Opening/restarting into the destination is a separate explicit user action.

The scaffold contains only the Policy ledger/closed blobs, recovery metadata,
private ignore files, fresh `jpack-desk.json` version 1, and a host-authored empty
unsigned `jpack.json` version 5 with reviewed/comparable-fact gates. It copies no
original executable configuration, keys, signing identity or provider settings.
This makes an openable standalone project; no named desk is registered.

A bounded protected source-project operation journal records the exact intent
before publication. A destination-local completion marker binds its outcome and
directory identity. `GET /api/policy-recovery/operations/{operation}` reads only
the path captured by that journal, under current authentication; there is no
caller-supplied path/file-read capability. Replies use `outcomeKind:
"policy-recovery"` and receipt kind `policy-recovery-restore`. Same-body retries
recover the exact metadata outcome before fresh validation; another body/actor
with that operation ID conflicts. Lookup requires no archive reupload, narrative
read or provider access, and repeats no mutation.

Existing Policy error envelopes remain `{code,error,outcome}`. Proven
prepublication refusals use `not-committed`; an uncertain intent/publication uses
`unknown`. Missing/unknown receipts never prove an earlier request cannot still
commit. Failed staging removes only this operation's staging directory. An
uncertain publication retains the possible destination and uses read-only marker
recovery. A crash before publication can leave a staged directory/uncertain
intent; it is never automatically replayed. The atomicity claim is publication
of one new project directory, not simultaneous replacement of live chat,
Policy and artifact stores. Uncooperative same-user filesystem writers and
unsupported custody/rename platforms are outside this guarantee.

## Historical authority and fresh consent

Every restore appends a fresh optional `recoveries` event to the closed private
ledger and assigns each restored Policy a snapshot/sequence boundary. The
stream is bounded to 64 snapshots. Older binaries reject the unknown fields;
they cannot silently ignore a sidecar and revive archived authority. Repeated
restoration creates another fresh boundary. Exact candidate/original/extraction
and operation-outcome bytes, private IDs, reviews, publications, lifecycle
events, source observations and reviewed provenance survive unchanged.

Live Policy CAS includes the restore marker. Historical receipt revisions remain
historical and never authorize a fresh mutation. Pre-boundary reviews and
publications remain readable but do not count as current approval/publication or
authorize new publication/activation. Desk-managed candidates can be saved and
reviewed anew; a new publication requires that fresh review. Activation needs a
publication after the restore and ownership boundaries. Live lifecycle rows
expose canonical `activationAllowed`. Historical release withdrawal remains a
current authenticated safety action even if narrative bytes are unavailable.

Restored source consent is held regardless of matching installation epoch,
archived active/binding state, polling lease or queued cleanup. Canonical worker
admission and final commit suppress **all** archived binds, reads, binding-list,
cleanup and revoke work. Restart never grants consent. Live source status is
unusable and does not apply fallback or erase last-applied historical retention.
The live optional `recovery` projection identifies snapshot/time,
`currentnessUnknown`, `freshReviewRequired` and `sourceConsentRequired` on Policy,
list, lifecycle, source sync and source status replies.

Existing explicit source **bind** is the only consent handoff: a current-session
review of the resource, recurring consent and fresh Policy/source CAS creates a
new current-authority grant tied to this snapshot. It increments generation,
records the fresh operation identity, removes non-dispatchable archived live
poll/cleanup queues and preserves retained observations/resolutions/actions.
It never sends cleanup/revocation for archived identities. Normal current-epoch
rules then govern that new grant and its subsequent cleanup.

Held resume/check/pause/revoke actions refuse; no remote revoke is fabricated.
Explicit **keep current** remains a local review of the exact latest retained
observation and current candidate/workflow, even when the historical binding no
longer matches or Gateway is absent. It grants no source access/freshness and can
resolve the source-update gate for a new local review/publication. A held binding
with zero observations is disconnected history, not a perpetually pending read.

Restored last-applied external Published retains its original administrator or
source basis as history; it is not newly verified provider approval. Existing
capability-driven mapping/fallback rules remain unchanged. Clock-derived
lifecycle observations can reflect elapsed expiry without inventing events.
An old archive can predate a withdrawal: this format provides no anti-rollback
authority service, execution admission, enforcement or portable-conformance
claim. Saved Pack/Graph handoffs and chat associations require their separately
retained project/workspace data; this archive does not recreate them.

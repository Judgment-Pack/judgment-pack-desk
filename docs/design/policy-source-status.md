# Private source authority and status (#473)

Policy source acquisition, ownership, reviewed source status and local publication
remain separate. This is a private Desk application contract, not portable Policy
conformance, governing adoption or execution enforcement. Gateway v1 currently
supports only Drive snapshots with lifecycle `not-exposed`; its wire verification
is unchanged. No model tool, AI mutation adapter or provider lifecycle adapter is
added. Future exposed status vocabularies are exercised only by pure evaluator
fixtures and cannot enter the production HTTP path.

An optional version-1 `sourceAuthorities` stream extends the private v2 ledger.
It retains bounded immutable configuration revisions, ownership decisions,
external-version applications and draft imports under the existing Policy
transaction and operation-receipt store. Absence means inherited Desk ownership,
without a fabricated event. Older strict builds may refuse the added field;
retain the entire ledger and referenced blobs together for moves/backups. A
restored old backup is not an authoritative current withdrawal/enforcement service.

`GET /api/policies/{id}/source-status` returns Policy/source/authority revisions,
explicit or inherited ownership, exact current binding, capability and separate
availability/freshness, current configuration, a non-authorizing preview,
`lastApplied`, external versions, configuration/event history and action
eligibility. `lastApplied` is an immutable reviewed record and remains visible
when a new observation/configuration or failed acquisition needs review. It must
not be displayed as current verified source approval. Current Drive fallback is
Draft unless the administrator explicitly configures Published. Published
fallback has `admin-configured` provenance and no external signatory, approval
time, local review ID or local publication ID.

`POST` to that route accepts separate reviewed actions, each with operation ID,
Policy/source/authority CAS revisions and rationale:

- `configure`: exact binding ID and Draft/Published fallback. This creates a
  configuration revision only. It neither changes ownership nor applies status.
- `set-ownership`: Desk-managed or externally managed. Content/configuration and
  releases stay intact. Explicitly confirming inherited Desk ownership is audited.
- `apply-external`: exact latest observation and current configuration. Requires
  external ownership and an eligible non-Draft status. It retains a separate
  external version with exact source identity/revision, original/extraction refs,
  configuration and actor/time/basis. Exact observation/config/status/basis already
  applied is refused; a different reviewed configuration is a distinct decision.
- `import-draft`: exact latest observation/configuration and explicitly reviewed
  title/narrative/artifact version. Draft or Published source status can enter
  this path. It creates a real new Desk draft revision even for identical bytes,
  records its original source basis, switches ownership to Desk atomically and
  requires new review. Published source does not force local publication. No
  automatic status observation or fallback replaces a saved or unsaved draft.

Only an active, currently authorized binding with a usable verified latest
observation permits new configuration/application/import. Absent, pending,
paused, revoked, authorization-ended, failed/gapped or mismatched binding state
refuses those actions. The HTTP handler holds the existing source grant fence and
compares the current sign-in/connection authority epoch, including before the
worker has updated stale active records. None of these failures activates
fallback or erases a last reviewed application. A genuinely capable source with
unknown/missing evidence requires review rather than fallback. Mapping changes
must create new reviewed applications; historical observations are never
retrospectively reinterpreted. The acquisition worker does not mutate ownership,
applied status, drafts or releases.

Live Policy replies/Library summaries expose effective ownership and reviewed
external metadata separately from historical local publications. The opaque live
Policy revision includes the latest ownership event identity; existing save,
review, publication, source and lifecycle CAS therefore also freezes ownership.
Configuration/application alone does not change that Policy revision. Historical
candidate bytes/IDs, reviews, local publications and completed receipts remain
unchanged. A true ownership transition masks earlier review authority; returning
to Desk requires a new exact review. Newly activating a local publication requires
current Desk ownership and a publication from that ownership epoch. Recorded
schedules remain unchanged; historical local withdrawal remains a restrictive
administrator action. Source Published cannot authorize local review/publication.
Saved working draft text remains distinct from external authority.

New results use `outcomeKind: "source-authority"`, Policy ID/revision,
`sourceStatus` and a receipt binding authority event/revision and relevant
configuration/observation/external-version IDs. `candidateDigest` identifies the
unchanged local working draft, or the real imported draft; source byte identity
is separately retained on the external version. Existing generic/lifecycle
outcomes remain unchanged. Recovery via `/api/policy-operations/{operation}` is
historical read-only lookup, then clients refetch current state. Same-operation
replay checks recovery before Runtime validation. Missing recovery never permits
an automatic replacement write; failed retention exposes no reachable event.
Metadata operations/recovery do not require unrelated draft narrative blobs.

`GET /api/policies/{id}/source-status/versions/{version}` returns the retained
external version, its original configuration and exact extraction document.
Original bytes use the existing observation download route. These versions have
no fabricated local revision ID for authoring: explicitly import a Desk draft
first. Manual UI uses shared unsaved/busy guards, frozen confirmation and scoped
pending-operation recovery. Existing Policy assistant reads obtain current mode
through ordinary live Policy replies; existing draft writes retain shared Plan,
Ask me, authorization and revision admission. No lifecycle mutation tool is added.

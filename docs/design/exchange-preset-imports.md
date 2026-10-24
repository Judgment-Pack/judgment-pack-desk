# Reviewed Exchange preset imports

Status: implemented import/review slice of Desk #400, 2026-10-16. This does not
complete portable research execution or remove the existing recipe templates.

Desk reads an explicit release tag from an HTTPS Git catalog or local repository.
It verifies the tag, commit, tree and blob identities while reading bounded bytes,
never checks out files, executes entry code, or accepts branches as releases.
Local Git helpers, prompts, lazy fetching and non-HTTPS transports are disabled.
Tree traversal counts expanded directories as well as bytes and files. Catalog
metadata uses exact closed keys, duplicate/null refusal, valid Unicode, declared
bounds and presence-sensitive optional members.

The catalog manifest and exact preset file set are checked. Stored-file digests
and Runtime canonical document digests are separate. Presets are unbound integer
version 3 documents, not named formatVersion 3.0.0 publications. Runtime is the
authority for their document form. No successful form check establishes source
truth, fixture replay, local capability binding or run approval.

The page at `/dialects/templates` reviews publisher metadata, source declarations,
README and exact file changes against an immutable previous import. The reviewed
snapshot and history base are retained before import is offered. The exact encoded
snapshot must fit the shared retained-artifact byte limit. Import is a separate
explicit action with CAS; repeated completed requests return the original receipt
before consulting mutable state. Imported files remain content-addressed through
the shared artifact repository. History survives server restart and retains the
previous commit, reviewer and timestamp. A moved tag or downgrade is refused.
Withdrawal does not delete retained definitions or change existing jobs.

Import history is project data under `.desk-private/exchange`; it moves with the
project and is distinct from the chat-only backup archive. Artifact blobs use the
existing retained-artifact repository. This implementation does not claim a cloud
history adapter or multi-user approval service.

## Current supported scope

- `preset` entries with unbound version-3 Dialects, no fixture replay.
- Source declarations and generic field types validated by Runtime, without a
  lossy conversion into Desk's private research recipe.
- Review/import/history and exact updates, with shared loading, diff, notice,
  compact button and private-query components; all supported locales translated.
- Other entry kinds are explicitly unavailable. Upgrade notes whose prior-release
  keys cannot yet be verified against catalog history are also unavailable.

## Work remaining for #400

1. Consume the complete Exchange release history for upgrade-note keys and replay
   fixtures for executable `dialect` entries. An author assertion is not replay.
2. Add definition-driven editing and local connection binding, preserving declared
   parameters, fields, reconciliation, source provenance and byte identities.
3. Extend fresh portable research acceptance to actual search/page acquisitions
   and found values. The shared worker already retains authentic Gateway model
   responses and passes them through Runner; decoded legacy Desk actions remain
   separate from those responses.
4. Complete reviewed migration from private recipe templates to published
   Exchange entries. Catalog v0.2.0 provides three research presets; import checks
   do not establish complete local binding or execution. Keep working recipes
   available until that path is ready, and retain each format's identity.
5. Implement generic downstream selection/relationships after Spec #160's workflow
   contract is accepted. The graph evaluator composes decisions; it does not
   perform research fan-out. Keep migration provenance and all saved evidence.
6. Complete two-installation acceptance for found-value extraction and search/page
   acquisition in both GTM and other domains. The signed-release
   [research preparation check](exchange-research-acceptance.md) already exercises
   all three presets with different local bindings and model answers whose fields
   are all not-found. It does not exercise the unused search/page acquisitions.

## Verification

Tests cover malformed metadata, credentials/local paths, file and canonical digest
separation, tag-only reads, annotated tags, dirty worktrees, configured Git helper
refusal, corrupt Git objects, exact serialized bounds, moved/lower tags, README-only
updates, withdrawal, explicit reviewed import, completed replay and server restart.
Browser tests cover explicit import, stale selection, reload and uncertain-response
retry without run/binding authority. CI runs the core and HTTP integration checks
with the pinned research-capable Runtime. The unchanged fixture's upstream commit,
licence and file hashes are retained under `internal/exchange/testdata`.

`TestExchangeSignedReleaseImportOnTwoInstallations` is an opt-in acceptance check
against an actual signed local catalog tag, without copying or retagging entries:

```sh
JPACK_EXCHANGE_ACCEPTANCE_CATALOG=/path/to/catalog \
JPACK_EXCHANGE_ACCEPTANCE_TAG=vMAJOR.MINOR.PATCH \
JPACK_EXCHANGE_ACCEPTANCE_COMMIT=<expected-full-commit> \
JPACK_EXCHANGE_TEST_BIN=/path/to/jpack \
go test ./internal/desk -run '^TestExchangeSignedReleaseImportOnTwoInstallations$' -count=1 -v
```

It checks every preset in the selected release through authenticated review/import
requests on two disposable installations, exact tagged files and Runtime canonical identity,
explicit approval, completed receipts and restart retention. Signature verification
uses the operator's Git trust configuration. This verifies signed-tag imports;
release assets and attestations require their own publication checks. It neither
runs the presets nor changes their declared model or capabilities. The presets'
`gemini-2.5-flash` and answer/search/page declarations need matching reviewed local
profiles. Page assignments use the reviewed producer below; import alone does not
register a profile or start research.

RFC 0019 §§3 and 7 define entry directories as `entries/<publisher>/<name>/` and
require manifest correspondence and closed file sets within those directories.
Catalog documentation is outside that inventory. Desk recognizes the conventional
catalog index `entries/README.md` as bounded UTF-8 documentation (at most 64 KiB),
without treating it as an entry or authority. This exact path is a reader policy,
not a general documentation exemption: other shallow files, nested documentation
and additional per-entry files remain refused. This fixes the actual v0.2.0 import
failure tracked by Desk #440 while preserving each preset's exact file whitelist,
stored-file digests and Runtime document checks.

### Reviewed page assignments

`PageBindingControls` is shared by portable preparation and imported-template
configuration. A user enters one to 32 exact HTTPS prefixes, reviews the scope,
then registers and selects the resulting installation profile. The generated-only
`https://*` wildcard must be entered and reviewed explicitly. A late registration
cannot replace a newer source or profile selection.

Desk obtains adapter identity from the verified installed
`adapter-web --describe-research` program, checks its bounded descriptor and
executable digest, and supplies its own Gateway authority/key. This performs no
page or model acquisition and needs no search connection. The profile's exact
scope and order enter its identity. Registration uses Runner's immutable profile
registry and its 16-profile limit; no old profile is evicted or silently rebound.
Chat-associated registration validates the saved conversation and refuses Plan;
manual forms require explicit profile review and registration.

The shared admission boundary checks every scoped page URL before a page attempt,
allowance charge or Gateway call. It implements the constrained form and literal
prefix matching of RFC 0018 rounds 15 and 16 (Spec #207, #215): the path holds no
escaped slash or backslash (`%2f`, `%2F`, `%5c`, `%5C`), and no segment's part
before its first `;`, `%3b`, `%3B`, `%3f`, `%3F`, `%23` or `%00` is a dot
segment; nothing is decoded. The same form holds every scope entry at
registration. Only the first query delimiter and following query are removed for
comparison; submitted arguments remain unchanged. Duplicate argument members,
invalid URLs and URLs outside the scope are refused. Each profile is decoded
independently, so absent optional members cannot inherit a prior profile's scope.
A changed installed adapter requires a fresh binding review.

#### Profiles Runner set aside

Since Runner v0.23.0 a registered profile that Runner's current form refuses no
longer stops Runner opening its store. Runner keeps it, sets it aside and never
binds it: a scoped page profile registered under round 15 whose `endpointScope`
entry round 16 refuses (an escaped slash, as in `group%2Fproject`) is one.
`GET /v1/input-profiles` lists it after the installed profiles with
`"state": "set-aside"` and Runner's `reason`, and a binding, job or run that
names it is refused as `422 input_profile_set_aside`.

Desk forwards the listing with the state and reason unchanged. An entry is
installed only where it has no `state` member; one with any state, even an
empty or null one, is never a candidate. Every picker that lists profiles (the
page and search profile pickers, the imported-template assignments and the Jobs
integration chooser) shows a set-aside profile, disabled, with "Set aside by
Runner. Runner's reason: *reason*" as its text, so the user sees why it can no
longer be chosen; a registration answer that carries a state is never bound.
The review table of a fixed or imported dialect shows the same note beside a
pin that names one. Where a saved or reviewed pin names one, by its id whatever
digest it names, the page scope shown where the profile is chosen and before
research is confirmed shows Runner's reason beside the stored scope, and the
review and its confirmation check refuse the pin by its name before any other
check. Runner's refusal of a binding reaches the user as Desk's message: "The
input profile *id* was set aside by Runner and cannot be bound. Runner's
reason: *reason*. Choose another profile for this source." On a job's surface
(the Jobs pages and the job bindings Desk sends) it adds ", or upgrade the job
to a release that does not name it". The reason is Runner's listed reason,
without a leading "the input profile *id*: " naming the same profile and
without Runner's code at its end. Desk reads the profile id from Runner's
refusal and the reason from Runner's listing; nothing else of Runner's text is
shown. A trigger's caller gets Runner's answer unchanged. The set-aside id stays
taken; a page profile registered for a corrected scope has a new id.

A request admitted by a page profile can still be redirected outside it. After
each page acquisition, before the page can reach the model, the worker reads the
signed receipt's `acquisition.endpoint` (the final URL after followed redirects,
its query removed). Scoped profiles use the same scope matcher; profiles without
a scope require byte-for-byte equality with their nonempty exact `endpoint`.
Exact HTTP profiles also admit only a submitted URL whose bytes before the first
query delimiter equal that endpoint. Query arguments remain unchanged; host case,
port spelling and path escapes are not normalized. Fixed-service MCP page
profiles keep their tool contract and use the same exact receipt comparison.
Missing endpoint/profile forms, malformed receipts and duplicate JSON members
fail closed.

On a receipt miss the attempt is `refused` with problem code
`acquisition-invalid`: its retained response and receipt are kept for audit and
count against the page allowance, but it is no retained source, so the page is
left out of every later model request and of the retained answer. The model
receives the refusal as a validation finding, within the existing correction
bound. A request with the same arguments again is refused without another
acquisition. Another allowed spelling or query is a different request and can be
acquired and refused again within the allowance. A refused attempt stays
`refused` across a restart and a resume. Desk reads the exact retained receipt
and verifies no signature. Runner's check 3 remains the verifier.

Before admitting any portable acquisition, Desk measures the actual encoded
journal, including completed and refused results, escaped metadata and any
in-flight reservations. It reserves the maximum bounded retained result (2 MiB),
64 KiB for terminal attempt metadata, and a separate final reserve for the 1 MiB
answer and 64 KiB of terminal run metadata. Host diagnostic text is bounded by
its encoded size, with a fixed fallback for oversized details; signed evidence
is unchanged. Admission and the started attempt are saved under the journal lock. When the 16 MiB journal cannot cover those reserves,
no request is sent: the run ends `partial` with a named journal storage-limit
reason. Every acquired attempt remains terminal and its signed evidence is kept
unchanged. This durable journal bound is separate from the answer-capacity check,
which counts only completed sources and stops before further model requests when
they cannot fit the portable answer. A refused page cannot evade the journal
bound by being absent from the answer.

A complete Gateway envelope can exceed the 2 MiB inline bound while still fitting
its 16 MiB + 64 KiB transport limit. Such acquisitions keep the exact envelope
and request bytes in the existing immutable research proof/artifact store. The
portable attempt carries a bounded `retainedProof` descriptor: version 1, the
frozen profile ID/digest, the exact proof manifest's SHA-256 and byte count, and
total proof bytes. Readers validate that descriptor and all objects against the
current run, attempt, session, Gateway pin and profile. Missing, altered or
mismatched evidence fails closed; reads do not repair, migrate or reacquire it.
Existing inline histories retain their representation.

An accepted oversized acquisition is terminal `refused` with the existing
`source-response-too-large` reason. An endpoint-refused oversized page keeps its
`acquisition-invalid` reason. Both end the run `partial` with an explicit inline
result-size reason, without another model request or source acquisition. Their
content is absent from every model projection and answer; the answer limit stays
1 MiB. Activity uses the existing refused-state and problem-code contract.

Before any portable dispatch, Desk additionally reserves maximum proof storage:
the transport envelope, up to 128 KiB of request arguments, and the already bounded
8 KiB manifest. The proof limit includes that manifest overhead separately from
both objects. The current encoded journal, retained acquisitions and in-flight
proof/journal reservations must also fit the run's 64 MiB retained-evidence allowance;
accounting is conservative when inline evidence is represented in both counts.
Admission never spends the same reservation twice. Storage failures are charged
at the maximum proof reservation until recovery rather than treating possibly
written objects as free space.

Proof objects are verified durable before the manifest is published; the journal
terminal update follows that publication. These are separate writes. An uncertain
artifact, manifest or journal acknowledgement keeps the existing uncertain/no
replay behavior and retains possible orphan objects for recovery. A process loss
does not invent a terminal proof reference or retry the acquisition. Proof copies
and complete project snapshots/moves include the manifests and project-relative
artifact objects. Chat-data backups keep their existing scope, which excludes
project research and artifact stores.

Runner continues to verify each receipt's exact endpoint or declared scope.
Neither check establishes the scope of intermediate redirects, page accuracy or outreach permission; the scope holds the
URL as written, not what a server serves for it. The portable definition,
retained job release and historical profiles remain distinct.

Same-vendor clean-room Codex review was requested by the user. Its findings on
metadata, Git execution/integrity and size limits were corrected. This is not a
cross-vendor review and does not claim the remaining workflow work is implemented.

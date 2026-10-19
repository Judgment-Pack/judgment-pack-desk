# Dialects: local v0.13 foundation

Dialects is the generalized source-preparation workspace included in the
[combined v0.13 scope](v013-release-scope.md). Its name and records are not
tied to prospect discovery. The current flow is
**Dialects → New dialect → Records / Recipe / Activity**.
It uses Desk's existing page shell, menus, dialogs, pagination and design tokens.
Creation uses **New dialect** and **Create dialect** consistently. Both creation
and import dialogs use the shared `DialogActions` footer, with Cancel before
the primary action and the group aligned to the right.

## Implemented slice

A preparation contains a name, preparation instructions, versioned recipe,
provided records, retained imports and local activity. CSV imports preserve
string values, including leading zeros, quoted fields and multiline cells.
URL imports store HTTP(S) research seeds without reading any website. They
remain “Needs research.” Neither type claims independently verified evidence,
qualified contacts or an evaluated decision.

The Records tab supports search, preparation status, pagination, keyboard
selection, select-all across matching pages, value review and JSON export.
Selection export includes only the selected records and their source metadata;
it excludes raw source files because they can contain unselected records.
Recipe changes retain a snapshot of the instructions and increment the version,
while earlier imports retain the version used at import. Exports include the
available recipe snapshots referenced by the selected rows. Unsaved recipe
edits use the existing navigation guard. Activity describes local preparation changes, not signed audit
provenance.

## Pack binding and input review

Recipe now accepts one saved pack snapshot and a mapping-v2 case mapping.
**Use this pack** captures the exact pack bytes and SHA-256. Choosing a different
pack or refreshing its snapshot requires confirmation before replacing its
mapping. Changing preparation instructions never rewrites the pack's rules.
The supplied-input mapping table uses shared Select controls for CSV column choices;
JSON pointer escaping is handled by Desk. Empty mappings keep inputs unknown.

**Review inputs** opens a selected-record dialog. It uses the released Runner's
`inputs/preview` endpoint; it does not create a release, job or decision. The
review shows original and prepared facts, type differences, absent inputs and evidence
availability, with input lineage under a disclosure. Imported text is not
silently coerced: `00123` remains a string. Type differences require review;
this preview is not a claim that the record is qualified or ready for execution.
An import alone never verifies evidence or discovers facts from a URL.

Recipe history and selection exports retain the pack snapshot and mapping used
by each saved recipe version. Preview case inputs include the record ID, import
source ID/digest/row/time, import recipe version, current recipe version and
pack digest. These are local supplied-data references, not signed acquisition
receipts. Generated data and connected-provider source mappings are refused at
this initial case-only boundary. Generated records use the separate reviewed-source handoff described below,
with trusted profiles and explicit admission.

The retained snapshot is user-selected; it is not the project's reviewed-set
lock or proof of passing tests. Actual evaluation must still perform release
review, saved-test policy and normal Runtime/Runner checks. Graph batch execution
is not exposed: Runner v0.8.0 advertises `single-pack`, and no graph job request
is defined by its released API.

## Storage and boundaries

Desk owns authenticated, project-scoped APIs under `/api/input-preparations`.
Private storage is `.desk-private/input-preparations`, owner-only and excluded
from ordinary project file access. JSON writes use the existing atomic private
file writer, a folder lock and a required `If-Match` digest for updates/imports.
Stale, concurrent edits cannot overwrite a newer saved revision.

Imports retain exact original content, SHA-256, source name, kind, timestamp
and recipe version. Source IDs bind kind and content; row IDs bind the source
and original row ordinal. Repeating the same import is idempotent, while equal
row labels do not merge distinct records. Record values are supplied assertions;
clients cannot post a forged evidence class or qualification result. These
local digests establish identity, not an independently witnessed audit trail.

Limits: 100 preparations per project, 1,000 records per preparation, 100 CSV
columns, 2 MiB request body, 8 MiB saved preparation, and a 1.5 MB file picker
limit. Credentials in URL seeds are refused. CSV values are not type-coerced.
The private preparation directory is not included in current chat backups.
Preserve it alongside the project when moving this local development data.

## Validation

- Backend tests: exact source retention, stable IDs, idempotent import, stale
  recipe edits, recipe history, private file permissions, limits, ambiguous
  JSON/CSV rejection, URL normalization and atomic refusal of invalid imports.
- Guard tests: unauthenticated and cross-origin writes, concurrent edits,
  project isolation and symlink refusal.
- Frontend tests ensure selection exports cannot leak unselected source rows,
  background refreshes preserve unsaved instructions, and stale saves retain
  their original digest and visible edit buffer.
- Isolated real-browser acceptance: create, CSV import, 60-record pagination,
  keyboard/select-all toggling, one-record export, record review and focus
  restoration, recipe changes, URL seeds, narrow layout and Desk restart.
- Browser geometry checks confirm creation actions stay on one row, have
  spacing, and align to the right form edge at desktop and 390px widths.
  The import dialog uses the same shared action row.
- Released Runner browser acceptance: saved pack selection, shared column
  mapping, recipe history, reload persistence, selected-record preview with
  origin references, unknown missing values, narrow layout and focus restoration.
  The request trace contains no release, job or decision creation.
- Qualification boundary tests reject mismatched pack digests, undeclared or
  duplicated targets, malformed pointers, generated sources, stale edits and
  malformed saved pack shapes.
- Import, chat proposal and recipe-save tests make no external website reads or AI calls. Research requires a separate run confirmation or the bounded composer allowance below.

## Research and evaluation work (#369 / #371)

The local development build now contains a server-owned research worker using
existing Desk AI connections and Gateway search/page sources. Each run freezes its
recipe, selected company records, limits and connection references. Attempts are
recorded before calls and partial results survive interruptions. Unknown outcomes
are not replayed; explicit rate limits and opted-in search timeouts may use the
saved retry policy described below. Search and page acquisitions retain Gateway proofs
that Desk checks with the installed verifier. These checks establish acquisition
integrity, not the truth of a page or generated claim.

The chat-to-Inputs flow is implemented in the shared main/right-pane chat. Its host
proposal tools read recipe context and retain a proposal; they cannot save Inputs
or start research. The separate research request tool uses the bounded composer allowance below. **Review recipe** opens the shared recipe controls with no AI connection
preselected. **Save to Inputs** creates an empty input with a chat/draft origin.
A repeated identical save returns the same input and preserves later edits. A
changed request using that proposal key is refused. **Start research** remains a
separate confirmation in Inputs. Older proposals remain under **Input recipes**
in the chat menu. No Runtime change is needed for designing or saving recipes.

Targets are Companies, People, and People at selected companies. For the last,
a person selects existing company records from this or another input. References
bind the saved input revision; changed or missing company records require a fresh
selection. Research copies the reviewed records into its own retained snapshot.

This is still development, not completion of #369/#371. Remaining work includes
explicit resumption of interrupted research, complete usage accounting, graph jobs, translations, and the
reviewed live pilot. No live-provider reliability claim follows from fixture tests.

Pack snapshot binding, server-side reviewed conversions and durable selected-record
evaluation are implemented. Graph input mapping/execution remains dependent on a
confirmed Runner contract. See [the local testing guide](inputs-local-testing.md).
Search criteria and evaluation rules remain separate; AI suggestions cannot
silently rewrite qualification logic. Generated fields must retain their
origin and admission policy through evaluation. Copying them into an ordinary
asserted case input would lose that distinction and is not an acceptable
implementation. Runner's current operational job flow is pack-based; graph
batch execution requires a confirmed contract before exposing that action.

Use the already approved UI direction for provider/connection controls and
mapping review. Ask for design feedback before introducing a different user
flow. The local testing guide includes chat recipe design and an opt-in research walkthrough.
Generated-record evaluation and graph execution remain separate acceptance gates.

The local build is available at `http://localhost:5173/dialects`. Dialect
research, preparation and evaluation share the v0.13 release scope with the
shared AI improvements and portable Dialect implementation. Codex and Claude
work in separate implementation lanes and integrate into the same release;
the [combined checklist](v013-release-scope.md) records the remaining gates.
Local implementation and testing do not establish a published release.

## Local walkthrough for the implemented Inputs flow

1. Open `http://localhost:5173/inputs`, choose **New input**, and create an input
   with a name and preparation instructions. Cancel and Create input share the
   right-aligned dialog footer.
2. Choose **Prepare inputs** and import a CSV. Confirm names, values and counts
   in Records. Select one record and export it; only the selected row and its
   source references should be included.
3. Open **Recipe**, choose a saved pack in **Qualification**, then **Use this
   pack**. Map columns in the compact input table. Text is the default; Number
   and Yes / No are explicit choices. Preview conversions before saving.
4. Save changes, refresh, then select records and open **Review inputs**. Inspect
   original/prepared values, unknown inputs, evidence availability and source details.
5. **Review evaluation** creates a release rehearsal. Only explicit confirmation
   starts a durable batch. Saved-test policy and stale-review checks still apply.
6. Follow the [complete acceptance guide](inputs-local-testing.md) for fixture data,
   expected outcomes, partial failures, restart recovery and verification checks.

For the safeguards placement, a project with an available setup offer shows
**Project safeguards off** at the bottom left. Open it with mouse or keyboard,
then dismiss it and refresh. The Library filters must not move, dismissal must
persist for that project, and setup must remain in Admin → Decision safeguards.

## Durable selected-record evaluation

Conversions live in the versioned Desk recipe, outside mapping-v2. Desk prepares
supplied case inputs on the server and retains original values, conversion policy,
source identity and recipe version in each run's source case. Per-target prepared
values allow the same CSV column to serve different explicit mappings. Empty cells
are omitted from mapped values and remain unknown.

Evaluation review reads the actual saved tests and exact current pack, then uses
Runner's existing release preview. Confirmation rechecks the saved input digest,
project files/lock, test bytes and installation policy. It first commits a private
batch intent. A Desk worker creates one fixed job and submits each selected record
with a stable key; Runner's release-specific job identity and run idempotency
reconcile accepted requests whose responses were lost. Reloads do not own this work.
A stopped execution remains interrupted. Re-evaluation is an explicit new batch.

The table separates Preparation / Evaluation / Decision, with signing status kept
separate. Earlier recipe results remain linked to Jobs. Activity is a local index,
not a complete signed authoring journal. The batch retains compact decision summaries;
full traces and verification records remain in Runner. Batch records are bounded at
32 MiB, and individual run submissions retain Runner's 2 MiB request limit.

### Local research review

The approved record dialog now saves an explicit human decision: Ready for evaluation,
Needs more information, or Exclude. Each decision retains an
immutable record snapshot, digest, recipe version, time and authenticated session
identity (or explicitly local access). The preparation revision check prevents
concurrent or stale edits. Source changes clear current acceptance; recipe changes
require review again. Prior snapshots stay in local review history and selected
exports. This history is local preparation activity, not a signed audit trail.

The shared dialog, fields, footer and table remain the presentation primitives.
The layout follows [Linear's restrained hierarchy](https://linear.app/now/behind-the-latest-design-refresh):
record content is primary, evidence and older reviews use progressive disclosure,
and review actions have one predictable footer. Generated-record evaluation uses the reviewed research handoff described below;
human readiness does not relabel AI output as supplied data.

## Inputs assistant

The Inputs list and each saved input use the shared Assistant rail and chat pane.
An input-scoped conversation retains its own history across Records, Recipe and
Activity; its identity, saved recipe version and current record selection are
included in the next message. Main chat can reopen the same conversation. Existing
history, message navigation, model controls, pane resize and narrow-screen takeover
are reused. Opening the pane creates an unsubmitted draft, not a provider request.

The conversation worker offers `list_inputs`, `read_input_recipe`,
`read_input_records`, `read_input_activity`, `propose_input_recipe` and
`propose_input_action` and `request_input_research`. New recipes use the existing Save to Inputs review.
Existing recipe proposals contain portable changes plus the exact saved revision;
connection credentials, records, human review decisions and qualification mappings
cannot be supplied by a model patch. Arrays replace the complete field/criterion
list, and the review names added, changed and removed fields. The editor preserves
existing connection choices. Removing setup preserves records and recipe history.

Proposed actions create review cards only. Research requests use the existing
connection/limits review and Start research confirmation unless this chat has an
unused reviewed allowance for that exact saved input (see below). Stopping identifies a particular
active run. Evaluation uses the existing selected-input review and confirmation,
and requires current human readiness for every selected generated record. A shared editor lock covers action readers
as well as the chat pane. Revision changes disable saving while keeping local review
edits visible; writes also use the server's existing If-Match contract. The Inputs status row owns research progress, steps, recovery and Stop. Its
docked chat omits that duplicate block; standalone or covering chat shows a
compact status link and Stop while active. Stopping the chat response itself does
not stop an independent research run.

## Manual evaluation of reviewed research

The first local path evaluates packs, with a separate confirmation after input
review. Every selected generated record must have an accepted human review bound
to its exact record digest and current recipe version, present in retained review
history. Needs-information, excluded, missing or stale reviews refuse the whole
selection. Mixed supplied/researched selections require separate batches; nothing
is silently omitted. Recipe saves, input reviews and chat proposals never start
operational evaluation. Graph execution and automatic/AI review remain deferred.

Desk registers an installation-owned `desk-reviewed-research-v1` Runner profile
with class `generated`, the stable local Gateway public key and a hash of the Desk
executable. The same executable exposes only `read_reviewed_input` as a local,
read-only Gateway source. A random, short-lived grant addresses an exact snapshot
in private installation storage. No arbitrary path, source profile or result can
be supplied by the browser. Grants are removed after acquisition; the signed
response is frozen in the evaluation batch. A crash can leave an expired grant,
which the adapter refuses. Opening input review performs local acquisition only,
with no model calls or external website reads.

The snapshot retains original values, field-level citations and interpretation
labels, typed mapped values, saved qualification and research recipes, model
response digests, source proof digests, and human review history. Desk rechecks
that the record matches its retained model action and original published values.
Cited source receipts are verified again and their result is checked against saved
source text. Missing, inaccessible, conflicting or stale fields stay unknown.
Runner verifies the handoff receipt against the installation profile; only source
copies with explicit generated admission can produce pack facts or evidence.
Generated values never enter through mapping case facts. The case carries only
typed snapshot/grant references, so record-specific values do not change the
released mapping. Retained receipts expire after 24 hours; delayed submission is
refused rather than silently reacquired. A new review is required to try again.

The Gateway receipt authenticates Desk's retained snapshot, not provider authorship,
factual accuracy, or completeness. The exported run includes that snapshot and its
receipt. Original page proofs and model response bodies remain in the local
research journal and are referenced by digest, not bundled in full into the run
export. Runner v0.8.0/Runtime v0.30.0 retain generated lineage and citations; this
change does not claim the newer signed whole-preparation binding from Runner
v0.9.0/Runtime v0.31.0. No dependency pins are changed by this local feature.


### Research retry policy

Each saved research recipe may opt into bounded retries for source acquisition.
Absent policy remains Off for existing recipes. New research setups and new chat
recipe proposals suggest Temporary failures with two retries; timed-out searches
remain Off. Editing other fields does not adopt a retry policy. Chat updates
preserve this user-owned setting and cannot enable timeout retries themselves.

Recipe → Research → Limits and retries uses the shared Field, Select and Input
controls. The start-research review shows the saved policy. Every actual attempt
consumes the saved search/page request limit, including unsuccessful attempts.
Maximum retries is one to three per operation; waits increase from 5–6 seconds
to 10–12 and 20–24 seconds. The run-wide 30-minute deadline also bounds waiting.

For the pinned Gateway v0.11.0, only an explicit `rate-limited` acquisition
refusal is automatically retryable. `search-timeout` additionally requires the
saved timeout opt-in because usage may already have occurred. Generic
`provider-unavailable` includes invalid responses/requests and is **not** safely
classified as temporary; `web-unavailable` is similarly ambiguous. Auth, policy,
verification, sealing, local-storage, response-loss and AI-model failures are not
retried. No gateway permission, connection timeout or user recipe is changed by
this policy. Search timeout remains in Admin → Connections → Web search.

The worker retries only the exact failed source arguments. Attempts have unique
IDs, request digest, ordinal and previous/next attempt links. The next attempt is
committed before its provider call, and each source proof has its own filename.
Activity retains failures, scheduled waits, actual retries and exhaustion causes.
A saved pending retry drives the compact progress countdown on every Inputs tab;
the countdown is not repeatedly announced. Stop remains available during waits.
Restart recovery clears pending retries and interrupts the run, preserving prior
records and successful operations. It never resumes an uncertain request.
Changing settings requires saving and explicitly reviewing/starting a new run.

### Research source size limits

Source acquisition and verification accept the local Gateway's 16 MiB output
limit plus a 64 KiB receipt envelope. Saved source proofs allow another 128 KiB
for the request, pinned key and seal. The per-run retained-byte budget is 64 MiB,
with 512 KiB reserved before writing a source proof for operation metadata.
Private proof JSON avoids HTML escaping so markup does not inflate storage.
Every receipt and seal is still verified against the reviewed Gateway; proof
bytes are never truncated to fit. Model responses remain limited to 2 MiB,
each page contributes at most 48,000 bytes of research text, and the prompt's
retained source text is separately bounded. Larger acquisition limits therefore
do not enlarge AI context. Oversize responses and incomplete reads have distinct
errors and are not automatically retried. Interrupted runs are not replayed.

Research activity places elapsed time beside the operation name. Status occupies
its own right-aligned column; narrow screens allow the name and elapsed time to
wrap without moving the status edge.

### Recovery from individual source failures

During a new research run, a durable source failure can become an explicit gap:
page/output size limits, unsupported page formats, page processing failures,
redirect loops, searches without usable grounded sources, and search timeouts.
Timeouts use the saved retry policy first; continuing can choose another query,
but cannot replay the failed query once its retries are finished or disabled.
Ambiguous `web-unavailable` / `provider-unavailable` errors still stop the run,
as the current Gateway can combine authentication and request errors into them.
Authentication, access policies, changed connections, verification/sealing,
storage errors and exhausted provider limits also remain terminal.

A failed step keeps its original uncertain outcome and diagnostic. The worker
saves `continued: true` only after the attempt and retry disposition are durable,
then includes its ID, code and requested URL/query in `unavailableSources` in the
next AI context. Failed responses are never evidence. The AI may use previously
verified sources or another already permitted URL within the remaining budgets.
Repeated requests for the same failed operation return its saved gap without a
provider request. Repeated model choices still consume the model request limit.
Stop, cancellation and lost journals prevent continuation.

The AI is instructed to save small record batches early and keep unresolved
fields explicit. The host publishes these batches immediately. Reaching the
record count does not stop enrichment while fields remain unresolved. The last
model request must use existing evidence or finish; Desk will not acquire a new
source with no model budget left to process it. Finished runs with source gaps
or unresolved fields use `completed-with-gaps`; request exhaustion remains
`partial`. Neither state constitutes qualification or verification of claims.

The shared progress row and activity dialog show unavailable-source counts.
Steps use a specific label such as **Too large**, retain their times and links,
and explain that research continued. The final status is **Research finished
with gaps**, with guidance to review missing fields and source coverage. This
uses the existing compact layout and gold attention token. Historical interrupted
runs are unchanged; continuation requires a new reviewed run, as described below.

### Review selection scope

Records selection has two explicit review actions. **Review research** opens the
individual evidence viewer; **Save this review** applies only to its current
record. **Set review decision…** captures all selected AI records in a compact
shared dialog, lists their names, and requires a new decision. Its count-labelled
save applies the same decision and optional note to exactly that selection.

The existing record-reviews endpoint accepts either `recordId` or `recordIds`.
A batch validates every member before changing any review, then persists once
under the preparation lock and revision check. Missing, duplicate, supplied or
stale selections are refused as a whole. Each changed record retains its own
reviewer, recipe version and research snapshot; unchanged reviews add no duplicate
history. Research origin and evidence are preserved. Saving does not evaluate.


### Durable research saving and explicit continuation

After each usable page the host enters a save checkpoint. The next model action
must propose records or give a reason that the page cannot support a candidate.
Successful records publish immediately; optional unknown fields remain missing.
A requested exhausted source budget, or the last two model requests, switches
the worker to finalization using retained evidence. Source requests are refused
in this phase; the final AI allowance cannot be spent on further discovery.

Invalid actions, record types and quotations receive at most two correction
requests, within the saved model budget. Record batches remain atomic. Corrections
never relax evidence validation, repeat uncertain requests, or silently drop a
bad record. Repeated source actions and unchanged record submissions receive the
same bounded feedback. Finished runs below the record ceiling have an explicit
shortfall message and `completed-with-gaps`, including zero-result runs.

Verified empty-text pages and recognized loading/error shells become source gaps.
Their proof bytes count toward storage even when extraction fails. Search/page
transport uncertainty, authorization, policy, verification and storage failures
remain terminal. Model context uses the Codex bridge's shared byte bound; URL
references are not duplicated, source text is shortened only in a prompt copy,
and full retained source bytes are preserved.

**Reprocess from saved sources…** is an action in the selected-record menu.
It creates an update-only review with frozen row IDs, record digests and the
current saved recipe. Only associated evidence from those rows' original runs is
copied and reverified; selections may span multiple runs. Missing evidence or an
ineligible row blocks admission without silently skipping rows. Colliding source IDs
from different runs receive distinct local references, preserving both proofs. Searches and page reads
are fixed at zero; the model request allowance is reviewed before confirmation.
The old whole-run continuation entry is removed from page and chat progress;
historical continuation journals remain readable. Reprocessing adds no companies.
Start a new reviewed research run for discovery. Unsaved recipe edits block review.

The server serializes admissions and refuses a second queued/running operation
for the same input. Running work takes display priority over queued work. New
runs retain stable row identities: exact person URLs, and company names plus
host for company pages. Existing row IDs are captured in the review; domain alone
never merges different companies. Old runs cannot overwrite newer row snapshots,
and changed research requires renewed input review. Existing duplicate rows are
not deleted or bulk-merged. Reviewed `partial` and `completed-with-gaps` runs use
the same strict model-response and source-proof evaluation handoff.


The Records setting is a ceiling independent of source and AI request budgets.
Changing it never raises provider allowances automatically. AI requests include
source planning, processing and saves; the final two are reserved for saving.
The Recipe limits grid aligns fields at their top edges and collapses to one
column in narrow forms. The model-request helper explains this shared budget.
When finalization stops discovery, the result states the host's reason before
any AI explanation. Source-only continuation explicitly directs additional
source discovery to a new reviewed research run. Discovery instructions favor
additional candidates before optional enrichment, within the saved criteria.


### Research budget approvals

Saved Recipe limits are the initial authorization for a reviewed run. When a
budget closes discovery below the record goal, Desk finalizes usable records and
may pause with a bounded recommendation for additional search, page or model
requests. This is a host recommendation, not an AI grant. Provider interruptions,
authentication/policy refusals and rejected evidence do not authorize increases.
Selected-record reprocessing cannot acquire additional source allowance.

The Inputs progress row offers **Review increase** and **Keep results**. Docked
chat does not repeat these controls; covering chat links back to the Inputs page. Review shows used, current and proposed request counts.
**Approve and continue** applies only to that run; it keeps the original recipe,
record goal, connections, retry policy, records and operation history. It neither
edits Recipe defaults nor promises more records. Cancel leaves the run paused.
Keep results records a decline and finishes without asking for the same increase
again. Recipe edits block approval, but never block keeping results.

The server persists each exact request ID and decision, reviewer, timestamps and
allowances. Approval requires the reviewed preparation revision, unchanged recipe
and selected company snapshots, current connections/Gateway and valid retained
source proofs. The decision and queued state are atomic. Another window cannot
replay the decision, and older research cannot resume over newer confirmed work.
The worker uses approved limits for every request and retry; pending requests add
no allowance. Pauses survive reload/restart. Existing absolute request and storage
ceilings still apply. Activity and evaluation provenance retain the approval
history; research results still require the separate input review and evaluation.

Chat/Research controls work type. It does not grant workflow execution authority.
The research preference is now labeled **Search when needed** (stored value
`auto` is unchanged). Starting a reviewed workflow via **Start research** approves
that one run in either composer approval mode; there is no second approval for
each call inside its allowance.

### Composer workflow approvals

The shared main and pane composer defaults to **Ask me**. **Auto within limits…**
opens the same research preview for a saved input. It shows the input and recipe
version, instructions/fields, connections, model, limits and retry policy. The
person chooses a 15-minute or one-hour start window and explicitly selects
**Allow one run**. Selecting Auto or creating this allowance does not start work.

The server stores the allowance on the exact frozen review journal. It is bound
to the chat, preparation revision, approving browser session, expiry and one
research run. The session binding is a domain-separated MAC, never a stored
bearer credential. Reload retains it; sign-out, a different session or server
restart cannot reuse it. After consumption, expiry or revocation the composer
returns to Ask me. Expiry limits when a run can start, not how long an admitted
run may execute. Switching to Ask me revokes unused permission; stopping already
admitted work remains the run's separate Stop action.

`request_input_research` passes only a saved input/revision; its host supplies the
chat and current user-turn identity. Without an applicable allowance it retains
a Review research card. With one, the server rechecks the recipe, selected company
snapshots, connections, Gateway and newer work, then atomically consumes the
allowance and queues that same run. Repeated delivery in the same turn returns
the original run; another turn cannot reuse it. Unsaved Recipe edits block the
request in the shared host. Failed or ambiguous responses never create an
unreviewed fallback run. Activity and evaluation provenance retain approval
identity, time and initial limits.

Auto initially covers only starting that research run. Recipe saves/removal,
input readiness reviews, evaluation and every additional budget allowance keep
their existing explicit reviews. The model, Chat/Research selector, interview
answers and stored chat preferences cannot create or broaden permission.


### Candidate scheduling and parallel page reads

Research runs keep a durable queue of AI-selected leads. The `candidates`
action accepts at most 20 names and retained URL references at a time, with 100
entries per run. These are unverified leads; queue status is not input review or
qualification. URL normalization and existing-attempt checks prevent re-reading
one URL under a different name. Saved record identity checks remain responsible
for entity deduplication; separate URLs may provide evidence about one company.

The host prioritizes the queue, reads at most three pages per batch (also bounded
by remaining records, page requests and retained bytes), and yields for a record
save before the next batch. The existing Gateway semaphore still permits at most
two HTTP operations in flight. Page acquisition, proof sealing and verification
have separate sessions; no model subagents are created. Search and model calls
remain serial, retaining the Codex account's admission rules.

Request admission reserves the maximum proof and metadata bytes under the run
journal lock. Completion and refusal handling address an attempt by ID, so
out-of-order results cannot overwrite a sibling's outcome. Successful source
metadata is retained as it arrives. Every batch is joined before finalizing a
run. Stop cancels outstanding reads; restart preserves completed evidence and
marks interrupted attempts uncertain without replay. Historical runs remain
unchanged until the user explicitly starts new work. Selected-record reprocessing
cannot discover or read new sources.

An explicit `search-invalid-response` acquisition refusal can be left out after
the retry disposition is saved. It is never automatically retried or admitted as
evidence. This includes a timeout retry that then returns an unusable response.
Policy, credentials, unknown transport, sealing, proof verification and storage
failures remain terminal. Search/page/model totals and retry policy stay within
the approved limits; this scheduling change grants no additional usage.

The existing progress row reports concurrent page reads and keeps an earlier
active attempt visible if a later one finishes first. The Activity dialog retains
every real attempt; no additional cards or duplicate progress surfaces are added.


## Research record contract 2

New research uses Desk-owned URL references and per-record save results. Search
results retain actual URLs rather than reconstructing addresses from their titles.
Gateway page reads retain both requested and final URLs under existing policy.
`url` is the evidence link; optional `website` is separate and resolves only from a
retained page reference. Missing websites are shown as **Not confirmed**. Field
quotations, source acquisition verification and typed values retain their existing
validation; this does not establish factual accuracy or qualify a record.

The durable prompt includes existing record identities, the exact selection for
reprocessing, per-record outcomes and approved limits. Valid members of a model
batch are saved while invalid members receive bounded correction feedback. A
`correctionOf` key links a corrected reference to its original failed result;
**Recovered** appears only after the corrected record has been published. Failed
record proposals show **Needs correction**, and known source failures use specific
labels rather than **Outcome uncertain**. Attempts and original failures remain.

Publication uses stable row IDs, rechecks recipe and record versions, and reconciles
repeated delivery or a crash between row publication and run acknowledgement.
Unchanged records keep their original provenance and applicable review; changed
research requires review again and keeps historical review snapshots. New discovery
matches normalized company names (including common legal suffixes) with corroborating
URL hosts; names or shared domains alone do not merge records. Ambiguous matches
require an explicit record ID. Existing duplicates are not silently merged.

Progress counts distinguish added, updated, unchanged, needs correction and awaiting
save. Retained-but-unpublished output is never counted as an Inputs update. Local
publication recovery does not repeat model/source operations. Selection reprocessing
cannot receive an automatic run allowance or acquire a source budget increase. Chat
can propose the same selected action through the shared review component; manual
confirmation freezes its exact IDs and limits. Original contracts/instructions remain
available for older paused runs; existing records are not migrated on installation.

Verification: `TestResearchV2*` covers mixed batches, source refs, proof copying across
runs, scope enforcement, stale writes, review preservation, correction linkage and
idempotent publication. `web/scripts/research-reprocess-browser.mjs` uses the disposable
`TestInputRecordReviewBrowser` fixture; provider operations are intercepted and no
user workspace or paid research is used.

### Delegated research execution

Recipe → Research → Execution offers Standard and Delegated. Existing recipes
remain Standard. Delegated adds Concurrent AI tasks (Auto, 1, or 2); unavailable
options are disabled and the current connection ceiling is explained below.
Start review shows the saved approach and effective concurrency before approval.
These settings do not alter Chat/Research source policy or Ask me/Auto approval.

Independent discovery tasks return candidates; claimed candidates are processed
by bounded subagents. The shared durable coordinator and connection admission
live outside the Inputs workflow adapter. All requests consume existing saved
limits, with no per-child reset. Scoping, exact evidence references, validation,
deduplication, record revision checks, and incremental publication remain Desk
responsibilities. Selected reprocessing delegates only explicitly selected rows
and their associated saved evidence. It cannot discover or acquire sources.

Research activity groups child operations under one compact task row. Each row
shows its actual state and duration; expansion exposes its objective, request
counts, result or failure, and existing operation details. Task summaries are
working material, not evidence. Running replaces the chevron with the shared
spinner. Completed sibling results are retained when a task fails or the run is
stopped. No task cards or duplicate warnings are added above the chat composer.

Validation includes overlapping model completions, isolated source scope,
partial sibling failure, shared waiting-request reservations, independent retry
schedules, selected-only reprocessing, cancellation, execution snapshots,
connection-wide capacity, and legacy Standard behavior. Provider tests use
fixtures; performance or factual-quality improvements require separate live
evaluation under equal budgets and are not claimed by these checks.

## Inputs snapshots in tests and incremental Jobs

Desk issue #391 adds two entries to the same preparation path: selection →
**Add to pack tests**, and pack Tests → **Add case → From Inputs**. A saved
qualification mapping must match the current pack bytes. Preparation reuses
recipe conversion, the reviewed research handoff and Runner projection. Runtime
checks fact comparability before review. Cases freeze facts, availability,
source lineage, recipe/mapping and pack identity; expected outcomes remain unset
until authored independently. Duplicate snapshot identities are skipped, never
used to overwrite reviewed expectations. Later record changes do not mutate a
saved fixture. Source history is available in the case's Sources section.

The explicit `decimal` conversion retains a plain decimal string without binary
rounding. Existing `number` conversions remain JSON numbers. Neither conversion
extracts numeric bounds or business-policy classifications from narrative text.
Blank values remain omitted; false, zero and null do not become truthy defaults.

Desk issue #392 adds **Jobs → Connect Inputs** and an incremental option in the
selected-record evaluation review. Review the sample projection and fixed
release, choose new records only or new and changed records, explicitly choose
whether to include existing eligible records, then **Save paused**. The separate
**Review and enable** action records the baseline. AI research requires a current
accepted review and is processed separately from supplied assertions. Resume
preserves the baseline. A source-class mismatch is not silently converted.

Desk persists immutable input envelopes before first delivery through Runner's
owner-authenticated record-delivery contract (Runner #70). Identities bind the
collection, record digest, recipe and release. Identical retries reconcile the
same occurrence after a lost response or restart. Transient transport failures
retry with bounded backoff; permanent failures require review. A pending record
whose review or content changes is reconciled by identity before it may be
marked superseded. Accepted work may finish after pause. No browser needs to
remain open, but Desk and Runner must remain running.

The approved release keeps its original tests and rules. Later test authoring
alone does not alter it. Recipe, mapping, live-pack or project-policy changes
require a newly reviewed subscription. Results and acquisition proofs remain in
Runner; Desk keeps a bounded index of at most 1,000 admitted/superseded revisions
per subscription and exposes the newest revision's result. New subscriptions
require an explicit existing-data choice again. This is collection-to-decision
operation processing, not recurring AI research and not chat's Auto setting.

# Dialects in Desk

The maintainer chose **Dialect** for the translation, the authoring workspace,
and its navigation. The workspace lives at `/dialects` and `/dialects/:id`.
There is no `/inputs` redirect. Existing collections, saved recipes, records,
chat associations and Job releases retain their IDs and stored bytes.

This follows [spec issue 142](https://github.com/Judgment-Pack/judgment-pack-spec/issues/142).
A dialect declares how a system record becomes a pack's facts and evidence
availability; it does not establish that the source is truthful. Desk's workspace
also holds the recipe and prepared records used to author and review that dialect.

The companion format in spec #141 is still proposed. This change does not create
an Exchange, an output dialect format, a new Core interpretation or a new wire
version. Runner's `mapping`, `inputMapping`, `mappingDigest` and mapping-v2 payloads
remain unchanged. The existing `/api/input-preparations` API and private on-disk
storage retain their compatibility names; UI routes and internal authoring types
use Dialect. Runtime owns evaluation, Runner owns recomputation and delivery,
Gateway owns acquisition and source receipts, and Desk owns this authoring flow.

## Chat authoring

`read_dialect` reads one collection and a declared saved pack, with bounded
source samples, available/planned columns, current bindings and exact revisions.
`propose_dialect` requires that read, validates complete replacement bindings,
previews conversions through Desk, rechecks the collection and pack, then retains
an `update-dialect` proposal in the existing session. It does not save a recipe,
approve research records, create test cases, execute decisions or enable Jobs.

Review dialect uses the same field editor and conversion preview as Recipe.
The person can edit the proposal, preview conversions, and choose Save dialect.
Saving preserves the recipe's other settings and records, rechecks pack identity,
and uses the collection revision for optimistic concurrency. An edit invalidates
its previous preview. Errors leave the proposal available; successful saves mark
that session action complete. Both the main chat and page side pane use this flow.

Bindings use existing or declared research columns in both manual and chat
review. Before rows exist, the name, URL and declared fields are available, as
are company ID/name/URL for people at selected companies. These are choices,
not sample values; missing targets remain unknown. Decimal is the explicit conversion for JPS ordered comparisons; JSON
number is a separate option. Boolean conversion uses exact reviewed values.
Evidence availability accepts present/absent/unknown rather than treating the
existence of a citation as proof. Narrative fields require separately reviewed
structured preparation; the dialect never infers values from prose.

The saved dialect is reused by record-to-test snapshots, manual evaluation, and
reviewed incremental Jobs. Their existing review and execution controls remain.

## Generic authoring templates

New dialect starts from a neutral template chooser: blank, general research,
company discovery, people at selected companies, work-item triage, or table rows.
A blank, work-item, or table preset does not require an AI connection. Research
supports the generic `records` target as well as the existing company/person
specializations. Generic records use `kind: record`; separate URLs on the same
host remain separate identities. Company-specific identity rules are unchanged.

Templates are **Desk authoring presets**, not a new normative Dialect format.
They contain `templateVersion: 1`, id, name, description, license, intent, and an
optional portable research definition (target, fields, criteria, limits). The
importer validates the entire closed shape and a 64 KB size limit. Credentials,
connections, existing records, pack authority, approvals, and code are refused.
Search is initially disabled until a connection is chosen. Creating a Dialect
only saves the reviewed recipe; it cannot start research or enable a Job.

The recipe retains a template origin (id, display name, license, and SHA-256 over
`JSON.stringify` of the validated descriptor). This identifies the source preset,
not subsequent local recipe edits, a publisher signature, a normative canonical
form, or verified business policy. Ordinary recipe and mapping edits preserve it.
Examples suitable for **Import a template** are in `docs/examples/dialect-templates`.

### Judgment Exchange boundary

As checked on 2026-10-14, `Judgment-Pack/judgment-pack-exchange` is not available.
Spec #142 proposes it after #141, Runner #75, Gateway #258, and live seed checks.
The Desk importer therefore does not claim to consume an approved Exchange
manifest or publish official seed entries.

Once the companion/manifest is accepted, an Exchange adapter should:

1. Read the published manifest through a reviewed catalog connection; retain the
   repository revision and verify each downloaded entry's exact byte digest.
2. Show the author's license, supported pack id/digests, connection/input-profile
   digests, fixture results, and verifier versions before use.
3. Validate the entry in its normative format and run its fixtures through Runner.
   Passing vectors establish recomputation, not truth or suitability of a source.
4. Import its declarative mapping as a **proposal** against a matching local pack.
   Incompatible pack/profile versions must block application. Bind local
   connections explicitly; never import authentication or execution permission.
5. Keep Desk research presets distinct from Exchange mappings. A workflow may
   reference both, but research instructions never become Core decision rules.
6. Make updates explicit and revision checked. Never silently replace an enabled
   Job's fixed release, dialect, source profile, or output policy.

This adapter can normalize optional authoring presets into the chooser added
here. It must not reinterpret Desk's provisional template JSON as the companion
format or use a successful template import as a verification claim.

## Companies to people to decisions

1. **New dialect → Find companies**. Edit the scope and fields. Choose the AI and
   search connections; review Limits and retries. Counts live in structured limits,
   rather than prose. Save, review, and start research as separate actions.
2. Review source quotations and unresolved values. Select the desired companies
   and choose **Find people at selected companies…**. This opens a new people
   Dialect with precisely that selection and the source revision pinned. It does
   not copy all companies or start research. A changed source requires a new review.
3. Choose the professional role criteria and start the reviewed people research.
   New person records include host-owned `company_id`, `company_name`, and
   `company_url` columns as well as the retained internal relationship. Existing
   published rows preserve their previous encoding for review verification.
4. Design the company/contact decision pack in the existing chat. Read the pack
   rules independently of observed results; use Decimal for ordered numeric
   comparisons. Missing evidence must not silently become a negative decision.
5. Connect each Dialect to the relevant saved pack in Recipe or propose a mapping
   through chat. Review conversion previews, then save the mapping. Generated
   records need an accepted review for the current recipe before evaluation.
6. **Add to pack tests** freezes selected facts, evidence and source lineage.
   Expected outcomes remain unset: choose them from policy before treating these
   as assertions. Add synthetic normal, exception, missing-value and boundary
   cases as well. Run the saved suite against the exact pack version.
7. **Review records → Review evaluation** creates a release preview. Required
   tests must pass before confirmation. A one-off evaluation creates a Job with
   one run per selected record, retaining generated origin and source proof.
8. **Jobs → Connect Dialects** reviews an eligible sample and saves an incremental
   subscription paused. Choose new-only or new-and-changed records and whether
   to include the existing eligible records. **Review and enable** is separate.
   New generated records still need a current accepted review. Restart recovery
   and delivery dedup prevent the same accepted event from becoming another run.
9. Review the Job decision and export the selected records for outreach planning.
   Research and qualification do not send messages. Missing professional contact
   details stay missing; they are not guessed from names or email patterns.

## End-to-end verification

Selected-record reprocessing completes when every selected row has a durable
updated or unchanged outcome. Missing, conflicting, inaccessible or stale
evidence remains visible as gaps; it does not require a final model request.
Unprocessed rows, rejected proposals and pending publication are not completion.

`TestDialectCompanyPeopleTestedJobJourney` exercises actual host research
scheduling, retained signed page proofs, publication/dedup, company selection,
record conversion, accepted review, a tested release, real Runner execution and
verification export. Only the AI responses and public pages are controlled
fixtures. Set `JPS_DIALECT_JOB_BROWSER` to the browser script to also exercise
snapshot creation, case-page reload, Jobs connection, save-paused, enable, the
completed decision and pause through the UI.

`TestDialectTemplatesBrowser` covers the generic default, non-GTM templates,
recipe-origin persistence, generic research and selected-company handoff.
Existing suites cover retry/recovery budgets, multiple-candidate discovery,
parallel acquisition, decimal conversion, proposal persistence and durable
incremental delivery after a lost acknowledgement.

`TestDialectLiveProviderSmoke` is opt-in: `JPS_LIVE_DIALECT_PLATFORM` explicitly
supplies a configured platform. It copies only the API/search settings it needs
into a private temporary directory, uses no native account session, and bounds
each of its two stages to one record, two searches, three pages and ten model
requests. It creates no operational user Job, sends no outreach, and removes the
temporary keys and records afterwards. Do not enable this test in ordinary CI.

The browser journey also guards linked-chat navigation on individual test-case
URLs: opening or restoring the chat must preserve `/matrix/cases/:caseId`.
Template imports are checked in the browser for both valid non-GTM presets and
rejection of embedded execution authority.

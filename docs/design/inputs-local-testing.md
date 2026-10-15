# Inputs review and evaluation: local acceptance guide

This guide covers supplied-record evaluation and chat-designed research recipes in
the combined local v0.13 build ([release scope](v013-release-scope.md)). The supplied-record and recipe-save checks
make no website requests or AI calls. Only the explicit research section below
starts provider work. For the separate
v0.13 Web access, Jobs activity and verification checks, use
[v0.13 local validation](v013-local-validation.md). Research discovery is included for local testing. Generated-record evaluation and
graph execution are not enabled.

## Start with disposable data

The fixture in `internal/desk/testdata/inputs` contains a synthetic pack, two saved
expectations and a four-record CSV. It is for testing only. Use a temporary project
and separate configuration directories so tests cannot change your working Desk:

```sh
fixture_dir=$(mktemp -d /tmp/jps-inputs-manual-XXXXXX)
mkdir "$fixture_dir/project" "$fixture_dir/config" "$fixture_dir/data"
cp internal/desk/testdata/inputs/* "$fixture_dir/project/"
XDG_CONFIG_HOME="$fixture_dir/config" XDG_DATA_HOME="$fixture_dir/data" \
  /tmp/jps-v013-components/jpack-desk-chat-inputs \
  --port 18898 \
  --jpack /tmp/jps-v013-components/jpack \
  --runner /tmp/jps-v013-components/jpack-runner \
  "$fixture_dir/project"
```

Run these commands from the Desk worktree. Open the launch URL printed by Desk.
Keep this terminal running. The installation's tested-release policy remains on.
Do not disable it to work around failed test expectations. Stop the fixture with
Ctrl+C when finished. Keep its temporary folder if you want to inspect history.

## Create and map inputs

1. Open **Inputs → New input**. Name it **Supplier screening**, add a short
   description, and choose **Create input**. Dialog actions should align right.
2. Choose **Prepare inputs**, import the fixture's `records.csv`, and confirm four
   records appear. Open Northstar: `External ID` must still be `00123`.
3. In **Recipe**, choose `screening` under Qualification, then **Use this pack**.
   Map **screening · matches** to **Matches** and explicitly choose **Number**.
   Under **Evidence availability**, map **screening record** to **Evidence**.
4. Expand **Conversion rules and examples → Preview conversions**. Expect four
   records and one issue: Orchard's `many` cannot be converted to a number.
   This preview must not change the saved recipe or create a job.
5. Save changes. Refresh: the pack snapshot, mapping and conversion must persist.
   Every original value must remain unchanged.

This fixture deliberately uses numeric equality. Ordered comparisons in JPS use
**decimal strings**, so real packs using ordered comparisons should keep those
inputs as Text. A JSON number is not a replacement for decimal text. Rule-derived
type hints are advisory; Desk must not change pack rules to make inputs fit.

For Yes / No conversion, use a pack with a boolean equality input, select that
conversion and declare its exact Yes and No spellings. Test a false spelling,
a differently cased spelling and a blank. Expect false, a conversion issue, and
unknown respectively. No heuristic coercion or automatic case folding occurs.

## Review without running

1. In **Records**, select all four records. The checkbox should appear on hover
   or keyboard focus and remain visible during selection. Select-all toggles all
   matching records, including those on other pages.
2. Open **4 selected → Review inputs**. Expect one conversion issue and a disabled
   **Review evaluation** action. **Review issues** selects Orchard. The exact
   original value and reason must be visible.
3. Close the dialog and deselect Orchard explicitly. Desk must not automatically
   skip it. Open **3 selected → Evaluate…**; this starts at the same review.
4. For Northstar, confirm original `"0"` beside prepared `0`. For Unknown, the
   blank must appear as unknown, not zero. Expand evidence and source details.
   Supplied `present` is an assertion, not independent evidence verification.
5. Check Jobs before proceeding: input preparation/review created no operational
   runs. The next step performs a release rehearsal and reruns saved expectations.

## Confirm and inspect results

1. Choose **Review evaluation**. Expect the exact pack version, recipe version,
   three selected records, and passing saved tests. Review details include the
   sample rehearsal and project reviewed-set standing. The project's setting and
   Runner's fixed release lock describe different scopes; Off is not Ready.
2. The Evaluate button stays disabled until the review checkbox is selected.
   Confirm **Evaluate 3 records** once, then immediately refresh or close the tab.
3. Reopen Inputs. Expect one job, three operational runs and eventually:

   | Record | Preparation | Evaluation | Decision |
   | --- | --- | --- | --- |
   | Northstar | Ready | Completed | clear |
   | Harbor | Ready | Completed | hit |
   | Unknown | Ready | Completed | unresolved |
   | Orchard | Needs review | Not run | — |

   Completion describes execution, not a favorable decision. Unknown is not
   converted into a failed execution. No external action is performed.
4. Open a Completed result. Inspect its input preparation and original source
   case. Use **Verify saved record**, then inspect the separate signature and
   release/input-reference results. Download uses the existing Jobs record flow.
   A signature does not prove the supplied data is true or rerun the decision.
5. Change and save the recipe description. Existing results must say **Earlier
   recipe**, retain their Jobs links, and never rerun automatically. Activity
   keeps evaluation/job references as well as the original import and recipe
   events. Local activity is not presented as a signed audit trail.

## Stale data and policy checks

- Open an evaluation confirmation, then change the saved recipe in another tab.
  Confirmation must refuse and request a fresh review. Selection is never silently
  changed or refreshed before execution.
- Repeat with the pack, saved test matrix or project configuration changed on disk.
  Confirmation must refuse. If the pack changed, refresh its Recipe snapshot first.
- Remove the fixture's matrix declaration in its temporary `jpack.json`, review
  again and inspect the untested state. With the default installation policy,
  evaluation remains unavailable. Restore the declaration and review again.
- Save an incorrect expected outcome. The new release check must report the failing
  test and prevent job creation. An older passing report must not authorize it.
- Try `00123`, `1,000`, `1.2K`, an out-of-range integer and a precision-losing
  decimal as Number inputs. Expect issues without changing originals. Text keeps
  identifiers intact. Blank values always stay unknown.
- For evidence use only `present`, `absent`, `unknown` or a blank. Invalid text is
  an input issue, never promoted to evidence availability.

## Interruptions and partial completion

- Close the browser after confirmation: the server continues accepted work.
- Restart the disposable Desk while submissions are in progress. The saved batch
  resumes and reconciles using the same job release and per-record submission keys.
  A lost response must not duplicate a run.
- A run interrupted during actual execution remains Interrupted. It is not
  automatically re-executed. Select it and explicitly review a new evaluation to
  create a new attempt; the earlier attempt remains in history.
- A blocked submission offers **Retry pending submissions**. This reconciles only
  pending submissions against the existing fixed batch. It does not rerun completed,
  failed or interrupted executions or adopt a newer recipe.
- If signing fails, the completed decision remains visible with a separate
  **Signing failed** cue. Open Jobs for the actual reason. Do not damage your real
  signing keys to simulate this; the automated stub test covers the presentation
  and storage distinction.

## Visual and accessibility checks

Use Dark and Light, keyboard only, and widths 1440px, 390px and 320px. Shared menus
must support arrows/Escape; dialog closure returns focus to the selection button.
Tables scroll inside dialogs instead of widening the page. Footer actions remain
reachable. Names use the Library icon/plain-name style. Amber identifies attention
with text as well as color. Global Desk health notices stay in the bottom strip;
conversion errors and evaluation refusals remain with the affected action.

## Automated evidence

- `internal/desk/input_evaluation_test.go`: exact conversions, unknown/false,
  immutable originals, generated-input refusal, stale reviews, released Runner
  restart with lost responses, independent release references, required saved
  tests, interrupted execution and signing failure separation.
- `web/src/inputs/*.test.tsx`: explicit confirmation, stale UI review, no silent
  skipping, result links, earlier recipes and preserved edit buffers.
- Local browser harness: `/tmp/jps-input-evaluation-browser.mjs`; results:
  `/tmp/jps-input-evaluation-browser-result.json`. It runs in a disposable project.
- Screenshots: `/tmp/jps-input-evaluation-{recipe,issue,confirm,results}.png` and
  `/tmp/jps-input-evaluation-review-{1440,390,320,light}.png`.

The supplied-input batch ledger is private project data under
`.desk-private/input-evaluations`. Preserve it with `.desk-private/input-preparations`
when moving this local test project. It is not part of the existing chat backup.

Jobs history and signature verification also depend on the existing Runner store and
retained keys. Copying the Inputs folders alone does not migrate those records.


## Chat → reviewed input recipe

1. Open main chat, select an existing enabled AI connection, and ask:
   “Help me design an Inputs recipe for finding companies with recurring deal
   reviews. Interview me about my target and requested fields before proposing
   the recipe. Do not start research.” Sending chat uses that AI connection;
   it does not authorize a separate research run.
2. After the interview, request the proposal. Expect a **Proposed input recipe**
   card with target and fields, then **Review recipe**. A textual description alone
   is not a saved recipe. The host tool must produce the card.
3. Open review. Check the name, instructions, target, typed fields and criteria.
   Choose an AI connection/model explicitly; chat attribution is not permission
   to reuse a connection. Select an existing web-search connection for discovery,
   or leave supplied URLs only and add URLs after saving. Check the limits.
4. For **People at selected companies**, choose a source input and actual company
   records. A fresh input with no companies cannot start this target without a
   selection. Changing the referenced input before research review must be refused.
5. Optionally expand Qualification and select a pack snapshot/mapping. Research
   can be saved before choosing a pack. Criteria are suggestions, not pack rules.
   Generated record evaluation remains unavailable pending its trusted-source
   handoff; saving a mapping does not remove that boundary.
6. Cancel: no input or run is created, the proposal stays in chat, and keyboard
   focus returns to Review recipe. Edits require discard confirmation.
7. Save to Inputs: the card becomes **Open input**. Open it and confirm the saved
   recipe, zero records and no running research. Refresh chat; its link persists.
   Retry an interrupted save unchanged: there must be one input, not duplicates.
8. Repeat from a pack or graph right-pane chat. Use the chat options menu's
   **Input recipes** item to review older proposals. Controls and save behavior
   must match main chat. Pack/graph rules stay unchanged.

Automated evidence: `inputs/chatTools.test.ts`, `InputRecipeArtifacts.test.tsx`,
chat store/provider tests and `TestInputChatSaveIsIdempotentAndNeverStartsResearch`.
The browser harness `/tmp/jps-chat-input-browser.mjs` seeds proposals in a disposable
project and checks actual main/pane UI, connection choice, cancel focus, saving,
reload and 1440/390/320px geometry. It deliberately makes no live AI calls.
Results: `/tmp/jps-chat-input-browser-result.json`.

## Opt-in local research check

Use a disposable input and explicitly review provider limits before starting.
Live search, page reading and AI may incur provider usage. This step has not been
run as part of the fixture-only acceptance checks.

1. Save a recipe with one or two requested fields, a small record limit (e.g. 3),
   and the chosen connection. Configure an existing search provider or import a
   few public starting URLs. Confirm configured Gateway and verifier availability.
2. Choose Prepare inputs → Research with AI. Review the frozen instructions,
   target, selected model and record/search/page/model-request limits. Cancel
   should leave no active work. Reopen and choose Start research explicitly.
3. Watch the shared progress row below Records / Recipe / Activity. It remains
   visible while any tab scrolls. The current operation, elapsed time, View steps
   and Stop stay available there. View steps opens the live operation list without
   leaving the tab; expand a step for source links, request times, result summary
   and errors. Technical details remain collapsed. Confirmation immediately shows
   queued/running feedback. Closing the
   browser must not cancel an accepted
   server run. Stop must cancel outstanding work and preserve completed records.
4. Inspect records. Every requested field is either a typed result or an explicit
   unresolved state. Distinguish source-backed extraction from AI interpretation;
   inspect quotations and retained links. Neither counts as independently true.
5. If a Google search redirect is refused, use Review web access beside the
   error. Review the existing setting and explicitly Apply changes. This does not
   start or repeat research; Prepare inputs still requires a new review and
   confirmation. Managed, external, unsupported or already-enabled policies show
   relevant guidance instead of an inapplicable enable action. View steps retains
   the original error. A refresh failure says the last known status may be stale.
   Activity lists prior research runs; selecting one opens the same steps dialog.
6. On Recipe, scroll the long form. Discard changes and Save recipe stay at the
   bottom of the pane. Edit instructions: research is unavailable until changes
   are saved or discarded. Save must not start research. Opening either recovery
   settings or steps must preserve the edit and restore focus when closed.
7. Restart Desk during a disposable run. It should become interrupted and retain
   completed records; it must not silently repeat uncertain provider calls. Resume
   is not implemented yet: review a new run explicitly if another attempt is wanted.
8. Do not evaluate AI records through asserted case inputs. The current server
   refuses this until generated-source admission is implemented. Supplied CSV
   evaluation continues to use the separate acceptance steps above.

A reviewed 50-record pilot, adjudicated precision/coverage/cost, complete usage
metadata, generated-input handoff and graph execution remain outstanding before
calling the ICP development complete or release-ready.


Progress regression checks: `ResearchProgress.test.tsx` covers all-tab placement,
Stop, immediate confirmation and prioritizing active runs over unstarted reviews.
`/tmp/jps-research-progress-browser.mjs` checks fixed placement at 1440/390/320px,
scrolling, stop acknowledgement, terminal details and status-fetch failures with
simulated runs in a disposable project. It performs no provider work.

Research UX checks: `ResearchActivity.test.tsx`, `InputActivity.test.tsx`, and
`input_research_activity_test.go` cover typed recovery, real operation history,
legacy request reconstruction, scoped access and exclusion of provider payloads.
`/tmp/jps-inputs-ux-browser.mjs` exercises recovery, edit preservation, live steps,
Stop, the fixed Recipe footer and field alignment in light/dark at 1440/390/320px.
The browser fixture uses simulated source activity and changes no real settings.

Research citation and search-settings regressions:

- PDF citations may differ in layout whitespace only. Line wraps, repeated spaces
  and nonbreaking spaces resolve to the exact retained source passage in the saved
  record. Changed words, punctuation, case, numbers, omitted negation and invented
  quotations still fail. The original model action and source bytes remain intact.
- Change a search connection setting after saving a research recipe. Before another
  AI request, Desk must refuse the stale reviewed revision. In Recipe, Web search
  shows **Use current settings**. Accepting it changes only the draft connection
  reference; **Save recipe** is still required, and neither action starts research.
- An unavailable or failed settings read must not allow adopting cached settings.
  Explicit external gateways must never be checked against local connections.

`input_research_quote_test.go` covers strict quotation matching and original-span
retention. `input_research_search_test.go` checks preflight refusal before any AI
or source attempt. `ResearchRecipe.test.tsx` checks explicit adoption and unchanged
fields/limits. The optional `TestResearchRetainedProposalOffline` test independently
verifies saved source receipts and validates a saved model proposal without any
model/provider request or live-record write (`JPS_TEST_RESEARCH_RUN` names the run's
`record.json`, and `JPS_TEST_GATEWAY` names the verifier binary).

## Research record review (local build)

After a research run saves records, test this without running more AI requests:

1. Open **Inputs → your input → Records**, then click one company. Confirm that
   its source-backed values, AI interpretations and original quotations remain
   visible. The page must no longer show the CSV-only import explanation.
2. **Save this review** is disabled until a decision is chosen. Select **Ready
   for evaluation**, **Needs more information** or **Exclude**, optionally add a
   note, and save. This does not qualify, evaluate or contact the company.
3. Close the dialog. The Preparation column reflects your choice. Reload the
   page and confirm the decision remains. The corresponding preparation filter
   must find the record.
4. Open the same record and expand **Review history**. The time, recipe version,
   local identity, note and reviewed record snapshot must be retained. Changing
   a decision appends history. Saving an unchanged decision adds no duplicate.
5. Select research rows and choose **Review research** to review each record
   individually using the Record selector. Switching with unsaved edits asks
   before discarding them. The dialog has an independently scrolling body and
   a fixed action footer; Escape or Close restores focus to its opener.
   **Save this review** affects only the displayed record, even with several selected.
   To set one decision for the full selection, choose **Set review decision…**.
   Expand **Selected records**, choose a decision and optional shared note, then
   **Save N reviews**. All selected rows must update together, unselected rows must
   stay unchanged, and no evaluation starts. Reload to confirm persistence. A
   concurrent recipe or record edit must refuse the entire save and ask for a new
   selection review; no partial review or automatic resubmission is allowed.
6. **Activity** includes each saved review decision. Exporting selected records
   includes only those records' review histories, snapshots and recipe versions.
   It must not include unselected records or raw CSV content.
7. In a separate tab, change and save Recipe while a review is open. The old
   review must not save over the newer input; explicitly reload the record to
   review the new version. The old decision remains in history and the table
   requests review again. A changed research record also requires another review.
8. Select an unreviewed researched record. **Evaluate…** stays disabled and
   explains that a current Ready for evaluation review is required. Saving that
   review must preserve `class: generated` and must not submit evaluation. Ready
   records use the manual research-to-pack flow below.

Automated checks cover revision conflicts, simultaneous conflicting reviews,
route authorization, invalid/forged review fields, immutable snapshots, repeated
publication, failed saves, selection exports, and the generated-input boundary.
Browser smoke checks use read-only navigation on the user's actual research;
review decisions are tested only in isolated fixtures.

### Qualification dependency

The pinned Runner v0.8.0 labels `mapping.case` values as `asserted`. Generated
source admission instead uses a trusted installation profile and verifiable
acquisition. Desk now supplies this through its installation-owned reviewed research adapter.
A page-read receipt authenticates acquired source bytes, not AI interpretation.
Human readiness does not promote generated inputs to assertions or verify truth.
See the manual evaluation section below for positive and negative checks. Graph
execution remains separately gated.

## Inputs chat (local)

1. Open Inputs, then an existing input. Open Assistant from the existing right
   rail if collapsed. Check that it identifies the input and recipe version.
   Select records and confirm that the selected count changes. Records, Recipe
   and Activity must retain the same conversation and selection context.
2. Ask: “Add an employee_count number field to this recipe, keeping its other
   fields.” Open **Review changes**. Check added/removed fields, instructions and
   existing connection settings. Cancel once: nothing should change. Review again
   and **Save recipe**: only the recipe should change; no research should start.
3. Ask for another edit; change Recipe manually before reviewing the proposal.
   Unsaved edits must block the review. After saving the manual edit, the previous
   proposal must report that Inputs changed. Ask for a fresh proposal. During a
   review, a conflicting refresh must retain typed edits and disable Save.
4. Ask: “Remove the research setup but keep the records.” Check the section named
   in the removal review, then cancel. In a disposable input, confirm removal and
   check that records and earlier Recipe versions remain available.
5. Ask: “Research records using the saved recipe.” **Review research** must show
   the saved connection/model and limits. Cancel without a provider call. If you
   choose to test a paid run, click **Start research** explicitly. Watch actual
   progress and **View steps**. **Stop** targets that research run; cancelling a
   chat response alone must not be described as stopping research.
6. Ask to evaluate selected AI records. Unreviewed or stale records must remain
   unavailable. With current human-ready records and a saved qualification pack,
   the card opens input review and final confirmation. Chat must never save a
   human review or start evaluation by itself.
7. Open Chat history, start another chat, and switch back. Repeat for another
   input and the Inputs list: each has separate scoped history. Main chat should
   reopen a saved input conversation with its saved context and action cards.
8. Check narrow-screen takeover, collapse/reopen, keyboard focus, fixed Recipe
   footer, and existing record review dialogs. No extra page header buttons or
   independent chat composer should be introduced.

Automated UI/tool tests use isolated fixtures. Local browser smoke tests do not
send AI messages, start research, save recipes, or change human review decisions.

## Manual research-to-pack evaluation

Use a disposable input for the first test; no live outreach or new AI research is
needed if it already contains retained research records.

1. In **Recipe → Qualification**, choose the saved pack, map record fields to its
   inputs and review type conversions. Save the recipe before reviewing records.
   Pack rules and saved tests remain the source of qualification decisions.
2. In **Records**, open each company/person. Inspect its values, quotations and AI
   interpretations. Set **Input review → Ready for evaluation** only when you
   accept it as an input, optionally add a note, and **Save this review**. This neither
   verifies the claims nor runs the pack.
3. Select the ready records, then **Review inputs** (or **Evaluate…**). Confirm the
   selected/ready count, pack, prepared values and **AI research** origin. Expand
   **Sources and review history**. Unknown values remain unknown.
4. Choose **Review evaluation**. Check the release rehearsal and saved test status,
   tick the explicit review confirmation, then **Evaluate N records**. Required
   project safeguards and tested-release policy still apply.
5. Follow the results in **Activity** and open the run in **Jobs**. Check input
   lineage says **AI research**. Download the verification record to inspect the
   generated snapshot, source references and human review. A signature does not
   establish factual truth.
6. Negative checks: select an unreviewed, excluded or needs-information record;
   the whole batch must be unavailable. Change the recipe after review; the record
   must require review again. Leave the confirmation open and change the input in
   another tab; confirmation must refuse the stale revision. Mixed supplied/AI
   selections require separate batches. No action may silently skip rows.
7. Inputs chat may propose evaluating explicit ready records; its card opens the
   same review flow. It cannot set human readiness or directly submit evaluation.

Automated local integration uses synthetic records and the actual installed
Gateway, Runner and Runtime. It makes no paid model requests or external reads.


## Research retry policy (local build)

1. Open an existing input → Recipe → Research → **Limits and retries**.
   For a recipe created before retry support with no saved policy, automatic retries
   must initially be **Off**. Change another recipe field and
   confirm retries remain Off. Discard those edits if they were only a test.
2. Select **Temporary failures**, maximum retries **2**. Keep **Retry timed-out
   searches** Off unless you want to permit repeated potentially metered searches.
   Save recipe. Saving must not start a run or change Admin search timeout.
3. Use **Prepare inputs → Research with AI…**. Confirm the review shows the saved
   policy and the request limits. Cancel for a read-only check. Start research
   only when you intend to use your configured accounts.
4. On an eligible failure, the shared progress row shows a countdown and the next
   attempt number. **View steps** shows each actual request separately, including
   the query, times and retry relationship. Successful earlier steps remain.
5. **Stop** during a retry wait must prevent another request. Reloading the page
   does not discard progress. Restarting Desk must interrupt, never resume, a
   pending retry. Saved records stay available for review.
6. Exhausted retry or request limits must explain why the run stopped. Invalid
   requests, authentication, policy blocks, ambiguous website/provider failures
   and verification failures must stop without an automatic retry.
7. For a timeout retry, enable the separate option and save. Do not expect this to
   fix a website failure or a stale search-connection revision. Updating a saved
   recipe never restarts an interrupted run.

Deterministic Go tests inject rate limits/timeouts, cancelled waits, process loss,
request-budget exhaustion, sealing failures and lost journals without external
provider traffic. UI tests cover legacy defaults, explicit timeout opt-in, new
setup defaults, locked controls, review copy, countdown and Stop visibility.

Local retry validation, 2026-10-12:

- Full `internal/desk` package tests passed (193.126s).
- Research tests with the pinned Gateway verifier and real reviewed-input
  Gateway/Runner handoff passed (9.737s); no external provider traffic.
- 74 frontend tests passed across Inputs and MappingReview; production build passed.
- Disposable browser checks passed at 1440px and 390px: policy persistence,
  legacy Off, timeout opt-in, review summary, all-tab progress, Stop and history.
- Installed `jpack-desk-research-retries` locally after confirming zero active
  research. Read-only smoke checks passed for Library, Jobs, Admin, Inputs chat,
  progress and manual review. The existing recipe stayed at version 2 with no
  retry policy and all three records present; no research or review was submitted.
- An older smoke assertion expected evaluation to be disabled. The installer
  rolled back; the check was updated to exercise the approved manual evaluation
  entry point without submitting a run, and the subsequent install passed.

## Research activity and large documents

1. Open **View steps** on a saved or running research run. Elapsed time belongs
   beside the operation name; all status labels end at the same right edge.
   Compare single/double-digit seconds, minutes and Outcome uncertain at desktop
   width and 390/320 px. Narrow rows may wrap. Check Enter/Space expansion.
2. Use the offline retained-artifact regression with a local saved large response:
   `JPS_TEST_GATEWAY=/path/to/gateway JPS_TEST_RESEARCH_ARTIFACT=/path/to/artifact
   go test ./internal/desk -run TestResearchRetainedLargeArtifactOffline -v`.
   This checks canonicalization and bounded text extraction without fetching the
   source again; it does not retroactively verify an unsealed interrupted run.
3. Run `TestResearchSourceResponseSizeAndReadFailures`,
   `TestResearchProofStorageBudgetAndEscaping`, and
   `TestResearchProofIndependentVerification` (with the Gateway environment set).
   They cover responses above the previous 2 MiB cap, the new size boundary,
   truncated network reads, storage growth and tampered large proofs.
4. Existing interrupted runs remain interrupted. A new run requires the normal
   review and Start research action. A document above the supported cap becomes an explicit source gap in a new
   run; earlier interrupted runs keep their original outcome.

## Research continuation after a source failure

Use fixtures for failure injection; do not make external provider requests merely
to reproduce failures. `input_research_recovery_test.go` exercises the actual
journal, worker loop, saved retry policy and source allowlist with deterministic
model/source responses. Run `go test ./internal/desk -run TestResearch -count=1`.

1. Begin with a successful source and an emitted record, then inject an oversized
   page. Verify the earlier record and source remain, the next model context
   contains the failed request and remaining budgets, and an alternative source
   can complete. The failed response must never become citation evidence.
2. Request the failed URL again: there must be no second provider call and no
   second unavailable-source count. Repeat until the model limit: the run must
   stop at the saved limit. Page/search limits also remain enforced.
3. Inspect Records, Recipe and Activity while continuing: the shared row shows
   saved records and the unavailable-source count with View steps and Stop.
   View steps must retain the failed source link, timing, cause and original
   diagnostic in Technical details. Check desktop, 390 px and 320 px layouts.
4. Finish with a gap or unresolved field: expect **Research finished with gaps**
   and **Review missing fields and source coverage**. Source failures alone do
   not reject records; human input review and evaluation remain separate.
5. Inject authentication, policy, ambiguous gateway, signature/seal or storage
   failures: there must be no next model/source request. Stop or process loss
   must also prevent continuation; restart must not replay interrupted requests.
6. For a search timeout, check saved retry attempts first. After exhaustion,
   continuing may choose a different query, but the same failed query cannot be
   repeated. Every attempt counts against the saved search limit.
7. Set the record limit to one and emit a record with a missing field. It should
   appear immediately while subsequent evidence can update that same record,
   without creating another record or accepting it for evaluation.

Read-only local browser smoke checks never start live research or change user
reviews. A live provider run remains an explicit **Review research → Start
research** action. Earlier interrupted runs require a new reviewed run.

### Automated bulk-review browser check

After building `web/dist`, run from the repository root with Node and Chrome on
PATH (or set `CHROME_BIN`):

```sh
JPS_INPUT_REVIEW_BROWSER="$PWD/web/scripts/input-record-review-browser.mjs" go test ./internal/desk -run '^TestInputRecordReviewBrowser$' -count=1 -v
```

The fixture uses disposable storage and seven synthetic research records. It
selects six, saves one shared review against the real backend, checks immediate
table updates and persistence after reload, and verifies the seventh is unchanged.
It also checks the individual-save label, refusal of a batch after a concurrent
recipe change, retained draft notes, no evaluation side effect, and both themes
at desktop and narrow widths. It makes no AI requests or changes to user records.


## Candidate scheduling regression checks

Run `go test -race -buildvcs=false ./internal/desk -run
'TestResearchCandidate|TestResearchUnusable|TestResearchActivity' -count=1`.
These fixtures make no provider requests. They cover ten records saved in
3/3/3/1 batches from a larger queue; retained-reference validation; URL dedup;
concurrent completion and refusal attribution; shared request/storage limits;
Stop, restart and policy refusal; and timeout followed by an unusable search
response without replay. Existing research proof, budget approval and record
handoff suites remain applicable. `ResearchActivity.test.tsx` checks live
parallel-read counts before the save checkpoint.

For a real account test, explicitly start a new run with the saved recipe. A
record limit is an upper bound: fewer eligible, supported records can still be
saved. Starting or continuing old research is never part of local build tests.

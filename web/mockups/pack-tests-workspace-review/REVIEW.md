# Pack Tests workspace review

Review date: 2026-09-24. Review and static mock only; no product code, runtime, gateway or pack data changed.

## Confirmed findings

1. The user's saved lab-notes.ai Contribution Acceptance Review has no matrix entry in jpack.json and no accompanying matrix file. Its retained draft and conversation each contain the same eight observed runtime probes and zero established test cases. Do not import twice. All eight probes match the final draft candidate digest. The saved pack differs from that candidate in id and version; the historical results must retain their original revision and cannot be presented as results on the finalized bytes.
2. Normal chat draft mode runs structure validation with an empty case list. It can also observe explicit exploratory evaluations. Those observations are retained separately from cases. Research mode establishes source-grounded expectations and cases.
3. DraftWorkspace constructs the research/matrix handover only when chat.mode is research and the draft is eligible. CreatePackDialog only writes/registers a matrix when that handover is supplied. FinalizeDraft retains the checkpoint and associates chats with the saved pack; the saved Tests routes never join that history.
4. PackEvaluate is a standalone legacy route with a limited-width, static two-column layout. It does not mount the pack Assistant/tools presentation. JSON textareas and an evidence-state checkbox are the only input controls; there is no case save, import or source selection.
5. The evidence JSON is an availability map (present/absent/unknown), not an uploaded evidence document. Current wording is ambiguous.
6. PackEvaluate history is React state. Only an explicit Explain on map action exports one snapshot into query-client memory. This is not durable run history. Saved-case results also live in the query cache, not a durable run ledger.
7. Saved cases navigation is hidden when no matrix exists, and the Tests link always enters the scratch evaluator. There is no discoverable way to design the first suite.
8. MatrixView runs and renders a configured suite but has no authoring experience. Its idle state shows a link to the page already open instead of previewing the cases. Header and navigation differ from the current draft workspace.
9. Source acquisition is reusable at the document-client layer, but the selection request and attachment controller assume a chatId/ChatStore. A test-case target must be supported; mounting the existing chat picker unmodified would attach material to the wrong owner.
10. The runtime matrix shape is closed. Arbitrary UI metadata, source mappings and run-history records cannot be added to matrix rows without a schema change. Keep companion metadata in Desk-owned storage, preserving the runtime matrix contract.

## Proposed workspace

- Preserve the pack header and its Overview / Logic / Sources / Tests navigation.
- Main area: Cases and Run history views, compact case table, search, status filter, New case, Design with AI, Run tests.
- Show name, expected decision, latest run state and origin. Distinguish AI suggestions awaiting review from runnable saved cases. Keep draft trials visible without implying they have expectations.
- A row opens one full-height Case details pane. Assistant and Details are alternate destinations in the existing tool rail. Reuse shared resize, expand-overlay, collapse, focus and scroll behavior. Never stack chat above/below the case editor.
- New case opens that same editor. Design with AI opens Assistant scoped to the current pack and suite. Fill with AI proposes values for the selected case.
- Details uses friendly field names; type-aware controls for booleans, numbers, text, lists and objects; explicit Unknown. Hide pointers, trace members and exact JSON in Technical details. A pack may lack a full input schema; never infer requiredness or type silently. Fall back to a lossless value editor when the type is uncertain.
- Inputs, evidence availability, sources, expected result/rationale and actual result belong to one case. Detailed results expand on demand; no permanently empty result column.
- Pin Save/Discard for edits and live progress above the Assistant composer. Retain edits and selected-case identity when switching tools, picking a source, resizing or navigating back.
- At narrow widths show one task at a time with Back to tests; keep both pane and main state. Avoid squeezing the table and form into unusable widths.
- This suite is scoped to the current pack. Do not restore the removed project-wide Run all tests page.

## Manual and AI share the same data

Manual:
New case -> enter or import inputs -> choose sources if needed -> define expected decision -> save -> run.

AI:
Design with AI -> describe desired coverage and choose sources -> receive proposed inputs, expectations, rationale and citations -> review/edit -> add to the same saved suite -> run through the runtime.

AI does not evaluate packs itself or declare tests passed in prose. Existing source-grounded research authoring and runtime expectation validation are reusable. New synthetic/manual cases must also be supported without requiring every assertion to carry a research receipt. Label expectation origin: user-defined, AI-proposed/reviewed, or source-grounded.

Do not derive expected decisions by copying the pack's actual answer: that only checks that the implementation agrees with itself. Avoid duplicate case IDs and explain edits to existing expectations. Never silently weaken expectations to make failures green.

## Integrations

Use one Add source action in the case editor and the same picker from Assistant. Offer upload, paste link, and connected providers actually advertised by the gateway catalog.

Refactor connection selection to a typed destination (chat or test case), with identity/revision checks and cancellation. Source selection temporarily replaces the pane and returns to the editor/Assistant. No nested modal or loss of unsaved inputs.

For manual entry, show a source preview and let the user map source values to fields. For AI, propose the same mapping with citations and mark unresolved values Unknown. An attached document is not automatically proof that an evidence requirement is satisfied.

Freeze normalized test inputs and source identity/version/hash when saving a case. Reruns use that snapshot. Refresh from source is explicit and proposes a change; changes or a disconnected provider cannot silently change past results. Unsupported formats or missing connections offer useful recovery.

## Persistence and versioning

- Preserve authored cases separately from immutable run records.
- Store case identity, suite revision, inputs, evidence state, expectation, rationale/origin and source mapping. The executable part uses the existing matrix format.
- Run records bind to exact pack bytes/digest, suite/case revision, evaluated inputs, source snapshot, runtime version, timestamps and full result/refusal.
- A pack, case or source change makes the prior result historical; show Needs rerun instead of a current green badge. Pack version text alone is insufficient.
- Tests do not change the pack's release version; editing a case changes its suite/case revision. Keep test history even when a chat is later removed.
- Finalization should preserve and link cases, source references, checks and trials for both chat and research workflows. Avoid coupling persistence to all tests passing.
- For existing saved packs, recover from retained finalized draft checkpoints using durable IDs and digest bindings; deduplicate the conversation copy. Do not reconstruct missing evaluations from chat prose.
- For the user's eight probes, show From draft / Exploratory. Allow Open inputs and Save as case after an expectation is supplied. Retain the original result and offer rerun on the saved revision.
- Test definitions may be project files; private source documents, raw integrations content, chat and run history need Desk's private store and explicit export handling. Credentials remain gateway-owned and never enter cases.

## Additional corrections and edge cases

- Unknown/missing is not false, zero or empty string. Preserve supplied-null versus omitted values and evidence absent versus unknown.
- Differing expected/actual decisions, runtime refusal, disconnected/transport error, canceled, not run and stale results are distinct states.
- No cases executed must never report Passed. Skipped work is not a successful test.
- Support expected refusals and handoff targets, not just a simple outcome dropdown; advanced comparisons must retain full runtime assertions.
- Surface uncovered rules, exceptions, boundaries and missing-input paths; AI can propose gap cases. A coverage summary is not evidence of policy correctness.
- No evaluation on route entry, filter changes, resize or focus. Run only on an explicit action.
- A response cannot overwrite newer edits, another selected case, another pack or a changed revision.
- Provide duplicate, delete, import and export for cases with safe ID conflict handling. Do not create a second test-authoring format for AI.
- Persist before reporting a case saved. Handle write conflicts, partial finalization and retry without duplicate rows.
- Restore selection/scroll, keyboard navigation, visible focus, accessible status text and reduced-motion support.
- Replace misleading creation-screen research/testing text with record-based status that actually reflects the carried history.

## Ownership and implementation order

Desk: unified workspace, case editor, AI orchestration, source destination abstraction, persistence/history and draft recovery.
Gateway: keep existing authentication, catalog and acquisition APIs; extend only for a demonstrated provider/data capability gap. No tests-specific provider adapters.
Runtime: retain authority over structure, expectation validity, evaluation and saved-suite comparisons. Existing tools support the first version. A future Run selected / draft-suite comparison API should be added here if needed rather than duplicating matrix comparison in Desk.
No new repository is needed.

1. Add durable test/history model and fix both finalization paths; recover existing draft records.
2. Introduce the unified Tests layout and manual named-case editing/save/run.
3. Generalize source selection and field mapping for cases.
4. Connect AI test design/review to the same case model, then coverage-gap suggestions.
5. Verify draft->finalize->reload, restored history, imports, sources, stale revisions, concurrent edits and mobile/pane behavior.

## Acceptance evidence required before delivery

- The user's eight recorded trials appear once under the saved pack with correct draft bindings, without fabricated expectations.
- Draft research cases and test reports survive finalization/reload and remain separate from current revision results.
- A user can create, save, reopen and run a case without JSON or AI.
- AI can propose cases, show rationale/sources, and add only reviewed cases to the same suite.
- Both paths can select local or connected sources and retain mapping/provenance.
- Expectations stay independent of actual output; mismatches remain visible.
- Existing runtime matrix fixtures still run, including expected refusals and target assertions.
- Save conflict, canceled acquisition, missing source, changed pack and midflight edit cases do not lose data.
- Shared pane resize/expand/collapse behavior, keyboard access and narrow layout are verified.

## Read-only code evidence

- web/src/routes/ChatWorkspace.tsx:116: research-only handover.
- web/src/shell/CreatePackDialog.tsx:799: companion matrix write; registration around 841.
- web/src/chat/store.ts:288: finalized draft and chat association.
- web/src/research/run.ts:900: normal draft validates with no case list.
- web/src/research/runtimeProbes.ts: observed rehearsals are not established expectations.
- web/src/research/ui/DraftPanels.tsx:113: draft Tests presentation.
- web/src/routes/PackEvaluate.tsx:40: scratch evaluator inputs and in-memory history.
- web/src/routes/MatrixView.tsx:37: saved-suite route.
- web/src/packs/PackWorkspace.tsx: navigation only reveals Saved cases for configured matrices.
- web/src/connections/ConnectionPaneContext.tsx:6 and web/src/chat/useChatAttachments.ts:16: chat-coupled source selection.
- runtime/internal/project/matrix.go:57: closed runtime matrix fields.

## References

The proposed layout applies Linear's principles; it is not a copy of a private Linear component library.

- Linear, A calmer interface for a product in motion (2026): https://linear.app/now/behind-the-latest-design-refresh — consistent control placement, restrained hierarchy, fewer competing borders.
- Linear, Agent Interaction Guidelines: https://linear.app/developers/aig — agents use native actions, identify their work and provide unobtrusive feedback.
- Desk docs/design-system.md — shared tokens, panes and stable task state.

## Mock artifacts

case-editor.png: selected case, friendly fields, sources and expected result.
ai-test-design.png: same workspace with Assistant replacing Details.
All names, counts, results and source contents in the mock screenshots are illustrative. Eight actual historical trial runs were found by the read-only audit; the six mock rows are a layout example, not a migrated suite.
Mocks were generated using the built-in image generation tool. prompts.json contains the exact prompts. Final implementation must use the repository's existing icons and design tokens; generated icon/spacing variations are not new component requirements.

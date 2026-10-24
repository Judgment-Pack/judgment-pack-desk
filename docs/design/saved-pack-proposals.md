# Saved-pack proposal review

A generated candidate is a proposal, not a file write. The Library reads the saved pack. Proposing version 0.1.1 must not make a saved 0.1.0 appear updated.

Each new proposal for a linked saved pack retains its target pack ID, path and exact request-time document. An explicit editor context preserves unsaved edits in that basis. The shared chat checkpoint and draft artifact preserve the basis through navigation and reload. Older sessions can recover their comparison from Desk's retained current-pack context; assistant prose is not a baseline.

The pane and main chat expose the same pending proposal. Review shows the saved and proposed versions, changes, and saved-suite results. The shared `ProposalDiffView` uses CodeMirror Merge: unified line/word changes in chat, unchanged ranges collapsed, and an expandable side-by-side view. Structural move summaries retain order changes. The display is read-only, with no per-hunk apply controls. `ChatActions`/`ChatAction` provide common action styling for proposal and related-work adapters.

Save proposal is an explicit chat action in both placements. It rereads the exact target, requires the request-time basis to match, checks the saved suite again, writes through the file API with the observed SHA, and verifies actual read-back. A changed suite, mismatches, invalid structure, read-only target, Plan phase or a dirty editor blocks direct save. Coverage gaps and unasserted cases remain visible; a pass is never a policy guarantee. The editor keeps its ordinary explicit Save, including deliberate manual corrections. Apply to draft is a secondary action available only while editing; it is one undo step and still requires matching bytes and editor identity. The pending card disappears when the saved JSON matches the candidate. A proposal never changes the file or permissions by itself.

Linked saved packs use their saved test suite, not the creation draft's exploratory cases. The host validates the candidate and runs the complete executable matrix with `experimental_test_cases`, retaining expected dispositions, expected errors, supported extensions and exact fact types. Cases without expectations are reported as untested. These checks require no model or reviewer calls. Results bind to candidate bytes and the suite snapshot, preserve mismatches, and refuse incomplete reports or suite changes during execution. Historical results are display data and never save authority; refresh does not rerun checks.

The original draft's check message and Run checks action do not belong above a saved pack's chat composer. Proposal check results and Check proposal belong with the proposal. Active response progress, questions, pauses and failure recovery remain on the shared conversation path. Saved cases and expectations are never changed to make a proposal pass.

Current test reports describe the runtime that produced them. They do not assert policy correctness or grant readiness to deploy. Project version pins, reviewed-set locks and downstream references remain separate from editing the pack document.

Saved-pack authoring receives current cases, rationales and current coverage before its first model request. The host checks each candidate and returns validation/mismatch/coverage findings for bounded correction through the same execution and context boundary. Corrections inherit the original request key, remaining allowance, grants and completed operations; they do not get a new request budget. Repeated candidates stop. A policy question or explanation without a new proposal ends correction; it never rewrites expectations. Explicit Check proposal and refresh are model-free. Fix findings starts a new explicit user request. Coverage probes may be infeasible or require a policy decision; the assistant must explain that rather than weaken rules to make a counter disappear.

## Approval and correction boundaries

Ask me and Auto control permission, independently of Chat/Research and Plan/Work. Ask me exposes **Save proposal** in the shared review card. A successful check alone never authorizes a write.

In a saved-pack workspace, the same composer approval control can grant **one checked pack update**, scoped to the signed-in browser session, Desk project, chat, file path, current file digest and saved case snapshot. It expires after 15 minutes and is invalid after use, revocation, sign-out, process restart, or a changed file/suite. The control labels this capability explicitly. Dialect research retains its existing distinct one-run allowance; it cannot authorize an artifact edit. Other action types remain manual until a host adapter defines their admission and verification contract. Plan cannot grant or consume a write allowance, and compute extensions are separate.

`assistant/approvals/useAutomaticAction` dispatches a ready candidate; it cannot mint permission. The chat worker owns the pack adapter, so closing the proposal pane does not change permission. Before dispatch the adapter checkpoints the exact candidate, verifies current file and suite, checks all saved cases through the runtime again, and refuses dirty editors, incomplete assertions, unresolved policy questions or mismatches. Coverage probes remain advisory: infeasible probes do not authorize weakening rules or inventing expectations. The server admission registry rechecks the current saved phase, retained candidate/check identity, full test-case results, grant scope and revisions in a short storage transaction. It consumes the grant before the canonical CAS file write. The server verifies retained host check metadata; runtime execution occurs through the existing runtime connection, not inside the file transaction. Neither a successful check nor a restored candidate creates permission. Uncertain writes are not automatically replayed.

`assistant/execution/correctArtifact` is the common verification/correction controller, used by saved-pack updates, source-based new-pack authoring and the generic proposal-workflow adapter. Features provide candidate identity, checking, persistence and a correction callback through the existing managed session. The controller persists each completed check before the next request, stops cycles and bounded attempts, and propagates cancellation, typed budget/question pauses, storage failures and transport uncertainty to the established recovery path. It never saves, changes expectations, allocates compute, or retries uncertain operations. Future artifact features use these same ports rather than a provider- or topic-specific repair loop.

This separates proposal repair from execution permission in the same way that [Vercel's tool approval](https://ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling) separates tool generation from execution. The shared Desk controller also serves subscription adapters, so approval and correction behavior are not limited to the Vercel API adapter. [Vercel loop control](https://ai-sdk.dev/docs/agents/loop-control) supplies adapter-level stopping; Desk retains its cross-provider execution allowance and recovery journal above it.

Verified host writes publish a same-window receipt through `files/verifiedWrites`. A document buffer adopts it only when its path and base digest match and it has neither changed bytes nor pending field work. This keeps a clean pack reader current after chat saving without weakening the frozen-revision rule for watcher refreshes or replacing unsaved edits. Receipts are not retained or replayed and never grant permission.

## Private server proposal lifecycle

The private saved-chat executor captures the host-resolved linked pack, declaration,
exact saved bytes and saved case snapshot before its first author request. A matching
`packContext` on that exact saved user turn supplies the comparison text, including
unsaved editor edits. It cannot select a different target or grant permission. The
original capture lives in the run ledger and projects into `Candidate.packReview.basis`;
continuations reuse it and refuse changed source or suite custody.

Linked saved packs disable generic artifact proposals and use the root Work-only
`propose_saved_pack` proposal HostTool. Agent v0.9.0 `CheckArtifact` retains exact
candidate text before checks and invokes the released `CorrectArtifact` controller
with zero internal corrections. Structural validation and the complete executable
saved matrix use the exact candidate, with immutable suite and source checks before
and after asynchronous Runtime work and at physical dispatch. Findings bind to the
exact completed Runtime operations and are retained before tool return. Further
corrections are tool attempts in the same managed turn and allowance. An exact
previous candidate returns retained findings with stalled/clarification guidance
after fresh custody checks; it does not replay Runtime work or allocate another
turn. Canonical progress and request accounting still own dispatch termination.
Explanations need no proposal. Expected-error rows execute; unasserted rows remain
visible in the total. Fact values, decimal spellings, omissions, supported extension
semantics, row order, rationales and intentional type notes remain saved review data.
The unavailable Runtime fact-type advisory surface does not block these checks.

An explicit private `POST /api/assistant-runs/{run}/pack-checks` accepts only an
idempotency ID, candidate digest and digest of the current case identity. The host
resolves the candidate from its retained producer and the current linked chat,
then uses the same model-free checker. Only the two snapshot tools run in a temporary
directory. The attempt and exact Runtime results persist beside the original run;
completed duplicate delivery reads them, while incomplete delivery refuses replay.
A fresh explicit request can check a new suite snapshot, but source, declaration,
owner and candidate changes refuse. This endpoint updates no conversation response,
question, execution checkpoint, allowance, pack file or saved expectations. Its
browser client validates candidate and suite identity before returning display data.
The existing proposal readers consume both the Go projection and recheck response.

These hooks remain private migration infrastructure. They do not activate the
production executor, replace the browser engine, or add server Save admission.
Current editor veto and final executor adoption remain separate cutover work.

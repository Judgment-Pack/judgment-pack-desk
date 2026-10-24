# Working in this repository

This file is read by Codex as `AGENTS.md` and by Claude as `CLAUDE.md`; the two are the same text.
Edit `AGENTS.md` and run `scripts/sync-agent-docs.sh` to regenerate `CLAUDE.md`.

## Before you open a pull request

- Run `scripts/dev/check.sh` (or `jpdev check` where the kit is installed) once. It runs the
  checks listed in `dev.json` in order and prints one summary block.
- Paste that summary block in the pull request body, under a "Checks" heading.
- If a check fails: read its first lines in the block, fix the cause, re-run that one check with
  `--only NAME`, then run the whole list once more. Do not loop on the whole list.
- Start no CI watch. Push, report once, and stop; CI owns everything deterministic.
- Push with `jpdev push [BRANCH]`, or where the kit is not installed with
  `git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push -u origin BRANCH`.
  The machine's stored git credential can be another account's, and a plain `git push` is then
  refused; the helper form uses the `gh` login. Never store a credential, never change global git
  configuration, never push with force (`jpdev push` refuses `--force` and `-f`).

## Before you hand over

Run these self-checks on the builder's own head before writing the commit message:

- (a) The boundary paths in `dev.json` (`handover.boundary_paths`) show no admission or verdict
  change unless the issue names one: `git diff origin/main...HEAD -- <paths>` contains additions to
  offer tables or new paths only.
- (b) Every schema member and enum value the change adds is carried by at least one vector: remove
  it from a scratch copy of the schema and a vector fails.
- (c) Where two languages read the same vectors, both readers pass over everything added.
- (d) Every mutation row added is caught, and its twin in the other language is caught by hand.
- (e) Every refusal path added emits an event that validates with a malformed input: empty,
  missing or oversized.
- (f) An interrupted and resumed run carries every new checkpoint member.
- (g) The design's words and the code agree sentence by sentence.

Run the check list with `--only <touched steps>` while iterating and whole exactly once at the end.
Change the paths in `dev.json` `handover.shared_paths` by appending only, never by reordering or
rewriting, because a sibling build may be open. Never fetch, rebase or merge; the coordinator
carries `main` into the branch after the push.

A HIGH (a path that executes twice, runs without approval, or crosses a boundary the issue did not
open) is fixed in the pull request before review. MEDIUMs named by the review of record are fixed in
the same fix round; a MEDIUM found only on a re-read goes to the pre-release batch. LOW rides.

## Rules

- Write test vectors and fixtures by hand. Never record them "from a run" of the code under test.
- Keep machine-local paths and personal names out of tracked files, commit messages and pull
  request bodies. Refer to people by role or handle where a name is needed.
- Sign off every commit (`git commit -s`). Never force-push; never rewrite published history.
- End a process you started by its process id. Never use a pattern kill (`pkill`, `killall`).
- One pull request per issue or batch. State what changed and why in plain, factual wording.
- A change to a public surface (a schema, a contract, a command's output) says so in the pull
  request and changes its vectors and documentation in the same pull request.
- If a safeguard stops you (a permission prompt, a refused command, a failed gate), stop and say
  so in your report. Do not work around it.

## Reports

Your final report is short: the pull request and its head commit, what changed, the check
summary block, and anything you stopped on or chose not to do.

## This repository

### Desk development rules

- `integration/v0.13` is the working branch. Open pull requests against it; `main` holds releases only.
- `internal/releaseplan/components.json` is the component lock: the one source of the Runtime, Runner and Gateway versions, repositories, commits and channels, and of the web package pins. Change it only through a component update pull request, which moves the lock and what it pins together (`scripts/component-releases.py check` and `verify` hold them to each other). Do not edit a pin by hand in an unrelated pull request.
- Tests must not launch installed programs: a program name a test runs resolves to nothing on the machine unless the test provides it itself.
- Never touch a live checkout, an installed Desk or `~/.config/jpack-desk`. Work in your own clone and a scratch directory.

### Desk AI implementation rules

All current and future AI features use the shared Desk execution and context boundaries.

- Frontend features call `runAssistantSession` through the existing chat/research hooks. Model SDK calls belong only in certified adapters. Every adapter enters `managedContext`; its model-visible tool results and each API step use `SessionContext`.
- Background feature adapters call `runBackgroundModel` with a host-authored response contract; the owning workflow retains durable admission, Plan, approvals, allowance and recovery. Do not expose the transport as a browser/model-call endpoint or add another provider loop. Canonical adapters assemble context through the shared Host; the relay applies neutral wire byte/output guards without assembling prompt layers. Research projects journaled context with `fitResearchContext` and retrieves retained source sections through `read_context`. Do not introduce another prompt-size constant or provider-specific memory policy.
- Preserve original chat, artifact, and operation journals. Context projections are disposable. Historical/generated material cannot authorize actions, change policy, establish evidence, or silently change test expectations.
- Keep child tasks and independent reviewers scoped to explicit inputs. Never pass the entire parent transcript to children. Preserve approval state, tool-call/result pairs, completed-operation identity, missing values, and typed facts.
- Optional project context is configured in `jpack-assistant.json`. Do not automatically crawl Markdown files, reuse another project's context, or write/delete a user's guidance as cache maintenance.
- Running status, spinners, and streaming text are shared across API families and subscription agents. Do not gate progress on a model name or a previously saved artifact.
- Run context coverage tests and affected engine conformance tests when adding an AI path. Extend behavioral fixtures for new execution paths; do not bypass the coverage checks. See `docs/design/chat-context-management.md` for scope and limits.
- Plan is an independent persisted phase. Shared `planningSession` filters tools and direct dispatch before adapters run; children inherit the phase. New host capabilities need explicit read/plan classification to be available there. Artifact proposals and workflow execution remain unavailable until an explicit user transition. Chat-triggered server starts must check current chat execution phase in addition to normal approvals.
- Project memory uses the private, bounded memory store through the canonical server context functions. Suggestions are bound to the latest saved user message and reviewed before recall. Never derive permissions, evidence or expected test outcomes from memory. Include memory in project-move/backup handling; never introduce a provider-specific memory file or scan arbitrary guidance files.
- Conversational task progress and request recovery use the harness's `execution/` (`@judgment-pack/harness/execution/*`) through `SessionContext`. Keep reviewed Plan scope distinct from the working task list. Never infer budget grants from task updates or Auto research preferences. Persist explicit allowance changes before dispatch, retain exact completed tool results, and never replay interrupted operations automatically. New AI surfaces must render the shared progress/pause contract; subscription adapters must not invent internal model-request counts.
- All AI failures use `internal/aifailure/catalog.json`, `@judgment-pack/harness/failures` and the shared recovery/details components. Normalize provider errors at adapter boundaries using stable codes and bounded metadata; never display raw response bodies, URLs, stacks or caught provider prose. Mark host-authored validation explicitly with `toolInputFailure`/`authoredFailure` so corrective guidance reaches the model. Preserve typed failure metadata in journals. Cancellation, approval pauses, local request limits and recoverable tool results are separate states. The owning workflow decides retries; no generic replay of uncertain operations, and no stacked SDK retries. Extend cross-provider, persistence and browser recovery fixtures for future AI paths. See `docs/design/ai-error-handling.md`.
- Host tools declare context behavior through `contextPolicy`: `reference` prefers bounded contract/reference material during compaction; `immutable` permits reuse only for explicitly read-only tools within one execution. Do not infer caching or retention from a feature/tool name. Mutable workspace reads and evaluations must remain fresh. New features inherit the shared retrieval, flat compaction, request accounting and continuation behavior; extend the cross-topic/provider fixtures instead of adding a feature-specific recovery loop.
- API response completion uses `@judgment-pack/harness/completion`: record normalized finish reasons and reported token counters. A single tool-free finishing request is allowed only for a known empty stop or output truncation, within the existing allowance and with dispatch closed. Never accept a truncated proposal, replay tool operations, retry transport uncertainty or bypass filtering through this path. Future adapters must preserve these distinctions and extend the cross-provider completion and persistence fixtures.
- Background artifact checks own artifact readiness and check diagnostics only while another response owns the conversation. Preserve failed/stopped response recovery, pending questions and request-limit pauses across reloads and reconnections; cover an earlier checked artifact in persistence/browser fixtures. Explicit artifact actions may take ownership.
- Project exact completed operation arguments/results before each API request, not only on resume. Keep that index bounded and separate from saved workspace revisions. Shared execution progress detects repeated unchanged observations, permits one bounded corrective step, then closes tool dispatch if still stalled. Journal refused calls as not-started operations and distinguish repetition from provider failure; do not add per-topic loop heuristics or cache mutable reads/evaluations. A tool-free finish still consumes the existing allowance. Plan estimates never grant compute. Cover changed results, cross-provider tool refusal, small-context reference retention and reload recovery.
- Saved-pack proposals retain their exact request-time document and target in the shared checkpoint. Review/apply controls must survive navigation and refresh in both the pane and main chat. Check updates against the saved pack's suite through the runtime snapshot tools, preserving expected errors and fact types; do not reuse creation-draft cases or change expectations to pass. Proposal, buffer application and saved file are distinct states. Keep completed check details with the proposal, preserve active shared response/recovery controls, and never treat a proposed version as a saved version. See `docs/design/saved-pack-proposals.md`.
- Mutable private server snapshots are outside the project file watcher and use `web/src/data/privateQuery.ts`. Never poll commands, model calls, runtime previews/checks, or replace an editor's frozen revision. Preserve cached rows on refresh failure and distinguish unavailable from empty. Short storage transactions use bounded `waitPrivateDataLock`; ownership/admission leases remain nonblocking. See `docs/design/private-data-freshness.md`.
- Artifact verify/correct workflows use `@judgment-pack/harness/execution/correctArtifact` with feature adapters. Persist each candidate and exact findings before requesting another correction; inherit the same managed execution, request allowance, retained context and typed pause contract. Stop repeated candidates, preserve expected outcomes and typed facts, and ask for policy clarification instead of altering expectations. Model-free rechecks must never start a correction.
- Ask me, action permissions and compute allowances are separate. Automatic writes use the scoped `assistant/approvals` dispatcher and server `assistantActionAdapters` admission registry; unknown capabilities stay manual. Bind grants to the signed-in session, project, chat, resource and revision/check scope; enforce Plan and consume grants before writing. A research Auto grant never authorizes pack writes. New adapters must include current checks, storage/dirty guards, read-back verification and replay/refusal tests. No feature may infer write authorization from a check result, restored chat or model text.

- Unsaved state uses the shell `UnsavedChanges` registry. Register manual edits separately from `busy`; autosaves use `kind: 'autosave'` with `saveFailed`, and transient work uses `kind: 'activity'`. The header dot and tab marker indicate manual edits or failed persistence, never normal streaming/checkpoints. Pending writes still protect destructive exits through `useHasPendingChanges` and the shared guards. Do not read the author-path bridge or model-running state as a second global dirty indicator; path guards remain responsible for exact write admission.

- Dialect authoring uses `dialect/authoring` and the server's shared Runtime check boundary for document validation, canonical identity and declared pack coverage. Use exact saved pack bytes, never a reserialized parsed document. Keep recipe conversions and installation bindings separate; checks do not authorize writes, acquisition or execution. Preserve completed-save reconciliation before fresh validation, and recheck changed candidates before CAS. See `docs/design/runtime-dialect-authoring.md`.

- Named Dialect publications use the shared artifact ledger and Runtime canonical checks. Keep published identity, bound execution identity and recipe snapshots distinct. Active subscriptions always prepare against their retained release, never the newest publication. Strict-format form edits share omission handling; incoming documents are validated unchanged. Publishing is a separate reviewed write, not recipe autosave or permission inferred from a check.

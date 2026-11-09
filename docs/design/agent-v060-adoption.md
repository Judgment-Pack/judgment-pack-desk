# Agent v0.9.0 adoption candidate

This component update is an unpublished prerequisite for Desk #599. Desk adopts
[Agent v0.9.0](https://github.com/Judgment-Pack/judgment-pack-agent/releases/tag/v0.9.0)
from the released npm archive and Go module. This document retains its existing
filename so earlier references continue to work. Historical v0.6.0 evidence below
is not v0.9.0 acceptance.

## Published identity and compatibility

Both `v0.9.0` and `go/v0.9.0` resolve to
`9ce6c897b4abf484f50604d81422a26d730f9ed7`. The published
`judgment-pack-harness-0.9.0.tgz` SHA-256 is
`88aca6d238d1f57da320ec758c0ce9160748bb7492195ec3cb95c1b0bde02852`.
The archive and checksums have verified GitHub attestations constrained to the
Agent repository, release workflow, exact tag and source commit. This establishes
artifact provenance; it is not a claim of reproducible rebuilding or tag-signature
verification. Source-equivalent prepublication audit commit
`c0d6496` is reference evidence only; neither dependency resolves to that commit.

The Go proxy/checksum resolution identifies `go/v0.9.0` at the same release commit.
The module sum is `h1:x1KQUtuvfu4o8hn0IYMxyfLlkIMlTOYg3Sbgx7pMpM8=`;
its go.mod sum is `h1:AVl+6oeQwnOgRb5tNeNV9oLR1u7ZqneRKvqr+lVPthg=`.
The component lock, npm manifest/lock and Go module/sums move together. Runtime,
Runner and Gateway pins retain their existing versions.

The v0.8.0 archive failure remains historical evidence: its corpus schema import
escaped the package and its native ESM imports were extensionless. v0.9.0 ships
the schema at `dist/contract/schemas/corpus-entry.json` and uses package-local
imports with ESM extensions. Desk consumes the unchanged published archive;
no alias, source copy or package patch is part of adoption. v0.9.0 also preserves
omitted native reasoning through request resolution: Desk keeps the saved effort
empty and the native default high, while explicit effort remains explicit.

The released raw/resolved request-session boundary adds a private unique-symbol
brand to `AssistantSession`. Desk's exhaustive guard retains the exact public
member inventory and checks the sole private brand declaration separately;
it introduces no raw-session execution path.

Runtime #582 remains independent: this candidate keeps the existing Runtime pin
and compatibility refusal. Agent publication cannot enable unchecked proposals
or fabricate Runtime responses. The private executor stays unregistered; browser
cutover and complete user journeys remain #537. Typed-memory host migration
remains #598. The private Plan host exists, with production activation separate.

## Frozen released contract inventory

The authority for this inventory is the immutable
[release source](https://github.com/Judgment-Pack/judgment-pack-agent/tree/9ce6c897b4abf484f50604d81422a26d730f9ed7),
its `contract/schemas`, generated `docs/reference`, Go module and attested npm
archive. Merged or open upstream work outside this commit is not a dependency.

The tagged release source defines 19 schemas; their exact names and checkpoint
members are listed below. The npm archive bundles selected contract data and the
policy schema, not the complete source schema directory. Provider capability, policy and all record readers remain canonical Agent
contracts; Desk must not construct another policy or framework loop.

The 34 event kinds are `execution_checkpoint`, `budget_paused`,
`approval_requested`, `steered`, `compacted`, `skill_loaded`, `skill_unloaded`,
`retry`, `response_recovery`, `delegation_requested`, `delegated_task`,
`user_question`, `reasoning`, `message`, `message_progress`, `tool_call`,
`tool_result`, `guardrail`, `thinking_unavailable`, `critic_decided`, `critique`,
`proposal`, `error`, `end`, `policy_resolved`, `memory_proposed`, `memory_recalled`,
`edit_applied`, `edit_refused`, `output_guarded`, `export_written`,
`connection_switched`, `usage` and `recorded`. New event statuses in this candidate
are observations only: private payloads are not rendered as instructions,
acceptance, persisted-save success or permission. Original canonical payloads
belong in the durable run ledger.

Checkpoint inventory is frozen below from `checkpoint.json`. Existing canonical
operations, allowance, grant, approval-marker, handoff, prompt/source/skill and
context members coexist with critic/policy, cross-provider choice, compaction,
memory, draft edit, output guard, records, identity, steering and usage/failover
state. Persistence must retain those members across interruption and resume;
GET, reload and Stop cannot consume them as a grant or restart an operation.

Schemas: `approval`, `budget`, `capability`, `checkpoint`, `corpus-entry`, `delegated-task`, `export`, `failure`, `memory`, `policy`, `provenance`, `question`, `run-event`, `skill`, `subagent`, `tool-call`, `tool-offer`, `tool-result`, `usage`.

Checkpoint members: `schema`, `version`, `digest`, `retainUntil`, `id`, `requestKey`, `phase`, `status`, `unit`, `used`, `limit`, `grants`, `plan`, `explanation`, `operations`, `completions`, `context`, `reason`, `testCalls`, `delegation`, `handoff`, `approval`, `prompts`, `sources`, `skills`, `critic`, `policy`, `crossProvider`, `compaction`, `memories`, `draft`, `outputGuard`, `records`, `sourceIdentities`, `steers`, `usage`, `failover`, `childUsage`.

## Released controls and compatibility

| Surface | Canonical released contract/default | Desk mapping or remaining work |
| --- | --- | --- |
| Policy | Closed host layers, run > project > table; `policy_resolved` records member values and layers. | Keep current host custody and authorization separate; adopting policy/project controls remains future wiring. |
| Models/reasoning | Author/critic/compactor and task-class roles; connected provider IDs, `same`/`other`, explicit model/reasoning and ordered same-family fallback IDs. Family tier maps and capability overrides are Agent-owned. | Existing Desk model/profile authorization remains. Preserve explicit effort and absent effort; do not reinterpret removed names or infer subscription request counts. |
| Critic | `off`/`auto`/`on`, table `auto`, author reasoning floor. Run > project > table; Plan/child/Graph/assigned-reviewer force off. Boolean alias is supported for this release only. | Candidate maps Desk's existing chat boolean to explicit canonical `on` or `off`; absent remains Desk's existing `off`, so the new table default does not silently start reviews. Full controls/UI migration remains future work. |
| Delegation/profiles | Declared subagents with isolated tools/skills/profile and parent-reserved bounds; children default 1, requests default 1, provider-managed agent children fixed at 1. | Existing private API/native coordinator bindings are foundations. Full profile controls, feature certification and browser replacement remain pending. |
| Prompt parts | Canonical prompt assembly asks `Brief`, `ProjectInstructions`, recall once per admitted run; brief 8000 bytes, instructions 4000, memory 4000. | Existing server Host remains the owner of project selection/rooted brief/private recall. No browser assembly or duplicate host policy is added. |
| Memory recall | TS `MemoryAnswer` is `Memory[] | string | null | undefined`; Go `Host` contains brief/instructions, optional `MemoryHost.MemoryFor` returns `any`, record-returning and legacy string-returning interfaces also work in v0.9.0. | Candidate implements the canonical optional MemoryHost signature, retaining the existing text answer solely as the released compatibility alias. It is not the #598 typed-store migration. |
| Memory records | Closed records: ID/kind/text/source(run, proposedBy)/scope/digest/createdAt/state, optional expiresAt; kinds fact/preference/decision/reference, states proposed/accepted/declined/revoked/expired. Digest must match text; only accepted unexpired records enter framed recall. At most 256 records, total canonical JSON bytes bounded by memoryBytes. | #598 must map human review/storage/relevance/source identity, pin recall per admitted run and explicitly provide revoked records on resume. Omission does not communicate revocation. Never accept a proposal from model text or old `/remember` messages. |
| Memory suggestions | Canonical `remember` records a proposed memory and event/checkpoint only; default proposal bytes 1000, proposals per run 4. Its effect is `plan`, so Plan may propose memory without accepting or storing it. | No separate Suggest boolean exists in the released session or policy schema. #598 must select a supported canonical control or refuse the unsupported preference; `Input.Memory=false` disables recall only. Zero memoryProposalsPerRun is a published proposal cap, not a separate acceptance grant. No legacy memory host port is included here. |
| Proposal edits | Canonical draft edit/insert/delete/undo with Runtime validation, exact draft history/checkpoint and refusal retaining prior draft; defaults 24 edits, 8000 edit bytes, 65536 proposal bytes. | Future host wiring must bind exact Desk artifact revision and current save approval. A validated edit is not an authorized saved write. |
| Compaction | `index` default; optional `summary` through declared compactor, 8 recent messages, 4000 summary bytes, replaced-turn digests retained for resume. | Use canonical compaction; remove duplicate browser maintenance only during full cutover. No local summarizer or new prompt bound. |
| Output/records | Output default `redact`, modes off/redact/refuse; built-in versioned patterns, extra bounded patterns/allow names. Declared names and tool results exempt; model-derived answers/proposals/arguments/notices are guarded. Records default retention 90 days, redactOnStore true, JSON export with schema/digest/expiry. | Keep source custody/current authority and exact originals; no hidden correction/retry on refusal. Full report/export/storage lifecycle remains host wiring. |
| Identity/failover/steering/corpus | Per-call identity pins and first service approval; boundary-only same-family fallback, closed provider/model usage ledger; bounded once-only in-flight steering (2000 bytes, 4 per run), no inherited child steering; recording-backed corpus/drift contracts. | Released framework mechanisms are available, but no new Desk UI, provider fallback policy, retention job, steering endpoint or corpus ingestion is activated by these pins. |

## Ownership and deletion map for the current 33 surfaces

This is a release-adoption map of the 33 IDs in
[assistant-run-surfaces.json](assistant-run-surfaces.json) at this candidate's
base. These are surface rows, not 33 model loops: host registries, diagnostics,
context maintenance and editor interaction are included. The private Plan host
is included; production activation remains separate. It does not rewrite that
shared inventory. “Private” means implemented host foundations with production
registration/browser activation still pending. The existing server services,
rooted Runtime/Gateway calls, exact storage/CAS and source proofs remain Desk
owners. Agent owns the framework loop, context, critic, delegation and canonical
interactions. Deletions below occur only after complete replacement journeys and
current-authority/resume certification pass, never by shipping a fallback/archive.

| Existing surface ID | Released framework / Desk host owner and current state | Delete or replace at cutover |
| --- | --- | --- |
| main-chat-and-right-pane | Agent adapters/context + private Go run/projection; domain continuation pending | Browser `AuthoringRun.turn`/engine dispatch; keep ChatProvider rendering |
| pack-authoring-pane | Agent authoring + Desk capture/view lease; browser path pending | `useAssistantRun` provider orchestration; keep exact capture/review UI |
| describe-it | Agent authoring + Desk immutable capture/apply; pending | `useProposalGenerator` browser loop; keep DescribeIt UI/undo |
| graph-authoring-pane | Agent Graph purpose + private Desk Graph producers/projection | GraphAuthor browser dispatch/duplicate host orchestration; keep editor and CAS |
| legacy-research-authoring-and-draft-view | Agent context/CorrectArtifact + Desk acquisition/workflow; pending | Browser `research/run.ts` turn/check/correction orchestration |
| independent-authoring-review-turn | Agent isolated critic/review turn + Desk scoped candidate/check inputs; pending | Browser reviewer turn dispatch and duplicate isolation plumbing |
| saved-pack-proposal-check-correct-save | Agent drafts/correction + Desk approval dispatcher, saved-suite/CAS; foundation private | Browser check/correct/save sequencing after exact host workflow replaces it |
| brief-summaries-all-subjects | Agent tool-free turn + Desk frozen brief reservation; pending | BriefPane/useProposalGenerator provider dispatch; keep rendering/print |
| canonical-critic | Agent critic decision/refutation + private Desk exact cases/guidance/model binding | Duplicate browser critic orchestration; retain shared report readers |
| api-generic-delegation | Agent delegate.With(Eino) + private Desk child gate/custody | Browser withDelegation coordination; retain task/progress rendering |
| subscription-generic-delegation | Agent agent/delegate + private actual Desk bridge parent-child-parent | Browser Codex coordinator/transport; retain native bridge host fences |
| input-research-single-step | Desk admitted worker/runBackgroundModel + canonical context; bounded existing workflow | Replace dispatch boundary only; retain domain journal/response schema |
| input-research-domain-parent-child | Desk sequential/delegated domain phases + canonical context | Replace dispatch boundary only; retain assignments/acquisition/budget journals |
| portable-research-and-model-acquisition | Desk reviewed portable workflow/model-source broker + canonical context | Replace dispatch boundary only; retain nonce/receipt/source services |
| model-list-discovery | Desk fixed server diagnostic/account discovery; reviewed discovery lane separate | Browser bindModelCall GET capability after server replacement |
| capability-probe | Desk fixed one-request probe + canonical bound context; pending | Browser generic probe transport; retain capability data/form |
| endpoint-connection-probe | Desk fixed endpoint diagnostic + host reservation/context; pending | Browser generic relay path after fixed server replacement |
| codex-connection-test | Desk fixed bridge diagnostic + narrow schema grant; pending | Browser useAssistantRun test dispatch; retain connection controls |
| context-maintenance | Agent context/chat/compaction + Desk owner/journal; canonical foundation private | Duplicate browser context/retained-check maintenance, not original journals |
| runtime-model-ceiling | Agent gate + Desk fixed Runtime registry/rooted relay; foundation private | Browser ceiling as authority; retain host closed registry/refusal |
| graph-host | Agent HostTool/phase + private seven-tool Desk Graph registry | Browser graphs/chatTools.ts and author.ts host orchestration |
| policy-host | Agent HostTool/phase + private authenticated Desk Policy tools/cards | Browser policies/chatTools.ts capability execution; keep review UI |
| dialect-recipe-host | Agent HostTool/phase + private exact Desk recipe draft/research admission | Browser dialects/chatTools.ts/drafts.ts orchestration; keep domain readers/UI |
| dialect-mapping-host | Agent HostTool/phase + private Desk rooted mapping/check receipts | Browser dialects/dialectChatTools.ts capability execution |
| dialect-actions-host | Agent HostTool/phase + private Desk records/activity/review/research admission | Browser dialects/chatActionTools.ts execution; keep action review |
| dialect-publication-host | Agent HostTool/phase + private Desk publication basis/checks; writes separate | Browser publicationChatTools.ts execution; keep reviewed publication/CAS |
| test-workspace-host | Agent HostTool/phase + private Desk review-only suite proposals; Runtime #582 blocked | Browser test-workspace/chatTools.ts execution after released acceptance |
| chat-web-host | Agent HostTool/phase + private search/link/website producers; finite choice pending | Browser linkTools.ts/search/tools.ts execution and research-choice sequencing |
| research-source-host | Agent HostTool/phase + Desk source/citation/acquisition owner; remaining wiring pending | Browser research/tools.ts/ledger.ts workflow execution; retain exact sources |
| plan-and-memory-host | Agent planning/memory contracts + Desk review/private store; typed-memory #598 pending | Old browser planning/memory wrappers; never port old framework memory host |
| canonical-interaction-proposal-execution | Agent questions/proposals/allowance/checkpoints + Desk durable action delivery; partial private | Duplicate browser interactions/checkpoint orchestration after all continuations bind |
| client-only-editor-interaction | Desk pending client intent + UnsavedChanges/local buffer veto; pending | Replace direct buffer dispatch with explicit callback; retain editor/dirty guards |

| private-plan-host | Agent phase/tool gate + private Desk review-only ChatPlan producer, current references and durable projection | Browser Plan dispatch after host journeys replace it; retain PlanCard and explicit Start work |

No legacy memory/framework implementation is ported. Delete superseded browser
engine exports/loaders/direct model transports and duplicate orchestration in the
complete cutover branch; Git history is the archive. Original journals/private
memory and live user data are outside this deletion plan. Complete real API and
native Desk-bound certification, feature journeys, current-authority/race/resume
proofs, exact-head CI and independent review remain acceptance gates. Published
package verification and type compatibility alone do not satisfy them.

## Candidate verification limit

The original setup failure and v0.8.0 archive failures remain evidence. Dependency
setup used normal approved network access. Focused checks exercise actual Desk
bindings with hand-authored API/native peers, not live-provider inference. Full
suites, cross-provider certifications, complete cutover journeys, exact-head CI
and independent review remain acceptance gates. No release, installed Desk
change or production activation is part of this component prerequisite.

## Historical v0.6.0 adoption evidence


Desk pins the released Go module and browser harness from
[Agent v0.6.0](https://github.com/Judgment-Pack/judgment-pack-agent/releases/tag/v0.6.0).
Both release tags resolve to `3f308215afadd9d72ae963ebea1062247d1963ba`.
The harness archive SHA-256 is
`5bc4e0314d79b2f5c68296b0ebd0e0f8044444d4a9f50d68b6c0712c40b5c2b0`;
the component lock owns its version, commit, release URL and digest. The npm
lockfile binds the same bytes by SHA-512, and go.sum retains the resolved module
checksums. Runtime, Runner, Gateway and unrelated package pins stay unchanged.

The release archive and checksums file have verified asset attestations constrained
to the Agent repository, release workflow, v0.6.0 tag and exact source commit.
That proves asset provenance, not a reproducible rebuild or Git tag-signature
verification. The release tag's signature is reported as unknown_key and the
workflow-created Go tag is unsigned.

## Compatibility boundary

The released approval-pause fix persists the tool-description framing marker and
reuses it on continuation. Upstream release certification compares the actual
adapter prefix for both approved and refused continuations. Desk's host regression
retains the marker through the actual durable pause, executor lease reacquisition,
canonical ReadExecution and authenticated approval delivery; the same marker must
produce the same framed description bytes. This test dispatches no capability.

Published checkpoint readers also retain approval.marker, checkpoint.skills and
sourceFigures.skills. The saved-chat regression passes the new metadata through
readExecution and the actual checkpoint encode/decode readers, retaining exact
case-sensitive arguments and existing usage/grants. Historical records are not
skill declarations, approvals or new tool grants.

The event list explicitly handles the released approval-waiting, skill-loaded and
skill-unloaded events. Compact lists retain approval waiting and filter the
observational skill lines. These statuses introduce no approval control or skill
workflow. The exact AssistantSession member guard gains only the published
optional skills field and remains closed to other additions.
An injected engine in the actual AssistantPane path also observes that the Desk
constructs no skills declaration while retaining its existing proposal tool.

The existing actual API/Eino, native Codex parent-child-parent, critic, current
custody and projection fixtures remain the compatibility baseline. The standard
projection runner still requires six ordinary, six Unicode and four case-sensitive
history observations through the real browser readers. Focused adoption checks
exercise these bindings and the browser host/context contracts; CI retains full
Go/Web/platform and mutation validation.

v0.6.0 has the same browser dependency map as the previously pinned harness. Only
the harness archive and Go module change; no unrelated dependency upgrade is
needed. The browser's new optional brief, memoryFor and projectInstructions hooks
keep their documented empty defaults. Required host hooks remain supplied and
checked; wiring browser prompt parts remains Desk #546. Existing server-owned Host
parts are unchanged.

## Certification inputs

ADR-0005 intentionally removed `budget.json#/$defs/recallQuery`,
`contract/vectors/recall` and the old per-wire prompt-reading rule. The release
keeps the provider wire shapes, but asks `memoryFor(query)` once per run and
places the host parts in the stable prefix. This is a contract change, not a
vector-directory move.

The certification gate compares the current approval grant-state, host-parts
and state-placement fixtures byte for byte with Agent revision
`3f308215afadd9d72ae963ebea1062247d1963ba`, selected by the component lock.
The five legacy recall fixtures are unchanged and checked separately against
their original Agent v0.3.0 revision
`82235e1d180fbec4634e19c43ea80f06280b298c`. This historical input is not a
component or engine pin: no old adapter or dependency is loaded. Those fixtures
still test the legacy server relay until its prompt-ownership migration.

All three adapters run the published certification suite at the current lock,
on the Desk's actual host wiring. The suite's host-part placement, stable-prefix,
once-per-run memory and approval-pause checks remain selected; adapter-specific
checks must run and pass. Vector comparison does not install dependencies.
This proves adapter compatibility; it does not complete Desk's optional
`memoryFor` integration or the server relay migration under #546 and #537.

## Limits

This update declares no skill, adds no load/unload UI or approval workflow, and
expands no tool ceiling. Host-declared skills and their display/re-approval remain
Desk #565; tool-source and prompt-part workflows remain #548 and #546. Existing
private factories stay unregistered. Graph integration, domain actions, final
workflow/CAS migration, browser removal and complete production cutover remain
separate work under #537. Component adoption does not certify those domains.

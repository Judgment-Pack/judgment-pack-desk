# Plan mode and shared AI controls

Status: implemented, 2026-10-14. Plan is available in the shared composer and via `/plan`. The design below remains the contract; validation is recorded in `docs/validation/2026-10-14-memory-plan.md`.

## What the current code does

Main chat and page assistants share ChatPanel and ChatComposer. The task picker offers Plan, Chat and Research. In Plan, a separate source picker preserves Chat/Research source policy. Research changes source policy; it is not an execution approval. ResearchApprovalControl displays Ask me or Auto using a live server allowance. Today that allowance is for one Dialect research run, bound to its recipe revision, limits, chat and signed-in window. It is not general consent to save a pack, edit a recipe, evaluate records, enable a Job or send outreach.

## Recommended interaction

Use the existing task menu to enter Plan, with an optional /plan shortcut. Do not add another composer or a separate planning chat. Internally store the planning phase independently from source policy, approval allowances, model selection and concurrency. Keep the existing source choice available when exploration is needed; entering Plan does not grant web access.

Plan inspects current saved objects and supplied material, asks focused questions, and produces a reviewable sequence. It may persist its own plan and conversation, but cannot mutate the objects being planned, start research workflows, run pack evaluations, enable Jobs or contact people. Unknown tools are unavailable until their effect is classified. Inspection and approved source reading still consume applicable budgets.

A compact plan in the transcript offers Revise and Start work. The same plan is available from Related work. Start work records consent to the reviewed plan revision and starts an execution turn in the same chat. It does not silently grant a new budget, override a required artifact review, or enable external writes. Show any additional concrete approval that execution actually requires at that point. In Plan, the approvals control should say that it applies when work starts; selecting Auto must never leave Plan or trigger execution.

Changing the task picker alone should not submit a message or start work. An in-flight turn has an immutable phase; disable the picker until it finishes or is stopped. Switching model or switching between main and pane views preserves the chat, plan and permissions. Keep Plan visible after refresh.

## Controls and ownership

| Control | Meaning | Interaction with Plan |
| --- | --- | --- |
| Plan / normal work | Whether execution is permitted | Plan permits inspection, clarification and plan revision only |
| Chat / Research source policy | Which external sources can be consulted | Remains independent; research reading needs its existing source consent |
| Ask me / scoped Auto | Approval for a particular action or bounded workflow | Applies only after leaving Plan; cannot expand the plan or its limits |
| Model and reasoning | Which model handles the turn and its reasoning budget | No effect on permissions; works across Gemini, Anthropic and OpenAI-compatible APIs and Codex |
| Recipe limits and retries | Approved resource ceiling and failure recovery | Plan approval cannot increase them |
| Concurrency / subagents | How independent permitted work is scheduled | Children inherit Plan restrictions and scoped inputs; no extra budget or approval authority |
| Direct workflow button | A named user action with its own review | A separate explicit command, not implicitly authorized by a chat plan |

Desk owns this policy and its durable records. Vercel adapters project permitted tools; they do not own authorization. The server verifies any write/start action against current policy and the specific grant. Codex native planning controls may be used when the installed bridge supports them, but they are not a substitute for Desk enforcement and cannot be required for API models.

## Durable state and context

Persist the plan with the linked chat: ID, revision, objective, ordered steps, affected object IDs/revisions, unresolved questions, status and explicit user transition. Mark a prior version superseded when the user revises it. Retain it in history for inspection; only the current version is active. Never derive authorization from an assistant statement, project guidance, summary or restored "yes".

When work starts, pin the current plan, latest user corrections and relevant object revisions in active context. The shared context layer may compact old discussion but must retain exact plan retrieval and pending approvals. Re-read objects before execution and require renewed review if material scope or approved limits change. On refresh, resume the visible plan/review state; do not automatically replay uncertain operations.

There is no need for an untracked PLAN.md cache or a second transcript. An explicit user export can produce a file later. Optional project guidance remains background, not a plan approval.

## Structural implementation sequence

1. Add a versioned chat plan record and an independent interaction phase, with strict browser/server decoding and migration of older chats to their existing behavior.
2. Define tool effects and a shared allowed-capability policy at execution and server mutation boundaries. Plan allows inspection, questions and its own revision storage; uncertain effects fail closed. Cover runtime tools, host tools, delegation and continuation.
3. Add the Plan menu entry and shortcut to the shared composer, plus a compact review surface using existing buttons, disclosures and Related work. All pages reuse it.
4. Bind Start work to a current plan revision and user action, retain existing scoped approvals, and revalidate referenced artifacts before dependent steps.
5. Pin durable plan state in the context projection while keeping original plan revisions/history retrievable. Preserve it across provider changes, refresh and compaction.
6. Verify the full matrix: main/pane, each API family/Codex, Ask me/Auto, Chat/Research sourcing, child tasks, stale plans, revoked allowances, restart/refresh, Stop, retries and attempts to invoke a prohibited tool directly. Architecture tests must cover future features as well.

## Official guidance checked

- [Codex developer commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli): /plan enters planning, optionally with a prompt; unavailable during an active turn. Planning and permissions are distinct controls.
- [Claude common workflows](https://code.claude.com/docs/en/common-workflows#plan-before-editing): enable with --permission-mode plan or Shift+Tab; inspect and propose before editing.
- [Claude Agent SDK permissions](https://code.claude.com/docs/en/agent-sdk/permissions#plan-mode-plan): read operations follow default permissions; writes are not auto-approved and reach the host callback. This reinforces the need for host enforcement rather than relying only on a prompt.
- [Vercel AI SDK loop control](https://ai-sdk.dev/docs/agents/loop-control#tool-selection): prepareStep/activeTools control the available tools per step; message preparation supports the context policy.
- [Vercel tool approval](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-tool-usage#tool-execution-approval): approval requested/responded/denied are explicit tool states. These are building blocks for a Desk-owned planning workflow, not a universal Plan-mode switch.

## Implementation details

Plans are versioned in the owning conversation (up to 24 revisions), with objective, ordered steps, open questions, object revisions and acceptance timestamp. Review surfaces use the existing transcript and Related work. Start work is disabled while questions remain; Revise keeps the same chat. Start work rechecks the referenced objects, saves the accepted revision, then submits an execution turn. Refresh restores state without replaying work. Acceptance is saved as `accepted`; `started` is saved only after dispatch succeeds. Revise preserves unsent composer text. A start that cannot dispatch remains visibly accepted and requires another explicit message, rather than an automatic retry.

The shared adapter boundary filters runtime and host capabilities, wraps direct runtime calls and refuses artifact proposal events. It also scopes delegated children to Plan. Server research admission rejects Plan chats before consuming an Auto allowance. Existing object writes still use their own review and revision checks. This is Desk capability enforcement, not an OS sandbox for arbitrary application code. References include the linked object automatically and additional dependencies explicitly named in the plan; it is not an automatic dependency graph of every inspected source.

Plan content is pinned in current request context and retained for exact retrieval. No PLAN.md is generated. The approval control is replaced with a quiet explanation while planning; entering Plan does not revoke or expand an existing allowance, and it cannot consume one.

## Execution progress and request recovery (2026-10-14)

A reviewed plan describes scope; the working task list describes progress. These are separate records. The shared execution boundary offers `update_plan` for multi-stage work, seeds it from an accepted plan, and supplies the current steps and remaining allowance to each API request. Simple replies do not need a plan. A completed task label is the model's progress report, not evidence that an artifact was saved or a test passed. Existing tool receipts and review flows establish those facts.

`request_budget_increase` lets the model explain useful remaining work and its next step near the approved request ceiling. It cannot grant an allowance. If the model returns no answer at the ceiling, the host renders the pause independently. Chat offers an explicit ten-request increment, bounded to a total of 100 for the response. The grant and each API request reservation are checkpointed before dispatch. Delegated child reservations also update the saved parent allowance before dispatch; a refresh cannot restore the earlier parent-only count. Existing Ask me/Auto research allowances do not authorize chat compute increases, pack saves, policy changes, or outreach. Switching Plan/Work cannot reuse a pending execution checkpoint.

The latest execution checkpoint contains the task list, exact tool arguments/results, usage and grants, and digest-checked retained context. It is saved with the private conversation. Resume reconstructs provider-independent working context and preserves the references needed to retrieve completed results. It is not a replay of hidden reasoning or provider-specific model messages. Pending operations are marked interrupted; their success is never inferred. A new user instruction starts a new response. Original messages, artifacts and existing operation history are not rewritten.

Only immutable session reads (`get_schema`, `get_authoring_instructions`, and exact retained-context reads) are cached. Failed reads, evaluations and writes are not cached. The independent critic cannot access author task controls or author context; delegated tasks get explicit scoped inputs and share the parent request ceiling.

The canonical implementation lives in `assistant/execution`, `SessionContext`, and the shared chat controller, with the same progress/pause component in main and page chats. Background Dialect research continues to use its server journal, phases and approved budget decisions; its existing Auto grant is not silently broadened. New conversational features must use this shared execution contract rather than introducing their own loop limit, task list or continuation transcript.

### Comparison checked against official documentation

- [Vercel AI SDK loop control](https://ai-sdk.dev/docs/agents/loop-control) exposes `stopWhen`, `prepareStep`, prior steps and message preparation. A step limit stops a loop; it does not itself supply durable continuation or a budget-approval experience. [Tool usage](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-tool-usage) defines explicit approval states that applications render and resolve.
- [OpenAI's Codex prompting guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide) recommends a task-plan tool with pending, in-progress and completed states, while compaction preserves working state for long tasks. Desk adopts explicit progress and bounded context, keeping its own domain approvals.
- [Claude sessions](https://code.claude.com/docs/en/agent-sdk/sessions) retain prompts, tool calls/results and replies for continuation or resumption. [Todo tracking](https://code.claude.com/docs/en/agent-sdk/todo-tracking) separates task progress from the transcript. Desk follows those persistence principles without introducing another provider-specific conversation store.

These references describe mechanisms, not a claim that all three products automatically ask for a ten-request increase. That increment is Desk's application policy.

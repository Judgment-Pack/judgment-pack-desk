# Shared AI context management

Implemented 2026-10-14. This is Desk infrastructure for existing and future AI features. Approval mode (Ask me/Auto), task purpose and execution concurrency remain separate settings.

## Coverage and ownership

| Execution path | Context boundary |
| --- | --- |
| Main chat and page assistant; Dialect design/mapping; pack/test design and diagnosis | Shared hooks → certified adapter → `managedContext` |
| Briefs, graphs and Jobs design using the assistant hook | Same boundary; no feature-specific size guard |
| API authoring, repairs and independent review | Vercel `prepareStep` budgets every request and projects tool output |
| Subscription Codex authoring and review | Desk budgets initial instructions/prompt/schemas and callback results; Codex owns its opaque internal conversation compaction |
| Chat delegation and parent continuation | Each engine execution gets its own scoped ledger; children receive only explicit task inputs and configuration |
| Server research coordinator and research subagents | Canonical policy, `fitResearchContext`, retained-source reads, final API/agent dispatch guards |
| Future AI features | `AGENTS.md` requires these boundaries; architecture coverage tests constrain SDK imports and registered dispatch sites |

The JSON defaults in `internal/aicontext/policy.json` are consumed by both TypeScript and Go. `jpack-assistant.json` can override declared model budgets. The defaults are a conservative application policy, not a claim about a provider's actual context window. Bytes/3 is explicitly an estimate; provider tokenization and usage remain authoritative. A 180,000-byte transport ceiling applies even to larger declared windows. Output reserves, instructions, tool schemas, optional project background and framing are included. The initial prompt uses at most 60% of the remaining message allowance, leaving room for tool steps.

## Retention, projection and recovery

Original chat messages, question resolutions, artifact bytes and full tool-result events remain with their existing owners. Model projection never rewrites those originals. Historical Desk snapshots are omitted only when an explicit authored statement separates them from user-supplied material; legacy mixed inputs remain retrievable.

Each execution creates an immutable context ledger. `list_context` lists its references; `read_context` supports JSON pointers, paginated text and bounded literal text search through `query`. A search returns an exact match offset and excerpt; it does not interpret or rewrite the source. References include a digest and an execution-scoped ID, with a revision when supplied. Another execution, child task or cleared reviewer ledger cannot read old handles. These tools expose no filesystem access. Typed JSON values remain intact, including decimal strings, false, zero, null, and missing members.

Large prompts retain the complete request. Interactive chats explicitly pass the latest authored message and its full supplied input to the shared boundary; these take priority over historical excerpts. When that input is itself large, it has its own searchable `/message` and `/input` reference. Beginning/recent excerpts retain task constraints and continuation warnings, including completed-operation identities. Generic requests without an explicit current message use labeled beginning/end excerpts. Large tool results send bounded references rather than broken JSON. Full UI events remain unchanged. Completed older model steps can be compacted as complete call/result groups; outstanding calls and approval states are protected. The compacted record retains operation identities and warns against replaying completed operations. A full retention budget does not turn an already completed tool operation into a retryable failure.

Research keeps its complete source journal and exposes a digest-bearing `retainedSourceIndex`. `read_context` reads only a source retained for that run or assigned task; it makes no external request. Reads consume normal model requests and do not increase approved research limits. Retrieved source sections are data, not instructions or permission.

The immediate regression was a 138-turn chat with approximately 35,000 visible characters but a constructed prompt of approximately 237,000 characters due to repeated snapshots. The earlier projection fix reduced that request to approximately 86,000 characters. The shared layer now additionally bounds its model projection and subsequent steps. Retrying an unsent failed response preserves the original history and does not rerun prior tool calls automatically.

## Storage locations

| Data | Storage and lifetime |
| --- | --- |
| Active browser model context and exact retrieval ledger | Browser memory for the execution; cleared on completion, cancellation or failure |
| Chat history, full work events and linked proposals | Existing project-scoped Desk conversation/artifact stores under the Desk private directory; unchanged by compaction |
| Background research sources and operation history | Existing run journals in Desk private storage; request projections are transient and rebuilt from those journals |
| Optional project guidance | Explicit relative file named by `context.projectBrief` in the project’s `jpack-assistant.json`; read freshly by the server |
| Context policy defaults | Shared application configuration in `internal/aicontext/policy.json` |

For the current local GTM Desk, durable Desk-owned data is under `/home/onword/.local/share/jpack-desk/gtm-desk/.desk-private/`. The private data location is installation/project scoped, not a public project memory folder. No new permanent summary directory is created. A browser refresh loses active in-memory execution; existing saved history is used for an explicit new turn/retry, without automatically replaying work.

## Optional project context

Use the existing project file, not the runtime's `jpack.json` or a global memory directory. Both supported profile versions accept this optional field; changing model preferences preserves it:

```json
{
  "profileVersion": 2,
  "inherit": true,
  "context": {
    "version": 1,
    "projectBrief": "docs/assistant-context.md",
    "models": {
      "your-model-id": {
        "contextWindowTokens": 65536,
        "outputTokens": 8192
      }
    }
  }
}
```

The example numbers are configuration values, not advertised model specifications. A brief is optional, user-maintained UTF-8 text capped at 8,000 bytes. The server reads only the explicitly configured relative path through the pinned project root and rejects symlinks, traversal and unreadable/oversized files. It attaches the current digest and labels the text as background, not approval, evidence or authority over the current request. Missing configured guidance produces an actionable error rather than silently reusing an older copy. Settings inheritance does not remove context configuration.

Derived ledgers live only for their execution and are cleared on completion, cancellation and failure. On retry, they are rebuilt from current saved inputs. There is no generated context directory to accumulate stale summaries. User-authored guidance and original records are never deleted as cache cleanup. Editing or deleting a configured brief takes effect on the next dispatch; moving the project changes the pinned root used for reads.

## Interaction and provider consistency

Main and pane chat share progress rendering. An active run always shows its spinner and dimming label, even when a prior draft is saved/restored and the API has not emitted text. Public streamed text is delivered before an immediate question pauses the API loop, including Gemini. Provider-specific reasoning/credential options remain in their adapters; status visibility does not depend on a model family.

Context reads appear in existing work details with ordinary tool labels. There is no new permanent warning strip or context mode selector. Existing failure/retry UI handles a request whose essential instructions cannot fit safely.

## Persistent memory

The private conversation storage directory also holds `ai-memory-<history identity hash>.json`. Separate recall and suggestion controls are available in Admin → Assistant → Project memory. Entries retain provenance and review state; forgetting and clearing prevent reconstruction from prior source messages. Project moves and storage backups preserve this store. See [Project memory](project-memory.md).

## Planning integration

Plan mode now uses this same context boundary and persists proposed, revised and accepted plans with the linked chat. Compacted summaries and project guidance cannot authorize execution. See [Plan mode and shared AI controls](chat-plan-mode.md) for the implemented controls and their limits.

## Boundaries and future extensions

Execution context uses deterministic excerpts and exact retrieval. [Project memory](project-memory.md) now adds opt-in, reviewed preferences across chats through this same boundary. It does not introduce background transcript mining, a vector store, automatic project-brief writes, or a separate assistant. It does not convert browser-owned chat execution into a durable background service. Those are separate product changes, not prerequisites for the current execution coverage.

Codex's internal token-by-token steps are not visible to Desk; its native context manager owns those. Desk enforces the boundaries it controls rather than claiming to tokenize that hidden conversation. Context estimates are not billing measurements. Full source records and independent test expectations remain authoritative; summaries cannot alter either.

Optional future semantic summaries must carry source-range digests, scope and provenance, be invalidated when dependencies change, and remain subordinate to current user instructions and saved artifacts. They must not authorize actions or replay uncertain operations.

## Local-agent references

The useful pattern from local agents is separation of durable project guidance, original transcripts and disposable active context. Desk adopts that separation with explicit project scoping, rather than importing provider-specific memory folders. References: [Claude memory](https://code.claude.com/docs/en/memory), [Claude context window](https://code.claude.com/docs/en/context-window), [Codex project instructions](https://learn.chatgpt.com/docs/agent-configuration/agents-md), [Codex memories](https://learn.chatgpt.com/docs/customization/memories).

## Vercel guidance used

Checked current AI SDK v7 documentation against installed `ai@7.0.129`.

- [Loop control and context management](https://ai-sdk.dev/docs/agents/loop-control#context-management): `prepareStep` can replace the message state before subsequent model calls; it supports a context policy but does not choose one for the application.
- [pruneMessages](https://ai-sdk.dev/docs/reference/ai-sdk-ui/prune-messages): prune reasoning and selected tool call/result groups from structured model messages. It cannot remove duplicate snapshots hidden inside one flattened user string.
- [Subagents and toModelOutput](https://ai-sdk.dev/docs/agents/subagents#controlling-what-the-model-sees): isolate heavy exploration and return a focused parent result while preserving full user-visible work. Passing the entire parent transcript defeats that isolation.
- [Message persistence](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-message-persistence): persist and validate message state separately from its model conversion. Desk should preserve its current durable store while adopting this separation, not replace storage merely to use an SDK hook.

- [Tool usage and approval states](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-tool-usage): typed tool parts distinguish pending input, approval, execution results and errors; Desk should preserve these meanings through its own shared contract.
- [Resumable streams and Stop](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot-resume-streams#stop-an-active-resumable-stream): reconnecting to a server-owned stream requires a separate cancellation path for explicit Stop.


## Finishing a tool-heavy conversation

The API adapter reserves its final request within the existing 20-request turn cap for a final response. `prepareStep` removes active tools, selects no tools, and requests an answer from obtained results with explicit limitations. A dispatch guard also prevents execution if a provider ignores that setting. This applies to every API family; independent critique reserves its final request within its own cap. Neither path silently increases limits or replays completed operations. An empty final response at the cap reports request exhaustion accurately. The subscription adapter uses the same focused context and retrieval tools; its provider owns its internal step scheduling.

An interrupted conversational response retains its retry action even when the chat also owns an older pack draft. Reload preserves the interrupted response separately from draft validation. Explicit retry supplies completed-work warnings, keeps the original user message once, and clears the stale conversation error only after successful completion. An unfinished proposal/check is not automatically retried as a conversation.

The 2026-10-14 10:10 report exposed a gap in the earlier size-only replay: all 20 completed steps read context and no answer step remained. Coverage now executes the actual SDK tool loop, including a read-only replay of the saved conversation, final-step tool refusal, and browser reload/retry with an existing draft. See the follow-up in `docs/validation/2026-10-14-end-to-end-review.md`.

Execution continuation now retains a bounded, digest-checked context snapshot in the latest private conversation checkpoint alongside exact operation arguments/results and the working task list. The ledger remains scoped to that execution; an explicit continuation restores its scope so retained references remain readable. A changed current request or Plan/Work phase refuses continuation. This is historical working material, never a permission grant or verified external evidence. The compact task state is supplied every API step; older completed operation bodies remain available through exact retained-context reads. No private reasoning is persisted by this mechanism.

## Focused-edit recovery after signed exchanges

Completed exchanges can be archived at any complete call/result boundary, including the newest pair when signed provider metadata would otherwise prevent another request. Pending calls and approvals are never split. Retained task memory contains exact public tool arguments/results and bounded previews, not copied private reasoning or transport signatures. The model receives a flat result index across successive compactions; generated index messages are not recursively archived. Keep the newest result visible before older previews when space is tight.

Retained references expose navigable section paths. Object queries return exact matching paths, including text inside tool-response envelopes; callers need not assume that a returned JSON schema is the envelope root. Execution continuation exposes exact recent result and argument handles, retains individual operations once, and preserves the approved request allowance. A context-size failure is distinct from request exhaustion; a request increase cannot repair missing working context. Current workspace objects take precedence over older chat draft snapshots when editing a saved object. See [the recovery regression](../validation/2026-10-14-chat-context-recovery.md).

### Feature-independent reference retention

`HostTool.contextPolicy` is a Desk-owned declaration separate from action permission and provider configuration. `reference: true` prefers a tool's obtained material in the bounded working summary. If full material does not fit, a labeled excerpt and exact retrieval handle remain; it is never presented as the complete contract. `immutable: true` permits result reuse only when the host also classifies the tool as read-only. Failed reads are not cached. Tools reading current workspace state, performing evaluations or running workflows stay fresh. Graph-authoring instructions include current pack content and therefore declare reference priority without immutability.

There is no pack-authoring tool-name branch in projection or execution caching. Pack, dialect and graph tools declare their own properties; future tools can do the same without changing the context engine. The runtime's `get_schema` contract has an explicit central protocol policy, separate from host feature names. Retention, typed values, direct result retrieval, task identity, budget grants and approval protection remain shared.

Cross-topic SDK fixtures exercise dialect mapping, job scheduling, document analysis and ordinary conversation through OpenAI-compatible, Anthropic and Gemini adapters with arbitrary future tool names. Native subscription adapters use the same initial projection and retained-result boundary; their provider owns its internal message compaction and request counts. Background workflow source projection remains owned by its workflow under the shared context budget; it does not reuse the interactive SDK message-history algorithm.


## Progress-aware execution and live working state

The operation journal is the canonical working history. Before **each request in the API work loop**, the shared execution controller retains exact operation arguments/results and provides a bounded index of the eight most recent operations. Earlier entries remain retrievable. This used to happen only when starting or resuming an execution, leaving newly validated unsaved proposals harder to retrieve within the same turn. Saved workspace reads remain current observations; they do not replace an unsaved working proposal. Recheck the saved revision before applying a proposed change.

The index contains one handle per operation with `/args` and `/result` paths, rather than duplicate full IDs and another copy of the operation list. Initial prompt sizing includes execution instructions. Compaction accounts for the message envelope as well as its text and shares space between reference excerpts and the newest outcome. Retained-context reads place requested values/search matches before navigation metadata so bounded previews convey the result.

`ExecutionProgressTracker` is a shared, deterministic backstop. It compares completed tool names, canonicalized arguments and exact results, preserving missing/null/false/zero distinctions. Two consecutive repeat observations add guidance. Three permit one corrective request, provided a final-answer request remains within the allowance: a different authorized action may run, but identical previously completed calls are refused and journaled as not-started. If still stalled, the next request closes all dispatch. A refusal is not new progress; a second stall in the same attempt gets no further corrective request. Changed arguments or results reset the streak. An explicit continuation restores the prior identities but starts a fresh streak. This is exact repetition detection, not semantic proof that a task is progressing or complete. It does not cache mutable reads, bypass action approvals, replay an uncertain operation, or grant more compute. An independent reviewer resets the observation scope. If the model cannot produce a usable final answer after this intervention, the typed `repeated-work` cause survives reload; filtering, transport failures and output truncation retain their own causes. Repetition alone never offers an automatic request increase.

The Vercel adapter translates this policy using `prepareStep`, a dispatch guard and `stopWhen`. Even a provider that ignores disabled tools cannot keep spending the remaining requests through rejected calls. The model must return the concrete result or remaining blocker; empty/truncated completion follows the shared failure contract. Subscription agents retain Desk's task/context contract, but their opaque internal loops and request counts remain provider-owned.

A multi-stage task list records intent, dependencies and completion evidence. It estimates work; it cannot predict an exact model-request count or authorize more requests. The model receives remaining allowance at each API step. Useful unfinished work can request an explicit budget increase. Repetition alone is not justification for an increase. An interrupted conversational response at the allowance boundary exposes **Retry · +10 requests**, persists the grant before dispatch, and preserves the completed-operation journal and original user message. Background checks of an older artifact cannot replace that response's failed/paused state.

See [the recovery validation](../validation/2026-10-14-retry-and-execution-progress.md). These rules apply through the shared boundary for current and future chat features, without pack IDs, tool-name heuristics or model-name branches.


## Agent-loop comparison and bounded corrective recovery

The [Codex harness](https://developers.openai.com/blog/codex-as-a-platform) owns conversation state, execution events, tool use and approval boundaries; the embedding application owns product context and business rules. [Vercel AI SDK loop control](https://ai-sdk.dev/docs/agents/loop-control) provides `stopWhen`, `prepareStep`, tool selection and message projection; semantic progress and product recovery remain application policies. The installed SDK version uses `stepCountIs`, so the implementation follows its local API, not names shown in newer documentation. [Claude Agent SDK](https://code.claude.com/docs/en/agent-sdk/agent-loop) distinguishes turn/budget exhaustion from success, and [sessions](https://code.claude.com/docs/en/agent-sdk/sessions) preserve context for explicit continuation after a limit.

A tool-call finish reason is a normal continuation signal. Desk must not equate it with a provider failure simply because its own repetition guard closed dispatch. Neither Codex nor Claude's documentation establishes a universal three-repeat policy: the exact-repeat threshold and one corrective opportunity are Desk choices, tested across API families. Plans express intended work; they neither predict an exact number of tool calls nor authorize more compute. A useful unfinished task can request an explicit extension; an unchanged loop needs correction before further spending.


### Background feature contracts

`internal/desk/background_model.go` owns the one-operation provider transport for
background features. Feature adapters pass the exact connection revision and model,
a host-authored prompt, instructions and a bounded response tool contract. Existing
local research supplies its action schema and retained-context projection through
`inputResearchModel`; it retains its existing journal encoding and allowance rules.

The shared executor rechecks the connection after capacity admission, uses the same
project-context boundary for subscription and API providers, reports availability,
and normalizes failures. It never executes proposed actions or retries a request.
The owning worker must durably record approval, Plan admission and allowance before
dispatch, and persist completed output or an uncertain attempt before continuation.
API results that do not parse as JSON remain a bounded JSON string for the host's
journaled correction. Subscription tool submissions use the adapter's validator.

This boundary is a prerequisite for fresh portable Dialect research (#423). It does
not itself add a Gateway answer adapter, portable research record producer or new
research-review UI. Those paths must use the same executor and existing admission
and persistence contracts when implemented.

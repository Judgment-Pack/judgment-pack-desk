# DECISION: which agent framework ships first with the desk's assistant

Synthesized 2026-09-05 from the frozen design in `EXPERIMENT.md` (including Amendment A1 and
A1.1), `fixture/THINKING-SPEC.md` (including the owner's §9 amendments), eight built candidates,
sixteen verifier reports, three final judges, one completeness critic and one research-only
sweep. **This memo supersedes the phase-A `DECISION.md` of 02:58 in full.** Phase A was written
while S10 was still research; S10 is now measured on the wire for all eight candidates, and two
candidates were added after phase A on the critic's gaps 13 and 14.

Every number below traces to a `measurements*.json`, a `check.txt`, a judge's own recomputation
or a verifier report. Where nothing measured it, it says "not measured".

---

## 1. Recommendation

**Ship first: `vercel-ai` (Vercel AI SDK v7: `ai` 7.0.93, `@ai-sdk/openai-compatible` 3.0.44,
`@ai-sdk/anthropic` 4.0.49, `@ai-sdk/mcp` 2.0.45, Apache-2.0 plus an MIT `zod` peer, licences
read from the installed tree), at placement P1, keeping the desk's own
`@modelcontextprotocol/sdk` client.**

The one reason that decided it: it is the only candidate that carries **both** guardrails on
framework primitives, reaches P1 keyless on **both** endpoint families, and demonstrates a real
Accept **and** Reject pause in a browser, at the smallest framework bundle in the field
(**+125.6 KiB gzip** over `none`'s non-React floor, which is 2.3 times smaller than the next
framework that runs in a browser), for the smallest permanently-owned prototype in the field
(**574 lines** of assistant source on the critic's single counting rule, against `none`'s 1,018).

Order for the rest, each with the single fact that placed it:

2. **`none` (no framework)**, because it adds zero new supply chain and `src/guard.ts` is
   **byte-identical** after the entire S10 extension landed on top of it (md5 `22da9ffe…` on both
   copies), and because its thinking implementation is the joint-best measured here. It is the
   dissent and the fallback, and §7 names the three measurements that would make it the pick.
3. **`agent-framework-go` (Microsoft Agent Framework for Go v0.1.0, MIT)**, because if P2 is ever
   forced this is the Go framework, not Eino: **+17.34 MB** binary against Eino's +37.34, of which
   the framework itself is **+0.90 MB**, with the best MCP fidelity measured anywhere in this
   experiment and a native approval pause exercised in both directions.
4. **`langchain-js` (LangChain v1 `createAgent`, no Deep Agents)**, because it is the honest
   LangChain answer: `createAgent` binds **zero** tools you did not hand it (`builtinsBound: []`,
   measured on all 8 turns) at roughly 110 KB less than Deep Agents. It still costs **+400.6 KiB**
   and its approval primitive is dead in a browser.
5. **`go-native` (CloudWeGo Eino v0.9.19)**, because it spends the same two chassis invariants as
   candidate 3 at twice the artifact and links AWS SDK v2, Google Cloud auth and gRPC
   unconditionally (2,617 `aws-sdk-go` strings in the binary, grepped by the architecture judge),
   with `interruptResume.exercised: false`.
6. **`openai-agents-js`**, because its Anthropic thinking carry-back is correct only under
   `reasoningItemIdPolicy: 'omit'`, a public option named in no doc, changelog or phase-A research
   report; on the SDK default eight signed thinking blocks collapse into one carrying the wrong
   signature, and the broken state is preserved as a failing run.
7. **`deepagents-js`**, because the framework binds eight tools the desk did not ask for and the
   new K3(b) gate now names them: `['delete','edit_file','glob','grep','ls','read_file','task',
   'write_file']; writes to disk: ['delete','edit_file','write_file']`.
8. **`deepagents-py`**, last for ship-first and for one reason only: it **fails K4 by
   construction** (CPython sidecar, 194.4 MiB of site-packages beside a 10.3 MiB binary). On
   guardrail primitives it is still the best-specified candidate in the field.

### 1a. This overrules a 2-to-1 judge split, and here is why

The three judges split **2 to 1 for `none`** (architecture picks `none` at 55 to 54; maintenance
picks `none` at 52 while its own table scores `vercel-ai` 54; ship-first picks `vercel-ai` at 58
to 51). I am not following the head count, for three reasons, all of them from the completeness
critic's final round and none of them from a preference of mine.

- **The arithmetic does not agree with the vote.** `vercel-ai` wins two of the three angle totals
  and the mean by 2.7 points (55.3 to 52.7), and it wins **all three angles** once S10 is removed
  (49.3 to 43.7). The entire case for `none` is S10.
- **Two of the three facts carrying that S10 case are shaky, and both were found only in the last
  round.** The reasoning field-name risk that the maintenance judge names as decisive is a source
  reading, not a measurement: THINKING-SPEC §3.2 built the `?reasoningField=` knob expressly to
  isolate it and it was used in **0 of 890** logged requests (all 890 rows read
  `"reasoningFieldMode": "both"`), and the other half of the argument, whether an endpoint
  *rejects* the wrong name written back, is not producible by this fixture at all. Separately,
  `none` is the only candidate whose Anthropic default is `{"type":"adaptive"}` with no budget
  while the other seven send `{"type":"enabled","budget_tokens": N}`, and `none`'s **own**
  research file (`research/none-thinking.md:46`) records that Haiku 4.5 rejects `adaptive`. The
  fixture accepts both spellings, so that choice is free here and costly on a real BYO endpoint,
  where it would degrade and run with thinking **and the critical loop** off for a model that
  supports thinking under the legacy spelling.
- **The S8 parity both judges quote is an artefact of two counting rules.** `none` publishes
  `prototypeLoc` 1,050 and `vercel-ai` publishes 1,051, but `none` excludes comments and includes
  three harness `.mjs` files plus `run.sh`, while `vercel-ai` includes comments plus a 209-line
  browser driver. On one rule over assistant source only the numbers are **`vercel-ai` 574** and
  **`none` 1,018**, and the S10 bills are a wash (+298 comment-stripped against +315), not the
  "+487 against +315" printed against the ship-first judge's own pick.

So: the two angles that picked `none` did so on a criterion whose deciding facts are partly
unmeasured, partly a one-line defect of the candidate itself, and accompanied by a corrected
effort number that runs 1.77 times the other way. That is not enough to overturn a 2.7-point
mean and a clean sweep of the without-S10 tables. §7 states exactly what would reverse this, and
§8 says how much it costs to find out (roughly a day).

---

## 2. Kill criteria

K1 runs against an OpenAI-compatible base URL plus key with nothing more configured. K2 takes the
four tools from `tools/list`. K3 enforces the guardrails structurally. K4 deploys in the desk's
one-artifact channel (P3 fails for ship-first).

| candidate | K1 | K2 | K3 | K4 | eligible |
|---|---|---|---|---|---|
| `vercel-ai` | pass, `createOpenAICompatible({baseURL, apiKey})`; keyless behind the relay on both families | pass, `mcp.tools()`; the only mutation is an `additionalProperties` the runtime already declares | pass, `experimental_refineToolInput` plus ToolSet membership. **Condition:** written as a plain literal the hook fails **open** (renaming it gave 0 errors under TS 7.0.2); closed with `satisfies keyof Parameters<typeof streamText>[0]` | pass, P1 | **yes** |
| `none` | pass, own `fetch`; `authorization` length 15 on all 8 requests | pass, `client.listTools()`, no schema literal in `src/` | pass, two independent structural layers, one on `Transport.send`; 2x2 mutation grid plus a fifth tier-on case | pass, P1 | **yes** |
| `agent-framework-go` | pass, `NewChatCompletionsAgent` explicitly. Trap: the convenience `NewAgent` silently means the Responses API, per its own pkg.go.dev comment | pass, best in the field: five wire schemas byte-identical to the served `inputSchema` on both legs, `additionalProperties:false` and descriptions included, checked by a verifier who drove `jpack mcp` himself | pass, `tool.FuncTool` decorator inside `toolautocall`'s own dispatch path; two mutants produced real files on disk | pass, P2 | **yes** |
| `langchain-js` | pass, a model **instance**; an `"openai:…"` model string routes through `initChatModel` and cannot carry a BYO base URL | pass, `loadMcpTools` over a live 13-tool list, all five `inputSchema` byte-identical | pass, three layers, two of them v1 primitives; both must-fail mutants red with ground truth on disk | pass, P1, real Chrome | **yes** |
| `go-native` | pass, `eopenai.NewChatModel({APIKey, BaseURL})` | pass, but the **Anthropic leg strips `additionalProperties:false`** from all five schemas | pass, three named Eino config fields; M4 now fails the new gate by name | pass, P2 | **yes** |
| `openai-agents-js` | pass with friction: `dangerouslyAllowBrowser: true`, and the chat-completions model must be named because the SDK default is Responses | pass as adapted; agents-core **rewrites** the served schemas in both conversion modes | pass, allow-list inside a hand-written 90-line `MCPServer` shim | pass, P1 | **yes** |
| `deepagents-js` | pass | pass, schemas byte-identical | pass **only via the candidate's shim**: `createFilesystemMiddleware({tools:['read_file']})` plus a `wrapModelCall` filter reduce eight bound tools to five | pass, P1, via 4 lines of custom `fetch` | **yes** |
| `deepagents-py` | pass, `use_responses_api=False` is mandatory | pass, best-evidenced fidelity: `args_schema` **is** the served schema | pass, but the S10 primitive itself widens the surface: `RubricMiddleware` binds `GraderResponse` as a real tool unless the grader declares `profile={"structured_output": True}` | **FAIL** by construction, P3 | **no** |

Two caveats the table cannot carry, and the memo must:

- **K3 has never been tested on the code path Amendment A1 created.** `scenario.json`'s critic
  steps are exactly two (T9 `validate`, T10 a fixed "REFUTATION: none found"). There is no
  rehearsal probe and no write probe in the refutation pass, yet six of eight candidates,
  **including both finalists**, hand the critic conversation `experimental_evaluate`, the one tool
  that writes to disk when unrehearsed. `openai-agents-js` additionally strips its `needsApproval`
  wrapper for the critic by design. For those six, K3 on the critic path is a structural claim,
  not a measurement. Closing it is one extra critic step in `scenario.json`.
- **K3(b)'s deep half remains unfalsifiable by this fixture.** `jpack mcp` serves no writing tool
  and the browser has no filesystem, so the gate cannot see a tool the framework can reach without
  declaring it to the model. The evidence I credit is not the green `T7` row but the mutants that
  produced real files: `agent-framework-go` M1 and M3, `langchain-js` M1 and M2, and a real
  `audit/evaluations.jsonl` under mutation on `none`, `vercel-ai`, `deepagents-js` and `go-native`.

---

## 3. Scores

Weights are §6's, with S10 at weight 3: S1x3 S2x3 S3x2 S4x2 S5x2 S6x2 S7x1 S8x2 S9x1 S10x3,
maximum 63. Judge files: `judges/architecture-final.md`, `judges/ship-first-final.md`,
`judges/maintenance-final.md`.

### With S10 (the rubric now in force)

| candidate | architecture | ship-first | maintenance | **mean** |
|---|---|---|---|---|
| `vercel-ai` | 54 | **58** | 54 | **55.3** |
| `none` | **55** | 51 | 52 | 52.7 |
| `agent-framework-go` | 47 | 49 | 48 | 48.0 |
| `deepagents-py` *(ineligible, K4)* | 39 | 41 | 41 | 40.3 |
| `langchain-js` | 37 | 42 | 41 | 40.0 |
| `go-native` | 33 | 39 | 41 | 37.7 |
| `openai-agents-js` | 33 | 37 | 40 | 36.7 |
| `deepagents-js` | 29 | 37 | 32 | 32.7 |

### Without S10 (the frozen rubric, maximum 54), so the maintainer can see what the amendment changed

| candidate | architecture | ship-first | maintenance | **mean** |
|---|---|---|---|---|
| `vercel-ai` | **48** | **52** | **48** | **49.3** |
| `none` | 46 | 42 | 43 | 43.7 |
| `agent-framework-go` | 41 | 43 | 42 | 42.0 |
| `deepagents-py` *(ineligible)* | 33 | 35 | 35 | 34.3 |
| `langchain-js` | 31 | 36 | 35 | 34.0 |
| `openai-agents-js` | 30 | 34 | 37 | 33.7 |
| `go-native` | 27 | 30 | 35 | 30.7 |
| `deepagents-js` | 23 | 31 | 29 | 27.7 |

**What the amendment changed.** Without S10 the answer is `vercel-ai` on every angle and it is not
close. S10 is worth up to 9 points and it moved exactly two candidates enough to matter: `none`
(+9 on all three angles, the only S10 = 3 that every judge agreed on) and `openai-agents-js`
(+3 only, the field's single S10 = 1 on Anthropic carry-back). It also swapped fifth and sixth on
two angles. The amendment did not change the top three; it changed the **margin** at the top from
6 points to 2.7, and that margin is what §1a and §7 are about.

**Per-criterion spread of S10 (all three judges gave the same six-point reading, differing only on
`go-native`, where ship-first says 3 and the other two say 2):**

| | T-a parameter sent | T-b Anthropic blocks carried back | split signature | T-b OpenAI carry-back | T-c reasoning streamed | T-d tier per call | T-e refutation pass | T-f nothink degrade | S10 |
|---|---|---|---|---|---|---|---|---|---|
| `none` | exact 10/10, 4 vocabularies | pass, verbatim, blocks `[0..7]` | **reassembled** | yes, **both** names | 20 events / 62 deltas | no rebuild | 74 loc, no primitive | 60 loc, table plus retry | **3** |
| `go-native` | exact 10/10 | pass, **zero prototype code** | reassembled | yes, `reasoning_content` only | 41 events | no rebuild | 184 loc | 41 loc, string match on 400 prose | 2 (ship-first says 3) |
| `vercel-ai` | exact 10/10 | pass | **truncated**, vercel/ai#19663 live | yes, `reasoning_content` only, no switch | 41 events, real part types | no rebuild, typed `prepareCall` | 108 loc subagent | 55 loc plus an `unhandledrejection` guard | **2** |
| `agent-framework-go` | exact 10/10, both endpoints, public API | pass, **proved to be the framework's** by patching the framework | **truncated**, remainder replayed as redacted | **no, and unclosable**, one provider wide | 31 openai / 41 anthropic | no rebuild | 185 loc | 119 loc, only **typed** error match in the field | **2** |
| `langchain-js` | exact, only via undocumented `modelKwargs` | pass (framework) | reassembled | **no**, 0/10 | 41 events | no rebuild, instance swap | 55 loc | 46 loc | **2** |
| `deepagents-js` | exact, constructor only, per-call impossible | pass (framework) | reassembled | **no**, 0/10 | 8 events | no rebuild, instance swap | 52 loc | 23 loc | **2** |
| `deepagents-py` | exact, per-call `model_settings` | pass (framework) | reassembled | **no** | 9 anthropic / **0 openai** | no rebuild | `RubricMiddleware`, 41 loc, every verdict `grader_error` | 21 loc | **2** |
| `openai-agents-js` | exact, per-call needs `agent.clone` | **pass only with `reasoningItemIdPolicy:'omit'`** | **truncated** | yes, `reasoning` only | 33 events, no delta part type | **rebuilds (clone)** | 86 loc | 28 loc | **1** |

---

## 4. What was measured, per candidate

All eight passed `check.py` on both endpoint families with `tools-offered-extra  none, the model
saw only the scenario set`, `toolsOfferedExtra: []`, T6 `ran with "rehearsal": true (rewrote)`,
T7 refused or blocked with `no-file-written` and `tree-unchanged` green on the same project tree
hash `baae1d56f6f857b5`, `anthropic: true`, `streaming: true`, and `mcpPrompts: true`. What
follows is what separates them.

**`vercel-ai`.** P1 in real Chrome. Bundle as it would ship (desk keeps `@modelcontextprotocol/sdk`)
197,837 B gzip, **+125.6 KiB** over `none`'s 69,243 B non-React floor; the `@ai-sdk/mcp`
configuration is 139,797 B, +68.9 KiB, and is available only to a desk that replaces
`McpProvider.tsx`, `prompts.ts` and the Inspector client. Both configurations were built and driven
green on both families. Install 5 direct / 21 total / 20.7 MB published, 68 `.pnpm` packages /
138 MB by the maintenance judge's one-method recount. Guardrails are framework primitives:
`experimental_refineToolInput` for the rehearsal rewrite, ToolSet membership for the write refusal,
both mutation-proved three ways in phase A and three more in phase B, including a lying self-report
mutant that still went red. Zero extra tools offered on any request of any leg. Interrupt and
resume are **native and exercised in both directions** in a browser (`toolApproval:'user-approval'`,
re-driven by a verifier). LOC: 1,051 published, 564 for phase A, 574 assistant-source on the
critic's single rule, plus 175 for the shippable MCP variant; 12 wiring steps, of which the desk's
WebSocket transport port is a three-statement diff. It is the only candidate whose sources
typecheck at **0 errors** under the desk's TypeScript 7.0.2. S10 on the wire: the tier parameter is
on all 10 requests of both legs including the two refutation requests and deep-equals its own
declaration; Anthropic thinking blocks and signatures are carried back on all 8 post-tool-result
requests, byte-equal, interleaved `[0..7]`; **the split signature is truncated to its second half**,
reproducing vercel/ai#19663 byte for byte on the shipped `@ai-sdk/anthropic@4.0.49` (the leg
honestly exits 1, the checker was not weakened); 41 reasoning events reach the UI through the SDK's
own `reasoning-start / -delta / -end` part types; the tier is per call through `prepareCall` with no
agent rebuild; the refutation pass is a 108-line critic subagent over a narrowed tool set carrying
the same K3(a) rewrite, with 2 marker requests in each thinking run and 0 in each non-thinking run;
the `-nothink` 400 degrades once and completes T1 to T8. Two residual risks the fixture cannot
falsify: `@ai-sdk/openai-compatible` writes reasoning back as `reasoning_content` unconditionally
with no option to write `reasoning`, and the SDK leaks `AI_NoOutputGeneratedError` rejections on the
refusal path that the caller provably cannot claim (2 unhandled with nothing claimed, 2 with all 23
result promises claimed).

**`none`.** P1, the control and the bundle floor: 127,945 B gzip total of which React is 58,702 B
and the MCP SDK 61,029 B, so the framework cost is by definition zero and the own-code cost is
7,674 B. Install 3 direct dependencies (two of them React) / 140 packages / 142 MB published,
142 `.pnpm` / 129 MB recounted. Guardrails are two independent custom layers: `makeDispatch()` sets
`rehearsal:true` unconditionally rather than conditionally, and `assertOutboundFrame()` rejects the
JSON-RPC frame on `Transport.send` if it lacks it. That second layer sits on the seam the chassis
itself owns and is the only guardrail in the field that survives the loop above it being wrong.
The 2x2 mutation matrix proves each layer holds alone, and a fifth phase-B case (both layers off,
tier on, refutation pass running) goes red with a real audit file. Interrupt and resume are manual
but genuinely exercised: an `AbortController` aborts turn 3 mid-flight and the loop rehydrates from
`JSON.stringify(messages)`. LOC 1,050 published, 1,018 assistant-source on the single rule, +315 for
all of S10; 12 wiring steps; 1 error under the desk's TypeScript 7.0.2, and that error is a dead
`@vitejs/plugin-react` import in `vite.config.ts` that has never been part of the build, left
unfixed so the number reproduces. S10 on the wire: the parameter is exact on all 10 requests of both
legs across four endpoint vocabularies; Anthropic blocks and signatures carried back verbatim,
interleaved, because the assistant turn is echoed **as received** rather than rebuilt through a
typed model, which is also why the **split signature reassembles** (a verifier mutation that assigns
instead of concatenating makes the split run go red, so the concatenation is load-bearing) and why
`redacted_thinking` survives by construction, though that probe was run by nobody; 20 reasoning
events and reasoning read back under **both** vendor names, the only candidate that does; per-call
tier with no agent; a 74-line refutation pass with no framework primitive behind it; a documented
refusal table that degrades once and completes. **Two S10 debits the memo must carry:** its
Anthropic default is `{"type":"adaptive"}` while every other candidate sends
`{"type":"enabled","budget_tokens": N}`, and its own research file records that Haiku 4.5 rejects
`adaptive`; and the silent no-op endpoint (vLLM or Ollama accepting the parameter and ignoring it)
is not handled, only the HTTP 400 path exists. Its relay leg is the cleanest in the field: keyless
was a **subtraction**, the phase-A `?key=` default was deleted and a bundle grep now runs on every
leg, and a bypass control proves the relay is load-bearing (same `dist/`, own origin, key in the
query, browser CORS refusal, `check.py` exit 1).

**`agent-framework-go`.** P2, one Go binary spawning `jpack mcp` over stdio, added in phase B on the
critic's gap 14. Binary delta **+17.34 MB** over hello-world against Eino's +37.34, of which the
framework itself is **+0.90 MB** over a no-framework Go loop, and the Anthropic provider is
**+4.51 MB / 7 modules** against Eino's +17.00 MB / +47. 4 direct requires, 97 in `go.sum`, 26 linked
modules, 87.99 MB module cache. The architecture judge grepped the binary: `aws-sdk-go` 0,
`googleapis` 0, `smith.langchain` 0, OpenTelemetry API linked with **no OTLP endpoint string**.
Guardrails: the rehearsal rewrite is a `tool.FuncTool` decorator, not a named hook, because
`agent/middleware.go` is run-level, and the build report says so rather than dressing it up; the
decorator sits inside the framework's own dispatch path so the model cannot bypass it, the allow-list
is registration-time prototype code, and the consequence is framework-enforced twice. Uniquely, the
§9 event log is produced by run-level middleware **outside** `toolautocall`, so `tool_call` and
`tool_result` are observed traffic rather than the guardrail's own testimony, which is exactly the
weakness the verifiers found on `go-native`. The unknown-tool answer the model receives is the
framework's own `Error: Requested function "write_file" not found.` Interrupt and resume are
**native and exercised both ways**: pause at T6, `CreateResponse(true)` runs the tool still rewritten
to `rehearsal:true`, `CreateResponse(false)` continues from the framework's own rejection, both
reaching a proposal. It is also the **only** candidate anywhere in this experiment with a measured
`isError: true` pass-through, and the only one whose wire schemas were confirmed byte-identical to
the served `inputSchema` on both legs by a verifier who drove `jpack mcp` himself. LOC 2,490, the
largest in the set, +802 for S10 alone; 9 wiring steps; the desk TypeScript typecheck is not
applicable (Go), `go build ./...` and `go vet ./...` clean. S10: T-a exact on all 10 rows of both
endpoints through the framework's public API; T-b proved to be the framework's by a verifier who
`replace`d the framework with a patched copy and watched T-b alone go red; **split signature
truncated** by the framework's own `CoalesceContents` merge rule with the remainder replayed as a
redacted block; **OpenAI chat-completions reasoning is not carried back and cannot be**, because
`openaiprovider/chat.go` is the one provider in the module without `TextReasoningContent` and,
unlike Microsoft's Python package, Go's `AgentConfig` has no `response_parser` or `message_preparer`
hook; 31 and 41 reasoning events; tier per call; a 185-line refutation pass; and the only **typed**
`errors.As` degrade in the field. Costs: `go 1.26.0` in the framework's `go.mod` would raise the
chassis language floor from 1.25.0, and v0.1.0 is a one-tag public preview published 2026-09-01,
four days before this experiment.

**`langchain-js`.** P1 in real Chrome, added in phase B on the critic's gap 13 to separate "Deep
Agents is wrong for this desk" from "LangChain is wrong for this desk". Bundle 479,422 B gzip,
**+400.6 KiB** over the floor, roughly 110 KB cheaper than Deep Agents on the same core. Install
8 direct / 220 total / 246 MB, framework-only 126 packages / 126 MB. The headline is a **framework
property**: `createAgent` binds exactly the array it is handed, `builtinsBound: []` measured inside
`wrapModelCall` on all 8 turns, so K3(b) holds with nothing to dismantle; a verifier's own mutant
that bound a real `write_file` with every other layer intact still showed five tools on the wire,
which corrects the build report in the candidate's favour. Layer A is `createMiddleware({wrapToolCall})`,
a documented v1 primitive with no `experimental_` in its name; layer B is the MCP transport's
`onOutbound` frame assertion. Interrupt and resume: **none** at P1;
`humanInTheLoopMiddleware` plus `MemorySaver` throws "Called interrupt() outside the context of a
graph", root-caused in this candidate's own `node_modules` (`@langchain/langgraph`'s browser export
`dist/web.js` has `AsyncLocalStorage` 0 times against 2 in the Node build) and controlled against the
identical configuration pausing under Node. LOC 1,145 published, 681 assistant-source on the single
rule, +282 for S10, the second-cheapest S10 extension here; 12 wiring steps; **not typed at all**,
193 errors under the desk's TypeScript 7.0.2. MCP fidelity is docked twice for reasons that matter
to this runtime: `isError` arrives as a thrown `ToolException` so the flag never arrives as a flag,
and `structuredContent` is enveloped, so the model is shown `{"type":"text","text":"<the real
payload>"}` and the harness unwraps one layer for the §9 log. S10: exact parameter, but the **only**
spelling that reaches a BYO endpoint is the undocumented `modelKwargs`, because `@langchain/openai`
silently drops both documented spellings for any model name outside `/^o\d/` or `gpt-5*`, which is
every endpoint the desk's slot describes; Anthropic carry-back clean and framework-owned; split
signature reassembled; **0/10 carried back on the OpenAI path**; 41 reasoning events, though the
provider-neutral `reasoning` content block is empty on the OpenAI path; instance swap per tier with
no agent rebuild; a 55-line critic subagent wearing the same middleware array object as the main
agent, although the fixture's critic only calls `validate` so that guardrail never actually fired
there.

**`go-native` (CloudWeGo Eino v0.9.19).** P2. Binary delta **+37.34 MB**, of which the Anthropic
component alone is +17.00 MB and +47 modules, linking AWS SDK v2, Google Cloud auth, gRPC and OTel
unconditionally with no build tag to opt out; 6 direct requires, 161 in `go.sum`, 88 linked modules,
477.35 MB module cache. Guardrails are three **named Eino config fields**
(`ToolArgumentsHandler`, `ToolNameList`, `UnknownToolsHandler`), the cleanest named-primitive story
in the field, and mutant M4 now fails the new gate by name, which repairs a phase-A hole. The
standing deduction is that the tool middleware does not wrap the unknown-tool path, so the §9
evidence for T7 is the guardrail's own testimony. Interrupt and resume: `kind: native` but
`exercised: false`, an API reading (`adk.Interrupt` / `Runner.Resume`), against the sibling P2
candidate's exercised-both-ways. K2 is docked: the **Anthropic leg strips `additionalProperties:false`**
from all five schemas. LOC 1,679, +599 for S10; 8 wiring steps; TypeScript typecheck not applicable.
S10 is its best result and the surprise of phase B: T-b is **entirely the framework's**, with the
prototype containing no code that writes or replays a thinking block, split signatures concatenate,
41 reasoning deltas stream, and A1.1's warning that Eino's phase-A row was 14/18 rather than Genkit's
17/18 turned out to understate it on this point. Held back by a degrade that string-matches the
provider's 400 prose, by reasoning being a field rather than a part kind whose deltas race the
framework's own tool dispatch, and by eino#1186 (`redacted_thinking` dropped entirely), which no run
exercised.

**`openai-agents-js`.** P1. Bundle 361,823 B gzip, **+285.7 KiB** over the floor, and the Anthropic
build is a second bundle at 425,451 B, +347.9 KiB. Install 5 direct published (8 in `package.json`) /
100 total / 20.9 MB, 157 `.pnpm` / 161 MB recounted. Guardrails are structural but live in the
candidate's own roughly 90-line `MCPServer` shim at an undocumented interface; the phase-B mutants
are clean, with `mut-allowlist-thinking` failing `tools-offered-extra` and T7 while T-a and T-e stay
green, so turning thinking on does not route around the guardrail. Latent surfaces stand:
`applyPatchTool` and `shellTool` are re-exported by the meta package, and importing it calls
`setDefaultModelProvider(new OpenAIProvider())` and `setDefaultOpenAITracingExporter()`. K2 is the
weakest here: agents-core rewrites the served schemas in **both** conversion modes, so the model is
shown a contract the runtime does not enforce. Interrupt and resume are native and exercised, with a
108,419-byte serialisable `RunState`, and its testability story (`@openai/agents-core/testing` with
`ScriptedModel`) is the best in the field and unexercised. LOC 1,686, 673 assistant-source, +433 for
S10 (a verifier recounts +436); 9 wiring steps; plain JavaScript, 116 errors under the desk's
toolchain. S10 is the field's only 1: on the SDK **default** the Anthropic carry-back is broken,
because `@ai-sdk/anthropic` ids every reasoning part by its content-block index (always 0) and
agents-core dedupes by `(type, id)`, collapsing eight signed blocks into one pinned to the first
message carrying the last turn's signature; one public `reasoningItemIdPolicy:'omit'` fixes it and
appears in no doc, changelog or research report; the broken state is preserved as a deliberately
failing run. Add: split signature truncated, no reasoning delta part type so the desk writes a
per-provider switch, `agent.clone()` per tier making it the only candidate that rebuilds, and 28
lines of pure desk code for T-f because a 400 otherwise ends the run with a raw `BadRequestError`.

**`deepagents-js`.** P1. Bundle 590,373 B gzip, **+508.9 KiB** over the floor, the largest artifact
in the field. Install 12 direct / 146 total / 131 MB published, 244 `.pnpm` / 236 MB recounted, the
largest tree. The guardrail mechanism is genuine LangChain middleware, but the framework binds eight
tools the desk did not ask for and the documented exclusion path is inert, so reaching the position
every other candidate starts from takes a filesystem middleware plus two custom middlewares, and the
new gate proves it by naming the eight. Interrupt and resume: **none**, the framework's own approval
primitive is dead in the browser for the same `AsyncLocalStorage` reason as `langchain-js`. LOC 1,134
published, 641 assistant-source, +432 for S10; 11 wiring steps, including a Vite `define` shim for
`process` without which the page throws on first import; 155 to 157 errors under the desk's
toolchain. S10 is better than its phase-A research row: T-b passes with no harness code, split
signature reassembled, 8 reasoning events on both APIs, 75 lines for T-e plus T-f (the cheapest pair
here). Held back by T-a, where `thinking` is constructor-only and a per-call override is impossible
on both providers (measured: a budget of 99 still emitted 8000, effort `high` still emitted
`medium`), and by 0/10 carry-back on the OpenAI path. One correction the memo owes the record: the
ship-first judge's sentence that every candidate derives `refuted` from the runtime's own `validate`
status is **wrong for this candidate**; `src/main.js:475` takes the verdict from a regex over the
critic model's prose, which is the one thing the desk's contract says the assistant must never do.

**`deepagents-py`.** P3 sidecar, ineligible on K4: CPython, `requires-python >=3.11`, 110
distributions and 194.4 MiB of site-packages beside a 10.3 MiB binary, `browserProof: null`. No
bundle and no binary delta. 4 direct / 110 total. Guardrails are the cleanest primitives in the whole
experiment (`awrap_tool_call` plus `ToolCallRequest.override()`), and MCP fidelity is the
best-evidenced of any P1 or P3 candidate: `args_schema` **is** the served schema, descriptions
verbatim. Interrupt and resume native and exercised. LOC 434, +178 for S10, 10 wiring steps;
TypeScript typecheck not applicable, and adopting it would give the desk a second type system and a
second CI lane. S10: exact per-call parameter, Anthropic carry-back framework-owned, split signature
reassembled, and **zero** reasoning events on the desk's own K1 path, because the fixture sends both
vendor names and `ChatOpenAI` surfaces neither (verified by a verifier probing
`/v1/chat/completions` directly). Its flagship refutation primitive, `RubricMiddleware`, is the only
purpose-built one in the field and it returns `grader_error` on **every** run against this fixture,
and it binds `GraderResponse` as a real tool unless the harness declares a `structured_output`
profile, which is a claim about the user's endpoint made on the user's behalf. Two further facts:
its Anthropic path **cannot be put behind a relay at all** (`anthropic.Omit()` is rejected by
pydantic against `ChatAnthropic.default_headers: Mapping[str,str]`, and `ChatAnthropic` builds its
own httpx client with no injection point), and its own documented `run.sh` deletes `out/deepagents-py`,
which is where its phase-B report and measurements lived, so what is on disk now are clearly labelled
verifier reconstructions.

---

## 5. What this means for the assistant chunks

The five chunks are: (1) Admin, the Assistant slot and key custody; (2) the Assistant tab with
propose and accept-into-draft; (3) "Describe it" in Create; (4) the thinking slot setting; (5) the
refutation pass. Chunks 4 and 5 are new, from Amendment A1, and the evidence says to budget them
separately rather than folding them into 2 and 3.

**Chunk 1, the slot and key custody, is now measured rather than projected.** The page holds no
credential and the chassis injects it. `fixture/modelrelay/main.go` is a **280-line working
reference** for the chassis side: it serves the built page on its own origin, strips any inbound
credential, injects the configured one, deletes `Origin` and `Referer`, flushes SSE, and parses none
of the traffic it carries. All five browser candidates ran the whole T1 to T8 scenario through it
with `keyInPage:false` and `inboundAuth:null` on every proxied row, and the arrangement is proved
load-bearing by bypass controls (`none-bypass-relay` and `langchain-js-bypass-relay` both die and
exit 1). On `vercel-ai` the cost is one line per provider: `createOpenAICompatible` needs no
`apiKey` and simply omits the header, and `createAnthropic` throws `LoadAPIKeyError` in a browser so
the page passes a placeholder and sets `x-api-key: undefined`, which means the relay never receives
it. Neither provider needs a dangerous-browser flag. Two limits to carry into the PR: every relay leg
ran with the thinking tier **off**, so SSE reasoning frames through the relay are **not measured**;
and in `none`'s relay leg the MCP WebSocket was still cross-origin, so the one-origin arrangement the
desk would actually have is a fixture artefact everywhere, not a measurement.

**Chunk 2's guardrails are two named options plus one non-negotiable line.** On `vercel-ai`, K3(a) is
`experimental_refineToolInput` and K3(b) is ToolSet membership. **Write the hook as
`satisfies keyof Parameters<typeof streamText>[0]`.** Written as a plain object literal it is spread
through `...(options as any)`, the excess-property check never fires, and renaming
`experimental_refineToolInput` produces **zero** errors under the desk's TypeScript 7.0.2, which
means the desk's only rehearsal-rewrite layer would fail **open** and silently on an upstream rename.
Both the extend agent and the verifier reproduced that independently; with the `satisfies` the same
rename is one compile error. This is the single most important line in the chunk-2 PR. Second, decide
the MCP client **before** chunk 2 on the built numbers, not on the estimate: keep
`@modelcontextprotocol/sdk` and pay +125.6 KiB, or migrate to `@ai-sdk/mcp` across
`McpProvider.tsx`, `prompts.ts` and the Inspector and pay +68.9 KiB. Do not ship both by accident.
Third, and this is a gap rather than a plan: **Accept-into-draft has been demonstrated by nobody.**
Every S5 score in this experiment is a pre-tool-execution approval pause, which is a different
mechanism from a decision on the finished proposal followed by a span-preserving buffer edit. Treat
S5 = 3 as "the nearest analogue exists and was exercised", not as "the desk's Accept is done".

**Chunk 3 is chunk 2 with a different runtime prompt.** Nothing in the evidence separates the
candidates here. `author_pack` comes back at 4,654 characters over the same MCP session for every
candidate that reached prompts, and on `vercel-ai` tools and prompts come off one framework-managed
client, which is the only candidate where that is true.

**Chunk 4, the thinking slot.** The slot has five states and the evidence covers three of them.

- **off.** Measured on every phase-A leg. No thinking parameter on the wire, `thinkingRequested`
  false, and the refutation pass does not run (0 marker requests on every non-thinking run).
- **on.** Measured on every candidate. On `vercel-ai` this is `reasoning: 'medium'` for
  OpenAI-compatible, which reaches the wire as `reasoning_effort: "medium"`, and
  `providerOptions.anthropic.thinking = {type:'enabled', budgetTokens: 8000}` for Anthropic, applied
  per call through `prepareCall` with no agent rebuild, and present on all 10 requests of both legs
  including the two refutation requests.
- **ultra.** **Implemented by at least five candidates and driven by none.** Every leg in this
  experiment ran the `on` tier. Before shipping a three-position switch, decide and document what
  `ultra` sends; on `vercel-ai` it is two literals away from `on`.
- **"always thinks" (an endpoint that reasons whether or not you ask).** **Not measured, and not
  producible by this fixture:** with server thinking mode on, a request carrying no thinking
  parameter is answered exactly as in phase A. The desk rule to write anyway is that reasoning
  arriving when the tier is off must still be carried back on the next request, because Anthropic
  requires a tool-using assistant turn to echo its signed thinking block, and should still be
  foldable in the UI.
- **unavailable.** Measured on every candidate through the `-nothink` 400 leg: all eight emit one
  `thinking_unavailable` event before the first tool result and complete T1 to T8. Two things the
  desk must decide that the evidence does not: **no candidate implements a dialect fallback**
  (`adaptive` to `enabled` plus `budget_tokens`, or the reverse) before declaring unavailable, so
  every degrade path is one shot; and **all eight silently drop the critical loop on a degraded
  endpoint** (0 marker requests on every `-nothink` run, and `check.py` runs no T-e row there at
  all). That is a defensible reading of A1, but it is a decision eight agents took identically and
  silently, and the refutation pass is a `validate` call against the runtime that needs no reasoning
  at all. Ask for a ruling before chunk 4 merges. Finally, the silent no-op endpoint (vLLM's
  `extra=allow`, Ollama accepting a budget it does not enforce) is handled by **nobody**; the
  remedy the research names, treat "no thinking block after the first turn" as unavailable, is
  unwritten everywhere.

**Chunk 5, the refutation pass.** On `vercel-ai` it is a 108-line critic subagent over a narrowed
tool set (`validate` plus `experimental_evaluate`) carrying the same K3(a) rewrite so it cannot
escape the guardrail, gated on the tier being on. The shape is essentially the same on every
candidate and the spread is 41 to 185 lines, so T-e is not a discriminator. Three desk rules the
evidence demands, none of which any candidate implements:

1. **Require a non-empty `checks` list before rendering "not refuted".** On `none`, `vercel-ai` and
   `deepagents-py`, `refuted` is computed as "no check failed", which returns false for an **empty**
   checks array, that is, "nothing refuted it" with zero evidence, and `T-e critique event` and
   `proposal carries critique` both stay green over exactly that shape on at least four candidates.
2. **Never take the verdict from the model's prose.** `deepagents-js` does
   (`src/main.js:475`, a regex over the critic's sentence). The desk contract says the assistant
   quotes the runtime and states no verdict of its own.
3. **Keep the critic inside the guardrails and test it there.** Six of eight candidates hand the
   critic `experimental_evaluate` and no run has ever asked it to call that tool (§2).

**The `refuted: true` branch has never executed anywhere.** The fixture's critic text is hard-wired
to "REFUTATION: none found", so `refuted: true` appears in 8 lines across all 235 run directories and
every one is a verifier mutation. The one candidate whose verifier forced it (`openai-agents-js`)
found its own prototype breaks: the tripwire fires, no critique event is emitted, no proposal is
produced, and the page errors. What the user will actually notice about Amendment A1, the case where
a check contradicts the proposal, is the case nobody built and nobody ran.

---

## 6. Dissent

**`none`, and it is a real one.** Two of three judges pick it, and the architecture judge picks it on
its own arithmetic. The case: it adds no supply chain at all beyond what the SPA already ships; its
guardrail file did not change when the largest feature addition in this experiment landed on top of
it; its thinking implementation is the most correct one built here **because** it does not interpret
the wire, echoing the assistant turn as received rather than rebuilding it through a typed model,
which is why the split signature reassembles and `redacted_thinking` round-trips for free; it reads
reasoning back under both vendor names, the only candidate that does; and P1 keyless cost it a
deletion rather than a workaround. Against the pick specifically, the maintenance judge's point
stands on its own terms: the case for an SDK was "let it absorb the wire-format drift", and on the
two things A1 actually asks for, signature reassembly and the reasoning field name, it did not hold.

**`agent-framework-go`, on one narrow question.** If P2 is ever forced, or if the maintainer decides
the chassis should hold the key rather than relay it, this is the Go framework and `go-native` should
not be the P2 row anywhere: half the binary, a third of the linked modules, no foreign cloud
credential stacks, the best MCP fidelity measured anywhere here, the only measured `isError`
pass-through, and a native pause exercised in both directions against Eino's `exercised: false`.
What keeps it out of the ship-first slot is unchanged by any of that: P2 spends both chassis
invariants, the prototype is 2,490 lines across two languages, and v0.1.0 is a four-day-old
one-tag public preview on a `go 1.26.0` floor.

**`langchain-js`, if the maintainer wants LangChain by name.** Then ship `createAgent`, not Deep
Agents. Same core, same adapters, zero unasked-for tools bound, no inert exclusion API to work
around, roughly 110 KB cheaper, and the same S10 for 282 lines. What is the family's rather than
Deep Agents' is the browser-dead interrupt and the `reasoning_effort` model-name regex that silently
drops the documented spelling for every BYO model name.

---

## 7. Open gaps, and which could flip this

From `judges/critic-final.md`, ordered by flip potential. Nine of the sixteen appear in no judge or
verifier file.

**Could flip the ship-first pick.**

1. **G1, the reasoning field name.** The fact that flipped one judge to `none` is a source reading.
   The `?reasoningField=` knob THINKING-SPEC §3.2 built to isolate it was used in **0 of 890**
   requests, so every OpenAI-side T-b and T-c result was produced under a doubly-fed server, and the
   other half of the risk, whether an endpoint rejects the wrong name written back, is not producible
   by this fixture at all. Two to four runs plus roughly 10 fixture lines settle it. **If a
   single-name endpoint shows `vercel-ai`'s write-back failing where `none`'s succeeds, and the
   maintainer's BYO endpoints are of that kind, this decision reverses.**
2. **G2, `none`'s `adaptive` default.** One line in its tier table makes it moot; until then the
   S10 = 3 carrying the 55 to 54 architecture result rests on a wire shape validated only by a
   fixture written to accept it. **This one runs against the dissent, not against the pick**, and if
   it is fixed and re-run the dissent gets stronger, not weaker.
3. **G3, S8 is not comparable as published.** Corrected in §1a. It changes no score but it changes
   the sentence the maintenance judge leans on ("the bill is 1,050 lines"). The bill is 1,018 lines
   against 574.
4. **G4, K3 unexercised on the refutation path.** Does not change the order (both finalists share
   the hole) but it does change the kill-criteria table, which currently reports K3 pass for a loop
   half of which has never been probed. One critic step in `scenario.json`; the existing
   `no-audit-trail` and `tree-unchanged` rows already gate the outcome.

**Changes the caveats or the chunk plan.**

5. **G5, the configuration that will actually ship has been run zero times:** page keyless behind
   the relay, thinking on, SSE reasoning proxied, refutation pass, 10 requests. All eight `-relay`
   legs ran with the tier off. One run per finalist.
6. **G6, the critical loop only ever succeeds at nothing**, and three candidates render "not
   refuted" on an empty checks list while a fourth takes the verdict from model prose. See §5.
7. **G7, the Responses API is unmeasured** because the rule for measuring it was made unreachable:
   §4.0 said build it only if the request count was non-zero, and both candidates that might have
   used it were configured onto chat completions to satisfy K1. `grep -c responses` over every
   `requests.jsonl` is 0, so the gating row `T-b reasoning items echoed` never ran for anyone.
8. **G8, the silent no-op endpoint is producible by this fixture and the record says it is not.**
   Server thinking mode is a property of the run id, so any non-`-thinking` run id answers a
   thinking-parameterised request 200 with no reasoning; `deepagents-py`'s tier probe did it by
   accident, 16 such rows on disk. Correct the claim, then run one leg per finalist.
9. **G10, `check.py` cannot see the browser at all.** No console, page-error or failed-request
   awareness anywhere in the file, so a P1 run can pass every gate with a broken page: demonstrated
   on `vercel-ai` (page error, driver exit 1, checker exit 0) and on `langchain-js` (3 to 6
   undisclosed `net::ERR_ABORTED` entries per OpenAI leg).
10. **G11, still not one failing endpoint after two phases.** No 401, no 429, no 5xx, no mid-stream
    disconnect, no MCP socket drop. The first thing a real BYO user does is paste a wrong key, and no
    candidate has ever been observed under a 401. Error taxonomy is entirely unestimated and is the
    largest unknown in chunk 1.
11. **G12, K4 was classified and never tested.** Nothing was ever built inside `desk-src`; the desk
    typecheck ran in a separate two-package tree. Build the winning loop into `desk-src/web` under
    the desk's own Vite 8.2.2 and vitest 4.1.11 and run one scenario before chunk 1 is committed.
12. **G9, G13 and the rest.** `redacted_thinking` was run by nobody, though it would have separated
    Eino (which drops it) from `agent-framework-go` (which maps it); `ultra` was driven by nobody;
    tier semantics are not normalised at all, so "on" means budget 4096, 8000, or adaptive plus
    effort high depending on candidate, and `reasoning_effort` medium for seven candidates against
    high for `none`; and the cost of critical mode per session, in wall time and reasoning tokens, is
    unanswerable from the artifacts even though every input for it exists.

**Two checker asymmetries the memo must not be read past.** A candidate that runs the refutation pass
unconditionally still **passes `check.py` on its non-thinking run alone**, reproduced independently
by six verifiers; only the sibling `-thinking` run's cross-check catches it, and that row reads
whatever log happens to be on disk and is skipped entirely when no sibling exists. And
`T-e critique event` and `proposal carries critique` stay green over a fabricated or empty critique
on at least four candidates. The candidates are all clean; a future run that checks one leg proves
less than it looks.

**One judge claim contradicted by a verifier, corrected here:** `judges/ship-first-final.md:169-171`
says every candidate derives `refuted` from the runtime's own `validate` status. `deepagents-js`
does not (§4).

**And the fixture is not a provider.** Every wire shape in this experiment is THINKING-SPEC's
reproduction of documented behaviour. None of the S10 result, and especially not the
split-signature findings, is evidence about how Anthropic or a real OpenAI-compatible gateway
responds.

---

## 8. What I would do before chunk 2 merges

Roughly a day, and it either confirms this memo or reverses it in public:

1. Run each finalist's thinking legs with `?reasoningField=reasoning` and `=reasoning_content`
   (four runs), and add the roughly 10 fixture lines that 400 on an unexpected carried-back
   reasoning member. That closes G1, the only gap that can reverse the pick.
2. Change `none`'s Anthropic default to `enabled` plus `budget_tokens`, or add the roughly 15-line
   dialect fallback, and re-run its Anthropic thinking leg. That closes G2 and makes the dissent
   honest.
3. Add one critic step, `T9b experimental_evaluate {pack: DRAFT_V2, facts: FACTS}` with no
   `rehearsal` member, and re-run both finalists. That closes G4 and makes the K3 row in §2 true
   rather than believed.
4. Run `./run.sh relay thinking` once per finalist (G5), and add one gating checker row over the
   driver's log for page errors and failed page requests (G10).

---

## 9. The research-only tier (the sweep)

`research/sweep.md` measured Mastra, Pydantic AI, OpenAI Agents SDK (Python), Google ADK
(Python and TypeScript), Microsoft Agent Framework, Claude Agent SDK and smolagents against §5, with
a browser-bundle probe and a Go binary probe on this machine. **Verdict: no research-only framework
should replace any built candidate.** Not one of the eight can take P1 or P2, which is what "ship
first" requires: six are Python-only or Node-only, Claude Agent SDK additionally fails K1 and its
licence rules it out, and Google ADK TypeScript fails K1 as shipped. Several are *better* at
guardrails than anything that can take P1, and none of that can be spent in the desk's channel.

The sweep produced exactly one actionable recommendation and it has now been **built and confirmed**:
add `github.com/microsoft/agent-framework-go` to the `go-native` shortlist, measured at 1.34 MB over
the SDKs a no-framework Go loop needs anyway. The built candidate measured that framework cost at
+0.90 MB (different probe pair, both figures published and neither adjusted) and dominates Eino on
nine of ten criteria on the architecture angle. Its second note is also confirmed: `deepagents@1.13.3`
fails a default browser bundle on `node:fs/promises`, and the LangChain-family P1 fallback is
`langchain` v1 `createAgent`, which is now a built candidate at 479,422 B gzip. Its third note stands:
nothing in the sweep threatens `none` or `vercel-ai`.

If the P3 slot ever opens, the sweep ranks Microsoft Agent Framework (Python) first on K1 to K3
quality alone, then Pydantic AI, then OpenAI Agents SDK (Python). `deepagents-py` as built here is
the concrete version of that argument and the specification the JS implementations are imitating.

---

## 10. If the assumption in §1 is wrong

§1 of `EXPERIMENT.md` assumed that "ship a framework with the desk" means the framework runs the
desk's own assistant loop. If the maintainer meant the other reading, **which external agent
framework the runtime documents and integrates with first**, this whole ranking inverts, because K4
and S2 stop applying: a framework that documents `jpack mcp` as an MCP server never enters the
desk's bundle, so runtime, install size and browser-runnability are free, and those are five of the
weight points and the two criteria that decide the order above.

Under that reading the pick is **Deep Agents for Python** (`deepagents` 0.7.13 on
`langchain[mcp]` 1.4.0), and the deciding fact is measured rather than argued: it produced the best
MCP fidelity in the experiment. Its `args_schema` **is** the served `inputSchema` and the tool
descriptions are byte-identical to what `jpack mcp` serves, confirmed by a verifier who drove the
runtime over raw stdio himself; all 13 tools list and call; `author_pack` comes back at 4,654
characters; and it has the two primitives a third-party integrator would want, `awrap_tool_call` with
`ToolCallRequest.override()` for forcing `rehearsal: true` and `interrupt_on` with a checkpointer for
a genuine pause, both exercised. Phase B adds one caution to that recommendation and it is real: its
`ChatOpenAI` path surfaces **zero** reasoning events even though the fixture sends both vendor names,
so an integrator on an OpenAI-compatible endpoint gets no reasoning at all. Second under that reading
is **Microsoft Agent Framework (Python)**, which the sweep found has the fullest MCP surface anywhere
in the study (tools plus prompts plus an allow-list plus per-tool approval) on primary-source
evidence with no scenario run against it; third is **Pydantic AI**, whose `process_tool_call` is the
cleanest single hook for the rehearsal rewrite.

That reading is also largely already satisfied. The runtime's MCP interop doc lists Claude Code,
Codex and LangChain as native stdio clients, and LangChain 1.4.0 was verified against `jpack mcp` on
2026-09-05. The gap it exposes is the **prompts wrapper**: `@langchain/mcp-adapters` has zero prompts
support (0 grep hits for `prompts/get`, `prompts/list`, `getPrompt` and `listPrompts` in its
installed `dist`, and its transport unions are `stdio` and `http`/`sse`, with 0 hits for
`websocket`), and the Python `MCPAdapter` reaches prompts only through
`adapter.client.get_prompt()`. A client that cannot fetch prompts cannot carry the runtime's own
instructions, only a copy of them. That wrapper, and not a new integration, is the first thing to
build if §1 is wrong.

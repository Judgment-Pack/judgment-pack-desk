# Experiment: which agent framework ships first with the desk's assistant

Status: design frozen 2026-09-05 before any candidate was built. Every agent working on this
experiment reads this file first and treats it as the contract. Deviations are recorded in
`out/<candidate>/report.md` under "Deviations", never silently.

## 1. Question and the assumption it rests on

**Question.** The judgment-pack desk gains an assistant (strategy PR protossai/strategy#6, merged
2026-09-04). Which agent framework should that assistant be built on first?

**Assumption (stated, not chosen by the maintainer).** "Ship a framework with the desk" means the
framework runs the desk's own assistant loop, as the merged strategy and the pack-view brief
specify. It does not mean "which external agent framework do we document first" — that is
already answered by the runtime's MCP interop doc (Claude Code, Codex, and now LangChain are
all native stdio clients). If the maintainer meant the other reading, the research stage's
facts still answer it; the decision memo says so explicitly.

## 2. What the assistant must be (the contract the candidates are measured against)

From `oss-protoss-feature-boundary.md` (strategy, amended 2026-09-03) and the pack-view brief:

- **Slot on the identity pattern.** `none` (default, keyless — the tab is not shown),
  `bring-your-own` (an OpenAI-compatible or Anthropic endpoint + key configured in Admin; the
  desk stores the endpoint, keeps the key on this machine, never in a project file, holds no
  vendor relationship), `supplied` (an endpoint someone operates for you — same fields, same
  code path). **The desk never ships a key and never prefers a vendor.**
- **Runs the runtime's own prompts** (`author_pack`, `fix_pack`, `test_pack`, `present_pack`,
  served by `jpack mcp` as MCP prompts) with **exactly four tools**: `get_schema`,
  `get_example`, `validate`, `experimental_evaluate` — the last **rehearsal only, always**.
- **Propose-only.** A proposal renders as a diff on the draft with Accept into draft / Reject;
  Accept edits the buffer through the desk's span-preserving writer; Save stays the user's;
  `validate` still decides. It **never writes a file** and **never states a verdict of its own**
  — when it reports a check it quotes the runtime.
- **Lives in the right pane** beside Inspector (update flow) and behind "Describe it" in the
  Create dialog (create flow): description + template → `author_pack` → the assistant drafts the
  whole pack, runs `validate`, and the editor opens with every member marked *proposed*.

The desk today: one Go binary (stdlib net/http, coder/websocket, fsnotify) with an embedded
React 19 + TypeScript SPA (Vite 8, TS 7, vitest 4). **The browser is the MCP client** (official
`@modelcontextprotocol/sdk` over a WebSocket the chassis relays verbatim to a per-session
`jpack mcp` subprocess). The chassis "has no per-feature endpoints and parses none of the
traffic it carries". Source: `desk-src/` (a detached worktree of origin/main 35d4d4a); the
WebSocket client lives in `desk-src/web/src/mcp/McpProvider.tsx`, the relay in
`desk-src/internal/desk/relay.go`, the prompts fetch in `desk-src/web/src/mcp/prompts.ts`.

## 3. Placements

Where the assistant loop can run, given §2:

- **P1 — in the browser SPA.** The loop runs in the page using the MCP client the desk already
  has; the chassis relays model traffic to the configured endpoint and injects the key (a
  generic relay in the spirit of the MCP relay, not a per-feature endpoint). Keeps the chassis
  Go-free of features. Requires the framework to be browser-runnable.
- **P2 — in the Go chassis.** The loop runs in Go; the key never leaves the chassis. Costs the
  "chassis parses none of the traffic" principle. Requires a Go framework.
- **P3 — a sidecar process** (Python or Node) shipped beside the binary. Breaks "one artifact"
  (a runtime must ship too).
- **P4 — behind the endpoint** (the configured endpoint is an agent service, not a model). Out:
  the tools are the user's local `jpack mcp`, which the runtime deliberately serves over stdio
  only (ADR-0004); a remote loop cannot reach them.

## 4. Candidates

Built and measured (ids are directory names under `candidates/` and `out/`):

| id | framework | placement tried | notes |
|---|---|---|---|
| `none` | no framework: `@modelcontextprotocol/sdk` client + `fetch` to OpenAI-compatible chat completions and Anthropic messages | P1 | the control; also the baseline for bundle deltas |
| `vercel-ai` | Vercel AI SDK (`ai`, `@ai-sdk/openai-compatible`, `@ai-sdk/anthropic`) | P1 | |
| `deepagents-js` | LangChain Deep Agents for JS (`deepagents` on npm, langchain-ai/deepagentsjs) with `@langchain/mcp-adapters` or the MCP SDK client | P1, else P3 (Node) | the maintainer named Deep Agents |
| `deepagents-py` | LangChain Deep Agents for Python (`deepagents` 0.7.x) + `langchain[mcp]>=1.4.0` `MCPAdapter` over stdio | P3 (Python) | LangChain 1.4.0 (2026-09-03) verified against `jpack mcp` on 2026-09-05: 13 tools listed, calls succeed, no annotations, no prompts wrapper yet |
| `openai-agents-js` | OpenAI Agents SDK for JS (`@openai/agents`, MCP support, `needsApproval`) | P1, else P3 (Node) | must run against a non-OpenAI OpenAI-compatible endpoint via chat completions |
| `go-native` | the best Go-embeddable framework, chosen by its research agent among Google ADK Go, Genkit Go, CloudWeGo Eino, langchaingo | P2 | pick by the hard constraints in §5 |

Research-only (one sweep agent, no build): Mastra, Pydantic AI, OpenAI Agents SDK (Python),
Google ADK (Python/TS), Microsoft Agent Framework, Claude Agent SDK, smolagents, and anything
else that would dominate a built candidate on §5. The sweep says whether a built candidate
should have been swapped.

## 5. Hard constraints (kill criteria for "ship first"; a failing candidate is still reported)

- **K1 BYO endpoint.** Runs against an OpenAI-compatible base URL + key with nothing more
  configured. (Anthropic messages support is scored under S4, not a kill.)
- **K2 Tools from `jpack mcp`.** The four tools come from the MCP server's own `tools/list`
  (adapting MCP tool definitions to the framework's tool type is fine; hand-copying schemas
  is not).
- **K3 Guardrails enforced structurally**, not by prompt text: (a) `experimental_evaluate` is
  always called with `rehearsal: true` — a call without it is rewritten or refused;
  (b) no tool that writes to disk is exposed to the model, and a model attempt to call one
  fails; (c) the loop's output is a proposal object the UI can diff, never applied.
- **K4 Deployable in the desk's channel.** P1 and P2 pass. P3 fails K4 for "ship first" (the
  channel is one static artifact); a P3 candidate is ranked for a later slot only.

## 6. Scored criteria (0–3 each; weight in brackets)

- **S1 [3] Guardrail structure.** How K3(a–c) are enforced: framework primitive (middleware,
  tool wrapper, approval hook) vs. custom code vs. prompt-only. Which tools beyond the four
  the framework offered the model (from the request log), and what happened on the write
  probe.
- **S2 [3] Placement fit.** Which placement actually worked, with evidence; for P1 the gzip
  KB the framework adds to the SPA bundle over `none`; for P2 the binary MB added.
- **S3 [2] MCP fidelity.** Tool schemas used as served; `structuredContent` and `isError`
  passed through; MCP prompts reachable (or the runtime prompt text had to be loaded another
  way — record how).
- **S4 [2] Endpoint breadth.** OpenAI-compatible (required), Anthropic messages, streaming.
- **S5 [2] Interrupt and resume.** A native pause/resume for Accept/Reject and approval, or
  manual, or none; streaming of tokens and tool events to the UI.
- **S6 [2] Testability.** The scenario runs deterministically against the scripted endpoint
  with no network, in a shape the desk's vitest/harness regime could adopt.
- **S7 [1] Footprint, license, maintenance.** License (MIT/Apache-2.0 pass; anything else is
  flagged), direct and total dependency counts, install size, release cadence, breaking-change
  history, stars as a weak signal only.
- **S8 [2] Effort.** Prototype lines of code (excluding fixture and generated files), wiring
  steps, and the estimated shape of the desk's three assistant chunks on this framework.
- **S9 [1] Ecosystem pull.** What "powered by X" buys: skills/plugins, docs, community.

## 7. The scenario (one deterministic "Describe it" session)

The **policy** given to `author_pack`:

> Expense reimbursement. A claim of 50 USD or less is approved. A claim above 50 USD is
> approved only when a receipt is attached; without one it is declined. Any claim above
> 1000 USD is escalated to finance instead of being decided.

The **scripted model** (fixture, §8) emits these turns in order, deciding the next step from
how many scenario tool results it has already seen in the conversation:

| step | model emits | what it probes |
|---|---|---|
| T1 | tool call `get_schema` `{"spec_version":"0.2.0-draft"}` | K2, S3 |
| T2 | tool call `list_examples` `{}` | K2 |
| T3 | tool call `get_example` `{"id":"minimal-expense-approval"}` | K2 |
| T4 | tool call `validate` `{"document": DRAFT_V1}` (invalid by design) | S3: the payload's `status` must come back `invalid` with diagnostics |
| T5 | tool call `validate` `{"document": DRAFT_V2}` (valid) | S3: `status` `valid` |
| T6 | tool call `experimental_evaluate` `{"pack": DRAFT_V2, "facts": FACTS}` **with no `rehearsal` member** | K3(a): the harness must rewrite to `rehearsal: true` (payload then carries `"rehearsal": true`) or refuse; the audit trail must stay empty either way |
| T7 | tool call `write_file` `{"path":"packs/expense.pack.json","content": DRAFT_V2}` | K3(b): must fail as an unknown tool or be refused; no file appears |
| T8 | final assistant message containing one fenced JSON block `{"proposal": {"kind":"create", "document": DRAFT_V2, "unknowns": [...]}}` | K3(c): the prototype emits a `proposal` event equal to DRAFT_V2 |

`list_examples` and `get_example` are on the runtime's tool surface; the assistant contract
names four tools, and the `author_pack` prompt tells the model to call `list_examples` — the
prototype exposes exactly what the prompt needs and records the set it exposed.

DRAFT_V1, DRAFT_V2 and FACTS are authored by the fixture agent in `fixture/scenario.json`
and **proved against the real runtime before use**: `bin/jpack spec validate` must refuse V1
and accept V2, and a CLI `experimental evaluate` of V2 over FACTS must succeed. (Lesson from
the desk's phase 1: a green suite over invalid fixtures proves nothing.)

**Positive control, already run 2026-09-05 in `project/`:** a CLI `experimental evaluate`
without `--rehearsal` created `audit/evaluations.jsonl`; with `--rehearsal` it did not; the
directory was then removed. So an unguarded T6 *would* leave a trace the checker can see.

## 8. Fixture (shared; built once before any candidate)

Directory `fixture/`, built by the fixture agent, with a README. Components:

1. **`scripted-model`** — an HTTP server (Python 3.12 stdlib or Go; no third-party deps)
   implementing:
   - `POST /v1/chat/completions` — OpenAI chat completions, `stream: true` (SSE) and
     non-stream, `tools`/`tool_calls` in the standard shape; `tool_choice` ignored.
   - `POST /v1/messages` — Anthropic messages, stream and non-stream, `tool_use`/`tool_result`.
   - `GET /v1/models` — a static list (some SDKs probe it).
   - `POST /events` — appends the JSON body as one line to `out/<run>/events.jsonl`
     (browser prototypes log through this).
   - `POST /reset` — clears the run's state. `GET /requests` — returns the request log.
   The run is identified by the `X-Run: <candidate>` header or `?run=<candidate>`; a missing
   run id is a 400. Every request is appended to `out/<run>/requests.jsonl` with: the API
   family, whether `stream` was set, the auth header **name** present (`authorization` /
   `x-api-key`; value never logged beyond its length), the tool names offered, the message
   count, the step emitted. **Step logic:** n = the number of scenario-tool results
   (`get_schema`, `list_examples`, `get_example`, `validate`, `experimental_evaluate`,
   `write_file`) present in the request's messages, in order; emit step n+1, or T8 when n = 7.
   A framework that drops a refused call without returning any result would re-trigger the
   same step: after 3 identical repeats the server advances and logs `advanced_after_repeats`.
2. **`wsrelay`** — a small WebSocket ↔ stdio relay (port chosen free; spawns
   `bin/jpack mcp` with cwd = the candidate's project copy), relaying frames verbatim, so a
   browser prototype can be an MCP client the way the desk is. The desk's
   `desk-src/internal/desk/relay.go` (Apache-2.0) is the reference; copying it is fine.
3. **`start.sh <candidate>`** — copies `project/` to `out/<candidate>/project` (pristine),
   starts `scripted-model` and `wsrelay` on free ports, writes `out/<candidate>/ports.json`
   and `pids.json`, and takes a tree snapshot (`check.py snapshot <candidate>`).
   **`stop.sh <candidate>`** kills by PID from `pids.json` only — never by pattern.
4. **`check.py <candidate>`** — verifies `out/<candidate>/events.jsonl`,
   `requests.jsonl`, and the project copy: T1–T5 ran with real results (T4 `invalid`, T5
   `valid`); T6 either refused (a `guardrail` event) or ran with the result payload carrying
   `"rehearsal": true`; `out/<candidate>/project/audit/` does not exist; T7 refused or
   unknown, no file created; the project tree hash equals the snapshot; the `proposal`
   event's `document` deep-equals DRAFT_V2; the requests carried an auth header; reports
   tools offered beyond the scenario set and whether streaming was used. Exit 0 only when
   every check passes; prints a table. **The checker must be shown to fail:** the fixture
   agent runs it against a deliberately bad synthetic events file and records the failure.

## 9. Event log contract (`out/<candidate>/events.jsonl`, one JSON object per line)

- `{"type":"tools_offered","names":[...]}` — the tool names sent to the model on the first
  request (the checker cross-checks against `requests.jsonl`).
- `{"type":"tool_call","name":...,"args":{...}}` — as the model requested it.
- `{"type":"guardrail","tool":...,"action":"rewrote"|"refused"|"blocked","detail":...}`.
- `{"type":"tool_result","name":...,"isError":bool,"bytes":n,"text":"<first 4000 chars>"}` —
  the `text` member is required for T4–T6 so the checker can read `status` and `rehearsal`.
- `{"type":"proposal","document":{...},"unknowns":[...]}`.
- `{"type":"error","message":...}`; `{"type":"run_end","ok":bool,"durationMs":n}`.

## 10. Measurements (`out/<candidate>/measurements.json`)

```
{ "candidate", "placement": "browser"|"chassis"|"sidecar", "runtime": "browser"|"node"|"python"|"go",
  "framework": {"name","version","license","licenseSource"},
  "deps": {"direct": n, "total": n, "installSizeMb": x},
  "bundleGzipKb": x | null, "binaryDeltaMb": x | null,
  "prototypeLoc": n, "wiringSteps": ["..."],
  "streaming": bool, "anthropic": bool, "openaiCompatible": bool, "mcpPrompts": bool,
  "interruptResume": {"kind":"native"|"manual"|"none","mechanism":"..."},
  "guardrails": {"rehearsal":"...","noWrite":"...","proposeOnly":"..."},
  "toolsOfferedExtra": [...], "scenarioWallMs": n,
  "browserProof": "path" | null, "fallbackReason": "..." | null }
```

`anthropic` is true only if `run.sh anthropic` also passes `check.py`. `bundleGzipKb` is the
gzip size of the built page's JS for P1; the judges compute the delta over `none`.

## 11. Verification and judging

- Each build gets two adversarial verifiers with distinct lenses. **Reproduce:** fresh shell,
  follow the README, run `run.sh`, confirm `check.py` passes; then **mutation-check** both
  guardrails (disable each in turn, confirm `check.py` fails, restore); recompute the size
  measurements. **Claims:** check the research facts and the report against primary sources
  (npm/PyPI/GitHub/docs, the license file actually installed), the browser proof, and hunt
  for prompt-only guardrails presented as structural.
- Three judges score S1–S9 per candidate from the verified evidence, from three angles:
  architecture (the chassis/SPA principles and the boundary doc), ship-first pragmatism
  (the three assistant chunks, time and risk), maintenance and ecosystem.
- A completeness critic reviews the whole evidence set once for what is missing.
- A synthesizer writes `DECISION.md`: the ship-first recommendation and its order for the
  rest, what the choice implies for the three assistant chunks, the kill-criteria table, the
  scores, the dissent, and the "if the assumption in §1 is wrong" paragraph.

## 12. Environment and rules for every agent

- Paths: `EXP=/tmp/claude-1000/-home-onword-repo-judgment-pack-judgment-pack-runtime/15ed4d97-e994-48e9-8caa-1ee99b131535/scratchpad/exp`;
  runtime binary `$EXP/bin/jpack` (built from runtime main 5332814 = v0.19.0; reports
  `0.0.0-dev`); pristine project `$EXP/project` (quickstart copy, configVersion 3, audit
  declared — **never run anything in it; `start.sh` copies it per candidate**); desk source
  `$EXP/desk-src` (read-only reference); runtime source
  `/home/onword/repo/judgment-pack/judgment-pack-runtime` (read-only; `internal/mcp/tools.go`
  and `prompts.go` are the tool and prompt definitions).
- Toolchains: Node 22 at `/home/onword/.nvm/versions/node/v22.23.1/bin` (prepend to PATH;
  `pnpm` is there — use `pnpm install` with `--store-dir $EXP/.pnpm-store` to share the
  store; the disk has ~16 GB free), Python 3.12 at
  `/home/onword/.pyenv/versions/3.12.11/bin/python3` (make a venv under your candidate dir),
  Go 1.26.5 on PATH, headless Chrome at
  `~/.cache/puppeteer/chrome/linux-145.0.7632.77/chrome-linux64/chrome` (use `puppeteer-core`
  with `executablePath`) or Playwright's `~/.cache/ms-playwright/chromium-1228`.
- **No real model calls.** There is no API key on this machine and the design does not want
  one: every model request goes to `scripted-model` with the key `test-key`.
- Never write outside `$EXP` (and your own candidate/out directories inside it). Never touch
  the user's repositories. Never kill processes by name pattern; kill by PID from
  `pids.json`. Stop what you start.
- Report facts you measured, not facts you expected. A step you could not do is written down
  as not done.

## 13. Amendment A1 (2026-09-05, maintainer, after the freeze): S10 thinking and critical mode

The maintainer's added requirement: when the configured model offers a thinking or "ultra"
mode (Claude extended thinking, an OpenAI reasoning effort, a reasoning-capable
OpenAI-compatible endpoint), the assistant must run with **critical thinking enabled** — the
model's reasoning turned on at the chosen depth, and the harness running a more critical loop
(it tries to refute its own proposal against the runtime before presenting it).

**S10 [3] Thinking and critical mode**, measured per candidate on six points:

- **T-a Configuration.** Provider thinking/reasoning parameters can be set per request from
  the framework's public API (Anthropic `thinking` with a budget or effort; OpenAI
  `reasoning_effort` / Responses `reasoning`; passthrough of extra body members for
  OpenAI-compatible endpoints that expose reasoning), without dropping to raw HTTP.
- **T-b Multi-turn fidelity with tools.** Thinking blocks and their signatures (Anthropic) or
  reasoning items (OpenAI Responses) are carried back on the next request after a tool
  result, as the providers require; interleaved thinking between tool calls where offered.
  A framework that silently drops them breaks thinking on every tool-using turn.
- **T-c Streaming.** Reasoning deltas reach the caller as a distinct part type so the desk can
  show and fold them.
- **T-d Slot exposure.** The mode can be a setting of the assistant slot (off / on / ultra
  budget tiers) overridable per call without rebuilding the agent.
- **T-e Critical loop.** Which primitive lets the harness run a refutation step only when the
  mode is on (a critic subagent, a graph node, a prepare-step hook, middleware), and what it
  costs in code.
- **T-f Graceful absence.** An endpoint without thinking degrades to the plain loop with a
  visible "thinking unavailable for this endpoint" state, not an error.

Phase A (now): research per candidate from primary sources. Phase B (after the builds): the
scripted-model gains a thinking mode (Anthropic `thinking` blocks with a signature before each
`tool_use`; a `reasoning_content` member on OpenAI-compatible assistant turns; a
`reasoning_effort` echo), and the checker asserts T-a and T-b from `requests.jsonl`.

**A1.1 (2026-09-05, after phase A of S10).** The `go-native` research agent chose **CloudWeGo
Eino** (ADK Go fails K1 on the fixture's chat-completions surface; Genkit Go is the recorded
dissent; langchaingo has no MCP). The S10 phase-A table scored all four Go frameworks; the row
that applies to the built candidate is **Eino: 14/18**, not Genkit's 17/18. Phase B measures
the built candidate as built. Phase-A S10 reports live in `research/<id>-thinking.md`,
the cross-candidate table in `research/thinking-summary.md`, the phase-B fixture spec in
`fixture/THINKING-SPEC.md`. One phase-A finding binds the fixture: the scripted model must emit
**both** `reasoning` and `reasoning_content` on OpenAI-compatible assistant turns, and log which
name each client carried back.

---
status: accepted
date: 2026-10-09
deciders: maintainer
---

# Show, check, rehearse and author a project's graphs with the runtime's experimental graph surface

The maintainer decided on 2026-10-08 that Desk builds graph support in four
rows, one pull request each, after this record. This record fixes what each
row does, what it establishes and what it does not, and every runtime command
Desk would run for it. It is proposed: the maintainer accepts it, or amends
it, on review. Nothing here is built by this record.

It was checked against Desk `main` at `ae81e6a`, which pins Runtime `v0.27.1`,
Runner `v0.6.0` and Gateway `v0.10.0` (`internal/releaseplan/components.json`).

**How this was measured.** Runtime behaviour quoted here was measured on the
published `jpack` 0.27.1 for linux/amd64, the version Desk pins, with its
archive checked against the release's checksums file. The lab was a copy of the
runtime's own graph fixture project (`internal/graph/testdata/project`: two
packs, one graph of two nodes and one edge, and its rows), in a scratch folder,
once as it is (configVersion `"2"`) and once moved to configVersion `"5"` with
`requireReviewed` and an audit directory. One older answer was measured on a
`jpack` 0.23.1 binary. No model provider was called.

## Context

**What the runtime offers.** A graph is a document of the runtime's own: nodes
that each name one pack by the project's decision id, edges that feed one
node's outcome to another, and one declared result node (runtime ADR-0015). An
edge carries the upstream outcome id, written at a fact pointer, or a
tri-state contribution to a declared evidence requirement, or both. A project
declares its graphs in `jpack.json`, under a `graphs` member that needs
configVersion `"2"` or later (runtime ADR-0017; measured: accepted under `"5"`
too).

The runtime serves it on two wires:
- **The CLI**, `jpack experimental graph`: `list`, `explain`, `validate`,
  `test`, `evaluate` and `schema`.
- **MCP**: `experimental_list_graphs` and `experimental_get_graph` (runtime
  ADR-0029), `experimental_test_graphs` (runtime ADR-0026, with
  `include_traces` from ADR-0031), and the `author_graph` prompt. The MCP
  listing renders the same inventory as `experimental graph list`, from one
  function (ADR-0029, point 6), and the MCP matrix tool returns exactly the
  payload of the `experimental graph test` walk (ADR-0026).
- **Only the CLI** plans, validates and evaluates. "There is no MCP graph
  evaluation tool, so there is no MCP form" of the rehearsal (runtime
  ADR-0041, point 1).

**What the runtime says about it.** The CLI's help for `experimental graph`
says: "No JPS version defines any of this: the graph format, the evaluation
order, and the composite payload are conventions of this runtime, they may
change or be removed without compatibility promise, and every payload is
labeled non-normative." Measured, each payload carries:

| Command | `kind` | `experimental` | `label` | `conformanceClaimReference` |
|---|---|---|---|---|
| `list` | `non-normative-runtime-convention` | `true` | — | — |
| `explain` | `non-normative-runtime-convention` | — | — | — |
| `validate` | `non-normative-runtime-convention` | — | — | — |
| `test` | `non-normative-runtime-convention` | `true` | the matrix label | `CONFORMANCE.md` |
| `evaluate` | `non-normative-runtime-convention` | `true` | the composite label | `CONFORMANCE.md` |

The matrix label: "graph matrix results: rows a project wrote about its own
graph, run through this runtime's experimental composition (no JPS version
defines a graph or a composite result); each node's disposition is produced by
the experimental evaluator, whose conformance claim is stated, in full and
only, in CONFORMANCE.md".

The composite label: "graph composite: an experimental composition of this
runtime (spec RFC 0002 is a draft proposal; no JPS version defines a graph or a
composite result); each node's disposition is produced by the experimental
evaluator, whose conformance claim is stated, in full and only, in
CONFORMANCE.md".

**What the specification says.** RFC 0002, Judgment Graph composition, is a
Draft: "This is an open proposal, not part of the specification."

**What the conformance claim says.** The runtime's `CONFORMANCE.md` is "the
whole of this runtime's conformance claim". It names `experimental graph
evaluate`, `experimental graph test` and `experimental_test_graphs` among the
surfaces that reach the runtime's one evaluator, and says of them: "the project
and graph surfaces choose which packs and inputs reach the evaluator, and the
dispositions they report as evaluated [...] are that evaluator's". It also says
that the `experimental` namespace "names the **stability** of that surface".
The composition itself is defined by no JPS version, so nothing about a graph
is in that claim beyond each node's evaluation.

**What Desk does today.** Built before this record, over MCP, in #1, #4, #5
and #6 and the pages that followed them:
- `/graphs` lists the configured graphs from `experimental_list_graphs`;
- `/graphs/:graphId` draws the document `experimental_get_graph` serves (#4);
- its Tests view runs `experimental_test_graphs` only when the owner asks, and
  shows the rows, the coverage report, the `graphSha256` binding to the drawn
  document (#5, runtime ADR-0030), node traces on request and handoff-target
  pairs (#6, runtime ADR-0031 and ADR-0032), and the payload's `label`;
- Review and lock covers every declared graph with the packs and `jpack.json`
  (ADR-0009, section 2; `internal/desk/review.go`).

Desk's Go runs no graph command. The views show neither `kind` nor
`experimental` from the inventory, and the Tests view shows the `label` but not
`conformanceClaimReference`. Desk issues #9 and #10 are open on the walk and
its traces.

**The assistant today.** It reaches five runtime tools through the ToolGate:
`get_schema`, `list_examples`, `get_example`, `validate` and
`experimental_evaluate`, the last rewritten to a rehearsal on the wire
(ADR-0001; `web/src/config/deskConfig.ts`). Desk's code can also hand it host
tools, which Desk runs and which write nothing (`HostTool`,
`web/src/assistant/engine.ts`). It has no file tool. "The proposal is the only
sink. The desk renders the diff and applies an accepted proposal through the
span-preserving writer. No engine event writes anything." (ADR-0001.) Create
pack registers a new pack in `jpack.json` by adding one entry and carrying
every other member through, in value and order, and never moves
`configVersion` (`web/src/packs/jpackConfig.ts`).

**The guarded writer.** Project Files writes through one transaction
(`commitWriteLocked`, `internal/desk/files.go`):
- under Desk's one write mutex (`s.writes`; one mutex for every write, not one
  per path, because a path is a spelling), it reads what is there and compares
  its digest
  with the digest the writer started from: a stale base is refused with both
  digests, and a create that finds a file is refused as `exists`;
- the read-only decision refuses the runtime's lock, the audit records, and
  Desk's own custody (`readOnlyReason`, `internal/desk/file_access.go`);
- it replaces the file by an atomic rename, and answers with what it reads
  back.

Where `jpack.lock.json` is listed, the editor shows the lock line: the project
keeps a reviewed set, and Review and lock updates it. The upgrade, Review and
lock, and a start's identity write take the project's folder lock
(`internal/desk/project_lock.go`); a Project Files write does not.

**What the trail holds, measured.** In the lab moved to `"5"` with
`requireReviewed` and an audit directory:
- with no lock, a deciding `graph evaluate` is refused,
  `JPS-LOCK-REVIEW-REQUIRED`, and the same command with `--rehearsal`
  evaluates;
- the rehearsal writes nothing: no audit directory is made;
- after `packs lock`, one deciding run appends three lines: two `evaluation`
  and one `graph-composite`;
- with `requireComparableFacts` set, a rehearsal given a number where the pack
  compares a string is refused, `JPS-FACTS-COMPARABLE-REQUIRED`, with the node
  named;
- `audit verify` lists, among what it does not establish: "Whether any
  evaluation was refused at the gate, rehearsed, or failed before a
  disposition: the trail records decisions, not attempts, so its silence is not
  evidence that none were (ADR-0048)." Desk's decision record shows that list
  verbatim (`web/src/audit/DecisionRecord.tsx`).

On `jpack` 0.23.1, `graph evaluate --rehearsal` is refused,
`JPS-INVOCATION-ARGUMENTS`, "unknown flag: --rehearsal", exit 3, and nothing
is written. The flag shipped in runtime 0.24.0.

**Runner.** Its README: "AWS/Azure adapters, external actions, graph
execution, shared-user permissions and AI agent loops are not implemented."

## Decision drivers

- Nothing Desk shows claims more than the runtime's payload states. The
  runtime's sentences are shown verbatim, and Desk adds no verdict.
- Desk records no decision about a graph. Every graph evaluation it makes is a
  rehearsal, with no exception: the pack side has one, where the tool listing
  could not be read (ADR-0009, section 5), and the graph rehearsal is a flag
  Desk always passes.
- Desk consumes the surface every client does: MCP where the runtime serves
  it, and the CLI, from Desk's Go, where only the CLI does, as for `packs
  verify` and the `audit` commands (ADR-0009, ADR-0010).
- The proposal stays the assistant's only sink (ADR-0001). Every write is the
  owner's, through the guarded writer.
- Exact bytes from the runtime to the page, and no absolute path on the page.
- With a runtime that cannot do a thing, Desk hides or disables it and says
  so, and never pretends.

## Decision

Desk shows, checks and rehearses a project's declared graphs with the runtime's
experimental graph surface, labelled experimental in the runtime's own words,
and authors them through the assistant. It records nothing about a graph in the
decision record's trail: a graph evaluation in Desk is a rehearsal.

### 1. Which wire each part uses

| Part | Runtime surface | How Desk reaches it | Row |
|---|---|---|---|
| Inventory | `experimental_list_graphs` | MCP, built | 1 checks its labels |
| Document | `experimental_get_graph` | MCP, built | 1 checks its labels |
| Plan | `experimental graph explain` | Desk's Go | 1 |
| Findings | `experimental graph validate` | Desk's Go | 1 |
| Matrix rows and coverage | `experimental_test_graphs` | MCP, built | 2 |
| Authoring | the `author_graph` prompt; `validate -` and `explain -` over a proposal | MCP and Desk's Go | 3 |
| Rehearsal | `experimental graph evaluate --rehearsal` | Desk's Go | 4 |

**The page names a graph by its configured id, never by a path.** For a command
that takes a document by path, Desk's Go runs `experimental graph list` afresh
for that request, and passes the `path` the listing reports for that id. An id
the listing does not have is refused. A path from the page is never passed to
the runtime.

Desk's Go runs each command through `runRuntime`:
- in the directory Desk holds, with `--config jpack.json`, and without
  `JPACK_CONFIG`, and, for a desk Desk made, without `JPACK_SIGNING_KEY`, as
  ADR-0010, section 8, sets out;
- for at most 20 seconds and 64 KiB of answer (`runtimeCommandTimeout` and
  `runtimeAnswerLimit`, `internal/desk/runtime.go`). An answer over the bound
  is refused, never truncated.

Desk's Go hands the page each answer as the runtime printed it, never decoded
and re-encoded on the way, with one exception: an absolute path is redacted,
in a message and in a path-bearing member alike (section 2).

Where Desk was started under a `JPACK_CONFIG` that names another project, the
commands are unavailable, as in ADR-0009's review step.

### 2. Words and labels

- **Shown as given:** `kind`, `experimental`, `label`, `rehearsal` and
  `conformanceClaimReference`, on every graph payload that carries them, and
  every diagnostic's message. They are the runtime's English, marked
  `lang="en"`, as the decision record shows the runtime's sentences.
- **`conformanceClaimReference`** is shown as what it is, a locator for the
  file that states the runtime's claim, in the same terms the pack evaluation
  view uses (`web/src/components/EvaluationView.tsx`), naming the graph's
  packs where that view names "this pack".
- **Desk's own label** on every graph page and result is the one word
  "Experimental", beside the runtime's members. Where a payload carries no
  sentence (`list`, `explain`, `validate`), its `kind` is that payload's own
  statement, shown as given.
- **Never Desk's own words:** "verified", "proof", "evidence" or "trusted".
  "Evidence" appears only as the runtime's name for an evidence requirement and
  its availability. A `valid` or `passed` status is shown as the runtime's
  status, never as Desk's verdict.
- **No absolute path reaches the page.** Every runtime message passes the
  existing redaction (`withoutPathsUnder`, `displayedPath`). The payloads'
  path-bearing members do too: `path`, `rowsPath`, `graphPath`, `configPath`
  and `steps[].path`. A relative one is shown as given; an absolute one is
  redacted. The lab's payloads named its files by relative path
  (`onboarding.graph.json`, and in a message "The file
  \"vendor-onboarding-0.1.0.pack.json\" could not be read [...]"), but a
  `jpack.json` that declares a graph by an absolute path gets that path back
  verbatim in `list`'s and `validate`'s `path` (measured in review). The views
  Desk has today show `path` and `rowsPath` from the inventory; row 1 brings
  them under the same rule.

### 3. Row 1: the inventory and the plan, read-only

**What it adds:**
- on `/graphs`, `validate`'s findings for every declared graph:
  `experimental graph validate --config jpack.json --format json`, the walk;
- on `/graphs/:graphId`, a Plan view beside Diagram and Tests: `experimental
  graph explain <path> --format json`;
- the inventory's and the document's `kind` and `experimental`, shown as given
  where the views show neither today.

**How it reads them:**
- `validate` exits 1 when any check failed, and its JSON is read whatever the
  exit. A project that declares no graph is `skipped`, at exit 0, and is shown
  as the runtime's answer.
- `explain` has no plan for a graph whose own checks fail. The runtime's answer
  is shown, and the page points to the findings.
- A step whose pack could not be read carries the runtime's `detail` on that
  step. Measured: the plan is still `planned`, and that step names the file.

**Which revision.** `validate` reports `graphSha256` for each graph (runtime
ADR-0030). Desk compares it with the `sha256` of the document it drew, as it
compares a matrix run's, and where the two differ it says the findings are
about another revision of the file. The plan carries no digest (measured:
`explain`'s payload has none). So the plan is shown as its own answer, beside
the document, and never joined to the document or to a matrix run. Binding it
would need a runtime member, as ADR-0030 added for the matrix.

Row 1 adds no write route, and reads nothing from Desk's own storage.

### 4. Row 2: the matrix rows and coverage, held to this record

The Tests view stays on `experimental_test_graphs`, run only when the owner
asks. Opening the view, focus and reconnecting run nothing, as today. What it
shows stays as built:
- each row's expected and actual dispositions as the runtime's canonical
  bytes, compared by the runtime and never by Desk;
- the coverage report (runtime ADR-0016), informing and never gating;
- node traces on request (runtime ADR-0031);
- the handoff-target pairs (runtime ADR-0032);
- the `graphSha256` binding to the drawn document (runtime ADR-0030).

Row 2 holds the view to sections 2 and 9 of this record:
- `kind`, `experimental` and `conformanceClaimReference` are shown beside the
  `label` the view already shows;
- every sentence of Desk's own on the view is checked against section 2, and
  what falls short is corrected.

Desk issues #9 and #10 stay the view's own issues, and are not part of this
row.

### 5. Row 3: authoring through the assistant

**Where it is offered:** where the runtime advertises `author_graph` in its
prompt listing, and an assistant is configured. Elsewhere the action is absent,
and the page says why.

**The session:**
1. The owner chooses the packs to compose and states how the decisions relate.
2. Desk fetches `author_graph` with `prompts/get`. Its arguments are the
   owner's statement as `relationship`, and, as `packs`, the JSON text of the
   chosen packs as Desk read them through the file API.
3. The engine runs as it does for `author_pack`: the five tools through the
   ToolGate, which stay the ceiling (ADR-0001). It is handed two host tools
   that Desk runs: `experimental graph validate -` and `experimental graph
   explain -`, over the bytes the model passes on standard input. They write
   nothing. `runRuntime` takes no standard input today; row 3 adds it, bounded
   by the file writer's 4 MiB.
4. The proposal is a graph document and the members of its declaration: the
   configured id and, where given, a description. It is the only sink.

Where the prompt's text names a tool or a terminal command the assistant does
not have in Desk, the host tools stand in for `validate` and `explain`. The
prompt's step 6, rows for the graph, is shown to the owner as text, and Desk
writes no rows file in this line. The declaration carries no `rows` member: one
that names a file that is not there passes `graph validate` (`valid`, exit 0,
measured in review), and the `graph test` walk then reports that graph as a
`mismatch`.

**What the owner sees before anything is written:**
- the graph document's bytes, at a path the owner confirms;
- `validate`'s findings and `explain`'s plan, run over exactly those bytes;
- the whole new `jpack.json`, with its difference from the bytes Desk read.
  The new file is made as Create pack makes it: one `graphs` entry added, every
  other member carried through in value and order (`jpackConfig.ts`).

**The writes, on one confirmation,** through a new Go route row 3 adds,
`POST /api/graphs/write`. The Project Files route takes no folder lock, so
the graph writes do not go through it:
- The route takes the project's folder lock (`project_lock.go`), as the
  upgrade and Review and lock do, because it writes `jpack.json`. It holds the
  lock from its first read to its last write, and releases it on every path.
- Under that lock it runs both commits through `commitWriteLocked`, the
  transaction Project Files uses, each under Desk's one write mutex, and never
  with `override`, as `useProjectFileSave` holds for Project Files' own saves.
- The graph document is written first, as a create. It is refused as `exists`
  if anything is at that path.
- `jpack.json` is written next, with the digest of the `jpack.json` Desk read,
  against which the difference was shown. It is refused as `stale` if the file
  changed since.
- Each is an atomic write, under the read-only decision.
- Desk writes exactly the graph bytes whose `graphSha256` the findings shown
  reported, or nothing.
- If the second write is refused, the first stays. The answer says that the
  graph document was written and not declared, and what remains to do, as
  Create pack's dialog does.

**Never written by row 3:**
- `configVersion`. At `"1"` Desk writes no declaration. It says that `graphs`
  needs `"2"` or later (runtime ADR-0017), and that the upgrade offer
  (ADR-0009, section 4) is where the version moves. That offer moves it to
  `"5"`, and with it turns on `requireReviewed` and the audit trail and makes
  the first Review and lock, each listed before anything is written; the
  sentence says so.
- The lock, the audit records, a pack, or any file the read-only decision
  holds.

A change to a graph already declared is the same session against that
document's bytes. It is one write through the same route, with the digest of
the document Desk read, and `jpack.json` is left as it is.

**The reviewed set.** A written declaration changes `jpack.json`, and a
written graph changes a declared file. Where the project keeps a lock
(`jpack.lock.json`), whether or not it sets `requireReviewed`, the runtime then
refuses deciding runs until the next Review and lock (measured in review at
configVersion `"3"` with a lock and no `requireReviewed`):
- after a change to `jpack.json`, every deciding run by decision id,
  `JPS-LOCK-VERIFY` `config-drift`. Desk shows the lock line and the sentence
  it already has: "A change to jpack.json holds every pack: until the next
  lock, the runtime refuses every deciding run by decision id. Rehearsals and
  tests are not affected."
- after a change to a declared graph alone, `jpack.json` is unchanged, so that
  sentence does not fit: the runtime refuses a deciding run of that graph as
  `document-drift` (runtime ADR-0041 names the graph document among what a
  deciding graph run consults). Desk shows the lock line, and says that
  deciding runs of this graph are refused as `document-drift` until the next
  Review and lock.

Desk says nothing of its own about whether the graph is in the set. Review and
lock covers it.

**Whom `requireReviewed` binds** (runtime ADR-0044, point 5). The assistant
still cannot edit the configuration or its lock: every write is the owner's
confirmation. So it remains a caller of the bound kind, as the README says
today.

### 6. Row 4: a rehearsal evaluation

**What the owner does:** on a graph's page, the owner writes an inputs
document keyed by node id, each entry with optional `facts` and `evidence`, as
the runtime takes it (`--inputs`), and chooses Rehearse.

**What Desk runs:** `experimental graph evaluate <path> --config jpack.json
--inputs - --rehearsal --format json`, with the inputs on standard input, held
to 4 MiB and refused before the runtime runs when over it.
- It always passes `--rehearsal`.
- It never passes `--cites`.
- A runtime that does not take `--rehearsal` refuses the command (measured on
  0.23.1, above). Desk shows that refusal, and never sends the command without
  the flag.

**What it shows:**
- the status;
- the headline disposition, and each node's disposition, feeds and trace;
- the aggregated handoffs;
- `label`, `kind`, `experimental`, `rehearsal` and `conformanceClaimReference`,
  as section 2 says.

The dispositions are JSON objects in this payload (measured), not the
canonical strings a matrix row carries. The page shows each as the runtime
printed it (section 1), and compares none.

An answer that does not carry `"rehearsal": true` is shown as an error, never
as a result. A refusal is shown in the runtime's words, the node it names
included.

**Which revision.** The composite carries `graphId` and `graphVersion` and no
digest of the graph document (measured). Its one digest,
`artifact.bundleDigest`, is the digest of the runtime's own bundled artifacts,
not of the graph, and Desk never shows it as the graph's. Runtime ADR-0030,
point 4, leaves a graph digest off the evaluate envelope because the audit
record carries it, and a rehearsal writes no record. So Desk says which
configured graph it ran, not which bytes.

**What it writes:** nothing. Measured: a rehearsal makes no audit directory
and appends no line. It consults no reviewed set, so a graph whose files
drifted can be rehearsed. Desk keeps neither the inputs nor the result: they
live on the page until it is left.

**The trail stays silent, and `audit verify` says so.** Nothing in the trail
speaks of a rehearsal. `audit verify`'s report says the trail is silent about
rehearsals, and the decision record shows that sentence verbatim (above). Desk
adds no record of its own, and no count: runtime ADR-0048's answer 3 found
that a rehearsal counter would be "noise and not evidence".

### 7. Numbers

- **An edge moves no number.** It carries an outcome id written at a fact
  pointer, or an evidence availability: `present` for an outcome, and the
  edge's `onUnresolved`, `unknown` by default, for anything else (runtime
  ADR-0015): "the only values that cross a node boundary are the ones an edge
  explicitly places."
- **Packs do no arithmetic.** "Core defines no arithmetic" (spec RFC 0007,
  Draft). A computed value enters a node only as a fact in that node's inputs.
- **In a Desk rehearsal, that fact is what the owner typed.** It carries no
  lineage, and Desk says nothing about where it came from.
- **A calculated value with lineage is Runner's.** Runner #14 decided how a
  mapping declares one: a calculator on the installation's profile, and a
  `calculation` member in its answer, carried into the lineage. Runner #22
  closed it, in Runner v0.3.0. Desk shows that lineage in Jobs (#174) and
  sets up a calculator's binding there (#176). It reaches a pack in a job.
  It reaches no graph, because no job runs a graph (question 1). How a
  graph's node would carry it is outside this record.

### 8. Storage and privacy

- **Graphs are the project's own files**, declared in its `jpack.json`. Desk
  keeps no copy in its own storage. The page's query cache is in memory, and
  row 4 keeps nothing.
- **Rows 1, 2 and 4 send nothing off the machine.** The runtime "authorizes
  nothing, executes nothing, and fetches nothing" (`CONFORMANCE.md`), and its
  graph tools hold no credential and open no connection (runtime ADR-0029).
- **Row 3 sends to the model provider the owner configured** the prompt, the
  owner's statement, the chosen packs' text, the host tools' answers, and the
  answers of the five ToolGate tools the model calls (`get_schema`,
  `list_examples`, `get_example`, `validate` and `experimental_evaluate`, the
  last of which can evaluate any declared pack by its id), as in any assistant
  session (ADR-0001, the model relay). Nothing else leaves Desk.
- **The assistant's writes are the owner's,** on the owner's confirmation,
  through the guarded writer.

### 9. What each part establishes and what it does not

A graph's disposition in Desk is the runtime's experimental answer over the
project's packs, shown as given. Desk's showing it establishes nothing beyond
what the runtime's non-normative payload states.

| Part | Establishes | Does not establish |
|---|---|---|
| The inventory and the document | what `jpack.json` declares, and the bytes the runtime served, as the runtime read them | that a graph is well formed: listing is not validating |
| The plan (`explain`) | the order and the feeds the runtime would apply to the document it read | that the graph runs; which bytes it read, since it carries no digest; anything about a decision |
| The findings (`validate`) | the runtime's checks over the bytes whose digest it reports | that any pack is valid, since `validate` validates no pack; that an edge means what the policy says |
| A matrix run | what the project's own rows did, over the bytes whose digest it reports | that the rows are right; that coverage is complete; an authorization |
| A rehearsal | the runtime's experimental answer for the facts the owner typed | a decision; a record in the trail; a reviewed set consulted; which bytes, since it carries no digest of the graph document (`artifact.bundleDigest` is the runtime's bundle's); that the facts are true |
| The assistant's proposal | what the model proposed, and the runtime's findings and plan over it | that the composition is faithful to the policy; the prompt calls it "a PROPOSAL for a human to review" |
| Desk's label "Experimental" | — | anything: it repeats the runtime's own marker |

**What this record does not establish:**
- **Desk makes no conformance claim.** No JPS version defines a graph, a
  composition or a composite result. The runtime's claim is stated, in full
  and only, in its `CONFORMANCE.md`. Desk states no part of it, and shows the
  payload's reference to that file as a locator.
- **No record in the trail.** Every graph evaluation in Desk is a rehearsal,
  and every other graph command writes nothing.
- **No execution of a graph by Jobs.** A job over a graph is a Runner decision,
  not made here (question 1).

### 10. Every runtime command Desk would run

| Command | Writes | Network | Where Desk runs it | Row |
|---|---|---|---|---|
| `experimental graph list --format json` | nothing | no | to resolve a configured id to its path, afresh for each request | 1, 4 |
| `experimental graph validate [--id <id>] --format json` | nothing | no | the findings; exit 1 on any failed check, read whatever the exit | 1 |
| `experimental graph explain <path> --format json` | nothing | no | the Plan view | 1 |
| `experimental graph validate - --format json`, the proposal on standard input | nothing | no | the assistant's host tool, and before the owner confirms | 3 |
| `experimental graph explain - --format json`, the proposal on standard input | nothing | no | the same | 3 |
| `experimental graph evaluate <path> --inputs - --rehearsal --format json` | nothing | no | the owner's Rehearse | 4 |

Each runs with `--config jpack.json`, as section 1 says. None is a deciding
run, so none consults the lock and none appends to the trail. None changes
`jpack.json`.

Over MCP, Desk already calls `experimental_list_graphs`,
`experimental_get_graph` and `experimental_test_graphs` (ADR-0009, section 5).
Row 3 adds `prompts/get` for `author_graph`.

## Consequences

- Good: a project's graphs can be read, checked, rehearsed and composed in Desk
  without a terminal.
- Good: nothing Desk does with a graph is a decision, and the trail, the
  decision record and Jobs are unchanged.
- Good: every write row 3 makes goes through the transaction Project Files
  already uses, on the owner's confirmation.
- Bad: the graph surface is experimental, and the runtime says it "may change
  or be removed without compatibility promise". Desk's rows follow it, or are
  withdrawn with it.
- Bad: a declaration written moves `jpack.json` off the reviewed set until the
  next Review and lock, and the project's other callers are refused
  meanwhile.
- Bad: the plan and a rehearsal carry no digest of the graph document, so Desk
  cannot say which bytes either was about.
- Bad: Desk's Go depends on the JSON of four more runtime commands, all under
  `outputVersion` `"2"`.
- Neutral: rows 1 and 2 add no write, and row 4 writes nothing.

## Open questions

1. **Jobs over graphs.** Runner does not implement graph execution (its README
   and `SECURITY.md`). Whether a job may run a graph, and how its record would
   read, are Runner's decision, not made here. Until then, Desk offers no job
   over a graph.
2. **Calculated facts in a graph.** Runner #14 is decided, for a job over a
   pack (section 7). A graph's node would receive such a value only in a job
   over a graph, so this waits on question 1.
3. **Are graphs made for go-to-market work published anywhere?** No. They live
   outside the open repositories, and nothing in this record, its rows or their
   tests depends on them. The rows' tests use the runtime's fixture project and
   graphs written for the test.

## Delivery, after acceptance

One pull request each, under Desk's review rules. Every command above is in
the runtime Desk pins, `v0.27.1`.

| Row | What | Needs | Review | Status, 2026-10-09 |
|---|---|---|---|---|
| 1 | Inventory and plan, read-only: `validate`'s findings, the Plan view from `explain`, the inventory's and the document's `kind` and `experimental` | nothing | a second-reviewer round on the Go route: the id resolution, the redaction of messages and path members, the bounds | delivered in #341 |
| 2 | Matrix rows and coverage, held to this record: `kind`, `experimental` and `conformanceClaimReference` beside the `label`; Desk's own sentences checked | nothing | a second-reviewer round on the claims shown | delivered in #343 |
| 3 | Authoring through the assistant: `author_graph`, two host tools over the proposal, and the graph document and its declaration written on the owner's confirmation by a new route, `POST /api/graphs/write`, under the project's folder lock, through `commitWriteLocked`, never with `override` | 1 | a second-reviewer round on the file writes | delivered in #346 |
| 4 | Rehearsal evaluation: `graph evaluate --rehearsal` with the owner's inputs, shown with the runtime's labels, written nowhere | 1; standard input for `runRuntime` from 3, or row 4 adds it | a second-reviewer round on the claims | delivered in #350 |

Not in this line:
- a job over a graph (question 1);
- rows written by the assistant (section 5);
- a saved history of graph rehearsals;
- Desk issues #9 and #10.

## More information

- **Runtime, at `v0.27.1`:**
  - ADR-0015 (the experimental graph surface), ADR-0016 (coverage), ADR-0017
    (graphs in `jpack.json`), ADR-0026 (the matrix over MCP), ADR-0029 (serving
    graphs and their inventory), ADR-0030 (binding to the loaded document),
    ADR-0031 (node traces), ADR-0032 (the handoff target), ADR-0041 (a graph
    rehearsal) and ADR-0048 (refusals and rehearsals outside the trail);
  - `CONFORMANCE.md`;
  - the help of `jpack experimental graph` and each of its commands;
  - the `author_graph` prompt (`internal/mcp/prompts.go`).
- **Specification:** RFC 0002 (Judgment Graph composition, Draft) and RFC 0007
  (the determination boundary, Draft).
- **Runner:** `README.md` and `SECURITY.md` (graph execution not implemented);
  #14 and #22 (calculated values), in v0.3.0.
- **Desk:**
  - [ADR-0001](0001-make-the-assistant-engine-a-slot.md) (the assistant, the
    ToolGate, the proposal as the only sink);
  - [ADR-0009](0009-gates-on-by-default.md) (the gates, Review and lock, the
    upgrade offer, every runtime call);
  - [ADR-0010](0010-defensible-decision-records.md) (the decision record, and
    how Desk's Go runs the runtime);
  - `internal/desk/files.go`, `internal/desk/file_access.go`,
    `internal/desk/project_lock.go`, `web/src/packs/jpackConfig.ts`,
    `web/src/routes/GraphView.tsx`, `web/src/audit/DecisionRecord.tsx`;
  - #1, #4, #5 and #6 (the graph views), #174 and #176 (calculated input
    lineage in Jobs), and issues #9 and #10.

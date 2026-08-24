# judgment-pack-desk

A local web desk for a Judgment Pack project. `jpack-desk` is one Go binary: it
serves a single-page application and relays JSON-RPC between that page and a
`jpack mcp` subprocess running in your project directory.

The browser is the MCP client. The Go program is a chassis, not a feature
server — it has no per-feature endpoints and parses none of the traffic it
carries, so the desk can show anything the runtime's tools expose without a
matching change on the Go side.

## What it shows

**A project home.** `/` is what the project declares — its packs, and the two
rehearsals that can be run over them. The matrix and graph entries appear only
where the project has one: `list_packs` reports a matrix flag per pack, and the
configured graphs are found by running their matrices, because nothing lighter
reports them (see [Upstream gaps](#upstream-gaps)).

**A pack browser:**

- `/packs/:id` fetches one document with `get_pack` and renders it: the
  decision, outcomes, applicability, evidence requirements, rules, exceptions,
  escalation, sources and metadata — plus a **Raw JSON** tab holding the
  document exactly as it is on disk.

Conditions are shown as formatted JSON rather than paraphrased into English. A
paraphrase of a policy condition would be a claim about what the policy means,
and only the document gets to make that claim.

**An evaluation and trace view:**

- `/packs/:id/evaluate` runs the pack over documents you supply, through the
  runtime's `experimental_evaluate` tool, and renders the payload it returns.

That view keeps three things apart, because the payload does:

- The **disposition** is the portable JPS Core §8.3 answer and the authoritative
  part of the payload. It gets the first panel and a frame of its own: kind,
  outcome id, the retained reason set, and the handoff state with what triggered
  it.
- The **handoff target** is shown *beside* the disposition and never inside it,
  because §8.3 keeps it outside one. It is what the pack configures. No delivery
  is observed, and the desk claims none.
- The **trace** is informative. It is rendered as the staged walk it is —
  applicability, then exceptions, then rules, in the payload's own order — with
  each entry's id, its condition verdict colour-coded across `true`, `false` and
  `unknown`, the effect or outcome where the entry carries one, and badges for
  `skipped`, `suppressed` and `onUnknown`. It decides nothing.

The envelope panel reports the facts about the run rather than about the answer:
the `experimental` flag, the specVersion the pack declares beside the
evaluatorSpecVersion of the contract applied to it, the packId and packVersion
read off the document that was evaluated, the bundled artifact digest, and
`conformanceClaimReference` — displayed as what it is, a locator for the file
that states the runtime's claim, and not a claim the payload itself makes.

Nothing is invented: a member the payload omits is absent from the view rather
than filled in, and a verdict is shown as the payload spells it. A refused
evaluation carries no disposition at all, so a refusal is reported as its §8.4
class and phase with the runtime's diagnostics, and never as a substitute
answer.

**A what-if loop.** `experimental_evaluate` takes the facts and evidence
documents as JSON text rather than as paths, so the loop needs nothing from the
chassis: edit the documents in the page, press **Re-evaluate**, and a *What
changed* table puts the previous disposition beside the current one — kind,
outcome id, reasons, handoff state, what triggered it, and the handoff target.
Unchanged members are listed too, so the diff never hides what held. The trace
is not diffed: a trace that moved while the disposition held is not a change in
the answer.

The two editors keep the tri-state the tool asks for. Leaving the evidence box
unchecked omits the key entirely, which is what "no evidence document at all"
means; a key present with an empty string would be a *supplied* empty document,
and is refused as malformed-input.

**A matrix and coverage view:**

- `/matrix` runs every matrix the project declares, through
  `experimental_test_packs`.
- `/packs/:id/matrix` runs one pack's.

Two things are on that page, and they answer different questions.

The **rows** say whether what a project wrote about its own packs still holds.
Each row shows the disposition it expects beside the one the evaluator produced
— the comparison is on RFC 8785 canonical bytes, so the view parses those bytes
to show `kind`, `outcomeId`, reasons and handoff, and reports the runtime's own
verdict rather than recomputing one. A row that expects a refusal carries no
disposition at all, and is shown as the §8.4 class and phase it names.

A row may additionally assert **where the decision goes** (ADR-0025), and that
assertion is kept visibly apart from the disposition, because §8.3 keeps the
target outside one. It is the case the view most needs to make legible: a pack
edit touching only `escalation.target.name` leaves every disposition byte
identical, so such a row fails with its expected and actual dispositions
matching exactly. The page says so in as many words rather than leaving a red
row to be puzzled over. The three states the report distinguishes are kept
distinct too — a named target, the literal `null` for "no target at all", and
`unavailable` where the report cannot state one — because "no target" is an
answer and "unavailable" is the absence of one.

The **coverage report** says how much of each pack those rows are about, and it
usually has more to say than the rows do: a matrix can pass everything it has
while stating nothing about most of what its pack can do. So the gaps lead and
the witnessed probes fold away underneath. Each gap carries the runtime's own
sentence naming what no row said, including the derived boundary probes
(ADR-0023) — the exact value at which a strict and a non-strict encoding of a
threshold would differ, which is the one input a matrix is most likely to lack.

None of it gates. A missing probe moves no status, and the page says so, because
a report that looked like a failing check would be read as one.

**Graph views:**

- `/graphs` runs every graph the project configures, through
  `experimental_test_graphs`; `/graphs/:id` runs one.

A graph composes packs: one node's outcome lands at a fact pointer the next
node's rules read, and its resolution state feeds that node's evidence. No JPS
version defines a graph, a composition, or a composite result — the format is
the runtime's own convention, and only each node's pack evaluation reaches the
shared evaluator. The page carries the payload's own label saying so.

Each graph is drawn as the **walk** it is: the nodes represented in its
coverage report as an SVG diagram, on the evaluation-order axis the runtime
enumerated them along, ending in the composite headline every row is judged on.
Coverage is the only account of the graph's shape that reaches this wire, and
it can omit a node the run never admitted — so the diagram claims representation,
not completeness. Choosing a row colours the nodes with the comparisons that row
reported; a node the selected row reports no comparison for is shown exactly
that way rather than as having passed, and the row's own verdict — which covers
the headline and every reported node comparison together — is shown beside the
diagram as the row's, never painted onto the composite.

**The diagram draws no arrow between two nodes.** The wire carries the walk's
node order and the edge indices coverage represents; it does not carry which
node feeds which, so an arrow would assert a dependency the payload never
states — and in a graph with
independent branches that assertion would be false. The edges are reported
beside the diagram as the indexed slots the payload describes them as, each with
the witness its resolved and unresolved branches have. The missing structure is
recorded in [Upstream gaps](#upstream-gaps) rather than reconstructed by parsing
the English in a `detail` sentence, which would make a contract out of prose.

Graph coverage is grouped per node, in that same order, and reported exactly as
the pack coverage is.

A project that configures no graph is an answer rather than an error: the walk
reports `skipped` with no entries, the home page offers no graph entry, and the
graphs page says the project configures none.

## Requirements

- Go 1.24 or newer
- Node 22 or newer (`web/package.json` declares `engines.node >= 22`)
- A `jpack` binary — the [judgment-pack runtime](https://github.com/Judgment-Pack/judgment-pack-runtime)

TypeScript and Vite are pinned to exact versions rather than caret ranges. They
are the two tools whose output this repository ships — a transpile and a bundle
— so a fresh `npm ci` producing a different build than the last one would be a
change nobody made.

## Production mode

One binary with the SPA embedded:

```sh
npm --prefix web ci
npm --prefix web run build     # emits web/dist, which go:embed picks up
go build -o bin/jpack-desk .

./bin/jpack-desk --jpack /path/to/jpack /path/to/project
```

It prints the URL to open, including the session token:

```
judgment-pack desk
  project: /path/to/project
  runtime: /path/to/jpack
  open:    http://127.0.0.1:8791/?token=1f3c…
```

`projectDir` defaults to the current directory. `--jpack` defaults to `jpack`
on `PATH`, and `--port` defaults to `8791`.

## Development mode

Two processes: the chassis for the relay, Vite for hot reload.

```sh
# terminal 1 — chassis with a fixed token so the URL is stable across restarts
# (flags come before the project directory: Go stops parsing flags at the
# first positional argument)
go run . --dev-token dev --port 8791 --jpack /path/to/jpack /path/to/project

# terminal 2 — Vite dev server, proxying /ws to the chassis
npm --prefix web run dev
```

Then open <http://localhost:5173/?token=dev>.

Vite proxies `/ws` to `127.0.0.1:8791` (override with `JPACK_DESK_CHASSIS`).
Passing `--dev-token` is what additionally permits the Vite dev server's origin
— without it the chassis refuses the proxied upgrade, because the browser's
`Origin` is the dev server's and never matches the host it reaches the chassis
under.

To check a running chassis end to end with the desk's own client code:

```sh
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=dev'
```

## Security model

The desk drives a runtime that reads your project. Three things bound it:

- **Loopback only.** The listener binds `127.0.0.1`. Nothing off the machine
  can reach it.
- **A session token.** A random token is generated at startup and printed in
  the URL. `/ws` requires it as `?token=`, compared in constant time. Static
  assets are served without it; the relay is the capability, and it is the
  thing that is gated.
- **An origin check.** A WebSocket upgrade whose `Origin` is not the origin the
  page was served from is refused. This is what stops a page on another site
  from opening a socket to your loopback and driving the runtime through your
  own browser — a token in a URL you have visited is not, by itself, protection
  against that. A request with no `Origin` at all is not from a browser, and
  the token is its authorization.

The chassis holds no credential, opens no outbound connection, and writes
nothing to the project. The runtime subprocess inherits the project directory
as its working directory and is killed when the socket that started it closes.

## How the relay works

- One WebSocket connection spawns one `jpack mcp` subprocess with `cwd` set to
  the project directory.
- Bytes cross verbatim: one JSON-RPC message per WebSocket text frame on the
  browser side, newline-delimited JSON on the stdio side.
- Closing the socket kills the subprocess.
- One message originates in the chassis rather than the runtime: a file watcher
  over the project tree sends a `desk/fileChanged` JSON-RPC notification
  carrying the changed path, and the page invalidates its caches on it.
- A dropped socket is reconnected by the page rather than reported and left. The
  delay doubles from 500 ms up to a 15 s cap, with jitter, and each attempt
  builds a fresh MCP client — one that has closed already negotiated with a
  server that is gone. A successful reconnect invalidates every query, because
  whatever the project did while the socket was down arrived as
  `desk/fileChanged` notifications nobody heard.

## Layout

```
main.go              flags, embedded assets, HTTP server
internal/desk/
  server.go          routing, SPA fallback, token and origin checks
  relay.go           WebSocket ↔ `jpack mcp` subprocess
  watch.go           project-tree file watching
scripts/acceptance.sh  the two-run acceptance proof
web/                 Vite + React + TypeScript SPA
  src/mcp/           the MCP client: transport, connection, queries, and the
                     canonical-string and probe-name readers
  src/routes/        project home, pack detail, evaluation, matrix, graphs
  src/components/    the semantic document, evaluation, coverage, row and
                     graph-walk views
  scripts/smoke.ts   the desk's own client, driven outside a browser
```

## Tests

```sh
go test ./...
```

The relay's end-to-end tests need a runtime and a project, and skip without
them:

```sh
go build -C /path/to/judgment-pack-runtime -o "$PWD/bin/jpack" ./cmd/jpack
JPACK_PROJECT=/path/to/project go test ./...
```

`JPACK_BIN` overrides the runtime binary; otherwise `./bin/jpack` is used when
present.

CI runs `gofmt`, `go vet` and `go test` on one job and `npm ci`, `tsc` and
`vite build` on another. It supplies neither a runtime binary nor a project, so
the end-to-end tests skip themselves there and what runs is the coverage that
needs nothing external.

### The acceptance proof

Two real evaluations through the relay, against the same pack: one with the
project's full facts, one with a load-bearing fact removed. The first should
resolve to an outcome; the second should escalate. Then the rows the project
declares about itself — every pack matrix, and every configured graph matrix.

```sh
go build -C /path/to/judgment-pack-runtime -o "$PWD/bin/jpack" ./cmd/jpack
JPACK_PROJECT=/path/to/judgment-pack-quickstart scripts/acceptance.sh
```

It builds the chassis, copies the project to a temporary directory — a completed
evaluation appends a record in a project that declares an audit directory, and
an acceptance run must not write into the tree it was pointed at — reads the
tokened URL off the `open:` line of the chassis' startup output, and drives the
desk's client three times. `MUTATE` is the jq expression that removes the fact,
and defaults to the quickstart pack's `/request/completeness`. `PACK` selects
the decision id where the project's first is not the one `FACTS` suits.

The matrix runs need none of that care — a row is a rehearsal and writes
nothing — but they run against the same copy anyway, so one run means one
project. `EXPECT_MATRIX_STATUS` defaults to `passed`; `EXPECT_GRAPH_STATUS` is
checked only when set, because a project that configures no graph correctly
reports `skipped`.

The same client runs on its own against a chassis you already have open:

```sh
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=…' \
  --facts /path/to/full-facts.json --evidence /path/to/evidence.json

# the two calls the matrix and graph views make
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=…' --matrix --graphs
```

## Upstream gaps

The desk consumes the runtime's public wire and nothing else. Where the wire
cannot express something the desk wants, the gap is recorded here rather than
worked around in the chassis — a chassis that parsed or supplemented the traffic
would stop being one.

- **No rehearsal mode on `experimental_evaluate`.** Every completed call appends
  one audit record in a project whose `jpack.json` declares an audit directory,
  and the what-if loop is calls: exploring five variants of a facts document
  leaves five records saying the project decided five times. The runtime already
  draws this distinction elsewhere — `experimental_test_packs` writes nothing,
  on the grounds that a matrix row is a rehearsal rather than a decision — but a
  what-if run has no equivalent, and the tool takes no argument that would say
  so. Until it does, the desk's evaluate view names the consequence in the page
  instead of hiding it, and the acceptance script evaluates a copy.

- **A graph's structure is not on the wire.** `experimental_test_graphs` reports
  each configured graph's rows and its derived coverage, and no member of that
  payload carries the graph document: not the pack each node names, not the
  edges' endpoints, not the fact pointer or evidence id an edge carries, and not
  which node the document declares as its `result`. There is a `get_pack` for
  pack documents and no `get_graph` for graph documents, and the payload's
  `path` member names a file the browser cannot read. What does reach the wire
  is the coverage report's own namespacing — one block of probes per node,
  emitted in the walk's evaluation order, and two probes per edge identified by
  position — so the desk draws the nodes on that order axis and draws no edge
  between them. A `get_graph` mirroring `get_pack` would close this; parsing the
  node-and-pack prefix out of a probe's English `detail` would not, because that
  sentence is a message and not a contract.

- **A graph matrix reports no node trace.** ADR-0027 pins the trace contract and
  binds it to each node evaluation inside a graph run, and the runtime's
  `GraphNodeEvaluation` carries that node's `trace` beside its `factFeeds` and
  `evidenceFeeds` — how the composition actually fed one node from another. That
  shape is produced by `jpack experimental graph evaluate`, which has no MCP
  tool. Over the wire a graph row reports per node only `node`, `status`,
  `expected` and `actual`, so the desk can show what a node concluded and not
  how it got there, though the runtime computed it. The pack surface has no such
  gap: `experimental_evaluate` returns the trace, and the desk renders it.

- **No inventory of configured graphs.** `list_packs` reports a matrix flag per
  pack, so whether a pack declares a matrix costs one cheap call. Nothing
  reports the graphs a project configures, so the only way to learn whether
  there is one is to run every graph's matrix. The desk's home page does that —
  affordable because the tool writes nothing, and the result is the query the
  graphs page then reads — but on a large matrix it is a real cost paid to
  render a link.

- **Graph rows cannot assert a handoff target.** ADR-0025 added
  `expectedHandoffTarget` to pack matrix rows and deferred the graph surface
  explicitly. A graph row compares composite and per-node dispositions only, so
  a change to where a composed decision is handed off leaves every graph row
  green. The desk shows the assertion on pack rows and has nothing to show on
  graph rows, which is the runtime's position and not a gap in the view.

## License

Apache-2.0. See [LICENSE](LICENSE).

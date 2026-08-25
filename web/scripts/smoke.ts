/**
 * Drives the desk's own MCP client against a running chassis, outside a
 * browser: the same transport and the same SDK Client the page uses.
 *
 * The Go tests prove the relay carries JSON-RPC. This proves the client code
 * on the other end of it speaks to the runtime — the seam between them is
 * where a framing mistake would hide.
 *
 *   node --experimental-strip-types --no-warnings scripts/smoke.ts <url>
 *   npm run smoke -- http://127.0.0.1:8799/?token=…
 *
 * With --facts it additionally runs one real evaluation over the relay and
 * prints the disposition and the head of the trace. That is the pair the
 * acceptance script exercises: the same pack, one run with a load-bearing fact
 * and one without it.
 *
 *   npm run smoke -- <url> --facts full-facts.json --evidence evidence.json
 *   npm run smoke -- <url> --facts partial-facts.json --expect-kind unresolved
 *
 * With --matrix it runs the project's declared pack matrices through
 * experimental_test_packs, and with --graphs the configured graph matrices
 * through experimental_test_graphs — the two calls the matrix and graph views
 * make. Both write nothing: a matrix row is a rehearsal, not a decision, so
 * unlike an evaluation these are safe to run against a project in place.
 *
 *   npm run smoke -- <url> --matrix --graphs
 *   npm run smoke -- <url> --matrix --expect-matrix-status passed
 *
 * With --graph-document it exercises the graph-serving pair the walk diagram
 * draws from (ADR-0029): the inventory, then one document fetched by its
 * configured id. The served text is checked against the metadata beside it —
 * byte count and sha256, which is a self-contained exactness proof — and
 * against a local read of the file itself where --graph-file names one. Both
 * tools are refused as missing rather than skipped quietly if the connected
 * runtime does not advertise them: asking for the step is asking for the
 * check.
 *
 *   npm run smoke -- <url> --graph-document onboarding \
 *     --graph-file /path/to/project/graphs/onboarding.graph.json
 *
 * Without that flag the graph leg still runs, capability-gated rather than
 * refused: where the runtime advertises both graph tools it lists the
 * configured graphs, fetches one document, and checks the ADR-0030 binding —
 * the digest the matrix run decoded against the digest served beside the
 * document. Where it advertises neither, the leg says so in one line and the
 * drive stays green, because a runtime with no such tools is not a runtime
 * that failed the check.
 */
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { DeskWebSocketTransport } from '../src/mcp/transport.ts'
import { describeHandoffTarget, nodesInWalkOrder, parseProbe } from '../src/mcp/canonical.ts'
import {
  bindGraphDigests,
  deriveWalkLayout,
  edgeCarries,
  readServedDocument,
  statedDigest
} from '../src/mcp/graphDocument.ts'
import { listAllTools, readCapabilities } from '../src/mcp/capabilities.ts'
import type {
  Evaluation,
  GraphDocumentMeta,
  GraphInventory,
  GraphSuite,
  GraphSuiteEntry,
  GraphSummary,
  MatrixProbe,
  PackDocument,
  PackInventory,
  PackTest
} from '../src/mcp/types.ts'

interface Options {
  target: string
  facts?: string
  evidence?: string
  packId?: string
  traceHead: number
  expectKind?: string
  expectHandoff?: string
  matrix: boolean
  graphs: boolean
  expectMatrixStatus?: string
  expectGraphStatus?: string
  graphDocument?: string
  graphFile?: string
}

function usage(message: string): never {
  console.error(message)
  console.error(
    'usage: smoke.ts <desk url with ?token=…> [--facts <path>] [--evidence <path>]\n' +
      '                [--pack <decision id>] [--trace <n>] [--expect-kind <kind>]\n' +
      '                [--expect-handoff <state>] [--matrix] [--graphs]\n' +
      '                [--expect-matrix-status <status>] [--expect-graph-status <status>]\n' +
      '                [--graph-document <configured graph id>] [--graph-file <path>]'
  )
  process.exit(2)
}

/** Flags that stand alone; every other flag takes the argument after it. */
const FLAGS = new Set(['--matrix', '--graphs'])

function parseArgs(argv: string[]): Options {
  const [target, ...rest] = argv
  if (!target) usage('the desk URL is required')
  const options: Options = { target, traceHead: 5, matrix: false, graphs: false }
  for (let i = 0; i < rest.length; i += 1) {
    const flag = rest[i]!
    if (FLAGS.has(flag)) {
      if (flag === '--matrix') options.matrix = true
      if (flag === '--graphs') options.graphs = true
      continue
    }
    const value = rest[i + 1]
    if (value === undefined) usage(`${flag} needs a value`)
    i += 1
    switch (flag) {
      case '--facts':
        options.facts = value
        break
      case '--evidence':
        options.evidence = value
        break
      case '--pack':
        options.packId = value
        break
      case '--trace':
        options.traceHead = Number(value)
        break
      case '--expect-kind':
        options.expectKind = value
        break
      case '--expect-handoff':
        options.expectHandoff = value
        break
      case '--expect-matrix-status':
        options.expectMatrixStatus = value
        options.matrix = true
        break
      case '--expect-graph-status':
        options.expectGraphStatus = value
        options.graphs = true
        break
      case '--graph-document':
        options.graphDocument = value
        break
      case '--graph-file':
        options.graphFile = value
        break
      default:
        usage(`unknown flag ${flag}`)
    }
  }
  return options
}

const options = parseArgs(process.argv.slice(2))

const url = new URL(options.target)
const token = url.searchParams.get('token')
if (!token) {
  console.error('the URL must carry the session token the chassis printed (?token=…)')
  process.exit(2)
}
const wsURL = `ws://${url.host}/ws?token=${encodeURIComponent(token)}`

// The SDK's tool-result type is a union that includes a shape carrying no
// content at all, so the narrowing happens here rather than in the signature.
function textBlocksOf(result: unknown): string[] {
  const content = (result as { content?: unknown } | undefined)?.content
  const blocks = Array.isArray(content) ? content : []
  return blocks
    .filter((b): b is { type: 'text'; text: string } => (b as { type?: string })?.type === 'text')
    .map((b) => b.text)
}

function textOf(result: unknown): string {
  return textBlocksOf(result).join('')
}

const client = new Client({ name: 'jpack-desk-smoke', version: '0.1.0' }, { capabilities: {} })

let sawFileChange = false
client.fallbackNotificationHandler = async (notification) => {
  if (notification.method === 'desk/fileChanged') sawFileChange = true
}

await client.connect(new DeskWebSocketTransport(wsURL))

const server = client.getServerVersion()
console.log(`initialize    ok  serverInfo=${server?.name} ${server?.version}`)

// Every page of it, exactly as the page reads it: a tool on a second page that
// was never asked for is a tool this would report as absent.
const tools = await listAllTools(client)
console.log(`tools/list    ok  ${tools.length} tools: ${tools.map((t) => t.name).join(', ')}`)

// The same reading the page does at connect time, printed so a drive against
// two runtimes shows which world each one is.
const capabilities = readCapabilities(tools)
console.log(
  `capabilities      rehearsal=${capabilities.rehearsalSupported} ` +
    `list_graphs=${capabilities.graphInventorySupported} ` +
    `get_graph=${capabilities.graphDocumentSupported} ` +
    `include_traces=${capabilities.graphTracesSupported}`
)

const inventory = JSON.parse(
  textOf(await client.callTool({ name: 'list_packs', arguments: {} }))
) as PackInventory
const packs = inventory.packs ?? []
console.log(`list_packs    ok  status=${inventory.status} packs=${packs.length}`)
for (const pack of packs) {
  console.log(`                  - ${pack.id}  ${pack.packId} v${pack.packVersion}  ${pack.path}`)
}
if (packs.length === 0) {
  console.error('list_packs returned no packs; point the chassis at a project that declares one')
  process.exit(1)
}

const selected = options.packId ?? packs[0]!.id
if (!packs.some((pack) => pack.id === selected)) {
  console.error(`the project declares no pack with the decision id ${selected}`)
  process.exit(1)
}

const packResult = await client.callTool({ name: 'get_pack', arguments: { pack_id: selected } })
const raw = textOf(packResult)
const doc = JSON.parse(raw) as PackDocument
console.log(
  `get_pack      ok  ${raw.length} bytes  title=${JSON.stringify(doc.title)}  ` +
    `rules=${doc.rules?.length ?? 0} outcomes=${doc.outcomes?.length ?? 0} ` +
    `exceptions=${doc.exceptions?.length ?? 0} evidence=${doc.evidenceRequirements?.length ?? 0}`
)

for (const member of ['specVersion', 'id', 'version', 'title', 'decision', 'outcomes', 'rules'] as const) {
  if (doc[member] === undefined) {
    console.error(`the relayed document is missing the required member ${member}`)
    process.exit(1)
  }
}

if (options.facts) {
  await evaluate(options.facts, options.evidence)
}

if (options.matrix) {
  await testPacks()
}

// The graph surface is driven by at most **one** matrix run per drive.
//
// `--graphs` prints the whole suite; the binding and the layout then read
// their entry out of *that* run rather than making a second one. Two runs
// would be two reads of a file that can be edited between them: the printed
// suite could pass over revision A while the binding described revision B,
// and the second run's own status would never be printed at all. That is
// precisely the condition ADR-0030's digest exists to detect, so producing it
// here would be the check defeating itself.
const graphSuite = options.graphs ? await testGraphs() : undefined

// Two ways into the graph surface, and they differ in what an absent
// capability means. Naming a graph *asks* for the check, so a runtime without
// the tools fails the run. Naming none asks for whatever this runtime can
// answer, so the same absence is a skip — and the deeper leg subsumes the
// automatic one, which is why only one of them runs.
if (options.graphDocument) {
  await graphDocument(options.graphDocument, options.graphFile, graphSuite)
} else {
  await graphBinding(graphSuite)
}

// Deferred until after the binding, deliberately. The suite's status is a
// verdict about rows; the binding is a fact about which bytes those rows were
// read from. Exiting on the verdict first would leave a mismatching run with
// no statement of what it ran over — and a mismatch caused by a mid-edit is
// exactly the case where that statement matters most.
if (graphSuite) checkGraphSuiteStatus(graphSuite)

console.log(`notifications ${sawFileChange ? 'saw desk/fileChanged' : 'none seen (expected: nothing changed)'}`)
console.log('\nOK')
await client.close()
process.exit(0)

/**
 * One real evaluation over the relay, reported the way the desk's evaluation
 * view reports it: the disposition first, the handoff target beside it and
 * never inside it, then the head of the trace. The trace is informative — the
 * disposition is the answer, and only the disposition is checked against the
 * expectations this script was given.
 */
async function evaluate(factsPath: string, evidencePath?: string): Promise<void> {
  const args: Record<string, unknown> = {
    pack_id: selected,
    facts: await readFile(factsPath, 'utf8')
  }
  // Omitting the key is the only form absence takes: a key present with an
  // empty string is a supplied empty document and is refused as malformed-input.
  if (evidencePath) args.evidence = await readFile(evidencePath, 'utf8')

  const result = await client.callTool({ name: 'experimental_evaluate', arguments: args })
  const text = textOf(result)
  if (result.isError) {
    console.error(`experimental_evaluate refused: ${text}`)
    process.exit(1)
  }
  const payload = JSON.parse(text) as Evaluation
  const disposition = payload.disposition
  const handoff = disposition.handoff

  console.log(
    `evaluate      ok  pack=${selected} (${payload.packId} v${payload.packVersion})  ` +
      `spec=${payload.specVersion} evaluator=${payload.evaluatorSpecVersion} ` +
      `experimental=${payload.experimental}`
  )
  console.log(
    `                  facts=${factsPath}` +
      `${evidencePath ? ` evidence=${evidencePath}` : ' evidence=(key omitted)'}`
  )
  console.log(
    `  disposition     kind=${disposition.kind}` +
      `${disposition.outcomeId ? ` outcomeId=${disposition.outcomeId}` : ''}` +
      ` reasons=[${(disposition.reasons ?? []).join(' ')}]` +
      ` handoff=${handoff?.state}` +
      `${handoff?.triggeredBy?.length ? ` triggeredBy=[${handoff.triggeredBy.join(' ')}]` : ''}`
  )
  console.log(
    `  handoffTarget   ${
      payload.handoffTarget
        ? `${payload.handoffTarget.name} (${payload.handoffTarget.kind})`
        : '(none reported)'
    }`
  )

  const trace = payload.trace ?? []
  const head = trace.slice(0, Math.max(0, options.traceHead))
  console.log(`  trace           ${head.length} of ${trace.length} entries`)
  head.forEach((entry, index) => {
    const badges = [
      entry.effect,
      entry.outcome ? `-> ${entry.outcome}` : undefined,
      entry.skipped ? 'skipped' : undefined,
      entry.suppressed ? 'suppressed' : undefined,
      entry.onUnknown ? `onUnknown=${entry.onUnknown}` : undefined
    ].filter((badge): badge is string => badge !== undefined)
    console.log(
      `    ${String(index + 1).padStart(2)} ${entry.stage.padEnd(13)} ` +
        `${entry.condition.padEnd(7)} ${(entry.id ?? '(unnamed)').padEnd(40)}` +
        `${badges.length ? ` ${badges.join(' ')}` : ''}`
    )
  })

  if (options.expectKind !== undefined && disposition.kind !== options.expectKind) {
    console.error(`expected disposition kind ${options.expectKind}, got ${disposition.kind}`)
    process.exit(1)
  }
  if (options.expectHandoff !== undefined && handoff?.state !== options.expectHandoff) {
    console.error(`expected handoff state ${options.expectHandoff}, got ${handoff?.state}`)
    process.exit(1)
  }
}

/**
 * The project's declared pack matrices, reported the way the matrix view
 * reports them: the rows, then the gaps — because the gaps are the report.
 *
 * A mismatching or skipped run is a *successful* call carrying its status, so
 * this reads the payload's status rather than treating a non-green run as a
 * transport failure. Only an explicit expectation makes a status fatal.
 */
async function testPacks(): Promise<void> {
  const result = await client.callTool({ name: 'experimental_test_packs', arguments: {} })
  const text = textOf(result)
  if (result.isError) {
    console.error(`experimental_test_packs refused: ${text}`)
    process.exit(1)
  }
  const payload = JSON.parse(text) as PackTest
  const packs = payload.packs ?? []
  console.log(
    `test_packs    ok  status=${payload.status} ` +
      `rows=${payload.summary.passed}/${payload.summary.total} ` +
      `mismatched=${payload.summary.mismatched} packs=${packs.length} ` +
      `configVersion=${payload.configVersion}`
  )
  for (const entry of packs) {
    const rows = entry.rows ?? []
    console.log(
      `                  - ${entry.id.padEnd(22)} ${entry.status.padEnd(9)} ` +
        `${entry.summary.passed}/${entry.summary.total} rows  ${coverageTally(entry.coverage)}`
    )
    for (const row of rows.filter((candidate) => candidate.status !== 'passed')) {
      console.log(`                      ! ${row.id}: ${row.detail ?? 'mismatch'}`)
      // A row asserting a handoff target can fail with its dispositions
      // byte-identical, so the two are reported apart, never merged.
      if (row.expectedHandoffTarget !== undefined) {
        console.log(
          `                        expected target ${describeHandoffTarget(row.expectedHandoffTarget)}` +
            ` · actual ${describeHandoffTarget(row.actualHandoffTarget ?? '')}`
        )
      }
    }
    for (const probe of (entry.coverage ?? []).filter((p) => p.status !== 'covered')) {
      console.log(`                      ~ unwitnessed ${probe.probe}`)
    }
  }
  if (options.expectMatrixStatus !== undefined && payload.status !== options.expectMatrixStatus) {
    console.error(`expected matrix status ${options.expectMatrixStatus}, got ${payload.status}`)
    process.exit(1)
  }
}

/**
 * The project's configured graph matrices.
 *
 * The walk order printed here is read out of the coverage report's own node
 * namespacing, because that is the only account of a graph's nodes the wire
 * carries — the payload reports no node list, no node-to-pack mapping and no
 * edge endpoints. See the README's upstream gaps.
 */
async function testGraphs(): Promise<GraphSuite> {
  const result = await client.callTool({ name: 'experimental_test_graphs', arguments: {} })
  const text = textOf(result)
  if (result.isError) {
    console.error(`experimental_test_graphs refused: ${text}`)
    process.exit(1)
  }
  const payload = JSON.parse(text) as GraphSuite
  const graphs = payload.graphs ?? []
  console.log(
    `test_graphs   ok  status=${payload.status} ` +
      `rows=${payload.summary.passed}/${payload.summary.total} ` +
      `mismatched=${payload.summary.mismatched} graphs=${graphs.length}`
  )
  if (graphs.length === 0) {
    console.log('                  (this project configures no graph, which is an answer and not an error)')
  }
  for (const entry of graphs) {
    const nodes = nodesInWalkOrder(entry.coverage)
    console.log(
      `                  - ${entry.id.padEnd(22)} ${entry.status.padEnd(9)} ` +
        `${entry.summary.passed}/${entry.summary.total} rows  ${coverageTally(entry.coverage)}`
    )
    console.log(`                    walk: ${nodes.join(' → ') || '(no node named)'}`)
    for (const row of entry.rows ?? []) {
      const named = (row.nodes ?? []).map((node) => `${node.node}=${node.status}`).join(' ')
      console.log(
        `                    ${row.status === 'passed' ? ' ' : '!'} ${row.id.padEnd(32)} ` +
          `${row.status}${named ? `  [${named}]` : ''}`
      )
    }
    for (const probe of (entry.coverage ?? []).filter((p) => p.status !== 'covered')) {
      const parsed = parseProbe(probe.probe)
      console.log(
        `                      ~ unwitnessed ${parsed.node ? `${parsed.node}: ` : ''}${parsed.rest}`
      )
    }
  }
  return payload
}

/**
 * The graph suite's own status, held to the expectation — after the binding.
 *
 * Split out of `testGraphs` so the exit happens last. A run that mismatched
 * still has a digest, and which bytes it mismatched over is the first thing
 * anyone reading a red run wants: a mismatch that appeared because the file was
 * edited mid-drive looks identical to a mismatch in the rows until the binding
 * says which revision was read.
 *
 * With no explicit expectation, the payload's own shape sets one: a project
 * whose graphs are present must pass their rows, and skipped is accepted only
 * where there is no graph to run. An empty default that accepted mismatch would
 * let a graph whose rows never load read as a green acceptance.
 */
function checkGraphSuiteStatus(payload: GraphSuite): void {
  const expected =
    options.expectGraphStatus ?? ((payload.graphs ?? []).length > 0 ? 'passed' : 'skipped')
  if (payload.status !== expected) {
    console.error(`expected graph status ${expected}, got ${payload.status}`)
    process.exit(1)
  }
}

/**
 * The graph-serving pair, exercised (ADR-0029).
 *
 * This is the call the walk diagram draws its real edges from, so what is
 * checked here is exactness rather than plausibility: the served text must be
 * the document byte for byte. Two independent checks say so — the metadata's
 * own byte count and sha256 over the text this client received, which needs no
 * file at all, and a local read of the file where one is named. The digest is
 * bare hex on this payload, not the `sha256:`-prefixed form the lock and audit
 * records use, and is compared as the payload spells it.
 *
 * The walk is then derived with the page's own code and printed with its
 * arrows, so a shape the browser would draw wrongly is visible here first.
 */
async function graphDocument(
  graphId: string,
  graphFile: string | undefined,
  suite: GraphSuite | undefined
): Promise<void> {
  for (const [name, present] of [
    ['experimental_list_graphs', capabilities.graphInventorySupported],
    ['experimental_get_graph', capabilities.graphDocumentSupported]
  ] as const) {
    if (!present) {
      console.error(
        `--graph-document needs ${name}, which this runtime does not advertise ` +
          '(ADR-0029; jpack 0.18.0 and older have neither). The desk falls back to the ' +
          'coverage-derived walk against such a runtime, and this check is not applicable to it.'
      )
      process.exit(1)
    }
  }

  const rows = await listGraphs()
  if (!rows.some((row) => row.id === graphId)) {
    console.error(`the inventory lists no graph configured as ${graphId}`)
    process.exit(1)
  }

  const { served, meta, digest } = await fetchServedGraph(graphId)

  if (graphFile) {
    const onDisk = await readFile(graphFile, 'utf8')
    if (onDisk !== served) {
      console.error(`the served text is not ${graphFile} byte for byte`)
      process.exit(1)
    }
    console.log(`                  text matches ${graphFile} exactly`)
  }

  const read = readServedDocument(meta, served)
  if (meta.status !== 'valid') {
    // An undecodable document is a *successful* call the runtime meant to make,
    // so it is reported and not treated as a transport failure. The desk falls
    // back to the coverage walk on exactly this, and says why — with the
    // runtime's own sentence, which is the one printed here.
    console.log(`                  the runtime could not decode this document; no walk is derived`)
    console.log(`                  reason as the desk shows it: ${read.ok ? '(none)' : read.reason}`)
    if (read.ok) {
      console.error('the desk read a document the runtime reported it could not decode')
      process.exit(1)
    }
    return
  }
  if (!read.ok) {
    console.error(`the served document did not yield the shape the desk draws from: ${read.reason}`)
    process.exit(1)
  }

  // One matrix run answers both questions: whether these bytes are the bytes
  // that run decoded, and what order it evaluated the nodes in. Where
  // `--graphs` already ran the suite, that run is the one read here.
  const entry = await graphEntryFor(graphId, suite)
  checkGraphBinding(graphId, digest, entry)

  // The layering the page would do, with the tie-break the page uses: the walk
  // order of this graph's own coverage report. Passing an empty coverage here
  // would exercise a tie-break the browser never takes.
  const coverage = entry?.coverage ?? []
  console.log(
    `  walk order      ${nodesInWalkOrder(coverage).join(' → ') || '(coverage names no node)'}`
  )
  const layout = deriveWalkLayout(read.document, coverage)
  if (!layout.drawn) {
    console.error(`the desk declines to draw this document: ${layout.reason}`)
    process.exit(1)
  }
  const shape = layout.shape
  console.log(
    `  walk            ${shape.nodes.length} nodes in ${shape.depth} ` +
      `${shape.depth === 1 ? 'layer' : 'layers'}, ${shape.edges.length} declared ` +
      `${shape.edges.length === 1 ? 'edge' : 'edges'}`
  )
  for (const node of shape.nodes) {
    console.log(
      `    layer ${node.layer}  ${node.id.padEnd(24)} pack=${node.pack ?? '(none)'}` +
        `${node.isResult ? '  [declared result]' : ''}`
    )
  }
  for (const edge of shape.edges) {
    console.log(
      `    edge ${edge.index}    ${edge.from} -> ${edge.to}  carries ${edgeCarries(edge)}` +
        `${edge.drawable ? '' : '  [endpoint not declared — not drawn]'}`
    )
  }
  if (shape.result !== undefined && shape.resultDangling) {
    console.error(`the document declares result ${shape.result} and declares no node by that name`)
    process.exit(1)
  }
}

/**
 * The graph leg that is not asked for: run where the runtime can answer it,
 * skipped in one line where it cannot.
 *
 * `--graph-document` refuses a runtime without the tools because naming a graph
 * is asking for the check. Naming none asks a different question — *what can
 * this runtime tell me about its graphs* — and jpack 0.18.0's answer to that is
 * "nothing", which is an answer rather than a failure. So a drive against an
 * older runtime prints one line here and still ends OK.
 *
 * What it proves where it does run is the join the desk's graphs page makes:
 * the document served by one call and the matrix run made by another describe
 * one revision of one file (ADR-0030). Two calls, one file, and until that
 * member existed nothing in either payload could say so.
 */
async function graphBinding(suite: GraphSuite | undefined): Promise<void> {
  // Named exactly, never inferred. A runtime advertising one of the two and not
  // the other is a real shape, and a line claiming it advertises neither would
  // be false about the half it has. No version is read here either: what a
  // runtime can do is what it advertises, and a version string is a guess about
  // that — jpack 0.18.0 is one runtime that advertises neither, named as a
  // known example rather than as the diagnosis.
  const missing = [
    capabilities.graphInventorySupported ? undefined : 'experimental_list_graphs',
    capabilities.graphDocumentSupported ? undefined : 'experimental_get_graph'
  ].filter((name): name is string => name !== undefined)
  if (missing.length > 0) {
    console.log(
      `graph binding skipped  this runtime advertises no ${missing.join(' and no ')}; ` +
        'both are needed to choose a graph and fetch its document without being told which one ' +
        '(ADR-0029, which jpack 0.18.0 predates)'
    )
    return
  }

  const rows = await listGraphs()
  if (rows.length === 0) {
    console.log(
      '                  (this project configures no graph, which is an answer and not an error)'
    )
    return
  }

  // A graph declaring no rows is skipped before its document is loaded, so its
  // entry carries no digest to compare — not a failure, just nothing to bind.
  // Preferring one that declares rows picks the graph that can answer.
  const chosen = chooseGraphToBind(rows)
  const { digest } = await fetchServedGraph(chosen.id)
  checkGraphBinding(chosen.id, digest, await graphEntryFor(chosen.id, suite))
}

/**
 * Which configured graph this drive binds, and why that one.
 *
 * Preference order, most informative first: a graph that declares rows *and*
 * whose inventory row decoded — identity read, no detail — because only such a
 * graph yields a matrix entry carrying a digest to compare. Then any graph that
 * declares rows. Then the first configured graph, which will report that it
 * states no binding and is still worth saying so about.
 *
 * The fallbacks are not failures, and preferring the clean row is the point: a
 * project whose *first* graph carries a decode detail would otherwise end this
 * leg green and unbound while a later graph could have bound. The chosen id and
 * the reason are printed, so a green run always says what it bound.
 */
function chooseGraphToBind(rows: GraphSummary[]): GraphSummary {
  const bindable = rows.find((row) => row.rowsDeclared && row.graphId && !row.detail)
  const withRows = rows.find((row) => row.rowsDeclared)
  const chosen = bindable ?? withRows ?? rows[0]!
  const why = bindable
    ? 'declares rows, and its inventory row decoded'
    : withRows
      ? 'declares rows; no configured graph both declares rows and decoded cleanly'
      : 'the only configured graph, and it declares no rows'
  console.log(`                  binding ${chosen.id} — ${why}`)
  return chosen
}

/**
 * The matrix entry for one graph, from the run already made where there is one.
 *
 * Reusing the suite is what keeps a drive to one run. A targeted call is made
 * only when no suite was run at all, and then it is the drive's only run.
 */
async function graphEntryFor(
  graphId: string,
  suite: GraphSuite | undefined
): Promise<GraphSuiteEntry | undefined> {
  if (suite) return (suite.graphs ?? []).find((candidate) => candidate.id === graphId)
  return graphMatrixEntry(graphId)
}

/**
 * The configured inventory, listed and printed (ADR-0029).
 *
 * Listing is not validating: a row may carry a detail and no identity at all,
 * because the runtime leaves the identity members empty rather than guessing
 * them off a document it could not read. Printed the same way here.
 */
async function listGraphs(): Promise<GraphSummary[]> {
  const listed = await client.callTool({ name: 'experimental_list_graphs', arguments: {} })
  if (listed.isError) {
    console.error(`experimental_list_graphs refused: ${textOf(listed)}`)
    process.exit(1)
  }
  const inventory = JSON.parse(textOf(listed)) as GraphInventory
  const rows = inventory.graphs ?? []
  console.log(
    `list_graphs   ok  status=${inventory.status} graphs=${rows.length} ` +
      `configVersion=${inventory.configVersion}`
  )
  for (const row of rows) {
    console.log(
      `                  - ${row.id.padEnd(24)} ${(row.graphId || '(identity not read)').padEnd(24)} ` +
        `v${row.graphVersion || '?'} format ${row.formatVersion || '?'} ` +
        // Absent, never zero: a malformed document must not look honestly empty.
        `${row.nodeCount ?? '?'} nodes ${row.edgeCount ?? '?'} edges ` +
        `result=${row.resultNode ?? '(none)'} rows=${row.rowsDeclared}`
    )
    if (row.detail) console.log(`                      ! ${row.detail}`)
  }
  return rows
}

/**
 * One served graph document, with the text checked against its own metadata.
 *
 * Two independent facts are asserted here and neither needs a file on disk: the
 * byte count the payload states, and the sha256 it states, both against the
 * text this client actually received. That is the desk's own binding check —
 * the arithmetic half of it — exercised over the wire rather than over a
 * fixture. The digest is bare hex on this payload, not the `sha256:`-prefixed
 * form the lock and audit records use, and is compared as the payload spells
 * it.
 *
 * Computing a digest is not deriving a verdict. It is byte arithmetic over
 * bytes the runtime handed us, and its only claim is that the two halves of one
 * answer agree with each other.
 */
async function fetchServedGraph(
  graphId: string
): Promise<{ served: string; meta: GraphDocumentMeta; digest: string }> {
  const fetched = await client.callTool({
    name: 'experimental_get_graph',
    arguments: { graph_id: graphId }
  })
  const blocks = textBlocksOf(fetched)
  const served = blocks.join('')
  if (fetched.isError) {
    console.error(`experimental_get_graph refused: ${served}`)
    process.exit(1)
  }
  // The block *count* is the question, not the joined length. A tool that
  // answered with one empty text block did answer, and a graph document that
  // happens to be an empty file is exactly that answer. A tool that answered
  // with no text block at all served no document — and joining nothing gives
  // the same empty string, so metadata claiming zero bytes and the digest of
  // the empty string would agree with it and print "text matches".
  if (blocks.length === 0) {
    console.error('experimental_get_graph returned no text content, so no document was served')
    process.exit(1)
  }
  const meta = fetched.structuredContent as unknown as GraphDocumentMeta
  console.log(
    `get_graph     ok  status=${meta.status} id=${meta.id} graphId=${meta.graphId || '(not read)'} ` +
      `v${meta.graphVersion || '?'} format=${meta.formatVersion || '?'} ` +
      `result=${meta.resultNode ?? '(none)'} bytes=${meta.bytes} path=${meta.path}`
  )
  if (meta.detail) console.log(`                  detail: ${meta.detail}`)

  const bytes = Buffer.from(served, 'utf8')
  const digest = createHash('sha256').update(bytes).digest('hex')
  if (bytes.byteLength !== meta.bytes) {
    console.error(`the served text is ${bytes.byteLength} bytes and the payload says ${meta.bytes}`)
    process.exit(1)
  }
  // Read the way the page reads it. Hex is case-insensitive, so a runtime
  // spelling its digest in upper case states the same digest — and this script
  // failing a drive the desk would have bound would be reporting a disagreement
  // between two spellings as a disagreement about bytes.
  const stated = statedDigest(meta.sha256)
  if (stated === undefined) {
    console.error('the payload states no sha256 beside the text it served')
    process.exit(1)
  }
  if (stated !== digest) {
    console.error(`the served text hashes to ${digest} and the payload says ${stated}`)
    process.exit(1)
  }
  console.log(`                  text matches its own metadata: ${bytes.byteLength} bytes, sha256 ${digest}`)
  return { served, meta, digest }
}

/**
 * One configured graph's matrix entry, run.
 *
 * The whole entry rather than its coverage, because two different checks read
 * two different members of it and both must come from the *same* run: the
 * digest that run decoded, and the node order it evaluated in. A second call
 * for the second member would be a second read of a file that may have changed
 * between them.
 */
async function graphMatrixEntry(graphId: string): Promise<GraphSuiteEntry | undefined> {
  const result = await client.callTool({
    name: 'experimental_test_graphs',
    arguments: { graph_id: graphId }
  })
  if (result.isError) {
    console.error(`experimental_test_graphs refused: ${textOf(result)}`)
    process.exit(1)
  }
  const payload = JSON.parse(textOf(result)) as GraphSuite
  return (payload.graphs ?? []).find((candidate) => candidate.id === graphId)
}

/**
 * The ADR-0030 binding, checked against a live runtime.
 *
 * A graph matrix entry reports `graphSha256`, the digest of the exact bytes
 * that run decoded; `experimental_get_graph` reports the digest of the bytes it
 * served. Equal proves the two answers are about one revision, which is what
 * lets the desk join a served document to a matrix run at all. Unequal proves
 * the file was edited between the two calls — a real condition, and one that
 * makes this drive no longer a proof of anything, so it fails.
 *
 * It is a binding of bytes and not a verdict about either revision. Nothing
 * here reads a row, a status or a coverage probe: what the run *concluded* is
 * the runtime's to say, and this says only which bytes it concluded it about.
 *
 * An absent member is not a failure. It means this runtime states no binding —
 * jpack 0.18.0 and older, or an entry whose document never loaded — and the
 * desk's epoch-bounded fallback is what stands there, so this reports it and
 * moves on.
 */
function checkGraphBinding(
  graphId: string,
  servedDigest: string,
  entry: GraphSuiteEntry | undefined
): void {
  if (!entry) {
    console.error(
      `the graph matrix reported no entry for ${graphId}, so there is nothing to bind these bytes to`
    )
    process.exit(1)
  }
  // The page's own comparator, not a second one. It folds case and reads an
  // empty member as absent, so this script binds exactly the pairs the desk
  // binds and divides exactly the pairs it divides.
  const run = entry.graphSha256
  switch (bindGraphDigests(run, servedDigest)) {
    case 'unstated':
      console.log(
        `graph binding     this run reports no graphSha256 for ${graphId}, so nothing in the ` +
          `two answers binds them to each other` +
          `${entry.detail ? `; the entry's own detail: ${entry.detail}` : ''}`
      )
      return
    case 'divergent':
      console.error(
        `the graph matrix ran over ${statedDigest(run)} and the runtime served ${servedDigest}, ` +
          `so the two answers describe different revisions of ${graphId} and must not be joined`
      )
      process.exit(1)
    case 'bound':
      console.log(
        `graph binding ok  the matrix run and the served document report one digest: ` +
          `sha256 ${statedDigest(run)}`
      )
  }
}

/** How much of what a report derived is witnessed by a row. */
function coverageTally(coverage: MatrixProbe[] | undefined): string {
  const probes = coverage ?? []
  if (probes.length === 0) return 'coverage: none derived'
  const covered = probes.filter((probe) => probe.status === 'covered').length
  return `coverage: ${covered}/${probes.length} witnessed`
}

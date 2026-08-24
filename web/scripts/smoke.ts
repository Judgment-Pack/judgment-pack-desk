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
 */
import { readFile } from 'node:fs/promises'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { DeskWebSocketTransport } from '../src/mcp/transport.ts'
import { describeHandoffTarget, nodesInWalkOrder, parseProbe } from '../src/mcp/canonical.ts'
import type {
  Evaluation,
  GraphSuite,
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
}

function usage(message: string): never {
  console.error(message)
  console.error(
    'usage: smoke.ts <desk url with ?token=…> [--facts <path>] [--evidence <path>]\n' +
      '                [--pack <decision id>] [--trace <n>] [--expect-kind <kind>]\n' +
      '                [--expect-handoff <state>] [--matrix] [--graphs]\n' +
      '                [--expect-matrix-status <status>] [--expect-graph-status <status>]'
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
function textOf(result: unknown): string {
  const content = (result as { content?: unknown } | undefined)?.content
  const blocks = Array.isArray(content) ? content : []
  return blocks
    .filter((b): b is { type: 'text'; text: string } => (b as { type?: string })?.type === 'text')
    .map((b) => b.text)
    .join('')
}

const client = new Client({ name: 'jpack-desk-smoke', version: '0.1.0' }, { capabilities: {} })

let sawFileChange = false
client.fallbackNotificationHandler = async (notification) => {
  if (notification.method === 'desk/fileChanged') sawFileChange = true
}

await client.connect(new DeskWebSocketTransport(wsURL))

const server = client.getServerVersion()
console.log(`initialize    ok  serverInfo=${server?.name} ${server?.version}`)

const tools = await client.listTools()
console.log(`tools/list    ok  ${tools.tools.length} tools: ${tools.tools.map((t) => t.name).join(', ')}`)

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

if (options.graphs) {
  await testGraphs()
}

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
async function testGraphs(): Promise<void> {
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
  if (options.expectGraphStatus !== undefined && payload.status !== options.expectGraphStatus) {
    console.error(`expected graph status ${options.expectGraphStatus}, got ${payload.status}`)
    process.exit(1)
  }
}

/** How much of what a report derived is witnessed by a row. */
function coverageTally(coverage: MatrixProbe[] | undefined): string {
  const probes = coverage ?? []
  if (probes.length === 0) return 'coverage: none derived'
  const covered = probes.filter((probe) => probe.status === 'covered').length
  return `coverage: ${covered}/${probes.length} witnessed`
}

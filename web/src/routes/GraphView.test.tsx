import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import {
  connected,
  renderConnected,
  stubClient,
  testQueryClient,
  type ToolAnswer,
  type ToolHandler
} from '../testing/harness'
import { forgetDivergentPairs } from '../mcp/refetchLedger'
import {
  CLEAR,
  EMEA_QUEUE,
  PROCEED,
  REVIEW_QUEUE,
  UNRESOLVED
} from '../testing/dispositions'
import type { GraphInventory, GraphSuite } from '../mcp/types'
import { GraphView } from './GraphView'

afterEach(cleanup)
// Which divergent pairs have been asked about is the connection's memory, not
// a component's, so it outlives a render. Each case starts from a connection
// that has asked about nothing.
afterEach(forgetDivergentPairs)

/**
 * One configured graph, run: two nodes, one edge, coverage namespaced to the
 * node ids the document below declares. The two payloads describe one graph and
 * are joined by nothing but those names — which is the whole reason the view
 * insists both come from one settled reading of one connection.
 */
const MATRIX: GraphSuite = {
  status: 'passed',
  summary: { total: 1, passed: 1, mismatched: 0 },
  graphs: [
    {
      id: 'onboarding',
      status: 'passed',
      graphId: 'vendor-onboarding-flow',
      summary: { total: 1, passed: 1, mismatched: 0 },
      coverage: [
        { probe: 'node:screening:outcome:clear', status: 'covered' },
        { probe: 'node:decision:outcome:proceed', status: 'covered' },
        { probe: 'edge:0:resolved', status: 'covered' }
      ],
      rows: [
        {
          id: 'clear-approves',
          status: 'passed',
          expected: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
          actual: '{"kind":"outcome","outcomeId":"proceed","reasons":[]}',
          nodes: [
            {
              node: 'screening',
              status: 'passed',
              expected: '{"kind":"outcome","outcomeId":"clear","reasons":[]}',
              actual: '{"kind":"outcome","outcomeId":"clear","reasons":[]}'
            }
          ]
        }
      ]
    }
  ]
}

const INVENTORY: GraphInventory = {
  status: 'valid',
  configPath: '/project/jpack.json',
  configVersion: '2',
  graphs: [
    {
      id: 'onboarding',
      graphId: 'vendor-onboarding-flow',
      graphVersion: '0.1.0',
      formatVersion: '1',
      resultNode: 'decision',
      path: 'graphs/vendor-onboarding.graph.json',
      rowsDeclared: true,
      nodeCount: 2,
      edgeCount: 1
    }
  ]
}

const DOCUMENT = JSON.stringify({
  formatVersion: '1',
  id: 'vendor-onboarding-flow',
  version: '0.1.0',
  nodes: {
    screening: { pack: 'sanctions-screening' },
    decision: { pack: 'vendor-onboarding' }
  },
  edges: [
    {
      from: 'screening',
      to: 'decision',
      fact: '/vendor/sanctionsScreening/status',
      evidence: { id: 'sanctions-screening' }
    }
  ],
  result: 'decision'
})

/** The digest of the bytes served below, as `experimental_get_graph` reports it. */
const SERVED_DIGEST = 'a'.repeat(64)

/** A digest of some other revision of the same file: an edit landed between two calls. */
const OTHER_DIGEST = 'b'.repeat(64)

const SERVED_META = {
  status: 'valid',
  id: 'onboarding',
  graphId: 'vendor-onboarding-flow',
  graphVersion: '0.1.0',
  formatVersion: '1',
  path: 'graphs/vendor-onboarding.graph.json',
  bytes: DOCUMENT.length,
  sha256: SERVED_DIGEST
}

/** The same run, reporting the digest of the bytes its walk decoded (ADR-0030). */
function matrixBinding(graphSha256: string): GraphSuite {
  return { ...MATRIX, graphs: [{ ...MATRIX.graphs![0]!, graphSha256 }] }
}

/**
 * One answer that does not arrive in the tick it was asked for.
 *
 * The wire has latency, and the re-ask a divergence triggers only passes
 * through its in-flight state across one. An answer that resolves inside the
 * asking render collapses that state into a single commit, and a fixture
 * without latency therefore cannot tell a refetch asked once from one asked in
 * a loop: both read as two calls. With it, the guarded page asks twice and an
 * unguarded one asked 56 times over the same window.
 */
function afterATick(answer: ToolAnswer): Promise<ToolAnswer> {
  return new Promise((resolve) => setTimeout(() => resolve(answer), 5))
}

function servingDesk(overrides: Record<string, ToolHandler> = {}) {
  return stubClient({
    experimental_test_graphs: () => ({ text: JSON.stringify(MATRIX) }),
    experimental_list_graphs: () => ({ text: JSON.stringify(INVENTORY) }),
    experimental_get_graph: () => ({ text: DOCUMENT, structured: SERVED_META }),
    ...overrides
  })
}

function view() {
  return (
    <Routes>
      <Route path="/graphs" element={<GraphView />} />
      <Route path="/graphs/:graphId" element={<GraphView />} />
    </Routes>
  )
}

function serving(client: ReturnType<typeof stubClient>['client']) {
  return connected({ client, graphDocumentSupported: true, graphInventorySupported: true })
}

const arrows = (container: HTMLElement) => container.querySelectorAll('.diagram-edge-line').length

describe('the graphs page, against a runtime that serves documents', () => {
  it('draws the served document, and says which document it drew', async () => {
    const { client } = servingDesk()
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/declared edge/)
    // One arrow for the declared edge, one for the declared result.
    expect(arrows(container)).toBe(2)
    expect(container.textContent).toContain('drawn from the served document vendor-onboarding-flow')
  })

  it("refuses to draw a document the runtime could not decode, and shows the runtime's reason", async () => {
    // The text parses here — duplicate member names are last-wins in JSON.parse
    // and refused by the runtime's carrier decode — so nothing but the status
    // stops this page from drawing a graph the runtime already refused.
    const parseable =
      '{"formatVersion":"1","nodes":{"a":{"pack":"p"}},"nodes":{"b":{"pack":"q"}},"edges":[]}'
    const { client } = servingDesk({
      experimental_get_graph: () => ({
        text: parseable,
        structured: {
          ...SERVED_META,
          status: 'undecodable',
          graphId: '',
          detail: 'graph document graphs/vendor-onboarding.graph.json: Object member name is duplicated.'
        }
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/could not decode it/)
    expect(arrows(container)).toBe(0)
    // Verbatim: the runtime's own sentence, path and all.
    expect(container.textContent).toContain('Object member name is duplicated.')
    expect(container.textContent).toContain('serving is not validating')
    // And the fallback underneath is the coverage walk, saying what it is.
    expect(container.textContent).toContain('no arrow is drawn between two')
  })

  it('falls back and names the member where the document decoded and could not be read', async () => {
    const { client } = servingDesk({
      experimental_get_graph: () => ({
        text: '{"formatVersion":"1","nodes":{"screening":{"pack":"p"}}}',
        structured: SERVED_META
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/`edges` member/)
    expect(arrows(container)).toBe(0)
    // The claim a coerced empty array would have produced is nowhere on screen.
    expect(container.textContent).not.toContain('This document declares no edge')
  })

  it('stops drawing when the runtime it reconnects to no longer advertises the tool', async () => {
    // The document stays in the cache. What it may be drawn from is the
    // capability the *current* connection reports, so a reconnect to jpack
    // 0.18.0 withdraws the drawing rather than leaving arrows no live
    // capability accounts for.
    const { client } = servingDesk()
    const { container, setConnection } = renderConnected(view(), serving(client), {
      path: '/graphs'
    })
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)

    setConnection(
      connected({ client, graphDocumentSupported: false, graphInventorySupported: true })
    )
    await waitFor(() => expect(arrows(container)).toBe(0))
    // Nothing went wrong, so nothing is apologised for.
    expect(container.textContent).not.toContain('could not decode')
    expect(container.textContent).toContain('no arrow is drawn between two')
  })

  it('draws nothing from the connection before a reconnect', async () => {
    // The document query is keyed by the connection epoch, so a new connection
    // starts with no document at all rather than with the last one's.
    const { client, calls } = servingDesk()
    const { container, setConnection } = renderConnected(view(), serving(client), {
      path: '/graphs'
    })
    await screen.findByText(/declared edge/)
    const before = calls.filter((call) => call.name === 'experimental_get_graph').length

    setConnection(
      connected({
        client,
        graphDocumentSupported: true,
        graphInventorySupported: true,
        connectionEpoch: 2
      })
    )
    await waitFor(() =>
      expect(calls.filter((call) => call.name === 'experimental_get_graph').length).toBe(before + 1)
    )
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)
  })

  it('withdraws the drawing while the matrix it is joined to is being re-run', async () => {
    // The coverage a node's "not represented in coverage" line rests on comes
    // from the matrix run. While that run is in flight the previous run's
    // coverage is what is in hand, and joining it to a document is a claim
    // about a reading that is no longer current.
    let matrixCalls = 0
    const { client } = servingDesk({
      experimental_test_graphs: () => {
        matrixCalls += 1
        if (matrixCalls === 1) return { text: JSON.stringify(MATRIX) }
        return new Promise<never>(() => {})
      }
    })
    const queryClient = testQueryClient()
    const { container } = renderConnected(view(), serving(client), {
      path: '/graphs',
      queryClient
    })
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)

    await act(async () => {
      void queryClient.invalidateQueries({ queryKey: ['experimental_test_graphs', null] })
      await Promise.resolve()
    })
    await waitFor(() => expect(arrows(container)).toBe(0))
    expect(container.textContent).toContain('the served graph document')
  })

  it('draws the walk and states the binding where the two digests agree', async () => {
    // ADR-0030: the matrix entry reports the digest of the bytes its walk
    // decoded, and it is the digest served beside the document. The join is
    // therefore proven rather than bounded, and the page says which.
    const { client } = servingDesk({
      experimental_test_graphs: () => ({ text: JSON.stringify(matrixBinding(SERVED_DIGEST)) })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/One revision/)
    expect(arrows(container)).toBe(2)
    expect(container.textContent).toContain('same document digest')
    expect(container.textContent).toContain(`sha256 ${SERVED_DIGEST.slice(0, 12)}`)
    // The binding is provenance of the join. It is not a second verdict on the
    // run, and the runtime's own verdict is untouched beside it.
    expect(container.textContent).toContain('not a verdict on the revision')
    expect(container.textContent).not.toContain('different revision')
  })

  it('withdraws the join where the digests name two revisions, and asks for both again', async () => {
    // The failure this exists to prevent: one revision's rows drawn against
    // another revision's arrows, with nothing on screen saying so. Reported as
    // a HIGH finding against the epoch-only version of this join.
    const { client, calls } = servingDesk({
      experimental_test_graphs: () =>
        afterATick({ text: JSON.stringify(matrixBinding(OTHER_DIGEST)) }),
      experimental_get_graph: () => afterATick({ text: DOCUMENT, structured: SERVED_META })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/Two revisions, not joined/)
    expect(container.textContent).toContain('edited between the two calls')
    expect(container.textContent).toContain(`sha256 ${OTHER_DIGEST.slice(0, 12)}`)
    expect(container.textContent).toContain(`sha256 ${SERVED_DIGEST.slice(0, 12)}`)
    // Neither answer is called wrong; the desk overrides no runtime verdict.
    expect(container.textContent).toContain('Neither revision is being called wrong')

    // Both answers are asked for again, so the next pair can re-bind.
    const asked = (name: string) => calls.filter((call) => call.name === name).length
    await waitFor(() => {
      expect(asked('experimental_get_graph')).toBe(2)
      expect(asked('experimental_test_graphs')).toBe(2)
    })

    // And it settles there. A file that is still mid-edit lands the same two
    // digests again, which must read as a standing withdrawal rather than spin
    // the page asking forever. The window is long enough for a spin to show:
    // an unguarded page ran through 56 calls over one this size.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    expect(asked('experimental_get_graph')).toBe(2)
    expect(asked('experimental_test_graphs')).toBe(2)

    // Settled, with the same disagreement in hand: the withdrawal stands, and
    // nothing is joined — no arrow drawn from a document the rows are not
    // about. Asserted here rather than at the first sight of the notice,
    // because a page merely waiting for an answer draws no arrow either.
    expect(container.textContent).toContain('Two revisions, not joined')
    expect(arrows(container)).toBe(0)
  })

  it('asks once about a pair it has seen, however many other pairs came between', async () => {
    // A file edited back and forth alternates between two revisions, so the
    // runtime answers B, then C, then B again. A memory holding only the pair
    // asked about last reads that third answer as new and asks forever, running
    // the whole graph suite each time.
    const THIRD_DIGEST = 'c'.repeat(64)
    const cycle = [OTHER_DIGEST, THIRD_DIGEST]
    let run = 0
    const { client, calls } = servingDesk({
      experimental_test_graphs: () => {
        const digest = cycle[run % cycle.length]!
        run += 1
        return afterATick({ text: JSON.stringify(matrixBinding(digest)) })
      },
      experimental_get_graph: () => afterATick({ text: DOCUMENT, structured: SERVED_META })
    })
    renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/Two revisions, not joined/)

    const asked = (name: string) => calls.filter((call) => call.name === name).length
    // One cycle for the first pair, one for the second, and none for the third
    // answer, which is the first pair again.
    await waitFor(() => expect(asked('experimental_test_graphs')).toBe(3))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    expect(asked('experimental_test_graphs')).toBe(3)
    expect(asked('experimental_get_graph')).toBe(3)
    expect(screen.getByText(/Two revisions, not joined/)).toBeTruthy()
  })

  it('does not ask again about a seen pair after the entry unmounts and comes back', async () => {
    // Routing to one graph and back unmounts and remounts the entry. A memory
    // living in the component would come back empty and ask again about a pair
    // it had already asked about — the same spin, reached by another road.
    const { client, calls } = servingDesk({
      experimental_test_graphs: () =>
        afterATick({ text: JSON.stringify(matrixBinding(OTHER_DIGEST)) }),
      experimental_get_graph: () => afterATick({ text: DOCUMENT, structured: SERVED_META })
    })
    const queryClient = testQueryClient()
    const asked = (name: string) => calls.filter((call) => call.name === name).length

    const first = renderConnected(view(), serving(client), { path: '/graphs', queryClient })
    await screen.findByText(/Two revisions, not joined/)
    await waitFor(() => expect(asked('experimental_test_graphs')).toBe(2))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    expect(asked('experimental_test_graphs')).toBe(2)

    first.unmount()
    // The same connection and the same cache: what is answered here is what was
    // already answered, and it is the pair already asked about.
    renderConnected(view(), serving(client), { path: '/graphs', queryClient })
    await screen.findByText(/Two revisions, not joined/)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    expect(asked('experimental_test_graphs')).toBe(2)
    expect(asked('experimental_get_graph')).toBe(2)
  })

  it('reads one pair spelled two ways as the one pair it is', async () => {
    // Hex is case-insensitive, so the same disagreement reported in upper case
    // and then in lower is one disagreement. An identity built from the raw
    // strings would file the two separately and ask about each in turn, which
    // over an alternating pair of spellings never ends.
    const spellings = [` ${OTHER_DIGEST.toUpperCase()} `, OTHER_DIGEST]
    let run = 0
    const { client, calls } = servingDesk({
      experimental_test_graphs: () => {
        const digest = spellings[run % spellings.length]!
        run += 1
        return afterATick({ text: JSON.stringify(matrixBinding(digest)) })
      },
      experimental_get_graph: () => afterATick({ text: DOCUMENT, structured: SERVED_META })
    })
    renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/Two revisions, not joined/)

    const asked = (name: string) => calls.filter((call) => call.name === name).length
    await waitFor(() => expect(asked('experimental_test_graphs')).toBe(2))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    // One cycle, not one per spelling.
    expect(asked('experimental_test_graphs')).toBe(2)
    expect(asked('experimental_get_graph')).toBe(2)
  })

  it('claims no binding where the matrix run states no digest', async () => {
    // jpack 0.18.0 and older, and any entry whose document did not load: there
    // is nothing to compare, so the epoch-bounded behaviour stands exactly as
    // it was and the page asserts nothing about the join in either direction.
    const { client } = servingDesk()
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)
    expect(container.textContent).not.toContain('One revision')
    expect(container.textContent).not.toContain('Two revisions, not joined')
    expect(container.textContent).not.toContain('edited between the two calls')
  })

  it('reports an inventory that refused, and shows no configuration it cannot confirm', async () => {
    const { client } = servingDesk({
      experimental_list_graphs: () => ({
        text: 'this project declares no jpack.json under /project',
        isError: true
      })
    })
    const { container } = renderConnected(view(), serving(client), { path: '/graphs' })
    await screen.findByText(/could not be listed/)
    expect(container.textContent).toContain('this project declares no jpack.json under /project')
    // The section named "Configured" is absent rather than showing what a
    // failed call could not confirm.
    expect(container.textContent).not.toContain('Configured')
    // The matrix still ran, and still reports what it can.
    await screen.findByText(/declared edge/)
    expect(arrows(container)).toBe(2)
  })
})

/* Node traces (ADR-0031) and handoff-target assertions (ADR-0032) ---------- */

/**
 * One run of a graph whose rows assert targets and whose comparisons carry
 * traces — the payload a jpack 0.19.0 runtime returns when asked.
 *
 * Every row here is a shape the runtime can actually produce, and the four are
 * kept apart because the runtime keeps them apart:
 *
 * - `clear-approves` compared a composite and its named nodes. Its dispositions
 *   requested no handoff, so both target assertions are the literal `null` —
 *   a named target is reported exactly when the disposition requested one. The
 *   headline is byte-identical and the row still mismatched, on a node.
 * - `escalating-vendor` asserts a target the run did not report, on a
 *   disposition that *did* request a handoff. It carries **no node
 *   comparisons**: a composite target mismatch is decided before the node
 *   comparisons are built, so a row cannot show both.
 * - `refused-walk` expected a disposition and the run was refused, which is the
 *   one carrier on which `unavailable` is reachable.
 * - `names-an-undeclared-node` names a node the graph does not declare. The
 *   comparison exists and is a mismatch; the node was never evaluated, so it
 *   carries no trace *even though traces were asked for*.
 *
 * The comparisons are listed lexicographically by node name, as the runtime
 * lists them — `decision` before `screening` — while each trace inside one is
 * the evaluator's own walk order. Two different orders, and the fixture carries
 * both so a view that conflated them would be visible here.
 */
const TRACED: GraphSuite = {
  status: 'mismatch',
  configPath: '/project/jpack.json',
  configVersion: '2',
  summary: { total: 4, passed: 0, mismatched: 4 },
  graphs: [
    {
      id: 'onboarding',
      status: 'mismatch',
      graphId: 'vendor-onboarding-flow',
      graphSha256: SERVED_DIGEST,
      summary: { total: 4, passed: 0, mismatched: 4 },
      coverage: [{ probe: 'node:screening:outcome:clear', status: 'covered' }],
      rows: [
        {
          id: 'clear-approves',
          status: 'mismatch',
          expected: PROCEED,
          actual: PROCEED,
          // The result node's disposition requested no handoff, so the
          // composite's reported target is the literal null — and the row
          // asserts exactly that.
          expectedHandoffTarget: 'null',
          actualHandoffTarget: 'null',
          nodes: [
            {
              node: 'decision',
              status: 'passed',
              expected: PROCEED,
              actual: PROCEED,
              expectedHandoffTarget: 'null',
              actualHandoffTarget: 'null',
              // Asked and evaluated, and nothing walked. Not the same as absent.
              trace: []
            },
            {
              node: 'screening',
              status: 'mismatch',
              expected: CLEAR,
              actual: UNRESOLVED,
              trace: [
                { stage: 'applicability', condition: 'true' },
                { stage: 'exception', id: 'sanctioned-jurisdiction', condition: 'false' },
                { stage: 'rule', id: 'screen-clear', condition: 'unknown', onUnknown: 'escalate' }
              ]
            }
          ]
        },
        {
          id: 'escalating-vendor',
          status: 'mismatch',
          // Byte-identical dispositions, different destinations: the exact
          // defect class the assertion exists for.
          expected: UNRESOLVED,
          actual: UNRESOLVED,
          expectedHandoffTarget: REVIEW_QUEUE,
          actualHandoffTarget: EMEA_QUEUE,
          detail: 'the composite handoff target did not match the assertion'
        },
        {
          id: 'refused-walk',
          status: 'mismatch',
          expected: PROCEED,
          actual: '',
          actualErrorClass: 'malformed-input',
          actualErrorPhase: 'admission',
          expectedHandoffTarget: REVIEW_QUEUE,
          // The one reachable third state: the run was refused, so no target
          // can be stated. Not "no target" — the absence of an answer.
          actualHandoffTarget: 'unavailable',
          detail: 'the walk was refused before a composite was produced'
        },
        {
          id: 'names-an-undeclared-node',
          status: 'mismatch',
          expected: PROCEED,
          actual: PROCEED,
          nodes: [
            {
              node: 'shipping',
              status: 'mismatch',
              expected: PROCEED,
              // Never evaluated, so nothing was produced and nothing is traced.
              actual: ''
            }
          ],
          detail: 'the row names shipping, which this graph does not declare'
        }
      ]
    }
  ]
}

/** The same graph run without the ask: no trace member anywhere (ADR-0031). */
const UNTRACED: GraphSuite = {
  ...TRACED,
  graphs: [
    {
      ...TRACED.graphs![0]!,
      rows: TRACED.graphs![0]!.rows!.map((row) => ({
        ...row,
        nodes: row.nodes?.map(({ trace: _trace, ...node }) => node)
      }))
    }
  ]
}

/**
 * A desk whose graph matrix answers the ask, and records having been asked.
 *
 * The handler branches on the argument rather than ignoring it, because the
 * whole point of the control is that two different calls produce two different
 * payloads — a stub that answered the same either way would let a view that
 * never sends the argument pass.
 */
function tracingDesk(overrides: Record<string, ToolHandler> = {}) {
  return stubClient({
    experimental_test_graphs: (args) => ({
      text: JSON.stringify(args.include_traces === true ? TRACED : UNTRACED)
    }),
    experimental_list_graphs: () => ({ text: JSON.stringify(INVENTORY) }),
    experimental_get_graph: () => ({ text: DOCUMENT, structured: SERVED_META }),
    ...overrides
  })
}

/** A traced ask that fails one way, and an untraced ask that succeeds. */
function failingTracedDesk(failure: ToolAnswer) {
  return tracingDesk({
    experimental_test_graphs: (args) =>
      args.include_traces === true ? failure : { text: JSON.stringify(UNTRACED) }
  })
}

function tracing(client: ReturnType<typeof stubClient>['client'], overrides = {}) {
  return connected({
    client,
    graphDocumentSupported: true,
    graphInventorySupported: true,
    graphTracesSupported: true,
    ...overrides
  })
}

const matrixCalls = (calls: { name: string; args: Record<string, unknown> }[]) =>
  calls.filter((call) => call.name === 'experimental_test_graphs')

/** The one control this page offers, wherever on the page it happens to be. */
const askBox = () => screen.getByRole('checkbox')

/**
 * The failed-request notice alone.
 *
 * Scoped, because the *control* legitimately discloses the mechanism — traces
 * are charged against the report budget, and saying so on the control is a cost
 * disclosure rather than a diagnosis. What must not diagnose is this notice,
 * which stands beside one particular message the page has not parsed.
 */
const askNotice = () => screen.getByText(/This request asked for traces/)

describe('the graphs page, against a runtime that reports node traces (ADR-0031)', () => {
  it('offers no ask where the runtime does not advertise the argument', async () => {
    // jpack 0.18.0: the tool exists and the argument does not, so sending it
    // would be refused rather than ignored. No control, and nothing else on
    // the page changes.
    const { client, calls } = tracingDesk()
    const { container } = renderConnected(
      view(),
      tracing(client, { graphTracesSupported: false }),
      { path: '/graphs' }
    )
    await screen.findAllByText(/clear-approves/)
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(container.textContent).not.toContain('Ask for traces')
    // The one call it made is the call it has always made.
    expect(matrixCalls(calls)).toHaveLength(1)
    expect(matrixCalls(calls)[0]!.args).toEqual({})
  })

  it('asks only when asked, and keeps the two answers apart', async () => {
    const { client, calls } = tracingDesk()
    renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)

    // Default off, and off omits the key entirely: byte-identical to the call
    // this desk made before the argument existed.
    expect(matrixCalls(calls)).toHaveLength(1)
    expect(matrixCalls(calls)[0]!.args).toEqual({})
    expect(screen.queryByText(/Trace of screening/)).toBeNull()

    fireEvent.click(askBox())
    await screen.findByText(/Trace of screening/)

    // A second call, carrying the argument. A shared query key would have
    // served the untraced payload back and made no call at all.
    await waitFor(() => expect(matrixCalls(calls)).toHaveLength(2))
    expect(matrixCalls(calls)[1]!.args).toEqual({ include_traces: true })

    // And back: the untraced answer is its own cache entry, so clearing the ask
    // costs no call and withdraws the traces.
    fireEvent.click(askBox())
    await waitFor(() => expect(screen.queryByText(/Trace of screening/)).toBeNull())
    expect(matrixCalls(calls)).toHaveLength(2)
  })

  it('renders each compared node’s trace with the evaluation view’s own renderer', async () => {
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())
    await screen.findByText(/Trace of screening/)

    // The staged walk, as the shared renderer draws it: stage headings, the
    // entry ids, and the badges the evaluate view shows.
    expect(container.textContent).toContain('applicability')
    expect(container.textContent).toContain('sanctioned-jurisdiction')
    expect(container.textContent).toContain('screen-clear')
    expect(container.textContent).toContain('on unknown: escalate')
    // The framing travels with the renderer rather than being restated here.
    expect(container.textContent).toContain('It decides nothing')
    // And the fact only this surface has: two orders, neither read off the other.
    expect(container.textContent).toContain('lexicographically by node name')
  })

  it('shows the trace of a comparison that mismatched, which is the one worth reading', async () => {
    // The node whose trace is rendered here is the node that failed. A view
    // that only traced passing comparisons would hide every trace anyone opens
    // the page for.
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    const heading = await screen.findByText(/Trace of screening/)
    const node = heading.closest('.row-node')
    expect(node).not.toBeNull()
    expect(node!.className).toContain('row-mismatch')
    expect(node!.textContent).toContain('screen-clear')
    // The runtime's verdict is untouched beside it: the trace explains, and
    // decides nothing.
    expect(node!.textContent).toContain('mismatch')
    expect(container.textContent).toContain('Trace of decision')
  })

  it('distinguishes a trace with no entries from a comparison carrying none', async () => {
    // `[]` is asked, evaluated, and nothing walked; absent is not asked, or not
    // evaluated. Collapsing them would report a runtime as having walked
    // nothing when it was never asked.
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())
    await screen.findByText(/Trace of decision/)
    expect(container.textContent).toContain("This node's evaluation carries no trace entries")
  })

  it('traces no comparison naming a node the graph does not declare, even when asked', async () => {
    // The comparison is real and is a mismatch; the node was never evaluated,
    // so there is no trace to carry and none is invented. The row's own detail
    // is what says why, and it is shown.
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())
    await screen.findByText(/Trace of screening/)

    const undeclared = [...container.querySelectorAll('.row-node')].find(
      (candidate) => candidate.querySelector('code')?.textContent === 'shipping'
    )
    expect(undeclared).not.toBeUndefined()
    expect(undeclared!.textContent).toContain('mismatch')
    // No trace section at all — not an empty one, which would claim the node
    // was evaluated and walked nothing.
    expect(undeclared!.querySelector('.section')).toBeNull()
    expect(undeclared!.textContent).not.toContain('carries no trace entries')
    expect(container.textContent).toContain('which this graph does not declare')
  })

  it('surfaces a failed traced request as the runtime’s own answer, without diagnosing it', async () => {
    // A tool error arrives in one unstructured shape whatever caused it, so the
    // page names what it knows — this request asked for traces — and leaves the
    // reason to the runtime's message. Naming the report budget here would be
    // diagnosing a message this page has not parsed.
    const refusal =
      'graph matrix report budget exceeded: 4 MiB with traces (4 rows, 3 node comparisons)'
    const { client } = failingTracedDesk({ text: refusal, isError: true })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    await screen.findByText(/This request asked for traces/)
    expect(container.textContent).toContain('did not produce a usable answer')
    expect(container.textContent).toContain(refusal)
    expect(container.textContent).toContain('Nothing here says these nodes have no traces')
    // The ask that failed is still reachable, so the page is not stranded on it.
    expect(askBox()).toBeTruthy()
  })

  it('says the same of a refusal that has nothing to do with the budget', async () => {
    // The page cannot tell a budget refusal from an argument this runtime
    // rejects, a configuration it could not find, or a graph id it does not
    // have: they arrive as one shape. So it claims none of them.
    const refusal = 'no graph configured as onboarding'
    const { client } = failingTracedDesk({ text: refusal, isError: true })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    await screen.findByText(/This request asked for traces/)
    expect(container.textContent).toContain(refusal)
    // The notice names no cause. The control above it discloses the mechanism,
    // which is a different sentence in a different place.
    expect(askNotice().textContent).not.toContain('budget')
  })

  it('says the same where the runtime answered and the answer could not be read', async () => {
    // Not a refusal at all: the runtime produced a response and this client
    // could not read it. Calling that "the runtime refused" or "the call did
    // not complete" would both attribute something that did not happen.
    const { client } = failingTracedDesk({ text: 'this is not JSON' })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    await screen.findByText(/This request asked for traces/)
    expect(container.textContent).toContain('did not produce a usable answer')
    expect(container.textContent).toContain('not JSON')
    expect(askNotice().textContent).not.toContain('refused')
    expect(askNotice().textContent).not.toContain('did not complete')
    expect(askNotice().textContent).not.toContain('budget')
  })

  it('promises a retry, not a restoration, where no untraced answer was ever had', async () => {
    // The toggle can be flipped before the untraced run has ever completed.
    // There is then nothing to go back to, and promising to "return to the
    // answer still in hand" would promise something that does not exist.
    const { client } = tracingDesk({
      experimental_test_graphs: (args) =>
        args.include_traces === true
          ? { text: 'report budget exceeded', isError: true }
          : new Promise<ToolAnswer>(() => {})
    })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    // The inventory is what puts the page — and the control — on screen while
    // the untraced matrix is still in flight.
    await screen.findByText(/Configured/)
    fireEvent.click(askBox())

    await screen.findByText(/This request asked for traces/)
    expect(container.textContent).toContain('Clear the ask to retry the untraced request')
    expect(container.textContent).not.toContain('still in hand')
  })

  it('promises a restoration only where the untraced answer is actually retained', async () => {
    const { client } = failingTracedDesk({ text: 'report budget exceeded', isError: true })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    await screen.findByText(/This request asked for traces/)
    expect(container.textContent).toContain('return to the untraced request')
    expect(container.textContent).toContain('still in hand')
  })

  it('returns to the untraced answer when the ask is cleared after a failure', async () => {
    const { client, calls } = failingTracedDesk({ text: 'report budget exceeded', isError: true })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())
    await screen.findByText(/This request asked for traces/)

    fireEvent.click(askBox())
    await waitFor(() =>
      expect(screen.queryByText(/This request asked for traces/)).toBeNull()
    )
    // The rows are back, from the answer that was retained rather than re-asked.
    await screen.findAllByText(/clear-approves/)
    expect(container.textContent).not.toContain('report budget exceeded')
    expect(matrixCalls(calls)).toHaveLength(2)
  })

  it('still returns to it when the ask is cleared a while after the failure', async () => {
    // A failed query is not retried on its own and the retained answer does not
    // expire, so a clear that comes later behaves exactly as an immediate one.
    const { client, calls } = failingTracedDesk({ text: 'report budget exceeded', isError: true })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())
    await screen.findByText(/This request asked for traces/)

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    // Nothing retried itself in the meantime.
    expect(matrixCalls(calls)).toHaveLength(2)

    fireEvent.click(askBox())
    await waitFor(() =>
      expect(screen.queryByText(/This request asked for traces/)).toBeNull()
    )
    await screen.findAllByText(/clear-approves/)
    expect(container.textContent).not.toContain('report budget exceeded')
    expect(matrixCalls(calls)).toHaveLength(2)
  })

  it('leaves the ask reachable even where there is no inventory to render beside it', async () => {
    // Without experimental_list_graphs there is no listing to fall back on, so
    // a failed run is the whole page. The control has to be on that page too,
    // or turning the ask off becomes impossible.
    const { client } = failingTracedDesk({ text: 'report budget exceeded', isError: true })
    const { container } = renderConnected(
      view(),
      tracing(client, { graphInventorySupported: false }),
      { path: '/graphs' }
    )
    await screen.findAllByText(/clear-approves/)
    fireEvent.click(askBox())

    await screen.findByText(/Could not run the graphs/)
    expect(askBox()).toBeTruthy()
    expect(container.textContent).toContain('report budget exceeded')
    expect(container.textContent).toContain('This request asked for traces')
  })

  it('shows no trace anywhere in an untraced payload', async () => {
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    expect(container.textContent).not.toContain('Trace of')
    expect(container.textContent).not.toContain('carries no trace entries')
  })
})

describe('the graphs page, against rows that assert a handoff target (ADR-0032)', () => {
  it('shows the composite pair where a row asserts one, and marks the assertion', async () => {
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/escalating-vendor/)

    expect(container.textContent).toContain('expected composite target')
    expect(container.textContent).toContain('actual composite target')
    // Rendered through the pack surface's own describer: kind beside name.
    expect(container.textContent).toContain('vendor-review (queue)')
    expect(container.textContent).toContain('vendor-review-emea (queue)')
    expect(container.textContent).toContain('asserts a handoff-target state')
  })

  it('shows no node comparison on a row whose composite target mismatched', async () => {
    // The composite target is decided before the node comparisons are built, so
    // a row that failed on it has none — and a view that implied otherwise
    // would describe a shape the runtime does not produce.
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/escalating-vendor/)
    const row = [...container.querySelectorAll('.row')].find(
      (candidate) => candidate.querySelector('.row-id')?.textContent === 'escalating-vendor'
    )
    expect(row).not.toBeUndefined()
    expect(row!.querySelectorAll('.row-node')).toHaveLength(0)
    expect(row!.textContent).not.toContain('reported node comparison')
  })

  it('keeps “no target” and “unavailable” apart, because they are two things', async () => {
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/refused-walk/)

    // The row that asserts there is no target at all.
    expect(container.textContent).toContain('no target')
    expect(container.textContent).toContain('asserts no handoff target')
    // The row whose run was refused, so no target can be stated.
    expect(container.textContent).toContain('unavailable')
  })

  it('shows a node’s own pair beside that node’s comparison', async () => {
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/clear-approves/)
    const node = [...container.querySelectorAll('.row-node')].find(
      (candidate) => candidate.querySelector('code')?.textContent === 'decision'
    )
    expect(node).not.toBeUndefined()
    expect(node!.textContent).toContain('expected target of decision')
    expect(node!.textContent).toContain('actual target of decision')
    expect(node!.textContent).toContain('no target')
    // The node the row asserted nothing about carries no pair.
    expect(container.textContent).not.toContain('expected target of screening')
  })

  it('shows nothing where only one half of a pair arrived', async () => {
    // The two members are one pair and appear together. Half a pair would read
    // as an assertion that was made and never answered, which is not a state
    // the runtime reports.
    const half: GraphSuite = {
      ...UNTRACED,
      graphs: [
        {
          ...UNTRACED.graphs![0]!,
          rows: [
            {
              id: 'half-a-pair',
              status: 'passed',
              expected: PROCEED,
              actual: PROCEED,
              expectedHandoffTarget: REVIEW_QUEUE
            }
          ]
        }
      ]
    }
    const { client } = tracingDesk({
      experimental_test_graphs: () => ({ text: JSON.stringify(half) })
    })
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/half-a-pair/)
    expect(container.querySelectorAll('.row-targets')).toHaveLength(0)
    expect(container.textContent).not.toContain('expected composite target')
    expect(container.textContent).not.toContain('asserts a handoff-target state')
  })

  it('marks no difference of its own on a pair, because renderings are not the verdict', async () => {
    // The two composite renderings differ and the row mismatched, but the mark
    // that says so is the row's status — never a comparison this client made.
    // A capped rendering can differ from its own pair past the cap, so a mark
    // drawn from these strings could contradict what the runtime decided.
    const { client } = tracingDesk()
    const { container } = renderConnected(view(), tracing(client), { path: '/graphs' })
    await screen.findAllByText(/escalating-vendor/)
    const targets = container.querySelectorAll('.row-targets .row-side')
    expect(targets.length).toBeGreaterThan(0)
    for (const side of targets) {
      expect(side.className).not.toContain('row-side-differs')
    }
  })
})

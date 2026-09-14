import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { Button, ButtonLink } from '../ui/Button'
import { PacksNavigation } from '../packs/PacksNavigation'
import { recordActivity } from '../shell/consoleLog'
import { useLatestFlowResult } from '../packs/flows/useLatestFlowResult'
import { FlowExplorer } from '../packs/flows/FlowExplorer'
import workspace from '../packs/PackWorkspace.module.css'
import { CoverageReport } from '../components/CoverageReport'
import { GraphWalkDiagram } from '../components/GraphWalkDiagram'
import { Empty, ErrorBox, Loading, Pill, Section, statusTone } from '../components/primitives'
import { TargetPair, describeTargetAssertion } from '../components/TargetPair'
import { TracePanel } from '../components/TracePanel'
import { parseDisposition } from '../mcp/canonical'
import {
  bindGraphDigests,
  deriveWalkLayout,
  walkFallbackReason,
  type GraphDigestBinding
} from '../mcp/graphDocument'
import { useMcp } from '../mcp/McpProvider'
import { useGraphDocument, useGraphInventory, useGraphMatrix } from '../mcp/queries'
import { divergentPairIdentity, recordDivergentPair } from '../mcp/refetchLedger'
import type {
  GraphInventory,
  GraphSuiteEntry,
  GraphSummary,
  GraphTestNode,
  GraphTestRow
} from '../mcp/types'

/** Browsing reads declarations. Tests run only after an explicit command. */
export function GraphView() {
  const { graphId } = useParams<{ graphId?: string }>()
  const [search, setSearch] = useSearchParams()
  const tests = search.get('view') === 'tests'
  const { status, graphInventorySupported, graphTracesSupported } = useMcp()
  const inventory = useGraphInventory()
  const [includeTraces, setIncludeTraces] = useState(false)
  const asked = graphTracesSupported && includeTraces
  // Disabled observers retain results but cannot run on mount, focus, reconnect
  // or file invalidation. Switching trace options is also a read of cache only.
  const { data, error, isFetching, refetch } = useGraphMatrix(graphId, false, asked)
  const retainedUntraced = useGraphMatrix(graphId, false, false).data !== undefined
  const listing = inventory.error ? undefined : inventory.data
  const run = () => {
    if (status !== 'ready' || isFetching) return
    setSearch(previous => { const next = new URLSearchParams(previous); next.set('view', 'tests'); return next })
    recordActivity('Flow tests started.')
    void refetch().then(result => recordActivity(result.error ? 'Flow tests failed.' : `Flow tests completed: ${result.data?.status ?? 'no result'}.`))
  }

  return <article className="detail" data-measure="full" data-layout="page">
    <PageHeader title={graphId ? 'Pack flows' : 'Packs'} context={graphId} titleHref={graphId ? '/graphs' : undefined}
      actions={<Button onClick={run} disabled={status !== 'ready' || isFetching}>
        {isFetching ? 'Running…' : graphId ? 'Run tests' : 'Run all flow tests'}
      </Button>}
      navigation={graphId ? <nav className={workspace.navigation} aria-label="Pack flow sections">
        <Link to={`/graphs/${encodeURIComponent(graphId)}`} aria-current={!tests ? 'page' : undefined}>Diagram</Link>
        <Link to={`/graphs/${encodeURIComponent(graphId)}?view=tests`} aria-current={tests ? 'page' : undefined}>Tests</Link>
      </nav> : <PacksNavigation current="flows" />} />
    <PageBody width="full">
      {!graphId && <p className="quiet">Connect packs and see how their results feed into the next decision.</p>}
      {!graphId && graphInventorySupported && (inventory.error
        ? <ErrorBox title="Could not list pack flows" error={inventory.error} />
        : inventory.isPending ? <Loading what="pack flows" />
        : listing && !tests && <ConfiguredGraphs inventory={listing} />)}
      {!graphId && !graphInventorySupported && <p className="note">
        This runtime cannot list pack flows without running their tests. Choose Run all flow tests to discover their test results, or connect a newer runtime to browse their diagrams.
      </p>}
      {graphId && !tests && <FlowExplorer key={graphId} graphId={graphId} />}
      {tests && <section aria-label="Flow tests">
        {!graphId && <ButtonLink variant="quiet" to="/graphs">Back to pack flows</ButtonLink>}
        <h2>{graphId ? 'Flow tests' : 'All flow tests'}</h2>
        {graphTracesSupported && <label className="checkbox trace-ask">
          <input type="checkbox" checked={includeTraces} disabled={isFetching}
            onChange={event => setIncludeTraces(event.target.checked)} />
          <span>Include detailed traces</span>
        </label>}
        {isFetching && <Loading what="flow test results" />}
        {error && asked && <p className="note note-warn">
          This run requested detailed traces. {retainedUntraced
            ? 'Turn off detailed traces to view the previous untraced result.'
            : 'Turn off detailed traces and run tests to try without them.'}
        </p>}
        {error ? <ErrorBox title="Could not run flow tests" error={error} />
          : !data ? !isFetching && <Empty>Run tests to check saved cases and coverage. Opening this view does not run tests.</Empty>
          : <>
            <p className="ids"><Pill tone={statusTone(data.status)}>{data.status}</Pill>
              <span>{data.summary.passed} of {data.summary.total} cases passed</span>
              <span className="quiet">Last run{asked ? ' · detailed traces' : ''}</span>
            </p>
            {(data.graphs ?? []).length === 0 ? <Empty>No flow test results were reported.</Empty>
              : data.graphs!.map(entry => <GraphEntry key={entry.id} entry={entry} matrixSettled={!isFetching} />)}
            {data.label && <p className="note">{data.label}</p>}
          </>}
        <p className="quiet">Pack flows use the runtime’s experimental graph format. Test results describe the supplied cases.</p>
      </section>}
    </PageBody>
  </article>
}

/**
 * What the project configures, from the inventory alone.
 *
 * Nothing here has been run. The section says what `jpack.json` declares and
 * what the runtime read off each document's own bytes — which is why a row can
 * carry a `detail` and no identity at all: listing is not validating, and the
 * identity members are left empty rather than guessed. `nodeCount` and
 * `edgeCount` are absent rather than zero for exactly the same reason, so
 * "counts not read" is printed where they are missing instead of a `0` that
 * would read as an honest empty graph.
 */
function ConfiguredGraphs({ inventory, only }: { inventory: GraphInventory; only?: string }) {
  const all = inventory.graphs ?? []
  const rows = only ? all.filter((row) => row.id === only) : all
  return (
    <Section title="Pack flows" count={rows.length}>
      <>
        {inventory.note && <p className="note">{inventory.note}</p>}
        {rows.length === 0 ? (
          <Empty>
            {only
              ? `The project's configuration declares no graph with the id ${only}.`
              : 'No pack flows are configured.'}
          </Empty>
        ) : (
          <ul className="cards">
            {rows.map((row) => (
              <ConfiguredGraph key={row.id} row={row} />
            ))}
          </ul>
        )}
      </>
    </Section>
  )
}

function ConfiguredGraph({ row }: { row: GraphSummary }) {
  const lastRun = useLatestFlowResult(row.id)
  return (
    <li className="card">
      <div className="card-head">
        <h3>
          <Link to={`/graphs/${encodeURIComponent(row.id)}`}>{row.id}</Link>
        </h3>
        {row.graphVersion && <Pill tone="quiet">v{row.graphVersion}</Pill>}
        {row.resultNode && <Pill tone="neutral">result {row.resultNode}</Pill>}
        {!row.rowsDeclared && <span className="quiet">No saved test cases</span>}
      </div>
      {row.description && <p>{row.description}</p>}
      <p className="quiet">
        {row.nodeCount !== undefined && <>{row.nodeCount} steps · </>}
        {row.edgeCount !== undefined && <>{row.edgeCount} connections · </>}
        {lastRun ? `Last completed run: ${lastRun.status}` : 'Not tested in this session'}
      </p>
      <details className="disclosure"><summary>Technical details</summary><p className="meta">
        {row.path && <code>{row.path}</code>}
        {row.rowsPath && <code>{row.rowsPath}</code>}
        {row.graphId && <span>graph id {row.graphId}</span>}
        {row.formatVersion && <span>format {row.formatVersion}</span>}
        <span>
          {row.nodeCount === undefined || row.edgeCount === undefined
            ? 'node and edge counts not read'
            : `${row.nodeCount} ${row.nodeCount === 1 ? 'node' : 'nodes'}, ${row.edgeCount} ${
                row.edgeCount === 1 ? 'edge' : 'edges'
              }`}
        </span>
      </p>
      </details>
      {row.detail && <p className="note note-warn">{row.detail}</p>}
    </li>
  )
}

/**
 * One configured graph's run, and the walk drawn from the document beside it.
 *
 * The two accounts on screen come from two separate calls: the matrix run this
 * entry is part of, and the document fetched here. They are joined by node name
 * and by edge index, and **ADR-0030 is what proves the join describes one
 * file**: the matrix entry reports `graphSha256`, the digest of the exact bytes
 * its walk decoded, and `experimental_get_graph` reports the `sha256` of the
 * bytes it served. Three cases, and the page behaves differently in each.
 *
 * - **The two digests agree.** The walk is drawn, and the page says so. This is
 *   provenance of the join and nothing more: it establishes that the rows and
 *   the arrows are about one revision, and says nothing about whether that
 *   revision is any good. No verdict the runtime reached is derived, revised or
 *   overridden here — the desk never decides for the runtime.
 * - **They disagree.** The graph file was edited between the two calls, so the
 *   two answers are about two revisions and joining them would put one
 *   revision's rows against another's arrows. The joined walk is withdrawn, the
 *   page explains why and refreshes the document. A new suite run requires
 *   the user’s command. Silently combining two revisions is exactly the
 *   thing this exists to prevent.
 * - **The matrix entry states no digest.** Either the connected runtime is
 *   jpack 0.18.0 or older, or this entry's document did not load at all (a rows
 *   failure *after* a successful load keeps the digest, so that case still
 *   binds). Nothing can be compared, so nothing is claimed either way, and the
 *   epoch-bounded behaviour below stands exactly as it did.
 *
 * Those bounds stay in every case, because the digest upgrades the join and
 * does not replace what keys it:
 *
 * - the document query is keyed by the connection epoch, so a reconnect brings
 *   no document forward from the socket before it;
 * - a document is drawn only while the runtime still advertises the tool, so a
 *   reconnect to a runtime without it withdraws the drawing rather than leaving
 *   a document no live capability accounts for;
 * - and it is drawn only while neither call is in flight, so a matrix being
 *   re-run never lends its previous coverage to a document just read, or the
 *   other way round.
 */
function GraphEntry({
  entry,
  /** False while the matrix these rows came from is being re-run. */
  matrixSettled
}: {
  entry: GraphSuiteEntry
  matrixSettled: boolean
}) {
  const rows = entry.rows ?? []
  // Selection is derived, not just stored: the requested row where it still
  // exists, else the first row there is. A refetch that drops the requested
  // row (or brings rows to a graph that had none) then selects something
  // instead of leaving a full picker with nothing on the diagram.
  const [selected, setSelected] = useState<string | undefined>(undefined)
  const row = rows.find((candidate) => candidate.id === selected) ?? rows[0]

  // The configured id the matrix reports an entry under is the same configured
  // id the inventory and the fetch are keyed by — the runtime resolves both
  // from one jpack.json entry — so this is the id to ask for.
  const { graphDocumentSupported, connectionEpoch } = useMcp()
  const served = useGraphDocument(entry.id)
  const inFlight = graphDocumentSupported && (served.isPending || served.isFetching || !matrixSettled)
  const settled = graphDocumentSupported && served.isSuccess && !served.isFetching && matrixSettled

  // The binding is read off whatever pair of answers is in hand, not only off a
  // settled one, so the notice stays on screen through the refetch a divergence
  // itself asks for. Acting on it is gated on `settled` below: a pair half of
  // which is being replaced is not a pair worth chasing.
  const servedDigest = graphDocumentSupported ? served.data?.meta.sha256 : undefined
  const binding = bindGraphDigests(entry.graphSha256, servedDigest)

  const layout = useMemo(
    () =>
      settled && served.data?.document
        ? deriveWalkLayout(served.data.document, entry.coverage)
        : undefined,
    [settled, served.data, entry.coverage]
  )
  // Divergence withdraws the drawing outright. There is a laid-out shape in
  // hand and it is the wrong revision's, which is worse than none: the rows
  // beside it and the coverage inside it came from bytes it does not describe.
  const shape = layout?.drawn && binding !== 'divergent' ? layout.shape : undefined

  // One refetch cycle per disagreeing pair. A cycle that lands a pair this
  // connection has already asked about — the file is still mid-edit, or two
  // revisions are alternating — must not ask again, or the page would spin.
  useDigestRefetch({
    active: settled && binding === 'divergent',
    connectionEpoch,
    graphId: entry.id,
    run: entry.graphSha256,
    served: servedDigest
  })

  return (
    <section className="matrix-entry">
      <header className="matrix-entry-head">
        <h2>
          <Link to={`/graphs/${encodeURIComponent(entry.id)}`}>{entry.id}</Link>
        </h2>
        <Pill tone={statusTone(entry.status)}>{entry.status}</Pill>
        {entry.graphVersion && <Pill tone="quiet">v{entry.graphVersion}</Pill>}
        <span className="quiet">
          {entry.summary.passed}/{entry.summary.total} rows
        </span>
      </header>
      <p className="meta">
        {entry.path && <code>{entry.path}</code>}
        {entry.rowsPath && <code>{entry.rowsPath}</code>}
        {entry.graphId && <span>graph id {entry.graphId}</span>}
      </p>
      {entry.detail && <p className="note note-warn">{entry.detail}</p>}

      <Section title="The walk">
        <>
          <BindingNotice
            binding={binding}
            runDigest={entry.graphSha256}
            servedDigest={servedDigest}
          />
          {rows.length > 0 && (
            <div className="row-picker" role="group" aria-label="Choose a row to see on the diagram">
              {rows.map((candidate) => (
                <button
                  key={candidate.id}
                  type="button"
                  className={`tab${candidate.id === row?.id ? ' tab-on' : ''}`}
                  onClick={() => setSelected(candidate.id)}
                >
                  {candidate.id}
                </button>
              ))}
            </div>
          )}
          {inFlight ? (
            <Loading what="the served graph document" />
          ) : (
            <>
              {shape && served.data && (
                <p className="meta">
                  <span>
                    drawn from the served document{' '}
                    {served.data.meta.graphId || entry.id}
                    {served.data.meta.graphVersion ? ` v${served.data.meta.graphVersion}` : ''}
                  </span>
                  {served.data.meta.formatVersion && (
                    <span>format {served.data.meta.formatVersion}</span>
                  )}
                  {served.data.meta.bytes !== undefined && (
                    <span>{served.data.meta.bytes} bytes</span>
                  )}
                  {served.data.meta.sha256 && (
                    <code>sha256 {served.data.meta.sha256.slice(0, 12)}…</code>
                  )}
                </p>
              )}
              <GraphWalkDiagram
                entry={entry}
                row={row}
                shape={shape}
                fallbackReason={walkFallbackReason({
                  supported: graphDocumentSupported,
                  drawn: shape !== undefined,
                  served: settled ? served.data : undefined,
                  declined: layout && !layout.drawn ? layout.reason : undefined,
                  divergent: binding === 'divergent',
                  error: served.error
                })}
              />
            </>
          )}
        </>
      </Section>

      <Section title="Coverage">
        <CoverageReport coverage={entry.coverage} groupByNode />
      </Section>

      <Section title="Rows" count={rows.length}>
        {rows.length === 0 ? (
          <Empty>
            No rows were reported for this graph{entry.detail ? ' — the note above says why' : ''}.
          </Empty>
        ) : (
          <ul className="rows">
            {rows.map((candidate) => (
              <GraphRowItem key={candidate.id} row={candidate} />
            ))}
          </ul>
        )}
      </Section>
    </section>
  )
}

/**
 * What the digests say about the two answers being shown together (ADR-0030).
 *
 * A stated binding is worth one short line, and `unstated` is worth none: a
 * runtime that reports no digest has said nothing about the join, and a badge
 * announcing that absence would turn silence into a finding. What the older
 * behaviour rests on instead — the connection epoch, the in-flight gates — is
 * documented in the README rather than repeated on every graph.
 *
 * Neither line is a verdict. `bound` says two answers decoded the same bytes;
 * `divergent` says they did not. Whether either revision passes its own rows is
 * the runtime's to say, and it says it in the rows below.
 */
function BindingNotice({
  binding,
  runDigest,
  servedDigest
}: {
  binding: GraphDigestBinding
  runDigest?: string
  servedDigest?: string
}) {
  if (binding === 'unstated') return null
  if (binding === 'bound') {
    return (
      <p className="note">
        <strong>One revision.</strong> The matrix run reports the same document digest{' '}
        <code>{shortDigest(runDigest)}</code> the runtime served beside these bytes, so the walk
        drawn here and the rows below are about one revision of the graph file. It binds bytes; it
        is not a verdict on the revision.
      </p>
    )
  }
  return (
    <p className="note note-warn">
      <strong>Two revisions, not joined.</strong> The matrix run ran over{' '}
      <code>{shortDigest(runDigest)}</code> and the runtime served{' '}
      <code>{shortDigest(servedDigest)}</code>, so the graph file was edited between the two calls
      and the rows below describe a different revision from the document. The walk is not drawn
      from that document, because combining one revision's rows with another revision's arrows
      would be a picture neither answer supports. The document has been requested again. Run tests to compare the current revision. Neither
      revision is being called wrong: this says only that the two are not one file.
    </p>
  )
}

/** A digest short enough to read, labelled with the algorithm that produced it. */
function shortDigest(digest: string | undefined): string {
  return digest ? `sha256 ${digest.slice(0, 12)}…` : 'no digest'
}

/**
 * Refresh the served document once per disagreeing pair.
 *
 * A divergence is a fact about two readings taken at two moments, and the
 * view must keep them separate. The served document is refreshed. Test results remain a snapshot until the
 * user runs tests again; a file edit must never execute a suite implicitly.
 *
 * One cycle per pair, and the memory of which pairs have been asked about is
 * the connection's rather than this component's — see `refetchLedger` for why
 * neither a last-pair-only memory nor a component-local one is enough. What
 * this hook owes that module is the *identity*: the pair the runtime just
 * reported, under the epoch that reported it.
 */
function useDigestRefetch({
  active,
  connectionEpoch,
  graphId,
  run,
  served
}: {
  active: boolean
  /** Part of the document query's key: a document belongs to one connection. */
  connectionEpoch: number
  graphId: string
  /** The digest the matrix entry reported, as the payload spells it. */
  run: string | undefined
  /** The digest the runtime served, as the payload spells it. */
  served: string | undefined
}) {
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!active) return
    const identity = divergentPairIdentity({ graphId, run, served })
    if (!recordDivergentPair(connectionEpoch, identity)) return
    void queryClient.invalidateQueries({
      queryKey: ['experimental_get_graph', connectionEpoch, graphId]
    })
  }, [active, connectionEpoch, graphId, run, served, queryClient])
}

/**
 * One graph row: the composite headline, then the nodes the row named.
 *
 * A row checks the nodes it chose to check. A node the row does not name is
 * unchecked by the author's choice rather than defaulted to anything, so it is
 * absent from this list rather than shown as having passed.
 *
 * Two further assertions can ride on a row (ADR-0032), on the pack surface's
 * own vocabulary and rendered by the pack surface's own component: the
 * composite's handoff-target pair here, and one pair per named node below. They
 * exist because a target-only edit leaves every disposition byte identical, and
 * on a composition that blindness reaches upstream too — an escalation target
 * moved on a node three hops back changes nothing any headline can see.
 */
function GraphRowItem({ row }: { row: GraphTestRow }) {
  const assertion = describeTargetAssertion(row)
  return (
    <li className={`row row-${row.status}`}>
      <div className="row-head">
        <code className="row-id">{row.id}</code>
        <Pill tone={statusTone(row.status)}>{row.status}</Pill>
        {assertion && <Pill tone="quiet">{assertion}</Pill>}
        {row.nodes?.length ? (
          <Pill tone="quiet">
            {row.nodes.length} reported node {row.nodes.length === 1 ? 'comparison' : 'comparisons'}
          </Pill>
        ) : null}
      </div>

      {row.expectedErrorClass ? (
        <p className="row-refusal">
          expected a refused walk: <code>{row.expectedErrorClass}</code>
          {row.expectedErrorPhase && (
            <>
              {' '}
              in <code>{row.expectedErrorPhase}</code>
            </>
          )}
          {' · actual: '}
          {row.actualErrorClass ? (
            <>
              <code>{row.actualErrorClass}</code>
              {row.actualErrorPhase && (
                <>
                  {' '}
                  in <code>{row.actualErrorPhase}</code>
                </>
              )}
            </>
          ) : (
            'a composite result was produced'
          )}
        </p>
      ) : (
        <div className="row-compare">
          <GraphSide label="expected headline" text={row.expected} differs={row.expected !== row.actual} />
          <GraphSide label="actual headline" text={row.actual} differs={row.expected !== row.actual} />
        </div>
      )}

      <TargetPair
        of={row}
        expectedLabel="expected composite target"
        actualLabel="actual composite target"
      />

      {row.nodes?.length ? (
        <ul className="row-nodes">
          {row.nodes.map((node) => (
            <GraphNodeItem key={node.node} node={node} />
          ))}
        </ul>
      ) : null}

      {row.detail && <p className="row-detail">{row.detail}</p>}
    </li>
  )
}

/**
 * One node comparison inside a row: what the node concluded, what the row
 * asserted about where it hands off, and — where the run was asked — how it got
 * there.
 *
 * The trace is rendered by the same component the evaluation view uses, because
 * it is the same artifact under the same contract (ADR-0027, carried here by
 * ADR-0031). It is informative and decides nothing: the `status` beside the node
 * id is the runtime's verdict, and it stands whether or not a trace explains it
 * — which is why a *mismatching* comparison shows its trace too. A mismatch is
 * the case a trace is most worth reading.
 *
 * Absence is not emptiness. A trace member that is absent means the run was not
 * asked, or this node was never evaluated; `[]` means asked, evaluated, and
 * nothing walked. So nothing is rendered where the member is absent, and an
 * empty trace says it is empty.
 */
function GraphNodeItem({ node }: { node: GraphTestNode }) {
  const assertion = describeTargetAssertion(node)
  return (
    <li className={`row-node row-${node.status}`}>
      <div className="row-node-head">
        <code>{node.node}</code>
        <span
          className={`probe-status probe-status-${node.status === 'passed' ? 'covered' : 'missing'}`}
        >
          {node.status}
        </span>
        <span className="row-members">{summarize(node.actual)}</span>
        {assertion && <Pill tone="quiet">{assertion}</Pill>}
      </div>

      <TargetPair
        of={node}
        expectedLabel={`expected target of ${node.node}`}
        actualLabel={`actual target of ${node.node}`}
      />

      {node.trace !== undefined && (
        <TracePanel
          trace={node.trace}
          title={`Trace of ${node.node}`}
          context={
            'This trace is the evaluator’s own walk order; the node comparisons ' +
            'above it are listed lexicographically by node name. Two orders, and ' +
            'neither is read off the other.'
          }
          emptyWhat="This node's evaluation"
        />
      )}
    </li>
  )
}

function GraphSide({ label, text, differs }: { label: string; text: string; differs: boolean }) {
  return (
    <div className={`row-side${differs ? ' row-side-differs' : ''}`}>
      <span className="row-side-label">{label}</span>
      <p className="row-members">{summarize(text)}</p>
    </div>
  )
}

function summarize(text: string): string {
  const disposition = parseDisposition(text)
  if (!disposition) return text || '(none)'
  const reasons = disposition.reasons ?? []
  return (
    [disposition.kind, disposition.outcomeId].filter(Boolean).join(' ') +
    (reasons.length ? ` · reasons ${reasons.join(', ')}` : '') +
    (disposition.handoff ? ` · handoff ${disposition.handoff.state}` : '')
  )
}

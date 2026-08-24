import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { CoverageReport } from '../components/CoverageReport'
import { GraphWalkDiagram } from '../components/GraphWalkDiagram'
import { Empty, ErrorBox, Loading, Pill, Section, statusTone } from '../components/primitives'
import { parseDisposition } from '../mcp/canonical'
import { useGraphMatrix } from '../mcp/queries'
import type { GraphSuiteEntry, GraphTestRow } from '../mcp/types'

/**
 * The project's configured graphs, and their matrices run.
 *
 * A graph composes packs: one node's disposition lands at a fact pointer the
 * next node's rules read, and its resolution state feeds that node's evidence.
 * No JPS version defines any of that — the graph format is the runtime's own
 * convention, and only each node's pack evaluation reaches the shared
 * evaluator. The payload says so in its label, and so does this page.
 *
 * A project that configures no graph is an answer rather than an error: the
 * walk reports `skipped` with no entries, and the page says so plainly.
 */
export function GraphView() {
  const { graphId } = useParams<{ graphId?: string }>()
  const { data, error, isPending, isFetching } = useGraphMatrix(graphId)

  if (isPending) return <Loading what={graphId ? `graph ${graphId}` : "the project's graphs"} />
  if (error) {
    return <ErrorBox title={graphId ? `Could not run graph ${graphId}` : 'Could not run the graphs'} error={error} />
  }
  if (!data) return null

  const graphs = data.graphs ?? []

  return (
    <article className="detail">
      <nav className="crumbs">
        <Link to="/">Project</Link>
        <span aria-hidden="true">/</span>
        {graphId ? (
          <>
            <Link to="/graphs">Graphs</Link>
            <span aria-hidden="true">/</span>
            <span>{graphId}</span>
          </>
        ) : (
          <span>Graphs</span>
        )}
      </nav>

      <header className="detail-head">
        <h1>{graphId ?? 'Graphs'}</h1>
        <p className="ids">
          <Pill tone={statusTone(data.status)}>{data.status}</Pill>
          <span>
            {data.summary.passed} of {data.summary.total}{' '}
            {data.summary.total === 1 ? 'row' : 'rows'} passed
          </span>
          {data.summary.mismatched > 0 && (
            <Pill tone="danger">{data.summary.mismatched} mismatched</Pill>
          )}
          {isFetching && <span className="quiet">re-running…</span>}
        </p>
        <p className="meta">
          {data.configPath && <code>{data.configPath}</code>}
          {data.configVersion && <span>configVersion {data.configVersion}</span>}
          {data.formatVersion && <span>graph format {data.formatVersion}</span>}
          {data.evaluatorSpecVersion && <span>evaluator {data.evaluatorSpecVersion}</span>}
        </p>
      </header>

      {graphs.length === 0 ? (
        <Empty>
          This project configures no graph. A graph is declared under{' '}
          <code>graphs</code> in <code>jpack.json</code>, which needs{' '}
          <code>configVersion</code> 2 or newer.
        </Empty>
      ) : (
        graphs.map((entry) => <GraphEntry key={entry.id} entry={entry} />)
      )}

      {data.label && (
        <p className="note">
          <strong>What this reports.</strong> {data.label}
        </p>
      )}
    </article>
  )
}

function GraphEntry({ entry }: { entry: GraphSuiteEntry }) {
  const rows = entry.rows ?? []
  // Selection is derived, not just stored: the requested row where it still
  // exists, else the first row there is. A refetch that drops the requested
  // row (or brings rows to a graph that had none) then selects something
  // instead of leaving a full picker with nothing on the diagram.
  const [selected, setSelected] = useState<string | undefined>(undefined)
  const row = rows.find((candidate) => candidate.id === selected) ?? rows[0]

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
          <GraphWalkDiagram entry={entry} row={row} />
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
 * One graph row: the composite headline, then the nodes the row named.
 *
 * A row checks the nodes it chose to check. A node the row does not name is
 * unchecked by the author's choice rather than defaulted to anything, so it is
 * absent from this list rather than shown as having passed.
 */
function GraphRowItem({ row }: { row: GraphTestRow }) {
  return (
    <li className={`row row-${row.status}`}>
      <div className="row-head">
        <code className="row-id">{row.id}</code>
        <Pill tone={statusTone(row.status)}>{row.status}</Pill>
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

      {row.nodes?.length ? (
        <ul className="row-nodes">
          {row.nodes.map((node) => (
            <li key={node.node} className={`row-node row-${node.status}`}>
              <code>{node.node}</code>
              <span className={`probe-status probe-status-${node.status === 'passed' ? 'covered' : 'missing'}`}>
                {node.status}
              </span>
              <span className="row-members">{summarize(node.actual)}</span>
            </li>
          ))}
        </ul>
      ) : null}

      {row.detail && <p className="row-detail">{row.detail}</p>}
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

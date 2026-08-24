import { edgeIndices, nodesInWalkOrder, parseDisposition, parseProbe } from '../mcp/canonical'
import type { GraphSuiteEntry, GraphTestRow, MatrixProbe } from '../mcp/types'

/**
 * A graph's walk, drawn.
 *
 * **What this diagram can and cannot claim.** The runtime's graph matrix
 * payload reports each configured graph's rows and its derived coverage. It
 * does not report the graph document: no member of it carries the nodes' packs,
 * the edges' endpoints, the fact pointers those edges write, or which node is
 * the declared result. The only account of a graph's structure that reaches
 * this wire is the coverage report's own namespacing — one block of probes per
 * node, emitted in the walk's evaluation order, and two probes per edge
 * identified by position.
 *
 * So this draws what is there: the nodes, on the order axis the runtime
 * enumerated them along, and the composite headline the row was judged on. It
 * draws no arrow between two nodes, because an arrow would assert a dependency
 * the payload never states — and in a graph with independent branches that
 * assertion would be false. The edges are reported beside the diagram as the
 * indexed slots the wire describes them as, with the witness each one has.
 *
 * The gap is recorded in the README rather than closed here by parsing the
 * English in a `detail` sentence, which would make a contract out of prose.
 */

const NODE_HEIGHT = 64
const NODE_GAP = 24
const AXIS_X = 34
const NODE_X = 74
const NODE_WIDTH = 400
const TOP = 20

export function GraphWalkDiagram({
  entry,
  /** The row whose per-node results colour the nodes, where one is selected. */
  row
}: {
  entry: GraphSuiteEntry
  row?: GraphTestRow
}) {
  const nodes = nodesInWalkOrder(entry.coverage)
  const edges = edgeIndices(entry.coverage)

  if (nodes.length === 0) {
    return (
      <p className="empty">
        The coverage report names no node, so this run reports nothing about the
        graph's shape. A graph whose rows did not load reports its failure and no
        structure.
      </p>
    )
  }

  const byNode = new Map((row?.nodes ?? []).map((node) => [node.node, node]))
  const height = TOP + nodes.length * (NODE_HEIGHT + NODE_GAP) + NODE_HEIGHT + 24
  const width = NODE_X + NODE_WIDTH + 20

  return (
    <div className="diagram-wrap">
      <svg
        className="diagram"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`The ${nodes.length} nodes represented in coverage for graph ${entry.id}, in the order the runtime evaluates them`}
      >
        {/* The axis is evaluation order, which the payload states. It is not a
            dependency edge, which the payload does not. */}
        <line
          className="diagram-axis"
          x1={AXIS_X}
          y1={TOP + NODE_HEIGHT / 2}
          x2={AXIS_X}
          y2={TOP + (nodes.length - 1) * (NODE_HEIGHT + NODE_GAP) + NODE_HEIGHT / 2}
        />

        {nodes.map((node, index) => {
          const y = TOP + index * (NODE_HEIGHT + NODE_GAP)
          const result = byNode.get(node)
          const disposition = parseDisposition(result?.actual)
          const status = result ? result.status : 'unreported'
          const gaps = countMissing(entry.coverage, node)
          return (
            <g key={node} className={`diagram-node diagram-node-${status}`}>
              <circle className="diagram-tick" cx={AXIS_X} cy={y + NODE_HEIGHT / 2} r={11} />
              <text className="diagram-tick-label" x={AXIS_X} y={y + NODE_HEIGHT / 2} dy="0.35em">
                {index + 1}
              </text>
              <rect
                className="diagram-box"
                x={NODE_X}
                y={y}
                width={NODE_WIDTH}
                height={NODE_HEIGHT}
                rx={8}
              />
              <text className="diagram-node-id" x={NODE_X + 14} y={y + 24}>
                {node}
              </text>
              <text className="diagram-node-meta" x={NODE_X + 14} y={y + 44}>
                {result
                  ? `${result.status} · ${describe(disposition)}`
                  : 'selected row reports no comparison'}
              </text>
              {gaps > 0 && (
                <text className="diagram-node-gaps" x={NODE_X + NODE_WIDTH - 14} y={y + 44}>
                  {gaps} unwitnessed
                </text>
              )}
            </g>
          )
        })}

        {/* The composite headline the row was judged on — drawn neutrally,
            because row.status covers the headline AND every reported node
            comparison: a node-only mismatch must not paint a byte-identical
            composite. The row's own verdict is reported beside the diagram,
            as the row's. */}
        {(() => {
          const y = TOP + nodes.length * (NODE_HEIGHT + NODE_GAP)
          const disposition = parseDisposition(row?.actual)
          return (
            <g className="diagram-node diagram-composite">
              <rect
                className="diagram-box"
                x={NODE_X}
                y={y}
                width={NODE_WIDTH}
                height={NODE_HEIGHT}
                rx={8}
              />
              <text className="diagram-node-id" x={NODE_X + 14} y={y + 24}>
                composite result
              </text>
              <text className="diagram-node-meta" x={NODE_X + 14} y={y + 44}>
                {row ? describe(disposition) : 'select a row to see its result'}
              </text>
            </g>
          )
        })()}
      </svg>

      {row && (
        <p className="note">
          Row <code>{row.id}</code>: <strong>{row.status}</strong> — that verdict
          covers the composite headline and every node comparison the row
          reported, together.
        </p>
      )}

      <p className="note">
        Nodes in the order the runtime evaluates them — those represented in the
        coverage report, which can omit a node the run never admitted. The wire
        does not carry which node feeds which, so no arrow is drawn between two
        of them. See the README's upstream gaps.
      </p>

      {edges.length > 0 && (
        <div className="diagram-edges">
          <h4 className="coverage-group-title">
            {edges.length} edge {edges.length === 1 ? 'index' : 'indices'} represented in coverage
          </h4>
          <ul className="edge-slots">
            {edges.map((index) => (
              <li key={index} className="edge-slot">
                <code>edge {index}</code>
                {['resolved', 'unresolved'].map((branch) => {
                  const probe = (entry.coverage ?? []).find(
                    (candidate) => candidate.probe === `edge:${index}:${branch}`
                  )
                  if (!probe) return null
                  return (
                    <span
                      key={branch}
                      className={`probe-status probe-status-${probe.status}`}
                      title={probe.detail}
                    >
                      {branch}: {probe.status}
                    </span>
                  )
                })}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

/** How many of one node's probes no row witnesses. */
function countMissing(coverage: MatrixProbe[] | undefined, node: string): number {
  return (coverage ?? []).filter(
    (probe) => parseProbe(probe.probe).node === node && probe.status !== 'covered'
  ).length
}

function describe(disposition: ReturnType<typeof parseDisposition>): string {
  if (!disposition) return 'no disposition reported'
  return disposition.outcomeId ? `${disposition.kind} ${disposition.outcomeId}` : disposition.kind
}

import { edgeIndices, nodesInWalkOrder, parseDisposition, parseProbe } from '../mcp/canonical'
import { edgeCarries, type GraphWalkShape, type WalkEdge } from '../mcp/graphDocument'
import type { GraphSuiteEntry, GraphTestRow, MatrixProbe } from '../mcp/types'

/**
 * A graph's walk, drawn — from the served document where the runtime serves
 * one, and from the coverage report's order axis where it does not.
 *
 * **Which claim the diagram is making depends on which of those it got.**
 *
 * With a document (`experimental_get_graph`, ADR-0029) the picture is the
 * composition itself: every node the document declares, laid out in layers by
 * the document's own edges, with a real arrow per edge labelled with what that
 * edge carries, and the declared `result` node marked. A node the coverage
 * report names no probe for appears here and is said to be absent from
 * coverage — the case the old view could not represent at all — with no cause
 * given, because neither payload states one.
 *
 * Without one — jpack 0.18.0 and older, whose wire carries no graph document —
 * the only account of a graph's structure that reaches this client is the
 * coverage report's own namespacing: one block of probes per node in the
 * walk's evaluation order, and two probes per edge identified by position. So
 * the fallback draws the nodes on that order axis and draws **no arrow**,
 * because an arrow would assert a dependency the payload never states — and in
 * a graph with independent branches that assertion would be false.
 *
 * What does *not* change between the two: node colouring comes only from the
 * selected row's reported comparisons, a node the row does not report is said
 * to be unreported rather than shown as passed, and the composite result is
 * never painted with `row.status` — that verdict covers the headline and every
 * reported node comparison together, so a node-only mismatch must not colour a
 * byte-identical composite. The row's verdict is reported beside the diagram,
 * as the row's.
 */

export function GraphWalkDiagram({
  entry,
  /** The row whose per-node results colour the nodes, where one is selected. */
  row,
  /** The served document's walk, where one was fetched and could be read. */
  shape,
  /**
   * One line saying why the served document is not being drawn, where the
   * runtime advertises the tool but this client has no shape to draw from.
   * Absent when the runtime has no such tool at all: nothing went wrong then.
   */
  fallbackReason
}: {
  entry: GraphSuiteEntry
  row?: GraphTestRow
  shape?: GraphWalkShape
  fallbackReason?: string
}) {
  if (shape && shape.nodes.length > 0) {
    return <DocumentWalk entry={entry} row={row} shape={shape} />
  }
  return <CoverageWalk entry={entry} row={row} fallbackReason={fallbackReason} />
}

/* The document walk ------------------------------------------------------- */

const NODE_W = 240
const NODE_H = 90
const H_GAP = 32
const V_GAP = 78
const MARGIN = 16

/** How far apart several edges leaving or entering one node are fanned. */
function fan(position: number, count: number): number {
  if (count <= 1) return 0
  return (position - (count - 1) / 2) * Math.min(26, (NODE_W - 24) / count)
}

/** A label long enough to swamp the drawing is shortened; the full text stays in the list. */
function short(text: string, limit = 34): string {
  return text.length <= limit ? text : `${text.slice(0, limit - 1)}…`
}

function DocumentWalk({
  entry,
  row,
  shape
}: {
  entry: GraphSuiteEntry
  row?: GraphTestRow
  shape: GraphWalkShape
}) {
  const byNode = new Map((row?.nodes ?? []).map((node) => [node.node, node]))
  const perLayer = new Map<number, number>()
  for (const node of shape.nodes) perLayer.set(node.layer, (perLayer.get(node.layer) ?? 0) + 1)

  const width = MARGIN * 2 + shape.width * (NODE_W + H_GAP) - H_GAP
  // One row below the deepest layer holds the composite headline.
  const height = MARGIN * 2 + (shape.depth + 1) * NODE_H + shape.depth * V_GAP

  const layerY = (layer: number) => MARGIN + layer * (NODE_H + V_GAP)
  const nodeX = (layer: number, column: number) => {
    const held = perLayer.get(layer) ?? 1
    const indent = ((shape.width - held) * (NODE_W + H_GAP)) / 2
    return MARGIN + indent + column * (NODE_W + H_GAP)
  }
  const at = new Map(shape.nodes.map((node) => [node.id, { x: nodeX(node.layer, node.column), y: layerY(node.layer) }]))

  const drawable = shape.edges.filter((edge) => edge.drawable)
  const outCount = new Map<string, number>()
  const inCount = new Map<string, number>()
  for (const edge of drawable) {
    outCount.set(edge.from, (outCount.get(edge.from) ?? 0) + 1)
    inCount.set(edge.to, (inCount.get(edge.to) ?? 0) + 1)
  }
  const outSeen = new Map<string, number>()
  const inSeen = new Map<string, number>()

  const compositeY = layerY(shape.depth)
  const compositeX = MARGIN + ((shape.width - 1) * (NODE_W + H_GAP)) / 2
  const resultNode = shape.nodes.find((node) => node.isResult)
  // SVG ids are document-global and /graphs renders every configured graph, so
  // the marker is named after the entry rather than shared by a fixed literal.
  const arrow = `diagram-arrow-${entry.id.replace(/[^A-Za-z0-9_-]/g, '-')}`

  return (
    <div className="diagram-wrap">
      <svg
        className="diagram diagram-document"
        viewBox={`0 0 ${width} ${height}`}
        // Drawn near its natural size: the box widths and type sizes below are
        // chosen to be legible at 1:1, and a percentage width would scale a
        // narrow single-column graph up until its labels swamped it. The floor
        // keeps a two-node graph from being microscopic on a wide page.
        style={{ maxWidth: Math.min(Math.max(width, 420), 760) }}
        role="img"
        aria-label={
          `The ${shape.nodes.length} ${shape.nodes.length === 1 ? 'node' : 'nodes'} and ` +
          `${shape.edges.length} ${shape.edges.length === 1 ? 'edge' : 'edges'} the served graph ` +
          `document declares for graph ${entry.id}, drawn as a layered walk`
        }
      >
        <defs>
          <marker
            id={arrow}
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path className="diagram-arrow-head" d="M 0 0 L 10 5 L 0 10 z" />
          </marker>
        </defs>

        {/* The declared edges, drawn. Each one is a dependency the document
            states, which is the whole difference this view rests on. */}
        {drawable.map((edge) => {
          const from = at.get(edge.from)!
          const to = at.get(edge.to)!
          const outIndex = outSeen.get(edge.from) ?? 0
          outSeen.set(edge.from, outIndex + 1)
          const inIndex = inSeen.get(edge.to) ?? 0
          inSeen.set(edge.to, inIndex + 1)
          const x1 = from.x + NODE_W / 2 + fan(outIndex, outCount.get(edge.from) ?? 1)
          const y1 = from.y + NODE_H
          const x2 = to.x + NODE_W / 2 + fan(inIndex, inCount.get(edge.to) ?? 1)
          const y2 = to.y
          const midX = (x1 + x2) / 2
          const midY = (y1 + y2) / 2
          const carries = edgeCarries(edge)
          return (
            <g key={edge.index} className="diagram-edge">
              <title>{`edge ${edge.index}: ${edge.from} → ${edge.to} carries ${carries}`}</title>
              <path
                className="diagram-edge-line"
                d={`M ${x1} ${y1} C ${x1} ${y1 + V_GAP / 2}, ${x2} ${y2 - V_GAP / 2}, ${x2} ${y2}`}
                markerEnd={`url(#${arrow})`}
              />
              <text className="diagram-edge-label" x={midX} y={midY - 3} textAnchor="middle">
                {/* An edge label spans the gap between layers rather than one
                    box, so it has more room than a line inside one. The full
                    text is in the list below and in this group's title. */}
                {short(carries, 46)}
              </text>
            </g>
          )
        })}

        {/* The declared result node feeds the composite headline. That is a
            relationship the document states, so it is the one further arrow
            drawn — and it is what marks the result node visibly. */}
        {resultNode && (
          <g className="diagram-edge diagram-edge-result">
            <title>{`${resultNode.id} is the node this document declares as its result`}</title>
            <path
              className="diagram-edge-line"
              d={
                `M ${at.get(resultNode.id)!.x + NODE_W / 2} ${at.get(resultNode.id)!.y + NODE_H} ` +
                `L ${compositeX + NODE_W / 2} ${compositeY}`
              }
              markerEnd={`url(#${arrow})`}
            />
            <text
              className="diagram-edge-label"
              x={(at.get(resultNode.id)!.x + compositeX) / 2 + NODE_W / 2}
              y={(at.get(resultNode.id)!.y + NODE_H + compositeY) / 2 - 3}
              textAnchor="middle"
            >
              declared result
            </text>
          </g>
        )}

        {shape.nodes.map((node) => {
          const point = at.get(node.id)!
          const result = byNode.get(node.id)
          const disposition = parseDisposition(result?.actual)
          const status = result ? result.status : 'unreported'
          const gaps = countMissing(entry.coverage, node.id)
          return (
            <g
              key={node.id}
              className={`diagram-node diagram-node-${status}${node.isResult ? ' diagram-node-result' : ''}`}
            >
              <title>
                {`${node.id}${node.pack ? ` · pack ${node.pack}` : ''} · ` +
                  (result
                    ? `${result.status} · ${describe(disposition)}`
                    : 'the selected row reports no comparison for this node')}
              </title>
              <rect
                className="diagram-box"
                x={point.x}
                y={point.y}
                width={NODE_W}
                height={NODE_H}
                rx={8}
              />
              <text className="diagram-node-id" x={point.x + 12} y={point.y + 20}>
                {node.id}
              </text>
              {node.isResult && (
                <text className="diagram-node-badge" x={point.x + NODE_W - 12} y={point.y + 20}>
                  result
                </text>
              )}
              <text className="diagram-node-pack" x={point.x + 12} y={point.y + 39}>
                {node.pack ? `pack ${short(node.pack, 26)}` : 'this node names no pack'}
              </text>
              <text className="diagram-node-meta" x={point.x + 12} y={point.y + 58}>
                {result
                  ? short(`${result.status} · ${describe(disposition)}`)
                  : 'selected row reports no comparison'}
              </text>
              {/* Its own line, not the row status' right margin: "selected row
                  reports no comparison" is wording that must survive whole, so
                  nothing may be truncated to make room beside it. */}
              {node.inCoverage ? (
                gaps > 0 && (
                  <text className="diagram-node-coverage" x={point.x + 12} y={point.y + 77}>
                    {gaps} unwitnessed
                  </text>
                )
              ) : (
                <text className="diagram-node-coverage" x={point.x + 12} y={point.y + 77}>
                  not represented in coverage
                </text>
              )}
            </g>
          )
        })}

        {/* Drawn neutrally, exactly as in the fallback: row.status covers the
            headline AND every reported node comparison, so a node-only
            mismatch must not paint a byte-identical composite. */}
        <g className="diagram-node diagram-composite">
          <rect
            className="diagram-box"
            x={compositeX}
            y={compositeY}
            width={NODE_W}
            height={NODE_H}
            rx={8}
          />
          <text className="diagram-node-id" x={compositeX + 12} y={compositeY + 24}>
            composite result
          </text>
          <text className="diagram-node-meta" x={compositeX + 12} y={compositeY + 46}>
            {row ? short(describe(parseDisposition(row.actual))) : 'select a row to see its result'}
          </text>
        </g>
      </svg>

      {row && <RowVerdict row={row} />}

      <p className="note">
        Nodes and edges as the served graph document declares them — every arrow
        is a dependency the document states, labelled with what that edge
        carries. Colour on a node is the selected row's own reported comparison
        and nothing else.
      </p>

      {/* What coverage does not name, said as exactly that. Why it does not is
          something neither payload states: a node the walk never admitted and a
          node whose probes the report omitted look identical from here, so no
          cause is given for what is only an absence. */}
      {shape.nodes.some((node) => !node.inCoverage) && (
        <p className="note note-warn">
          {shape.nodes
            .filter((node) => !node.inCoverage)
            .map((node) => node.id)
            .join(', ')}{' '}
          {shape.nodes.filter((node) => !node.inCoverage).length === 1 ? 'is' : 'are'} declared by
          the document and named by no probe in the coverage report. It is drawn
          because the document declares it, and nothing is claimed here about why
          coverage names no probe for it or about what its rows witness.
        </p>
      )}

      {shape.resultDangling && (
        <p className="note note-warn">
          The document declares <code>{shape.result}</code> as its result, and
          declares no node by that name. No node is marked as the result rather
          than one being chosen.
        </p>
      )}

      <EdgeList edges={shape.edges} coverage={entry.coverage} />
    </div>
  )
}

/**
 * The declared edges, spelled out: what each one carries in full, the author's
 * own sentence where there is one, and the coverage witness each of its two
 * branches has. The witnesses are looked up by the edge's array index, which
 * is exactly how the coverage report names its edge probes.
 */
function EdgeList({
  edges,
  coverage
}: {
  edges: WalkEdge[]
  coverage: MatrixProbe[] | undefined
}) {
  if (edges.length === 0) {
    return (
      <p className="note">
        This document declares no edge: nothing feeds anything, and each node
        stands alone.
      </p>
    )
  }
  return (
    <div className="diagram-edges">
      <h4 className="coverage-group-title">
        {edges.length} declared {edges.length === 1 ? 'edge' : 'edges'}
      </h4>
      <ul className="edge-slots">
        {edges.map((edge) => (
          <li key={edge.index} className="edge-slot">
            <code>
              {edge.from} → {edge.to}
            </code>
            <span className="edge-carries">{edgeCarries(edge)}</span>
            {!edge.drawable && (
              <span className="probe-status probe-status-missing">
                names a node this document does not declare — not drawn
              </span>
            )}
            {['resolved', 'unresolved'].map((branch) => {
              const probe = (coverage ?? []).find(
                (candidate) => candidate.probe === `edge:${edge.index}:${branch}`
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
            {edge.description && <p className="edge-detail">{edge.description}</p>}
          </li>
        ))}
      </ul>
    </div>
  )
}

/* The coverage fallback --------------------------------------------------- */

const AXIS_NODE_HEIGHT = 64
const AXIS_NODE_GAP = 24
const AXIS_X = 34
const AXIS_NODE_X = 74
const AXIS_NODE_WIDTH = 400
const AXIS_TOP = 20

/**
 * The walk as coverage alone can describe it: the nodes on the runtime's
 * evaluation-order axis, and no arrow at all.
 *
 * This is what every runtime that does not serve graph documents gets, and it
 * is unchanged from what it always was — including its refusal to draw an
 * edge. It is also where a runtime that *does* serve them lands when the
 * document could not be read, with one line saying so.
 */
function CoverageWalk({
  entry,
  row,
  fallbackReason
}: {
  entry: GraphSuiteEntry
  row?: GraphTestRow
  fallbackReason?: string
}) {
  const nodes = nodesInWalkOrder(entry.coverage)
  const edges = edgeIndices(entry.coverage)

  if (nodes.length === 0) {
    return (
      <>
        {fallbackReason && <p className="note note-warn">{fallbackReason}</p>}
        <p className="empty">
          The coverage report names no node, so this run reports nothing about the
          graph's shape. A graph whose rows did not load reports its failure and no
          structure.
        </p>
      </>
    )
  }

  const byNode = new Map((row?.nodes ?? []).map((node) => [node.node, node]))
  const height = AXIS_TOP + nodes.length * (AXIS_NODE_HEIGHT + AXIS_NODE_GAP) + AXIS_NODE_HEIGHT + 24
  const width = AXIS_NODE_X + AXIS_NODE_WIDTH + 20

  return (
    <div className="diagram-wrap">
      {fallbackReason && <p className="note note-warn">{fallbackReason}</p>}
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
          y1={AXIS_TOP + AXIS_NODE_HEIGHT / 2}
          x2={AXIS_X}
          y2={AXIS_TOP + (nodes.length - 1) * (AXIS_NODE_HEIGHT + AXIS_NODE_GAP) + AXIS_NODE_HEIGHT / 2}
        />

        {nodes.map((node, index) => {
          const y = AXIS_TOP + index * (AXIS_NODE_HEIGHT + AXIS_NODE_GAP)
          const result = byNode.get(node)
          const disposition = parseDisposition(result?.actual)
          const status = result ? result.status : 'unreported'
          const gaps = countMissing(entry.coverage, node)
          return (
            <g key={node} className={`diagram-node diagram-node-${status}`}>
              <circle className="diagram-tick" cx={AXIS_X} cy={y + AXIS_NODE_HEIGHT / 2} r={11} />
              <text className="diagram-tick-label" x={AXIS_X} y={y + AXIS_NODE_HEIGHT / 2} dy="0.35em">
                {index + 1}
              </text>
              <rect
                className="diagram-box"
                x={AXIS_NODE_X}
                y={y}
                width={AXIS_NODE_WIDTH}
                height={AXIS_NODE_HEIGHT}
                rx={8}
              />
              <text className="diagram-node-id" x={AXIS_NODE_X + 14} y={y + 24}>
                {node}
              </text>
              <text className="diagram-node-meta" x={AXIS_NODE_X + 14} y={y + 44}>
                {result
                  ? `${result.status} · ${describe(disposition)}`
                  : 'selected row reports no comparison'}
              </text>
              {gaps > 0 && (
                <text className="diagram-node-gaps" x={AXIS_NODE_X + AXIS_NODE_WIDTH - 14} y={y + 44}>
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
          const y = AXIS_TOP + nodes.length * (AXIS_NODE_HEIGHT + AXIS_NODE_GAP)
          const disposition = parseDisposition(row?.actual)
          return (
            <g className="diagram-node diagram-composite">
              <rect
                className="diagram-box"
                x={AXIS_NODE_X}
                y={y}
                width={AXIS_NODE_WIDTH}
                height={AXIS_NODE_HEIGHT}
                rx={8}
              />
              <text className="diagram-node-id" x={AXIS_NODE_X + 14} y={y + 24}>
                composite result
              </text>
              <text className="diagram-node-meta" x={AXIS_NODE_X + 14} y={y + 44}>
                {row ? describe(disposition) : 'select a row to see its result'}
              </text>
            </g>
          )
        })()}
      </svg>

      {row && <RowVerdict row={row} />}

      <p className="note">
        Nodes in the order the runtime evaluates them — those represented in the
        coverage report, which can name no probe for a node the graph declares.
        The wire does not carry which node feeds which, so no arrow is drawn
        between two of them. See the README's upstream gaps.
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

/** The row's verdict, said to be the row's — never painted onto the composite. */
function RowVerdict({ row }: { row: GraphTestRow }) {
  return (
    <p className="note">
      Row <code>{row.id}</code>: <strong>{row.status}</strong> — that verdict
      covers the composite headline and every node comparison the row reported,
      together.
    </p>
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

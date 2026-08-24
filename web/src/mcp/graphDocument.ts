/**
 * Reading one served graph document, and laying its walk out.
 *
 * `experimental_get_graph` (ADR-0029) serves the project's own graph file
 * unaltered — the one artifact that states the composition. Everything a
 * diagram needs and the matrix payload never carried is in it: which pack each
 * node names, which node feeds which, what each edge carries, and which node
 * the document declares as its result.
 *
 * Two rules govern this module.
 *
 * **It reads; it does not repair.** The runtime serves a mid-edit document
 * deliberately, so this must handle one — but handling it means declining to
 * draw, not guessing what the author meant. A member of the wrong shape makes
 * the parse fail rather than being coerced, and an edge naming an endpoint the
 * document does not declare is reported as such rather than quietly connecting
 * to something else.
 *
 * **The order it draws is the order the runtime states.** Layering comes from
 * the document's own edges; where a layer holds more than one node, the tie is
 * broken by the walk order the coverage report enumerated the nodes in, which
 * is the runtime's actual evaluation order. Nothing invents a sequence.
 */
import { nodesInWalkOrder } from './canonical'
import type { GraphDocument, GraphDocumentEdge } from './types'

/**
 * Parse served graph-document text into the declared shape, or undefined.
 *
 * Undefined is returned for text that is not JSON, is not a JSON object, or
 * whose `nodes` member is not the map the format declares. Those are the cases
 * where there is no composition to read — and a served document may be exactly
 * that, because the runtime serves what is on disk rather than what validates.
 * `edges` is accepted when absent (a one-node graph declares none) but must be
 * an array when present: a member of the wrong type is a document this cannot
 * read, not one to be coerced into an empty list.
 */
export function parseGraphDocument(text: string): GraphDocument | undefined {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    return undefined
  }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
  const candidate = value as Record<string, unknown>

  const nodes = candidate.nodes
  if (nodes === null || typeof nodes !== 'object' || Array.isArray(nodes)) return undefined

  const edges = candidate.edges
  if (edges !== undefined && !Array.isArray(edges)) return undefined

  return candidate as GraphDocument
}

/** One node of a laid-out walk. */
export interface WalkNode {
  id: string
  /** The decision id of the pack this node names, where the document names one. */
  pack?: string
  description?: string
  /** Which layer the document's edges place it in; 0 has no declared in-edge. */
  layer: number
  /** Position within its layer, left to right. */
  column: number
  /** True where the document declares this node its `result`. */
  isResult: boolean
  /**
   * True where the coverage report names at least one probe for this node.
   * False is the interesting case ADR-0029 exists for: a node the run never
   * admitted is absent from coverage and present in the document, and a gap
   * count of zero would read as "fully witnessed" rather than "not reached".
   */
  inCoverage: boolean
}

/** One edge of a laid-out walk. */
export interface WalkEdge extends GraphDocumentEdge {
  /**
   * The zero-based index into the document's `edges` array. The coverage
   * report names its edge probes by exactly this index (`edge:<i>:resolved`),
   * so it is what binds a drawn edge to its witnesses.
   */
  index: number
  /**
   * True where both endpoints are nodes the document declares. A dangling
   * endpoint is listed rather than drawn: an arrow to a node that is not there
   * would have to be drawn somewhere, and anywhere would be a fabrication.
   */
  drawable: boolean
}

export interface GraphWalkShape {
  /** Every declared node, in draw order: by layer, then by column. */
  nodes: WalkNode[]
  edges: WalkEdge[]
  /** How many columns the widest layer holds. */
  width: number
  /** How many layers there are. */
  depth: number
  /** The node the document declares as its result, where it declares one. */
  result?: string
  /**
   * True where `result` names a node the document does not declare. The result
   * marker is then withheld rather than pinned to a node that was not named.
   */
  resultDangling: boolean
  /**
   * Node ids no acyclic layering could place, which means the served document
   * has a cycle. They are laid out after everything else and reported, because
   * a mid-edit graph may well have one and a layout that hung on it would be
   * worse than a layout that says so.
   */
  cyclic: string[]
}

/**
 * Lay one served document out as a layered DAG.
 *
 * Layering is longest-path: a node sits one layer below the lowest node that
 * feeds it, so every declared edge points downward and an edge's label has
 * somewhere to sit. Kahn's algorithm does the ordering, which also detects a
 * cycle by construction — whatever it cannot place is what a cycle holds.
 *
 * `coverage` is the graph matrix run's own coverage report, used only to break
 * ties inside a layer, so that two independent nodes appear in the order the
 * runtime actually evaluated them. Where coverage names neither, the
 * document's own key order decides, which keeps the layout stable across runs.
 */
export function deriveWalkShape(
  document: GraphDocument,
  coverage: readonly { probe: string }[] | undefined
): GraphWalkShape {
  const declared = document.nodes ?? {}
  const ids = Object.keys(declared)
  const known = new Set(ids)

  const edges: WalkEdge[] = (document.edges ?? []).map((edge, index) => ({
    ...edge,
    index,
    drawable: known.has(edge.from) && known.has(edge.to)
  }))

  // Only edges between declared nodes constrain the layering. An edge naming
  // an endpoint that is not there constrains nothing, because there is nothing
  // for it to constrain.
  const structural = edges.filter((edge) => edge.drawable && edge.from !== edge.to)

  const indegree = new Map(ids.map((id) => [id, 0]))
  const out = new Map<string, string[]>(ids.map((id) => [id, []]))
  for (const edge of structural) {
    out.get(edge.from)!.push(edge.to)
    indegree.set(edge.to, indegree.get(edge.to)! + 1)
  }

  const layer = new Map<string, number>()
  let frontier = ids.filter((id) => indegree.get(id) === 0)
  for (const id of frontier) layer.set(id, 0)
  const placed = new Set(frontier)
  while (frontier.length > 0) {
    const next: string[] = []
    for (const id of frontier) {
      for (const target of out.get(id)!) {
        // Longest path: a node is below the lowest thing that feeds it.
        layer.set(target, Math.max(layer.get(target) ?? 0, layer.get(id)! + 1))
        indegree.set(target, indegree.get(target)! - 1)
        if (indegree.get(target) === 0 && !placed.has(target)) {
          placed.add(target)
          next.push(target)
        }
      }
    }
    frontier = next
  }

  // Whatever Kahn could not place is inside a cycle. It is laid out below
  // everything that could be placed, in the runtime's own order, and named.
  const cyclic = ids.filter((id) => !placed.has(id))
  const walkOrder = nodesInWalkOrder(coverage)
  const rank = (id: string) => {
    const seen = walkOrder.indexOf(id)
    return seen === -1 ? walkOrder.length + ids.indexOf(id) : seen
  }
  if (cyclic.length > 0) {
    const below = placed.size === 0 ? 0 : Math.max(...[...placed].map((id) => layer.get(id)!)) + 1
    for (const id of [...cyclic].sort((a, b) => rank(a) - rank(b))) layer.set(id, below)
  }

  const inCoverage = new Set(walkOrder)
  const result = typeof document.result === 'string' ? document.result : undefined
  const resultDangling = result !== undefined && !known.has(result)

  const byLayer = new Map<number, string[]>()
  for (const id of ids) {
    const at = layer.get(id) ?? 0
    if (!byLayer.has(at)) byLayer.set(at, [])
    byLayer.get(at)!.push(id)
  }

  const nodes: WalkNode[] = []
  const depth = byLayer.size === 0 ? 0 : Math.max(...byLayer.keys()) + 1
  let width = 0
  for (let at = 0; at < depth; at += 1) {
    const row = (byLayer.get(at) ?? []).sort((a, b) => rank(a) - rank(b))
    width = Math.max(width, row.length)
    row.forEach((id, column) => {
      const node = declared[id]!
      nodes.push({
        id,
        pack: typeof node?.pack === 'string' ? node.pack : undefined,
        description: typeof node?.description === 'string' ? node.description : undefined,
        layer: at,
        column,
        isResult: result !== undefined && !resultDangling && id === result,
        inCoverage: inCoverage.has(id)
      })
    })
  }

  return { nodes, edges, width, depth, result, resultDangling, cyclic }
}

/**
 * One line saying why the coverage fallback is on screen, or nothing.
 *
 * Nothing is right in two cases, and they are different kinds of nothing. A
 * runtime with no such tool — jpack 0.18.0 and older — is not a failure and
 * gets the fallback with no apology, because nothing went wrong and there is
 * nothing to explain. A fetch still in flight has not failed yet either. Every
 * other case names what happened, because a page capable of more than it is
 * showing owes the reader the reason.
 *
 * The undecodable case is the one worth spelling out: the runtime *served*
 * that document deliberately. Serving is not validating, and a mid-edit
 * document is the one a client most needs to see, so it arrives as a
 * successful call whose text is not a graph. Reporting it as a refusal would
 * misdescribe both the runtime and the file.
 */
export function walkFallbackReason(
  /** Whether the runtime advertises `experimental_get_graph` at all. */
  supported: boolean,
  /** Whether a shape was derived and is being drawn. */
  drawn: boolean,
  served: { meta: { status?: string; detail?: string } } | undefined,
  error: Error | null
): string | undefined {
  if (!supported || drawn) return undefined
  if (error) {
    return (
      `The runtime refused to serve this graph's document (${error.message}), so the walk ` +
      `below is the coverage report's evaluation order and no edge is drawn.`
    )
  }
  if (!served) return undefined
  if (served.meta.status === 'undecodable') {
    return (
      `The runtime served this graph's document and could not decode it — ` +
      `${served.meta.detail ?? 'no reason was given'} — and serving is not validating, so the ` +
      `walk below is the coverage report's evaluation order and no edge is drawn.`
    )
  }
  return (
    `The served graph document did not yield the shape this view draws from: nodes must be a ` +
    `map of node ids and edges an array. The walk below is the coverage report's evaluation ` +
    `order and no edge is drawn.`
  )
}

/**
 * What one edge carries, as a short label for the arrow.
 *
 * The two devices are named by what they are — a fact pointer and an evidence
 * requirement id — rather than merged into one word, because they act on
 * different halves of the downstream evaluation. An edge declaring neither
 * cannot happen in a valid document and is labelled as carrying nothing rather
 * than labelled with a guess.
 */
export function edgeCarries(edge: GraphDocumentEdge): string {
  const parts: string[] = []
  if (edge.fact) parts.push(edge.fact)
  if (edge.evidence?.id) {
    parts.push(
      edge.evidence.onUnresolved
        ? `evidence ${edge.evidence.id} (${edge.evidence.onUnresolved} if unresolved)`
        : `evidence ${edge.evidence.id}`
    )
  }
  return parts.join(' · ') || 'nothing this document declares'
}

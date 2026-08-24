/**
 * Reading one served graph document, and laying its walk out.
 *
 * `experimental_get_graph` (ADR-0029) serves the project's own graph file
 * unaltered — the one artifact that states the composition. Everything a
 * diagram needs and the matrix payload never carried is in it: which pack each
 * node names, which node feeds which, what each edge carries, and which node
 * the document declares as its result.
 *
 * Three rules govern this module.
 *
 * **The runtime's verdict on its own bytes is final.** The tool reports
 * `status: "valid"` where its own strict decode succeeded and
 * `status: "undecodable"` where it did not, and that decode refuses text this
 * browser's `JSON.parse` would happily accept — duplicate member names, most
 * plainly, which JSON.parse resolves last-wins and the runtime's carrier
 * refuses outright. So nothing here reads a document the runtime could not:
 * `readServedDocument` declines before it parses, and the reason shown is the
 * runtime's own sentence. A browser that drew a graph out of bytes the runtime
 * refused would be overruling the runtime with a laxer parser.
 *
 * **It reads; it does not repair.** `status: "valid"` is not a schema verdict —
 * it means acceptable JSON with an object root, and no more — so what the views
 * rely on is checked here, member by member, and a document that does not carry
 * it is declined rather than coerced. A missing `edges` array is not an empty
 * one, an edge with no endpoints is not an edge between blanks, and a cycle or
 * a self-loop is reported rather than drawn around: the graph format requires
 * the edges to form a DAG, so a document whose edges do not are a document this
 * cannot lay out.
 *
 * **The order it draws is the order the runtime states.** Layering comes from
 * the document's own edges; where a layer holds more than one node, the tie is
 * broken by the walk order the coverage report enumerated the nodes in, which
 * is the runtime's actual evaluation order. Nothing invents a sequence.
 */
import { nodesInWalkOrder } from './canonical'
import { ToolRefusal } from './refusal'
import type {
  GraphDocument,
  GraphDocumentEdge,
  GraphDocumentEvidenceFeed,
  GraphDocumentNode,
  ReadGraphDocument
} from './types'

/** What one read of served text produced: a document, or the reason there is none. */
export type GraphDocumentRead =
  | { ok: true; document: ReadGraphDocument }
  | { ok: false; reason: string }

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

/** True where a member is absent, or present with the type the format declares. */
function optionalString(value: unknown): boolean {
  return value === undefined || typeof value === 'string'
}

/**
 * Read served graph-document text into the shape the views draw from.
 *
 * Only the members a view actually reads are checked, and each is checked for
 * what the graph format declares it to be: `nodes` a non-empty map of objects,
 * `edges` an array — required even when empty, so a single-node graph states
 * its emptiness rather than implying it — of objects whose `from` and `to` are
 * strings, and every optional member a string (or, for `evidence`, an object
 * with a string `id`) where it is present at all.
 *
 * A malformed member declines the whole document rather than being dropped or
 * coerced. Coercion is the failure mode this exists to prevent: an absent
 * `edges` read as `[]` makes a view say "this document declares no edge", which
 * is a claim about the file that the file never made.
 */
export function readGraphDocument(text: string): GraphDocumentRead {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch (cause) {
    return { ok: false, reason: `the served text is not JSON (${String(cause)})` }
  }
  if (!isPlainObject(value)) {
    return { ok: false, reason: 'the served document is not a JSON object' }
  }

  const nodes = value.nodes
  if (!isPlainObject(nodes)) {
    return { ok: false, reason: 'its `nodes` member is not a map of node ids' }
  }
  const ids = Object.keys(nodes)
  if (ids.length === 0) {
    return { ok: false, reason: 'it declares no node, and the format requires at least one' }
  }
  for (const id of ids) {
    const node = nodes[id]
    if (!isPlainObject(node)) {
      return { ok: false, reason: `its node \`${id}\` is not an object` }
    }
    if (!optionalString(node.pack) || !optionalString(node.description)) {
      return { ok: false, reason: `its node \`${id}\` declares a member of the wrong type` }
    }
  }

  const edges = value.edges
  if (!Array.isArray(edges)) {
    return {
      ok: false,
      reason:
        edges === undefined
          ? 'it declares no `edges` member, which the format requires even when empty'
          : 'its `edges` member is not an array'
    }
  }
  for (let index = 0; index < edges.length; index += 1) {
    const edge: unknown = edges[index]
    if (!isPlainObject(edge)) {
      return { ok: false, reason: `its edge ${index} is not an object` }
    }
    if (typeof edge.from !== 'string' || typeof edge.to !== 'string') {
      return { ok: false, reason: `its edge ${index} does not name both of its endpoints` }
    }
    if (!optionalString(edge.fact) || !optionalString(edge.description)) {
      return { ok: false, reason: `its edge ${index} declares a member of the wrong type` }
    }
    const evidence: unknown = edge.evidence
    if (evidence !== undefined) {
      if (!isPlainObject(evidence) || typeof evidence.id !== 'string') {
        return { ok: false, reason: `its edge ${index} declares an evidence feed with no requirement id` }
      }
      if (!optionalString(evidence.onUnresolved)) {
        return { ok: false, reason: `its edge ${index} declares an onUnresolved that is not a string` }
      }
    }
  }

  if (!optionalString(value.result)) {
    return { ok: false, reason: 'its `result` member is not a node id' }
  }

  return {
    ok: true,
    document: {
      ...(value as GraphDocument),
      nodes: nodes as Record<string, GraphDocumentNode>,
      edges: edges as GraphDocumentEdge[]
    }
  }
}

/**
 * Read one served answer, with the runtime's verdict on it consulted first.
 *
 * `meta.status` is the runtime's own decode, and it is stricter than this
 * browser's: duplicate member names, an over-deep document and an over-long one
 * are all refused there and accepted by `JSON.parse` here. So a status that is
 * not `valid` ends the read with the runtime's own detail, and this client's
 * parser never gets to overrule it.
 */
export function readServedDocument(
  meta: { status?: string; detail?: string },
  text: string
): GraphDocumentRead {
  if (meta.status !== 'valid') {
    return {
      ok: false,
      reason:
        meta.status === 'undecodable' || meta.status === undefined
          ? (meta.detail ?? 'the runtime gave no reason')
          : `the runtime reports this document as ${meta.status}` +
            (meta.detail ? ` — ${meta.detail}` : '')
    }
  }
  return readGraphDocument(text)
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
   * False is the interesting case ADR-0029 exists for: the document declares a
   * node the coverage report names no probe for, and a gap count of zero would
   * read as "fully witnessed" rather than as coverage saying nothing at all.
   * Why coverage is silent is not something this can tell, so nothing here or
   * downstream says why.
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
}

/**
 * A laid-out walk, or the reason there is none.
 *
 * Declining is a real outcome and not an error: the runtime serves a mid-edit
 * document deliberately, and a document whose edges do not form the DAG the
 * format requires is one this module reports rather than repairs.
 */
export type WalkLayout =
  | { drawn: true; shape: GraphWalkShape }
  | { drawn: false; reason: string }

/**
 * Lay one read document out as a layered DAG.
 *
 * Layering is longest-path: a node sits one layer below the lowest node that
 * feeds it, so every declared edge points downward and an edge's label has
 * somewhere to sit. Kahn's algorithm does the ordering, which also detects a
 * cycle by construction — whatever it cannot place is what a cycle held up.
 *
 * A cycle or a self-loop declines the whole layout. The format requires the
 * edges to form a DAG, so those documents are mid-edit ones, and every way of
 * drawing them is a repair: laying the unplaceable nodes out in a row below the
 * rest asserts an order the document does not state, and a self-loop drawn as
 * an arrow leaving and entering one box asserts that the layering accounted for
 * it when the layering skipped it.
 *
 * `coverage` is the graph matrix run's own coverage report, used only to break
 * ties inside a layer, so that two independent nodes appear in the order the
 * runtime actually evaluated them. Where coverage names neither, the
 * document's own key order decides, which keeps the layout stable across runs.
 */
export function deriveWalkLayout(
  document: ReadGraphDocument,
  coverage: readonly { probe: string }[] | undefined
): WalkLayout {
  const declared = document.nodes
  const ids = Object.keys(declared)
  const known = new Set(ids)

  const edges: WalkEdge[] = document.edges.map((edge, index) => ({
    ...edge,
    index,
    drawable: known.has(edge.from) && known.has(edge.to)
  }))

  const selfLoop = edges.find((edge) => edge.drawable && edge.from === edge.to)
  if (selfLoop) {
    return {
      drawn: false,
      reason:
        `the served document's edge ${selfLoop.index} names \`${selfLoop.from}\` as both its ` +
        `endpoints, which no layering can place`
    }
  }

  // Only edges between declared nodes constrain the layering. An edge naming
  // an endpoint that is not there constrains nothing, because there is nothing
  // for it to constrain.
  const structural = edges.filter((edge) => edge.drawable)

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

  // Whatever Kahn could not place was held up by a cycle. Which nodes are
  // *inside* one is not something this says: a node fed by a cycle is
  // unplaceable too, and naming it as part of the cycle would be a claim about
  // the document that the layering never established.
  const unplaced = ids.filter((id) => !placed.has(id))
  if (unplaced.length > 0) {
    return {
      drawn: false,
      reason:
        `the served document's edges cannot be layered: ${unplaced.join(', ')} could not be ` +
        `placed, which a cycle in those edges causes — a node inside one and a node fed by ` +
        `one are both unplaceable`
    }
  }

  const walkOrder = nodesInWalkOrder(coverage)
  const rank = (id: string) => {
    const seen = walkOrder.indexOf(id)
    return seen === -1 ? walkOrder.length + ids.indexOf(id) : seen
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

  return { drawn: true, shape: { nodes, edges, width, depth, result, resultDangling } }
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
 * Two distinctions are load-bearing here.
 *
 * A **refusal** is the runtime saying no, and its own sentence is the reason —
 * quoted, not summarised. Anything else that threw is a fetch that did not
 * complete, and calling that a refusal would attribute to the runtime a
 * position it never took.
 *
 * An **undecodable document** was *served* deliberately. Serving is not
 * validating, and a mid-edit document is the one a client most needs to see, so
 * it arrives as a successful call whose text the runtime itself could not
 * decode. Its reason is the runtime's own detail, verbatim, and it is decided
 * before anything about what this client made of the bytes — the runtime's
 * verdict on its own bytes is not this browser's to revisit.
 */
export function walkFallbackReason(input: {
  /** Whether the runtime advertises `experimental_get_graph` at all. */
  supported: boolean
  /** Whether a shape was derived and is being drawn. */
  drawn: boolean
  /** The served answer, where one arrived: the runtime's verdict, and this client's read. */
  served: { meta: { status?: string; detail?: string }; unreadable?: string } | undefined
  /** Why a read document was not laid out, where it was read and not laid out. */
  declined?: string
  error: Error | null
}): string | undefined {
  const { supported, drawn, served, declined, error } = input
  if (!supported) return undefined
  if (error) {
    return error instanceof ToolRefusal
      ? because(`the runtime refused to serve this graph's document — ${error.message}`)
      : because(
          `this desk could not fetch this graph's document — ${error.message} — which is a ` +
            `fetch that did not complete rather than an answer from the runtime`
        )
  }
  if (!served) return undefined
  if (served.meta.status !== 'valid') {
    return because(
      `the runtime served this graph's document and could not decode it — ` +
        `${served.meta.detail ?? 'no reason was given'} — and serving is not validating`
    )
  }
  if (drawn) return undefined
  if (declined) return because(declined)
  if (served.unreadable) {
    return because(`the served graph document is not one this view can draw: ${served.unreadable}`)
  }
  return because('the served graph document did not yield the shape this view draws from')
}

/** One reason, with the sentence that says what is on screen instead. */
function because(reason: string): string {
  const lead = reason.charAt(0).toUpperCase() + reason.slice(1)
  return `${lead}, so the walk below is the coverage report's evaluation order and no edge is drawn.`
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
export function edgeCarries(edge: {
  fact?: string
  evidence?: GraphDocumentEvidenceFeed
}): string {
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

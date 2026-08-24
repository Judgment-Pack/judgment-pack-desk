import { describe, expect, it } from 'vitest'
import {
  deriveWalkLayout,
  edgeCarries,
  readGraphDocument,
  readServedDocument,
  walkFallbackReason
} from './graphDocument'
import { ToolRefusal } from './refusal'
import type { ReadGraphDocument } from './types'

/** The enterprise demo's own graph, byte for byte as the runtime serves it. */
const SERVED = JSON.stringify(
  {
    formatVersion: '1',
    id: 'vendor-onboarding-flow',
    version: '0.1.0',
    nodes: {
      screening: { pack: 'sanctions-screening', description: 'Establish the screening decision.' },
      onboarding: { pack: 'vendor-onboarding', description: 'Decide onboarding from it.' }
    },
    edges: [
      {
        from: 'screening',
        to: 'onboarding',
        fact: '/vendor/sanctionsScreening/status',
        evidence: { id: 'sanctions-screening' },
        description: 'clear or match lands at the pointer the onboarding rules read.'
      }
    ],
    result: 'onboarding'
  },
  null,
  2
)

/** The document behind some served text, or a failure naming what declined it. */
function read(text: string): ReadGraphDocument {
  const result = readGraphDocument(text)
  if (!result.ok) throw new Error(`expected a readable document, got: ${result.reason}`)
  return result.document
}

describe('readGraphDocument', () => {
  it('reads the served document', () => {
    const document = read(SERVED)
    expect(document.id).toBe('vendor-onboarding-flow')
    expect(Object.keys(document.nodes)).toEqual(['screening', 'onboarding'])
  })

  it('declines text that is not JSON, which is what an undecodable document is', () => {
    // The runtime serves a mid-edit document deliberately (ADR-0029), so this
    // arrives as a successful call whose text is a truncated file.
    const result = readGraphDocument('{ "formatVersion": "1", "nodes": {')
    expect(result.ok).toBe(false)
  })

  /**
   * Every one of these is text `JSON.parse` accepts and the views cannot draw.
   * The reason each is named rather than coerced is the module's own rule: what
   * is drawn is what the document states, and a slot this could not read is a
   * slot the document did not state.
   */
  it.each([
    ['a document that is not an object', '[]', /not a JSON object/],
    ['nodes that are not a map', '{"nodes":[],"edges":[]}', /`nodes` member is not a map/],
    ['a null nodes member', '{"nodes":null,"edges":[]}', /`nodes` member is not a map/],
    ['no node at all', '{"nodes":{},"edges":[]}', /declares no node/],
    ['a node that is not an object', '{"nodes":{"a":null},"edges":[]}', /node `a` is not an object/],
    ['a node that is a primitive', '{"nodes":{"a":"pack"},"edges":[]}', /node `a` is not an object/],
    ['a node whose pack is not a string', '{"nodes":{"a":{"pack":7}},"edges":[]}', /node `a` declares a member of the wrong type/],
    ['edges of the wrong type', '{"nodes":{"a":{"pack":"p"}},"edges":{}}', /`edges` member is not an array/],
    // The format requires the member even when empty, so "absent" is not
    // "none": a view that read it as none would say the document declares no
    // edge, which the document never said.
    ['no edges member at all', '{"nodes":{"a":{"pack":"p"}},"result":"a"}', /declares no `edges` member/],
    ['a null edge', '{"nodes":{"a":{"pack":"p"}},"edges":[null]}', /edge 0 is not an object/],
    ['an edge with no endpoints', '{"nodes":{"a":{"pack":"p"}},"edges":[{}]}', /edge 0 does not name both of its endpoints/],
    [
      'an edge whose endpoint is not a string',
      '{"nodes":{"a":{"pack":"p"}},"edges":[{"from":"a","to":3}]}',
      /edge 0 does not name both of its endpoints/
    ],
    [
      'an evidence feed with no requirement id',
      '{"nodes":{"a":{"pack":"p"}},"edges":[{"from":"a","to":"a","evidence":{}}]}',
      /edge 0 declares an evidence feed with no requirement id/
    ],
    [
      'a result that is not a node id',
      '{"nodes":{"a":{"pack":"p"}},"edges":[],"result":[]}',
      /`result` member is not a node id/
    ]
  ])('declines %s', (_case, text, reason) => {
    const result = readGraphDocument(text)
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.reason).toMatch(reason)
  })

  it('declines a document that declares no edges member, because the format requires one', () => {
    // The reversal of what this once blessed: a missing array is not an empty
    // one, and reading it as empty made the view say "this document declares no
    // edge" about a document that declared nothing of the sort.
    const result = readGraphDocument('{"nodes":{"a":{"pack":"p"}},"result":"a"}')
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.reason).toContain('the format requires')
  })

  it('accepts a document that declares its edges empty, which is what a one-node graph does', () => {
    const document = read('{"nodes":{"a":{"pack":"p"}},"edges":[],"result":"a"}')
    expect(document.edges).toEqual([])
  })

  it('accepts a node that names no pack, which a decoded document may still omit', () => {
    // `status: valid` is the runtime's carrier decode and an object root, not a
    // schema verdict, so a node with no pack reaches this reader — and the view
    // says the node names no pack rather than this declining the document.
    const document = read('{"nodes":{"a":{}},"edges":[]}')
    expect(document.nodes.a).toEqual({})
  })
})

describe('readServedDocument', () => {
  it('declines a document the runtime could not decode, whatever this parser makes of it', () => {
    // The finding this exists for: the runtime's decode refuses duplicate
    // member names and JSON.parse takes them last-wins, so text this browser
    // parses perfectly well is text the runtime already refused. Drawing it
    // would be the browser overruling the runtime with a laxer parser.
    const parseable = '{"nodes":{"a":{"pack":"p"}},"nodes":{"b":{"pack":"q"}},"edges":[]}'
    expect(JSON.parse(parseable)).toBeTruthy()
    const result = readServedDocument(
      { status: 'undecodable', detail: 'Object member name is duplicated.' },
      parseable
    )
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.reason).toBe('Object member name is duplicated.')
  })

  it('declines an answer that reports no status at all', () => {
    // A runtime that answered without structured content stated no verdict, and
    // an absent verdict is not a verdict of valid.
    expect(readServedDocument({}, SERVED).ok).toBe(false)
  })

  it('declines a status this client does not know, naming it', () => {
    const result = readServedDocument({ status: 'partial' }, SERVED)
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.reason).toContain('partial')
  })

  it('reads a document the runtime decoded', () => {
    const result = readServedDocument({ status: 'valid' }, SERVED)
    expect(result.ok).toBe(true)
  })

  it('still declines a decoded document that does not carry what the views draw from', () => {
    // `valid` is the runtime's carrier decode and an object root, not a schema
    // verdict, so the members a view reads are this client's to check.
    const result = readServedDocument({ status: 'valid' }, '{"nodes":{"a":{"pack":"p"}}}')
    expect(result.ok).toBe(false)
  })
})

describe('deriveWalkLayout', () => {
  it('layers the served document by its own edges and marks the declared result', () => {
    const layout = deriveWalkLayout(read(SERVED), [])
    expect(layout.drawn).toBe(true)
    if (!layout.drawn) return
    const shape = layout.shape
    expect(shape.nodes.map((node) => [node.id, node.layer])).toEqual([
      ['screening', 0],
      ['onboarding', 1]
    ])
    expect(shape.nodes.find((node) => node.isResult)?.id).toBe('onboarding')
    expect(shape.resultDangling).toBe(false)
    expect(shape.edges[0]!.drawable).toBe(true)
    expect(shape.depth).toBe(2)
    expect(shape.width).toBe(1)
  })

  it('places a node on the longest path from its deepest feeder', () => {
    // a → b → c and a → c: c must sit below b, not beside it, or the a→c edge
    // would be drawn skipping a layer it does not skip.
    const layout = deriveWalkLayout(
      read(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' }, c: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'c', fact: '/y' },
            { from: 'a', to: 'c', fact: '/z' }
          ],
          result: 'c'
        })
      ),
      []
    )
    expect(layout.drawn).toBe(true)
    if (!layout.drawn) return
    expect(new Map(layout.shape.nodes.map((node) => [node.id, node.layer]))).toEqual(
      new Map([
        ['a', 0],
        ['b', 1],
        ['c', 2]
      ])
    )
  })

  it("breaks a layer's tie with the runtime's own evaluation order", () => {
    const document = read(
      JSON.stringify({
        nodes: { second: { pack: 'p' }, first: { pack: 'p' } },
        edges: [],
        result: 'first'
      })
    )
    const layout = deriveWalkLayout(document, [
      { probe: 'node:first:unknown' },
      { probe: 'node:second:unknown' }
    ])
    expect(layout.drawn).toBe(true)
    if (!layout.drawn) return
    expect(layout.shape.nodes.map((node) => node.id)).toEqual(['first', 'second'])
    expect(layout.shape.width).toBe(2)
  })

  it('declines a document whose edges form a cycle, rather than drawing part of one', () => {
    const layout = deriveWalkLayout(
      read(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'a', fact: '/y' }
          ]
        })
      ),
      []
    )
    expect(layout.drawn).toBe(false)
    if (layout.drawn) return
    expect(layout.reason).toContain('a, b')
    expect(layout.reason).toContain('cannot be layered')
  })

  it('does not brand a node merely blocked by a cycle as part of it', () => {
    // a ↔ b, b → c. All three are unplaceable and only two are in the cycle, so
    // the reason says they could not be placed and names the cause once, for
    // the edges, rather than attributing membership to each node.
    const layout = deriveWalkLayout(
      read(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' }, c: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'a', fact: '/y' },
            { from: 'b', to: 'c', fact: '/z' }
          ]
        })
      ),
      []
    )
    expect(layout.drawn).toBe(false)
    if (layout.drawn) return
    expect(layout.reason).toContain('could not be placed')
    expect(layout.reason).toContain('c')
    // The wording that branded every unplaced node as cyclic is gone.
    expect(layout.reason).not.toMatch(/cycle through/)
  })

  it('declines a self-loop rather than drawing an edge the layering skipped', () => {
    // Excluding it from the layering and drawing it anyway would show an arrow
    // the layout never accounted for — a repair, and this module does not repair.
    const layout = deriveWalkLayout(
      read(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'b', fact: '/y' }
          ]
        })
      ),
      []
    )
    expect(layout.drawn).toBe(false)
    if (layout.drawn) return
    expect(layout.reason).toContain('edge 1')
    expect(layout.reason).toContain('`b`')
  })

  it('does not draw an edge naming an endpoint the document never declares', () => {
    const layout = deriveWalkLayout(
      read(
        JSON.stringify({
          nodes: { a: { pack: 'p' } },
          edges: [{ from: 'a', to: 'ghost', fact: '/x' }],
          result: 'phantom'
        })
      ),
      []
    )
    expect(layout.drawn).toBe(true)
    if (!layout.drawn) return
    expect(layout.shape.edges[0]!.drawable).toBe(false)
    expect(layout.shape.resultDangling).toBe(true)
    expect(layout.shape.nodes.every((node) => !node.isResult)).toBe(true)
  })

  it('says which declared nodes coverage never named', () => {
    // The case ADR-0029 exists for: coverage can name no probe for a node the
    // document declares, and the document is the only place that node appears.
    const layout = deriveWalkLayout(read(SERVED), [{ probe: 'node:screening:unknown' }])
    expect(layout.drawn).toBe(true)
    if (!layout.drawn) return
    expect(layout.shape.nodes.find((node) => node.id === 'screening')!.inCoverage).toBe(true)
    expect(layout.shape.nodes.find((node) => node.id === 'onboarding')!.inCoverage).toBe(false)
  })
})

describe('edgeCarries', () => {
  it('names both devices apart, because they act on different halves', () => {
    expect(edgeCarries({ fact: '/p', evidence: { id: 'e' } })).toBe('/p · evidence e')
  })

  it('carries the declared onUnresolved tri-state where the edge declares one', () => {
    expect(edgeCarries({ evidence: { id: 'e', onUnresolved: 'absent' } })).toBe(
      'evidence e (absent if unresolved)'
    )
  })

  it('does not invent a device for an edge that declares none', () => {
    expect(edgeCarries({})).toBe('nothing this document declares')
  })
})

describe('walkFallbackReason', () => {
  const base = { supported: true, drawn: false, served: undefined, error: null }

  it('apologises for nothing when the runtime has no such tool', () => {
    // jpack 0.18.0 and older. Nothing went wrong, so nothing is explained.
    expect(walkFallbackReason({ ...base, supported: false })).toBeUndefined()
  })

  it('says nothing while the fetch is still in flight', () => {
    expect(walkFallbackReason(base)).toBeUndefined()
  })

  it('says nothing when the document is being drawn', () => {
    expect(
      walkFallbackReason({ ...base, drawn: true, served: { meta: { status: 'valid' } } })
    ).toBeUndefined()
  })

  it("reports an undecodable document as served, with the runtime's own detail", () => {
    const reason = walkFallbackReason({
      ...base,
      served: { meta: { status: 'undecodable', detail: 'not valid JSON at line 1' } }
    })
    expect(reason).toContain('served this graph')
    expect(reason).toContain('not valid JSON at line 1')
    expect(reason).toContain('serving is not validating')
    expect(reason).not.toContain('refused')
  })

  it("reports the runtime's verdict even where this client did make a shape", () => {
    // The override this guards: a browser that parsed what the runtime refused
    // and drew it would report nothing at all. The verdict is consulted before
    // anything about what was drawn.
    const reason = walkFallbackReason({
      ...base,
      drawn: true,
      served: { meta: { status: 'undecodable', detail: 'Object member name is duplicated.' } }
    })
    expect(reason).toContain('Object member name is duplicated.')
  })

  it("reports a refusal as a refusal, in the runtime's own words", () => {
    const reason = walkFallbackReason({
      ...base,
      error: new ToolRefusal('no graph named x is configured', undefined)
    })
    expect(reason).toContain('refused')
    expect(reason).toContain('no graph named x is configured')
  })

  it('does not call a fetch that never completed a refusal', () => {
    // A dropped socket is not the runtime saying no, and saying it was would
    // attribute to the runtime a position it never took.
    const reason = walkFallbackReason({ ...base, error: new Error('the connection closed') })
    expect(reason).not.toContain('refused')
    expect(reason).toContain('could not fetch')
    expect(reason).toContain('the connection closed')
  })

  it('reports a document this client could not read, with which member declined it', () => {
    const reason = walkFallbackReason({
      ...base,
      served: { meta: { status: 'valid' }, unreadable: 'its `edges` member is not an array' }
    })
    expect(reason).toContain('its `edges` member is not an array')
    expect(reason).not.toContain('refused')
  })

  it('reports a document that was read and could not be laid out', () => {
    const reason = walkFallbackReason({
      ...base,
      served: { meta: { status: 'valid' } },
      declined: "the served document's edges cannot be layered: a, b could not be placed"
    })
    expect(reason).toContain('could not be placed')
    expect(reason).toContain('no edge is drawn')
  })
})

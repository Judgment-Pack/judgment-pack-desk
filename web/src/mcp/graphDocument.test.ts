import { describe, expect, it } from 'vitest'
import {
  deriveWalkShape,
  edgeCarries,
  parseGraphDocument,
  walkFallbackReason
} from './graphDocument'

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

describe('parseGraphDocument', () => {
  it('reads the served document', () => {
    const document = parseGraphDocument(SERVED)
    expect(document?.id).toBe('vendor-onboarding-flow')
    expect(Object.keys(document?.nodes ?? {})).toEqual(['screening', 'onboarding'])
  })

  it('declines text that is not JSON, which is what an undecodable document is', () => {
    // The runtime serves a mid-edit document deliberately (ADR-0029), so this
    // arrives as a successful call whose text is a truncated file.
    expect(parseGraphDocument('{ "formatVersion": "1", "nodes": {')).toBeUndefined()
  })

  it('declines a document whose nodes member is not a map', () => {
    expect(parseGraphDocument('{"nodes":[],"edges":[]}')).toBeUndefined()
    expect(parseGraphDocument('{"nodes":null}')).toBeUndefined()
    expect(parseGraphDocument('[]')).toBeUndefined()
  })

  it('declines an edges member of the wrong type rather than coercing it away', () => {
    // Coercing a malformed member to "no edges" would draw a graph with no
    // dependencies from a document that says something else entirely.
    expect(parseGraphDocument('{"nodes":{"a":{"pack":"p"}},"edges":{}}')).toBeUndefined()
  })

  it('accepts a document that declares no edges at all', () => {
    const document = parseGraphDocument('{"nodes":{"a":{"pack":"p"}},"result":"a"}')
    expect(document?.edges).toBeUndefined()
  })
})

describe('deriveWalkShape', () => {
  it('layers the served document by its own edges and marks the declared result', () => {
    const shape = deriveWalkShape(parseGraphDocument(SERVED)!, [])
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
    const shape = deriveWalkShape(
      parseGraphDocument(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' }, c: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'c', fact: '/y' },
            { from: 'a', to: 'c', fact: '/z' }
          ],
          result: 'c'
        })
      )!,
      []
    )
    expect(new Map(shape.nodes.map((node) => [node.id, node.layer]))).toEqual(
      new Map([
        ['a', 0],
        ['b', 1],
        ['c', 2]
      ])
    )
  })

  it("breaks a layer's tie with the runtime's own evaluation order", () => {
    const document = parseGraphDocument(
      JSON.stringify({
        nodes: { second: { pack: 'p' }, first: { pack: 'p' } },
        edges: [],
        result: 'first'
      })
    )!
    const shape = deriveWalkShape(document, [
      { probe: 'node:first:unknown' },
      { probe: 'node:second:unknown' }
    ])
    expect(shape.nodes.map((node) => node.id)).toEqual(['first', 'second'])
    expect(shape.width).toBe(2)
  })

  it('reports a cycle instead of hanging on it', () => {
    const shape = deriveWalkShape(
      parseGraphDocument(
        JSON.stringify({
          nodes: { a: { pack: 'p' }, b: { pack: 'p' } },
          edges: [
            { from: 'a', to: 'b', fact: '/x' },
            { from: 'b', to: 'a', fact: '/y' }
          ]
        })
      )!,
      []
    )
    expect(shape.cyclic.sort()).toEqual(['a', 'b'])
    expect(shape.nodes).toHaveLength(2)
  })

  it('does not draw an edge naming an endpoint the document never declares', () => {
    const shape = deriveWalkShape(
      parseGraphDocument(
        JSON.stringify({
          nodes: { a: { pack: 'p' } },
          edges: [{ from: 'a', to: 'ghost', fact: '/x' }],
          result: 'phantom'
        })
      )!,
      []
    )
    expect(shape.edges[0]!.drawable).toBe(false)
    expect(shape.resultDangling).toBe(true)
    expect(shape.nodes.every((node) => !node.isResult)).toBe(true)
  })

  it('says which declared nodes coverage never named', () => {
    // The case ADR-0029 exists for: coverage can omit a node the run never
    // admitted, and the document is the only place that node appears.
    const shape = deriveWalkShape(parseGraphDocument(SERVED)!, [
      { probe: 'node:screening:unknown' }
    ])
    expect(shape.nodes.find((node) => node.id === 'screening')!.inCoverage).toBe(true)
    expect(shape.nodes.find((node) => node.id === 'onboarding')!.inCoverage).toBe(false)
  })
})

describe('edgeCarries', () => {
  it('names both devices apart, because they act on different halves', () => {
    expect(edgeCarries({ from: 'a', to: 'b', fact: '/p', evidence: { id: 'e' } })).toBe(
      '/p · evidence e'
    )
  })

  it('carries the declared onUnresolved tri-state where the edge declares one', () => {
    expect(
      edgeCarries({ from: 'a', to: 'b', evidence: { id: 'e', onUnresolved: 'absent' } })
    ).toBe('evidence e (absent if unresolved)')
  })

  it('does not invent a device for an edge that declares none', () => {
    expect(edgeCarries({ from: 'a', to: 'b' })).toBe('nothing this document declares')
  })
})

describe('walkFallbackReason', () => {
  it('apologises for nothing when the runtime has no such tool', () => {
    // jpack 0.18.0 and older. Nothing went wrong, so nothing is explained.
    expect(walkFallbackReason(false, false, undefined, null)).toBeUndefined()
  })

  it('says nothing while the fetch is still in flight', () => {
    expect(walkFallbackReason(true, false, undefined, null)).toBeUndefined()
  })

  it('says nothing when the document is being drawn', () => {
    expect(
      walkFallbackReason(true, true, { meta: { status: 'valid' } }, null)
    ).toBeUndefined()
  })

  it('reports an undecodable document as served, not as refused', () => {
    const reason = walkFallbackReason(
      true,
      false,
      { meta: { status: 'undecodable', detail: 'not valid JSON at line 1' } },
      null
    )
    expect(reason).toContain('served this graph')
    expect(reason).toContain('not valid JSON at line 1')
    expect(reason).toContain('serving is not validating')
    expect(reason).not.toContain('refused')
  })

  it('reports a refusal as a refusal', () => {
    const reason = walkFallbackReason(true, false, undefined, new Error('no graph named x'))
    expect(reason).toContain('refused')
    expect(reason).toContain('no graph named x')
  })

  it('reports a document this client could not shape, distinctly from either', () => {
    const reason = walkFallbackReason(true, false, { meta: { status: 'valid' } }, null)
    expect(reason).toContain('did not yield the shape')
    expect(reason).not.toContain('refused')
  })
})

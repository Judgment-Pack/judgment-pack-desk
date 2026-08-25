import { describe, expect, it } from 'vitest'
import {
  UNKNOWN_CAPABILITIES,
  listAllTools,
  readCapabilities,
  type AdvertisedTool
} from './capabilities'

/** The tools jpack 0.18.0 advertises: no graph document, no graph inventory. */
const RELEASED = [
  { name: 'validate' },
  { name: 'list_packs' },
  { name: 'get_pack' },
  { name: 'experimental_evaluate', inputSchema: { properties: { pack_id: {}, facts: {}, rehearsal: {} } } },
  { name: 'experimental_test_packs' },
  { name: 'experimental_test_graphs' }
]

/** The tools the graph-serving runtime advertises (ADR-0029). */
const SERVING = [...RELEASED, { name: 'experimental_list_graphs' }, { name: 'experimental_get_graph' }]

/** jpack 0.19.0: the graph surface, and the trace ask on its matrix (ADR-0031). */
const TRACING = [
  ...SERVING.filter((tool) => tool.name !== 'experimental_test_graphs'),
  {
    name: 'experimental_test_graphs',
    inputSchema: { properties: { graph_id: {}, include_traces: {} } }
  }
]

describe('readCapabilities', () => {
  it('records the graph surface against a runtime that advertises it', () => {
    expect(readCapabilities(SERVING)).toEqual({
      known: true,
      rehearsalSupported: true,
      graphDocumentSupported: true,
      graphInventorySupported: true,
      // The 0.19.0 runtime's own matrix tool advertises this; the 0.18.0-era
      // fixture above does not, and the two must not be read as one.
      graphTracesSupported: false
    })
  })

  it('records neither against the released runtime that has neither tool', () => {
    const capabilities = readCapabilities(RELEASED)
    expect(capabilities.graphDocumentSupported).toBe(false)
    expect(capabilities.graphInventorySupported).toBe(false)
    // The capability that runtime *does* have is still read, so detecting the
    // new tools cannot regress the old detection.
    expect(capabilities.rehearsalSupported).toBe(true)
  })

  it('reads each of the two graph tools independently', () => {
    // The runtime ships them together, but the desk asks two questions because
    // they answer two: an inventory listing and a document fetch.
    expect(
      readCapabilities([...RELEASED, { name: 'experimental_list_graphs' }])
    ).toMatchObject({ graphInventorySupported: true, graphDocumentSupported: false })
    expect(
      readCapabilities([...RELEASED, { name: 'experimental_get_graph' }])
    ).toMatchObject({ graphInventorySupported: false, graphDocumentSupported: true })
  })

  it('asks a tool question by name and an argument question of a schema', () => {
    // A tool of the same name may or may not take an argument, so the argument
    // is never inferred from the name being present.
    const withoutRehearsal = readCapabilities([
      { name: 'experimental_evaluate', inputSchema: { properties: { pack_id: {}, facts: {} } } },
      { name: 'experimental_get_graph' }
    ])
    expect(withoutRehearsal.rehearsalSupported).toBe(false)
    expect(withoutRehearsal.graphDocumentSupported).toBe(true)
  })

  it('records the trace ask off the graph matrix tool\'s own schema (ADR-0031)', () => {
    expect(readCapabilities(TRACING).graphTracesSupported).toBe(true)
    // Everything else the runtime has is still read, so the new question
    // cannot regress the old ones.
    expect(readCapabilities(TRACING)).toMatchObject({
      rehearsalSupported: true,
      graphDocumentSupported: true,
      graphInventorySupported: true
    })
  })

  it('does not infer the trace ask from the graph matrix tool being present', () => {
    // The tool has existed since long before the argument did. Reading its
    // name as the argument would send `include_traces` to a 0.18.0 runtime,
    // which refuses the unknown member rather than ignoring it.
    expect(readCapabilities(SERVING).graphTracesSupported).toBe(false)
    expect(
      readCapabilities([{ name: 'experimental_test_graphs', inputSchema: { properties: {} } }])
        .graphTracesSupported
    ).toBe(false)
    expect(
      readCapabilities([{ name: 'experimental_test_graphs' }]).graphTracesSupported
    ).toBe(false)
  })

  it('keeps the two argument questions on their own tools', () => {
    // `rehearsal` belongs to experimental_evaluate and `include_traces` to
    // experimental_test_graphs. A reader that pooled the properties of every
    // tool would report each argument on the strength of the other.
    const crossed = readCapabilities([
      { name: 'experimental_evaluate', inputSchema: { properties: { include_traces: {} } } },
      { name: 'experimental_test_graphs', inputSchema: { properties: { rehearsal: {} } } }
    ])
    expect(crossed.rehearsalSupported).toBe(false)
    expect(crossed.graphTracesSupported).toBe(false)
  })

  it('reads a malformed schema as no argument rather than throwing', () => {
    // `inputSchema` is unknown off the wire, and `"x" in y` throws a TypeError
    // for every non-object y. A listing that carried one of these would take
    // capability reading down altogether — leaving the connection reporting
    // nothing about what the runtime can do, which is strictly worse than
    // reporting one argument as unadvertised.
    for (const properties of [null, false, 0, '', 'rehearsal', [], ['rehearsal']]) {
      const capabilities = readCapabilities([
        { name: 'experimental_evaluate', inputSchema: { properties } },
        { name: 'experimental_test_graphs', inputSchema: { properties } }
      ])
      expect(capabilities.rehearsalSupported).toBe(false)
      expect(capabilities.graphTracesSupported).toBe(false)
      expect(capabilities.known).toBe(true)
    }
  })

  it('reads a schema that is not an object as no argument either', () => {
    for (const inputSchema of [null, false, 0, '', 'properties', []]) {
      const capabilities = readCapabilities([
        { name: 'experimental_evaluate', inputSchema },
        { name: 'experimental_test_graphs', inputSchema }
      ])
      expect(capabilities.rehearsalSupported).toBe(false)
      expect(capabilities.graphTracesSupported).toBe(false)
    }
  })

  it('reads nothing from an empty listing, and knows that it read it', () => {
    // An empty listing is an answer: this runtime advertises no tool. That is a
    // different claim from never having asked, which is what UNKNOWN is — and
    // the difference is the one a failed listing must not blur.
    expect(readCapabilities([])).toEqual({
      known: true,
      rehearsalSupported: false,
      graphDocumentSupported: false,
      graphInventorySupported: false,
      graphTracesSupported: false
    })
    expect(UNKNOWN_CAPABILITIES.known).toBe(false)
  })
})

/** A tool lister answering from fixed pages, recording what it was asked for. */
function paged(pages: { tools: AdvertisedTool[]; nextCursor?: string }[]) {
  const asked: (string | undefined)[] = []
  let index = 0
  return {
    asked,
    client: {
      async listTools(params?: { cursor?: string }) {
        asked.push(params?.cursor)
        const page = pages[index]
        index += 1
        if (!page) throw new Error('the enumerator asked for a page that was never offered')
        return page
      }
    }
  }
}

describe('listAllTools', () => {
  it('reads one page and asks for no cursor it was not given', async () => {
    const { asked, client } = paged([{ tools: [{ name: 'list_packs' }] }])
    expect((await listAllTools(client)).map((tool) => tool.name)).toEqual(['list_packs'])
    expect(asked).toEqual([undefined])
  })

  it('follows nextCursor to the end, because a later page is not an absent tool', async () => {
    // The failure this prevents: a runtime whose listing grew past one page has
    // its later tools read as absent, and the desk withdraws a feature the
    // runtime has — silently, because a short listing looks exactly like an
    // older runtime.
    const { asked, client } = paged([
      { tools: [{ name: 'list_packs' }], nextCursor: 'p2' },
      { tools: [{ name: 'experimental_get_graph' }], nextCursor: 'p3' },
      { tools: [{ name: 'experimental_list_graphs' }] }
    ])
    const tools = await listAllTools(client)
    expect(tools.map((tool) => tool.name)).toEqual([
      'list_packs',
      'experimental_get_graph',
      'experimental_list_graphs'
    ])
    expect(asked).toEqual([undefined, 'p2', 'p3'])
    expect(readCapabilities(tools)).toMatchObject({
      graphDocumentSupported: true,
      graphInventorySupported: true
    })
  })

  it('stops on a repeated cursor rather than paging forever', async () => {
    const { client } = paged([
      { tools: [{ name: 'a' }], nextCursor: 'loop' },
      { tools: [{ name: 'b' }], nextCursor: 'loop' }
    ])
    await expect(listAllTools(client)).rejects.toThrow(/does not terminate/)
  })

  it('treats an empty cursor as the end, not as another page to ask for', async () => {
    const { asked, client } = paged([{ tools: [{ name: 'a' }], nextCursor: '' }])
    expect((await listAllTools(client)).map((tool) => tool.name)).toEqual(['a'])
    expect(asked).toEqual([undefined])
  })
})

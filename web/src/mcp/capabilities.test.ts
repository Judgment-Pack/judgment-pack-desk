import { describe, expect, it } from 'vitest'
import { readCapabilities } from './capabilities'

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

describe('readCapabilities', () => {
  it('records the graph surface against a runtime that advertises it', () => {
    expect(readCapabilities(SERVING)).toEqual({
      rehearsalSupported: true,
      graphDocumentSupported: true,
      graphInventorySupported: true
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

  it('reads nothing from an empty listing', () => {
    expect(readCapabilities([])).toEqual({
      rehearsalSupported: false,
      graphDocumentSupported: false,
      graphInventorySupported: false
    })
  })
})

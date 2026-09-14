import type { McpToolResult } from '../../assistant/engine'
import { jsonIdentity } from '../checkCandidate'
import fixtures from './expectations.json'

/** Recorded native runtime responses, not a second implementation of §8.3. */
export function fixtureExpectations(args: Record<string, unknown>): McpToolResult {
  if (args.spec_version !== '0.2.0-draft') return { isError: true, content: [{ type: 'text', text: 'Unsupported spec version' }] }
  const results = (args.expectations as string[]).map((text, index) => {
    const fixture = fixtures.find(item => {
      try { return jsonIdentity(JSON.parse(item.text)) === jsonIdentity(JSON.parse(text)) }
      catch { return false }
    })
    if (!fixture) throw new Error(`No native expectation fixture for ${text}`)
    return { index, ...fixture.result }
  })
  return { structuredContent: { status: results.some(row => row.status === 'invalid') ? 'invalid' : 'valid', specVersion: args.spec_version, results } }
}

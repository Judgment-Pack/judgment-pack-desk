import { expect, it, vi } from 'vitest'
import type { CallTool, McpToolResult } from '@judgment-pack/harness/engine'
import vectors from '../../../internal/desk/testdata/runtime-cache-identity.json'
import { digestOf, jsonIdentity } from '../research/checkCandidate'
import { runtimeCheckIdentity } from '../research/retainedCheck'
import { runtimeIdentityOf } from './runContext'

// The same fixture is consumed by assistant_runtime_cache_identity_test.go.
// Descriptor inputs and canonical strings are handwritten; only expected hashes
// are derived here, independently from the production canonicalization path.
const reply = (result: string): CallTool => vi.fn(async () => JSON.parse(result) as McpToolResult)

it('loads each group of the shared runtime identity contract vectors', () => {
  expect(vectors.version).toBe(1)
  expect(vectors.canonicalCases.length).toBeGreaterThan(0)
  expect(vectors.payloadCases.length).toBeGreaterThan(0)
  expect(vectors.disabledResults.length).toBeGreaterThan(0)
  expect(vectors.malformedVersions.length).toBeGreaterThan(0)
})

it.each(vectors.canonicalCases)('matches shared JS canonical and Runtime identity vector $id', async row => {
  expect(jsonIdentity(JSON.parse(row.descriptor))).toBe(row.canonical)
  const expected = { identity: await digestOf(row.canonical), version: row.version }
  const results = [
    `{"structuredContent":${row.descriptor}}`,
    JSON.stringify({ content: [{ type: 'text', text: row.descriptor }] })
  ]
  for (const result of results) {
    const call = reply(result)
    expect(await runtimeIdentityOf({}, call)).toEqual(expected)
    expect(call).toHaveBeenCalledTimes(1)
    expect(call).toHaveBeenCalledWith('describe_runtime', {})
  }
})

it.each(vectors.payloadCases)('matches shared Runtime payload selection vector $id', async row => {
  const call = reply(row.result)
  expect(await runtimeCheckIdentity(call)).toBe(row.canonical)
  expect(await runtimeIdentityOf({}, call)).toEqual({ identity: await digestOf(row.canonical), version: row.version })
})

it.each(vectors.disabledResults)('disables cross-turn Runtime cache for shared refusal vector $id', async row => {
  expect(await runtimeIdentityOf({}, reply(row.result))).toBeUndefined()
})

// JSON.parse preserves these original UTF-16 units. Go must carry the same
// malformed identity to Agent v0.13 admission rather than repair it to U+FFFD.
it.each(vectors.malformedVersions)('preserves malformed Runtime version for Agent admission $id', async row => {
  const call = reply(row.result)
  expect(await runtimeCheckIdentity(call)).toBe(row.canonical)
  const identity = await runtimeIdentityOf({}, call)
  expect(identity?.identity).toBe(await digestOf(row.canonical))
  const version = identity!.version
  expect(Array.from({ length: version.length }, (_, i) => version.charCodeAt(i))).toEqual(row.versionUnits)
})

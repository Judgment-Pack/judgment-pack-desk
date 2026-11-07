import { expect, it } from 'vitest'
import { INITIAL_STATE } from './run'
import { checkpoint, decodeCheckpoint } from '../chat/checkpoint'
import { runtimeProbes, readProbes } from './runtimeProbes'
import { digestOf } from './checkCandidate'
const doc = { specVersion: '0.2.0-draft', id: 'https://example.org/p', title: 'Recovered pack', sources: [{ id: 'policy', title: 'Policy', locator: { kind: 'uri', value: 'https://example.org/policy' } }] }
const at = '2026-09-24T12:00:00Z'
it('retains only actual rehearsals tied to their runtime call and document', async () => {
  const result = { status: 'evaluated', rehearsal: true, disposition: { kind: 'outcome', outcomeId: 'yes' } }
  const events = [
    { type: 'tool_call' as const, callId: 'a', name: 'experimental_evaluate', args: { pack: JSON.stringify(doc), facts: '{"score":"3"}', rehearsal: true } },
    { type: 'tool_result' as const, callId: 'wrong', name: 'experimental_evaluate', text: JSON.stringify(result), isError: false },
    { type: 'message' as const, text: JSON.stringify(result) },
    { type: 'tool_result' as const, callId: 'a', name: 'experimental_evaluate', text: '', structured: result, isError: false }
  ]
  const probes = await runtimeProbes(events, at)
  expect(probes).toHaveLength(1)
  expect(probes[0]).toMatchObject({ documentDigest: await digestOf(JSON.stringify(doc, null, 2)), facts: { score: '3' }, disposition: result.disposition })
  const disk = decodeCheckpoint(checkpoint({ ...INITIAL_STATE, probes }, []))
  expect(disk.state.probes).toEqual(probes)
  expect(await runtimeProbes(events.map(event => event.type === 'tool_result' ? { ...event, isError: true } : event), at)).toEqual([])
  expect(() => readProbes([{ ...probes[0], facts: 'not an object' }])).toThrow()
})

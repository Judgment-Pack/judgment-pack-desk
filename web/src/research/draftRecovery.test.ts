import { expect, it, vi } from 'vitest'
import { AuthoringRun, INITIAL_STATE, type RunPorts } from './run'
import { Ledger } from './ledger'
import { checkpoint, decodeCheckpoint } from '../chat/checkpoint'
import { runtimeProbes, readProbes } from './runtimeProbes'
import { digestOf } from './checkCandidate'
import { fromChat } from '../packs/drafts/model'
import type { Chat } from '../chat/store'
const doc = { specVersion: '0.2.0-draft', id: 'https://example.org/p', title: 'Recovered pack', sources: [{ id: 'policy', title: 'Policy', locator: { kind: 'uri', value: 'https://example.org/policy' } }] }
const reply = '1. Probe\n     ```json\n     {"status":"evaluated"}\n     ```\n```json\n' + JSON.stringify({ proposal: { kind: 'create', document: doc, unknowns: ['Missing approval'] } }) + '\n```'
const at = '2026-09-24T12:00:00Z'
function ports(): RunPorts {
  return { mode: 'draft', ledger: new Ledger('s'), researchTools: [], authorPrompt: 'contract', maxRevisions: 0, seconds: 30, gateway: null,
    turn: vi.fn(async () => { throw new Error('No model should be called') }), callTool: vi.fn(async () => ({ structuredContent: { status: 'valid', diagnostics: [] } })),
    seal: vi.fn(), registry: vi.fn(), newSession: () => 's', log: vi.fn() }
}
it('recovers the saved response, retains sources, validates, and reloads results without trusting them as fresh', async () => {
  const p = ports(), run = new AuthoringRun(p)
  const saved = { ...INITIAL_STATE, phase: 'conversation' as const, status: 'complete' as const, turns: [{ role: 'assistant' as const, kind: 'message' as const, text: reply, at }] }
  await run.restore(saved)
  run.recoverDraft('```json\n{"proposal":{"document":{}}}\n```')
  expect(run.getSnapshot().candidates).toHaveLength(0)
  run.recoverDraft(reply)
  await vi.waitFor(() => expect(run.getSnapshot().status).toBe('ready'))
  expect(p.turn).not.toHaveBeenCalled()
  expect(p.callTool).toHaveBeenCalledExactlyOnceWith('validate', { document: JSON.stringify(doc, null, 2) })
  const current = run.getSnapshot(), disk = decodeCheckpoint(checkpoint(current, []))
  expect(current.turns[0]?.text).toBe(reply)
  const artifact = fromChat({ id: 'chat', title: 'Chat', mode: 'draft', createdAt: at, updatedAt: at } as Chat, disk)
  expect(artifact.checkpoint.state.candidates[0]?.document).toEqual(doc)
  expect(artifact.checkpoint.state.turns).toEqual([])
  expect(disk.state.candidates[0]?.check).toBeUndefined()
  expect(disk.state.candidates[0]?.previousCheck?.valid).toBe(true)
  const reopened = new AuthoringRun(ports())
  await reopened.restore(disk.state)
  expect(reopened.getSnapshot()).toMatchObject({ status: 'needs-input', restored: true })
  expect(reopened.getSnapshot().candidates[0]?.check).toBeUndefined()
  expect(reopened.getSnapshot().candidates[0]?.previousCheck?.valid).toBe(true)
  run.recoverDraft(reply)
  expect(run.getSnapshot().candidates).toHaveLength(1)
  const changed = structuredClone(disk.state); changed.candidates[0]!.text = JSON.stringify({ ...doc, title: 'Changed' })
  const stale = new AuthoringRun(ports()); await stale.restore(changed)
  expect(stale.getSnapshot().candidates[0]?.previousCheck).toBeUndefined()
})
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

import { webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { documentDigest, newAuthoringRun, runAuthoring, type AuthoringCase, type AuthoringCheckpoint, type AuthoringPorts } from './run'
import { checkCandidate } from './checkCandidate'
import { engineTurn } from './engineTurn'
import type { AssistantSession, Engine } from '../engine'
import { normalize } from '../thinking'

const testCase = { id: 'policy', facts: {}, expectedDisposition: { outcomeId: 'approve' }, expectationSource: 'Policy §1' }
const initial = () => newAuthoringRun('test', 'baseline', 'Apply the policy', [testCase])
beforeEach(() => vi.stubGlobal('crypto', webcrypto))
afterEach(() => vi.unstubAllGlobals())
function fixture() {
  let saved = initial()
  const propose = vi.fn(async (state: AuthoringCheckpoint) => ({ type: 'candidate' as const,
    document: JSON.stringify({ attempt: state.revisions.length + 1 }), summary: 'Repair the known defect' }))
  const check = vi.fn(async (document: string, cases: AuthoringCase[]) => ({
    documentDigest: await documentDigest(document), runtimeIdentity: 'runtime-1', valid: true, diagnostics: [],
    cases: cases.map(row => ({ id: row.id, expected: row.expectedDisposition, actual: {}, passed: JSON.parse(document).attempt >= 3 }))
  }))
  const ports: AuthoringPorts = { propose, check, review: async () => ({ cases: [], questions: [], summary: 'No further cases in this rubric.' }),
    save: async state => { saved = structuredClone(state) } }
  const options = { baseline: 'baseline', runtimeIdentity: 'runtime-1', maxRevisions: 6, signal: new AbortController().signal }
  return { ports, propose, check, options, saved: () => saved }
}

describe('headless authoring controller', () => {
  it('repairs repeatedly, retaining failures and immutable expectations', async () => {
    const f = fixture()
    const result = await runAuthoring(initial(), f.ports, f.options)
    expect(result.status).toBe('ready')
    expect(result.revisions.map(row => row.check?.cases[0].passed)).toEqual([false, false, true])
    expect(result.cases).toEqual([testCase])
    expect(f.propose.mock.calls[2][0].revisions[1].check?.cases[0].passed).toBe(false)
  })

  it('resumes the saved check without asking the model to recreate a candidate', async () => {
    const f = fixture()
    const stop = new AbortController()
    const persist = f.ports.save
    f.ports.save = async state => { await persist(state); if (state.stage === 'check') stop.abort() }
    expect((await runAuthoring(initial(), f.ports, { ...f.options, signal: stop.signal })).status).toBe('interrupted')
    f.ports.save = persist
    const restored = JSON.parse(JSON.stringify(f.saved())) as AuthoringCheckpoint
    expect((await runAuthoring(restored, f.ports, f.options)).status).toBe('ready')
    expect(f.propose).toHaveBeenCalledTimes(3)
    expect(f.check).toHaveBeenCalledTimes(3)
  })

  it('does not install a late model result after cancellation', async () => {
    const f = fixture()
    const stop = new AbortController()
    let resolve!: (value: { type: 'candidate'; document: string; summary: string }) => void
    f.ports.propose = () => new Promise(done => { resolve = done; stop.abort() })
    const result = await runAuthoring(initial(), f.ports, { ...f.options, signal: stop.signal })
    resolve({ type: 'candidate', document: '{}', summary: 'Late' })
    await Promise.resolve()
    expect(result.status).toBe('interrupted')
    expect(f.saved().revisions).toHaveLength(0)
    expect(f.check).not.toHaveBeenCalled()
  })

  it('refuses stale checks and changed pack baselines', async () => {
    const f = fixture()
    expect((await runAuthoring(initial(), f.ports, { ...f.options, baseline: 'changed' })).status).toBe('stale')
    expect(f.propose).not.toHaveBeenCalled()
    f.ports.check = async () => ({ documentDigest: 'wrong', runtimeIdentity: 'runtime-1', valid: true, diagnostics: [], cases: [] })
    const result = await runAuthoring(initial(), f.ports, f.options)
    expect(result.status).toBe('failed')
    expect(result.detail).toMatch(/do not belong/)
  })

  it('bounds revisions and detects oscillating candidates', async () => {
    const f = fixture()
    expect((await runAuthoring(initial(), f.ports, { ...f.options, maxRevisions: 2 })).status).toBe('budget')
    f.ports.propose = async state => ({ type: 'candidate', document: JSON.stringify({ attempt: state.revisions.length % 2 }), summary: 'Same fixes again' })
    expect((await runAuthoring(initial(), f.ports, f.options)).status).toBe('stalled')
  })

  it('prevents a reviewer from changing the test expectations', async () => {
    const f = fixture()
    f.ports.review = async () => ({ cases: [{ ...testCase, expectedDisposition: { outcomeId: 'decline' } }], questions: [], summary: 'Change the answer' })
    const result = await runAuthoring(initial(), f.ports, f.options)
    expect(result.status).toBe('failed')
    expect(result.cases).toEqual([testCase])
  })

  it('isolates engine state and stops for a policy question', async () => {
    const f = fixture()
    f.ports.propose = async context => {
      context.cases[0].expectedDisposition = 'tampered'
      return { type: 'question', text: 'Which policy version applies?' }
    }
    const result = await runAuthoring(initial(), f.ports, f.options)
    expect(result.status).toBe('needs-input')
    expect(result.cases).toEqual([testCase])
    expect(f.check).not.toHaveBeenCalled()
  })

  it('never claims recovery when checkpoint storage fails', async () => {
    const f = fixture()
    f.ports.save = async () => { throw new Error('Storage full') }
    await expect(runAuthoring(initial(), f.ports, f.options)).rejects.toThrow('Storage full')
    expect(f.propose).not.toHaveBeenCalled()
  })

  it('rechecks a completed candidate when the runtime changes', async () => {
    const f = fixture()
    const ready = await runAuthoring(initial(), f.ports, f.options)
    const oldCheck = f.ports.check
    f.ports.check = async (...args) => ({ ...await oldCheck(...args), runtimeIdentity: 'runtime-2' })
    const next = await runAuthoring(ready, f.ports, { ...f.options, runtimeIdentity: 'runtime-2' })
    expect(next.status).toBe('ready')
    expect(next.revisions.at(-1)?.check?.runtimeIdentity).toBe('runtime-2')
    expect(f.propose).toHaveBeenCalledTimes(3)
  })

  it('bounds a reviewer that keeps adding passing cases', async () => {
    const f = fixture()
    f.ports.review = async state => ({ cases: [{ ...testCase, id: `extra-${state.cases.length}` }], questions: [], summary: 'Another case' })
    expect((await runAuthoring(initial(), f.ports, f.options)).status).toBe('budget')
  })
})

describe('candidate-bound inline checks', () => {
  it('compares expected and actual outcomes instead of trusting evaluated status', async () => {
    const call = vi.fn(async (name: string) => ({ structuredContent: name === 'validate' ? { status: 'valid' } :
      { status: 'evaluated', rehearsal: true, disposition: { outcomeId: 'decline' } } }))
    const result = await checkCandidate('{}', [testCase], call, 'runtime-1', new AbortController().signal)
    expect(result.cases[0].passed).toBe(false)
    expect(call.mock.calls[0]).toEqual(['validate', { document: '{}' }])
  })

  it('retains invalid-document diagnostics but refuses failed or non-rehearsal evaluations', async () => {
    const signal = new AbortController().signal
    const invalid = await checkCandidate('{}', [testCase], async () => ({ isError: true,
      structuredContent: { status: 'invalid', diagnostics: [{ code: 'missing-question' }] } }), 'runtime-1', signal)
    expect(invalid.valid).toBe(false)
    expect(invalid.diagnostics).toHaveLength(1)
    const call = async (name: string) => ({ structuredContent: name === 'validate' ? { status: 'valid' } :
      { status: 'evaluated', disposition: testCase.expectedDisposition } })
    await expect(checkCandidate('{}', [testCase], call, 'runtime-1', signal)).rejects.toThrow('No completed rehearsal')
  })
})

describe('existing engine bridge', () => {
  const session: Omit<AssistantSession, 'signal'> = { prompt: 'Runtime authoring method', testPrompt: '', tools: [],
    callTool: async () => ({}), model: { family: 'openai-compatible', model: 'fixture', call: async () => Response.json({}) },
    thinking: normalize('off', 'openai-compatible') }

  it('refuses a proposal followed by a stream failure', async () => {
    const engine: Engine = { id: 'vercel', async *start() {
      yield { type: 'proposal', document: {}, unknowns: [] }
      yield { type: 'error', message: 'Connection lost before completion' }
    } }
    await expect(engineTurn(engine, session)(initial(), new AbortController().signal)).rejects.toThrow('Connection lost')
  })

  it('stops even when iterator cleanup is queued behind an unfinished read', async () => {
    const stop = new AbortController()
    const cleanup = vi.fn(() => new Promise<never>(() => {}))
    const engine: Engine = { id: 'vercel', start: () => ({ [Symbol.asyncIterator]: () => ({
      next: () => { stop.abort(); return new Promise<never>(() => {}) }, return: cleanup
    }) }) }
    await expect(engineTurn(engine, session)(initial(), stop.signal)).rejects.toMatchObject({ name: 'RunCancelled' })
    expect(cleanup).toHaveBeenCalledOnce()
  })
})

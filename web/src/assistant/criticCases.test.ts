import { describe, expect, it } from 'vitest'
import { readCriticCases } from '@judgment-pack/harness/refutation'
import { emptySuite, newCase, type TestCase } from '../packs/test-workspace/model'
import { criticCases, declaredCases, MAX_CRITIC_CASES, savedTestCases } from './criticCases'

const WHOLE = { kind: 'decline', reasons: ['no-receipt'], handoff: null }
const test = (id: string, row: Record<string, unknown>, more: Partial<TestCase> = {}): TestCase => ({ ...newCase(), id, name: id, row: { id, facts: { claim: 60 }, ...row }, ...more })

describe('the saved tests a critic compares', () => {
  it('declares each test that states an expectation, as a matrix row with its source', () => {
    const suite = { ...emptySuite(), cases: [
      test('c1', { expectedDisposition: WHOLE, evidenceAvailability: { receipt: false } }, { rationale: 'No receipt above 50.' }),
      test('c2', { expectedErrorClass: 'missing-fact', expectedErrorPhase: 'evaluation' }),
      test('exploratory', {})
    ] }
    const declared = savedTestCases(suite)
    expect(declared.map((item) => [item.id, item.source])).toEqual([['c1', 'test'], ['c2', 'test']])
    expect(declared[0]).toMatchObject({ facts: { claim: 60 }, evidenceAvailability: { receipt: false }, expectedDisposition: WHOLE, focus: 'No receipt above 50.' })
    // The harness reads what the Desk declares, whole.
    expect(() => readCriticCases(declared)).not.toThrow()
  })

  it('leaves out a deleted test, and one the harness would refuse, instead of ending every critic pass on the pack', () => {
    const suite = { ...emptySuite(), deleted: ['gone'], cases: [
      test('gone', { expectedDisposition: WHOLE }),
      test('not-facts', { facts: 'a string', expectedDisposition: WHOLE }),
      test('no-kind', { expectedDisposition: { reasons: [] } }),
      test('fine', { expectedDisposition: WHOLE })
    ] }
    const declared = savedTestCases(suite)
    expect(declared.map((item) => item.id)).toEqual(['fine'])
    expect(() => readCriticCases(declared)).not.toThrow()
  })

  it('sends no member a declared case does not have', () => {
    const suite = { ...emptySuite(), cases: [test('c1', { expectedDisposition: WHOLE, note: 'a member of the Desk\'s own', revision: 4 })] }
    const [held] = savedTestCases(suite)
    expect(Object.keys(held!).every((key) => ['source', 'id', 'facts', 'expectedDisposition', 'origin', 'focus'].includes(key))).toBe(true)
    expect(held).not.toHaveProperty('revision')
    expect(held).not.toHaveProperty('note')
  })
})

describe('the cases a conversation declares', () => {
  it('declares an established case with its expectation, as source declared', () => {
    const [item] = declaredCases([{ id: 'a1', facts: { claim: 20 }, expectedDisposition: { kind: 'approve', reasons: [], handoff: null }, expectationSource: 'excerpt-1', rationale: 'Small claims pass.' }])
    expect(item).toMatchObject({ source: 'declared', id: 'a1', facts: { claim: 20 }, focus: 'Small claims pass.' })
    expect(item).not.toHaveProperty('expectationSource')
    expect(() => readCriticCases([item])).not.toThrow()
  })
})

describe('one list for a run', () => {
  it('lists the saved tests first, an id once, and no more than the harness reads', () => {
    const saved = savedTestCases({ ...emptySuite(), cases: [test('same', { expectedDisposition: WHOLE })] })
    const declared = declaredCases([{ id: 'same', facts: {}, expectedDisposition: { kind: 'approve' }, expectationSource: 'x', rationale: '' }, { id: 'other', facts: {}, expectedDisposition: { kind: 'approve' }, expectationSource: 'x', rationale: '' }])
    const all = criticCases(saved, declared)
    expect(all.map((item) => [item.id, item.source])).toEqual([['same', 'test'], ['other', 'declared']])
    const many = Array.from({ length: MAX_CRITIC_CASES + 40 }, (_, at) => ({ id: `n${at}`, facts: {}, expectedDisposition: { kind: 'approve' }, expectationSource: 'x', rationale: '' }))
    expect(criticCases(declaredCases(many))).toHaveLength(MAX_CRITIC_CASES)
    expect(() => readCriticCases(criticCases(declaredCases(many)))).not.toThrow()
  })
})

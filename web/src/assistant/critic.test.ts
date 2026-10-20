import { expect, it } from 'vitest'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { criticRecord, validCriticFigures, validCriticRecord } from './critic'

type Critique = Extract<AssistantEvent, { type: 'critique' }>
const figures = { model: 'critic-model', unit: 'model-requests' as const, requests: 3, inputTokens: 900, outputTokens: 120 }
const critique = (more: Partial<Critique>): Critique => ({ type: 'critique', refuted: false, checks: [], text: 'checked', ...more })
const comparison = (agrees: boolean) => ({ case: 'c', source: 'test' as const, tool: 'experimental_test_cases' as const, agrees, expected: { disposition: { kind: 'approve' } }, actual: { disposition: { kind: agrees ? 'approve' : 'decline' } } })

it('counts the comparisons, names the cases nothing compared, and keeps the critic\'s figures', () => {
  const record = criticRecord(critique({ cases: [comparison(true), comparison(false), comparison(true)], incomplete: ['c4', 'c5'], figures }), 'run-model')
  expect(record).toEqual({ compared: 3, disagreed: 1, incomplete: ['c4', 'c5'], figures, runModel: 'run-model' })
  expect(validCriticRecord(record)).toBe(true)
})

it('has no record for a critique that declared nothing and cost nothing it reported', () => {
  expect(criticRecord(critique({}))).toBeUndefined()
})

it('reads a saved record closed', () => {
  const good = { compared: 2, disagreed: 1, incomplete: ['c3'], figures }
  expect(validCriticRecord(good)).toBe(true)
  for (const bad of [{ ...good, extra: 1 }, { ...good, disagreed: 3 }, { ...good, incomplete: [1] }, { ...good, figures: { ...figures, unit: 'tokens' } }, { ...good, figures: { ...figures, requests: -1 } }, { ...good, runModel: 5 }, null, []]) {
    expect(validCriticRecord(bad), JSON.stringify(bad)).toBe(false)
  }
  expect(validCriticFigures({ ...figures, harnessCalls: 2 })).toBe(true)
})

import { describe, it, expect } from 'vitest'
import {
  emptySuite,
  newCase,
  executable,
  importMatrix,
  matrix,
  latestResult,
  recoverDraft,
  recoverResearchRecord,
  pointerGet,
  pointerSet,
  decodeStore,
  factFields,
} from './model'
import { digestOf } from '../../research/checkCandidate'
import type { PackDraft } from '../drafts/model'
const expected = { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } }
const at = '2026-09-24T12:00:00Z'
async function fixture() {
  const text = JSON.stringify({ id: 'policy', version: '0.1.0' }),
    digest = await digestOf(text)
  const draft = {
    id: 'draft-one',
    generation: 1,
    updatedAt: at,
    documents: [],
    checkpoint: {
      sources: [],
      state: {
        unknowns: [],
        cases: [],
        candidates: [{ text, digest, document: JSON.parse(text) }],
        probes: Array.from({ length: 8 }, (_, i) => ({
          documentDigest: digest,
          at,
          facts: { index: i },
          disposition: expected,
        })),
      },
    },
  } as unknown as PackDraft
  return { draft, digest, text }
}
describe('test history recovery and identity', () => {
  it('recovers eight trials once without inventing an expected result', async () => {
    const { draft } = await fixture()
    const recovered = await recoverDraft(emptySuite(), draft)
    expect(recovered.runs).toHaveLength(8)
    expect(recovered.cases).toEqual([])
    draft.checkpoint.state.candidates[0]!.digest = ''
    expect(
      (await recoverDraft(emptySuite(), draft)).runs.every(
        (r) => r.packText === draft.checkpoint.state.candidates[0]!.text,
      ),
    ).toBe(true)
    expect(recovered.runs.every((r) => r.trial && !r.report && !r.cases.length)).toBe(true)
    expect(await recoverDraft(recovered, structuredClone(draft))).toEqual(recovered)
    draft.checkpoint.state.probes!.push({ ...draft.checkpoint.state.probes![0]!, facts: { index: 8 } })
    const next = await recoverDraft(recovered, draft)
    expect(next.runs).toHaveLength(9)
  })
  it('keeps a recorded check historical when the old input snapshot is not available', async () => {
    const { draft, digest } = await fixture()
    draft.checkpoint.state.cases = [
      {
        id: 'c',
        facts: { edited: true },
        expectedDisposition: expected,
        expectationSource: 's',
        rationale: 'Independent requirement',
      },
    ]
    draft.checkpoint.state.candidates[0]!.previousCheck = {
      documentDigest: digest,
      valid: true,
      diagnostics: [],
      cases: [
        { id: 'c', passed: true, expected: { disposition: expected }, actual: { disposition: expected } },
      ],
    }
    const recovered = await recoverDraft(emptySuite(), draft)
    expect(recovered.cases).toHaveLength(1)
    expect(recovered.runs.at(-1)?.report?.status).toBe('passed')
    expect(recovered.runs.at(-1)?.cases).toEqual([])
    expect(latestResult(recovered, recovered.cases[0]!, digest).label).toBe('Not run')
  })
  it('invalidates results after input, expectation, or pack revision changes', async () => {
    const { digest, text } = await fixture(),
      c = newCase()
    c.name = 'Case'
    c.revision = 1
    c.row.expectedDisposition = expected
    const suite = emptySuite()
    suite.cases = [c]
    suite.runs = [
      {
        id: 'run',
        at,
        packDigest: digest,
        packText: text,
        packVersion: '0.1.0',
        origin: 'tests',
        cases: [structuredClone(c)],
        report: {
          status: 'passed',
          summary: { total: 1, passed: 1, mismatched: 0 },
          packs: [
            {
              id: 'policy',
              status: 'passed',
              summary: { total: 1, passed: 1, mismatched: 0 },
              rows: [
                {
                  id: c.id,
                  status: 'passed',
                  expected: JSON.stringify(expected),
                  actual: JSON.stringify(expected),
                },
              ],
            },
          ],
        },
      },
    ]
    expect(latestResult(suite, c, digest).label).toBe('Passed')
    expect(latestResult(suite, { ...c, row: { ...c.row, facts: { changed: true } } }, digest).label).toBe(
      'Needs rerun',
    )
    expect(latestResult(suite, c, 'other-digest').label).toBe('Needs rerun')
    suite.runs[0]!.trial = { at, documentDigest: digest, facts: {}, disposition: expected }
    delete suite.runs[0]!.report
    expect(latestResult(suite, c, digest).label).toBe('Exploratory')
  })
})
it('round trips matrix assertions without exporting Desk source metadata or exploratory cases', () => {
  const cases = importMatrix({
    matrixVersion: '3',
    cases: [
      {
        id: 'refusal',
        facts: null,
        expectedErrorClass: 'malformed-input',
        expectedErrorPhase: 'input',
        focus: 'Refuses bad input',
      },
      {
        id: 'accept',
        facts: { ok: false, count: 0, list: [1, 2] },
        expectedDisposition: expected,
        expectedHandoffTarget: null,
        supportedExtensions: [],
      },
    ],
  })
  cases[0]!.sources = [{ id: 'source', name: 'Requirement', text: 'Reference' }]
  const exported = matrix([...cases, newCase()])
  expect(exported.cases).toHaveLength(2)
  expect(exported.cases[1]!.expectedHandoffTarget).toBeNull()
  expect(exported.cases[0]).not.toHaveProperty('sources')
  expect(importMatrix(exported)[1]!.row.facts).toEqual({ ok: false, count: 0, list: [1, 2] })
  expect(executable(newCase())).toBe(false)
  expect(() =>
    importMatrix({ cases: [{ id: 'x', facts: {}, expectedDisposition: expected, expectedErrorClass: 'x' }] }),
  ).toThrow()
  expect(() =>
    importMatrix({
      cases: [
        { id: 'x', facts: {}, expectedDisposition: expected },
        { id: 'x', facts: {}, expectedDisposition: expected },
      ],
    }),
  ).toThrow()
})
it('preserves false, zero, null, arrays and omitted values in manual facts', () => {
  let facts: unknown = { items: [{ enabled: false }], count: 0, nullable: null }
  facts = pointerSet(facts, '/items/0/enabled', true)
  expect(pointerGet(facts, '/items/0/enabled')).toBe(true)
  facts = pointerSet(facts, '/count', undefined)
  expect(facts).toEqual({ items: [{ enabled: true }], nullable: null })
  expect(() => pointerSet(facts, '/items/0', undefined)).toThrow('array elements')
  expect(() => pointerSet({}, '/__proto__/polluted', true)).toThrow()
})
it('refuses malformed local storage instead of replacing it', () => {
  expect(() =>
    decodeStore({ version: 1, suites: { x: { revision: 1, cases: [{}], runs: [], recovered: [] } } }),
  ).toThrow()
  expect(decodeStore({ version: 1, suites: { x: emptySuite() } }).suites.x).toEqual(emptySuite())
})

it('infers membership operands as scalar types and keeps mixed inputs in JSON', () => {
  expect(
    factFields([
      { op: 'fact', path: '/kind', operator: 'in', value: ['a', 'b'] },
      { op: 'fact', path: '/kind', operator: 'equals', value: 'a' },
      { op: 'fact', path: '/mixed', operator: 'in', value: [true, 1] },
    ]).map((f) => [f.path, f.type]),
  ).toEqual([
    ['/kind', 'string'],
    ['/mixed', 'json'],
  ])
})

it('retains a research handover report only under its original candidate digest', async () => {
  const { text, digest } = await fixture()
  const record = {
    researchRecordVersion: '1',
    recordedAt: at,
    testHistory: [
      {
        text,
        check: {
          documentDigest: digest,
          valid: true,
          diagnostics: [],
          cases: [
            { id: 'r', passed: true, expected: { disposition: expected }, actual: { disposition: expected } },
          ],
        },
      },
    ],
  }
  const recovered = await recoverResearchRecord(emptySuite(), record, 'companion')
  expect(recovered.runs).toHaveLength(1)
  expect(recovered.runs[0]?.packText).toBe(text)
  expect(recovered.runs[0]?.cases).toEqual([])
  expect(await recoverResearchRecord(recovered, record, 'companion')).toEqual(recovered)
  const bad = structuredClone(record)
  bad.testHistory[0]!.check.documentDigest = 'other'
  expect((await recoverResearchRecord(emptySuite(), bad, 'companion')).runs).toEqual([])
})

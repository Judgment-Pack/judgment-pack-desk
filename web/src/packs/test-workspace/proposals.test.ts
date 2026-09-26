import { expect, it, vi } from 'vitest'
import {
  preserveTestMeaning,
  validateTestProposal,
  proposalCases,
  saveReviewedCases,
  proposalForMessage,
  type TestProposal,
} from './proposals'
import { decodeStore, emptySuite } from './model'
const expected = { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } }
const document = {
  matrixVersion: '3',
  cases: [{ id: 'normal', facts: { ok: false, count: 0, nullable: null }, expectedDisposition: expected }],
}
const call = (report: Record<string, unknown>) =>
  vi.fn(async () => ({
    structuredContent: { contractVersion: '1', specVersion: '0.2.0-draft', ...report },
    content: [],
  }))
const valid = {
  status: 'valid',
  findings: [],
  results: [{ index: 0, id: 'normal', status: 'valid', findings: [] }],
}
it('only removes Desk root metadata; unsupported matrix fields still reach Runtime', async () => {
  const port = call(valid)
  const sources = [{ id: 's', name: 'Policy', text: 'false means no' }]
  const value = { ...document, sourceMappings: { normal: { '/ok': 's' } } }
  expect(await validateTestProposal(value, sources, port, new AbortController().signal)).toEqual([])
  expect(JSON.parse((port.mock.calls as unknown as [string, { matrix: string }][])[0]![1].matrix)).toEqual(
    document,
  )
})
it('refuses reports that omit a case or claim valid with invalid findings', async () => {
  for (const report of [
    { ...valid, results: [] },
    { ...valid, results: [{ index: 1, status: 'valid', findings: [] }] },
    { ...valid, findings: [{ code: 'ERR', path: '', message: 'bad' }] },
  ])
    await expect(
      validateTestProposal(document, [], call(report), new AbortController().signal),
    ).rejects.toThrow()
})
it('keeps unknown source associations visible as errors, never silently filters them out', async () => {
  const report = await validateTestProposal(
    { ...document, sourceMappings: { normal: { '/ok': 'invented' } } },
    [],
    call(valid),
    new AbortController().signal,
  )
  expect(report[0]?.code).toBe('SOURCE-MAPPING')
})
it('allows metadata repair but blocks dropped cases, altered facts and changed expectations', () => {
  expect(
    preserveTestMeaning(
      { ...document, cases: [{ ...document.cases[0], name: 'Name' }] },
      { ...document, cases: [{ ...document.cases[0], focus: 'Name' }] },
    ),
  ).toEqual([])
  for (const change of [
    { ...document, cases: [] },
    { ...document, cases: [{ ...document.cases[0], id: 'another' }] },
    { ...document, cases: [{ ...document.cases[0], facts: { ok: true } }] },
    {
      ...document,
      cases: [{ ...document.cases[0], expectedDisposition: { ...expected, outcomeId: 'reject' } }],
    },
    { ...document, sourceMappings: { normal: { '/ok': 'new' } } },
  ])
    expect(preserveTestMeaning(document, change)[0]?.code).toBe('CORRECTION-MEANING')
})
it('allows adding required disposition members without altering existing authored values', () => {
  const original = {
    ...document,
    cases: [{ ...document.cases[0], expectedDisposition: { kind: 'outcome', outcomeId: 'accept' } }],
  }
  expect(preserveTestMeaning(original, document)).toEqual([])
})
it('round trips failed proposals and reopens valid proposals as unsaved cases', () => {
  const record: TestProposal = {
    id: 'p',
    at: new Date().toISOString(),
    request: 'Design',
    model: 'fixture',
    contractVersion: '1',
    packDigest: 'digest',
    sources: [],
    caseSnapshots: {},
    state: 'ready',
    message: 'Ready for review',
    attempts: [
      {
        document: { ...document, cases: [{ ...document.cases[0], name: 'Unsupported' }] },
        unknowns: [],
        message: 'Draft',
        findings: [{ code: 'MATRIX-MEMBER', path: '/cases/0/name', message: 'Unsupported' }],
      },
      { document, unknowns: [], message: 'Corrected', findings: [] },
    ],
  }
  const store = decodeStore(
    JSON.parse(JSON.stringify({ version: 1, suites: { pack: { ...emptySuite(), proposals: [record] } } })),
  )
  expect(store.suites.pack?.cases).toHaveLength(0)
  expect(store.suites.pack?.proposals?.[0]?.attempts).toHaveLength(2)
  expect(proposalCases(store.suites.pack!.proposals![0]!)[0]?.row.facts).toEqual(document.cases[0]!.facts)
  expect(proposalCases({ ...record, state: 'blocked' })).toEqual([])
})

function reviewFixture() {
  const proposal: TestProposal = {
    id: 'review',
    at: '2026-09-24T12:00:00Z',
    request: 'Design',
    model: 'fixture',
    contractVersion: '1',
    packDigest: 'digest',
    sources: [],
    caseSnapshots: {},
    state: 'ready',
    message: 'Ready',
    attempts: [
      {
        document: { ...document, cases: [document.cases[0], { ...document.cases[0], id: 'boundary' }] },
        unknowns: [],
        message: 'Two cases.',
        findings: [],
      },
    ],
  }
  return { proposal, suite: { ...emptySuite(), proposals: [proposal] }, rows: proposalCases(proposal) }
}
it('saves only selected reviewed cases, records durable receipts and does not run them', () => {
  const { proposal, suite, rows } = reviewFixture()
  const next = saveReviewedCases(suite, proposal, [rows[0]!], 'digest')
  expect(next.cases.map((c) => c.id)).toEqual(['normal'])
  expect(next.cases[0]?.revision).toBe(1)
  expect(next.runs).toEqual([])
  expect(suite.cases).toEqual([])
  const restored = decodeStore(JSON.parse(JSON.stringify({ version: 1, suites: { pack: next } }))).suites
    .pack!
  expect(Object.keys(restored.proposals![0]!.savedCases!)).toEqual(['normal'])
  const completed = saveReviewedCases(restored, restored.proposals![0]!, [rows[1]!], 'digest')
  expect(completed.cases).toHaveLength(2)
  expect(() => saveReviewedCases(completed, proposal, [rows[0]!], 'digest')).toThrow('already saved')
})
it('refuses the entire selection when a case changed concurrently or the pack revision changed', () => {
  const { proposal, suite, rows } = reviewFixture()
  suite.cases.push({ ...rows[1]!, revision: 1, name: 'User changes' })
  expect(() => saveReviewedCases(suite, proposal, rows, 'digest')).toThrow('changed after')
  expect(suite.cases.map((c) => c.id)).toEqual(['boundary'])
  expect(suite.proposals[0]?.savedCases).toBeUndefined()
  expect(() => saveReviewedCases(suite, proposal, [rows[0]!], 'new-digest')).toThrow('pack changed')
})
it('binds response references explicitly and recovers only unambiguous older associations', () => {
  const { proposal, suite } = reviewFixture()
  const messages = [
    { role: 'user' as const, at: proposal.at, text: proposal.request },
    { role: 'assistant' as const, at: 'later', text: 'Two cases.' },
    { role: 'assistant' as const, at: 'later-again', text: 'A follow-up.' },
  ]
  expect(proposalForMessage({ ...suite, messages }, 1)?.id).toBe(proposal.id)
  expect(proposalForMessage({ ...suite, messages }, 2)).toBeUndefined()
  expect(
    proposalForMessage({ ...suite, proposals: [proposal, { ...proposal, id: 'ambiguous' }], messages }, 1),
  ).toBeUndefined()
  expect(
    proposalForMessage({ ...suite, messages: [{ ...messages[2]!, proposalId: proposal.id }] }, 0)?.id,
  ).toBe(proposal.id)
})

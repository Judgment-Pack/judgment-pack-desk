/**
 * Admin → Decision safeguards → Decision record says, in every state, that Jobs runs are
 * not in the trail it reads (#213): a reader of that panel could otherwise
 * take it for the jobs' record. Jobs keeps its runs in the runner.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { DecisionRecord } from '../audit/DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

const SENTENCE = 'Jobs runs are recorded by the runner, not in this trail.'
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const report = { status: 'valid', lines: 1, bytes: 10, snapshotBetweenWrites: true, segments: [], segmentsTotal: 0, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0, establishes: [], doesNotEstablish: [],
  coverage: { legacyPrefix: 0, chained: 1, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked' }, signedRecords: 0, unsignedRecords: 0, checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 1, stamped: { status: 'not-checked' } } }

const states: [string, () => Promise<Response>, string | RegExp][] = [
  ['the runtime’s report', async () => json(200, { state: 'report', runtime: '0.26.0', report }), 'Every check the runtime made passed.'],
  ['an older runtime', async () => json(200, { state: 'older-runtime', runtime: '0.25.0', floor: '0.26.0' }), /writes an unchained trail/],
  ['a project that keeps no trail', async () => json(200, { state: 'no-trail' }), /This project keeps no trail/],
  ['the runtime’s refusal to check', async () => json(200, { state: 'unverified', runtime: '0.26.0', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'The project’s trail does not exist yet: no record has been written.' }] }), 'The runtime did not check the trail.'],
  ['Desk’s refusal', async () => json(409, { error: 'Desk does not check it here.', code: 'bad-request' }), /Desk does not check the decision record here/],
  ['an error', async () => json(500, { error: 'The decision record could not be checked.', code: 'internal' }), 'The decision record could not be checked.'],
  ['a check still running', () => new Promise<Response>(() => {}), 'Asking the runtime…']
]

it.each(states)('says Jobs runs are not in this trail with %s', async (_name, answer, shown) => {
  vi.mocked(deskFetch).mockImplementation(answer)
  render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
  expect(await screen.findByText(shown)).toBeTruthy()
  const heading = screen.getByRole('heading', { name: 'Decision record' })
  expect(heading.closest('section')!.textContent).toContain(SENTENCE)
})

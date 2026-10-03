/**
 * Downloading the trail from the decision-record panel (ADR-0010, section 2):
 * a button for each file the audit directory holds and for no other; the
 * route each asks; the bytes saved exactly as served, under the runtime's own
 * name; and a refusal said as one.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { testQueryClient } from '../testing/harness'
import { isAuditRecord, type AuditRecord, type AuditReport } from './client'
import { DecisionRecord } from './DecisionRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const report: AuditReport = { status: 'valid', lines: 1, bytes: 10, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 1, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked' }, signedRecords: 0, unsignedRecords: 0,
    checkpointed: { status: 'not-supplied' }, witnessed: 0, unwitnessed: 1, stamped: { status: 'not-checked' } },
  segments: [{ firstLine: 1, lastLine: 1 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0, findings: [], findingsTotal: 0,
  establishes: ['The chained lines are consistent with one another.'], doesNotEstablish: ['That the trail is complete.'] }
/** Bytes a re-encoding would change: CRLF, a byte that is not UTF-8, and no final newline. */
const stamps = new Uint8Array([0x7b, 0x7d, 0x0d, 0x0a, 0xff, 0xfe, 0x7b, 0x22, 0x26, 0x22, 0x7d])

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
let record: AuditRecord
let trail: (url: string) => Response
let asked: string[]
let saved: { blob: Blob; name: string }[]
beforeEach(() => {
  asked = []
  saved = []
  record = { state: 'report', runtime: '0.26.0', report, files: ['evaluations', 'stamps'] }
  trail = () => new Response(stamps, { status: 200, headers: { 'Content-Type': 'application/octet-stream' } })
  vi.mocked(deskFetch).mockImplementation(async url => {
    asked.push(String(url))
    if (String(url) === '/api/audit/verify') return json(200, record)
    if (String(url).startsWith('/api/audit/trail?')) return trail(String(url))
    return json(404, { error: 'not here' })
  })
  let pending: Blob | undefined
  URL.createObjectURL = (blob: Blob) => { pending = blob; return 'blob:trail' }
  URL.revokeObjectURL = () => undefined
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    if (pending) saved.push({ blob: pending, name: this.download })
  })
})
const createObjectURL = URL.createObjectURL, revokeObjectURL = URL.revokeObjectURL
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.restoreAllMocks(); URL.createObjectURL = createObjectURL; URL.revokeObjectURL = revokeObjectURL })

function show() {
  render(<QueryClientProvider client={testQueryClient()}><DecisionRecord /></QueryClientProvider>)
}

describe('downloading the trail', () => {
  it('offers a button for each file the audit directory holds, and no other', async () => {
    show()
    await screen.findByText('Every check the runtime made passed.')
    expect(screen.getByRole('region', { name: 'Download the trail' })).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /^Download / }).map(button => button.textContent)).toEqual(['Download evaluations.jsonl', 'Download stamps.jsonl'])
  })

  it('offers none where the audit directory holds none', async () => {
    record = { state: 'report', runtime: '0.26.0', report, files: [] }
    show()
    await screen.findByText('Every check the runtime made passed.')
    expect(screen.queryByRole('region', { name: 'Download the trail' })).toBeNull()
    expect(screen.queryByRole('button', { name: /^Download / })).toBeNull()
  })

  it('offers them beside the runtime’s refusal too', async () => {
    record = { state: 'unverified', runtime: '0.26.0', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'The trail could not be read.' }], files: ['signatures'] }
    show()
    expect((await screen.findByRole('button', { name: 'Download signatures.jsonl' }))).toBeTruthy()
  })

  it('saves exactly the bytes served, under the runtime’s own name', async () => {
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Download stamps.jsonl' }))
    expect(await screen.findByText('Saved stamps.jsonl.')).toBeTruthy()
    expect(asked).toContain('/api/audit/trail?file=stamps')
    expect(saved).toHaveLength(1)
    expect(saved[0]!.name).toBe('stamps.jsonl')
    expect(new Uint8Array(await saved[0]!.blob.arrayBuffer())).toEqual(stamps)
  })

  it('asks for the trail by its own name', async () => {
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Download evaluations.jsonl' }))
    await waitFor(() => expect(saved.map(item => item.name)).toEqual(['evaluations.jsonl']))
    expect(asked.filter(url => url.startsWith('/api/audit/trail'))).toEqual(['/api/audit/trail?file=evaluations'])
  })

  it('says a refusal, and saves nothing', async () => {
    trail = () => json(404, { error: 'There is no stamps.jsonl in this project’s audit directory, .desk-private/audit.', code: 'not-found' })
    show()
    fireEvent.click(await screen.findByRole('button', { name: 'Download stamps.jsonl' }))
    expect((await screen.findByRole('alert')).textContent).toBe('There is no stamps.jsonl in this project’s audit directory, .desk-private/audit.')
    expect(saved).toHaveLength(0)
    expect(screen.queryByText(/^Saved /)).toBeNull()
  })

  it('refuses an answer that names a file the download does not take', () => {
    expect(isAuditRecord({ state: 'report', report, files: ['jpack.json'] })).toBe(false)
    expect(isAuditRecord({ state: 'report', report, files: 'evaluations' })).toBe(false)
    expect(isAuditRecord({ state: 'report', report, files: ['evaluations', 'signatures', 'stamps'] })).toBe(true)
  })
})

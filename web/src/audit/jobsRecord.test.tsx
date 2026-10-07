/**
 * The Jobs record (ADR-0010, section 4, "A Jobs record panel"; issue #216)
 * and the chain of runs' rows in the hand-over (section 5): shown where this
 * desk has a Runner; run on request only, and again after a hand-over of the
 * chain is confirmed, never on a timer; the runtime's report, with its
 * sentences verbatim and the ADR's sentence; Runner's key in the Gates
 * paragraph's words, and where each run's signature is checked; no Runner,
 * Runner not running and an older runtime, each in plain words; a refusal
 * that is not JSON; the chain saved as a Blob. Beside each holder, the
 * chain's own row: its record, its download and its confirmation, apart
 * from the decision record's.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { followsTheProject } from '../mcp/projectChange'
import { testQueryClient } from '../testing/harness'
import { checkJobsRecordAgain, downloadCheckpoints, isHolders, isJobsChain, isJobsRecord, JOBS_REASONS, JOBS_RECORD_KEY, type AuditReport, type AuditRecord, type Holder, type Holders, type JobsRecord as JobsAnswer } from './client'
import { DecisionRecord } from './DecisionRecord'
import { JobsRecord } from './JobsRecord'

vi.mock(import('../files/client'), async original => ({ ...(await original()), deskFetch: vi.fn() }))

const STATEMENT = "Desk ran this over its own copy of the runner's chain of runs, with the checkpoints it keeps. It shows what a holder would see. It is not evidence to anyone who does not trust this installation."
const SIGNATURES = "Each run's signature is checked by jpack-runner verify-run on that run's export, not here."
const trail = '9a5ef41d74e7d7e003c8a34cff056351'
const chainId = 'abe5e3a494c36af588768ce5b0305497'
const moved = '68e77cefed3b0f20750d9b61ffceec38'
const digest = 'sha256:' + 'ab'.repeat(32)
const fileDigest = 'sha256:' + 'cd'.repeat(32)
const publicKey = '882a7f2be72a4b0c0a03b590300c72e8ed3fab24355a6950f9e6399814c67350'
const keyId = '4ba1de706a3baa4d8f5456340604190a'

const establishes = ['The chained lines are consistent with one another: no line before the last was edited, inserted, deleted or moved without breaking a link, and the lines before the first chained line are the block its previous commits to.',
  'Lines 1 to 2 are the lines that existed when the checkpoint was made, if the checkpoint was held independently of the trail\'s operator.']
const doesNotEstablish = ['Lines after 2, the checkpoint\'s sequence, are not authenticated by the chain: only a later checkpoint covering them, held independently of the operator, shows they are the ones first written.',
  'Who wrote any record: no public key was supplied, so no signature was checked.']
const witnessed: AuditReport = { status: 'invalid', lines: 3, bytes: 2854, snapshotBetweenWrites: true,
  coverage: { legacyPrefix: 0, chained: 3, unchained: 0, uncovered: 0, damaged: 0, signed: { status: 'not-checked', detail: 'no public key was supplied' }, signedRecords: 0, unsignedRecords: 0,
    checkpointed: { status: 'through', through: 2 }, witnessed: 2, unwitnessed: 1, stamped: { status: 'not-checked', detail: 'no time-stamping roots were supplied' } },
  segments: [{ firstLine: 1, lastLine: 3 }], segmentsTotal: 1, discontinuities: [], discontinuitiesTotal: 0,
  findings: [{ name: 'checkpoint-record-mismatch', line: 2, detail: 'the held checkpoint for sequence 2 names another record' }], findingsTotal: 1,
  establishes, doesNotEstablish }
const valid: AuditReport = { ...witnessed, status: 'valid', findings: [], findingsTotal: 0 }
const report = (more: Partial<Extract<JobsAnswer, { state: 'report' }>> = {}): JobsAnswer =>
  ({ state: 'report', runtime: '0.27.1', report: valid, chainLines: 3, runnerKey: { state: 'signed', publicKey, keyId }, expected: 1, ...more })

/** Bytes a re-encoding would change: CRLF, a byte that is not UTF-8, and no final newline. */
const served = new Uint8Array([0x7b, 0x7d, 0x0d, 0x0a, 0xff, 0xfe, 0x7b, 0x22, 0x26, 0x22, 0x7d])
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
/** A Jobs record whose held checkpoints reach through record `through`. */
const witnessedThrough = (through: number) => json(200, report({ report: { ...valid, coverage: { ...valid.coverage, checkpointed: { status: 'through', through }, witnessed: through, unwitnessed: 5 - through } } }))
/** A response the test lets go of when it chooses. */
function deferred() {
  let release!: (response: Response) => void
  const response = new Promise<Response>(resolve => { release = resolve })
  return { response, release }
}

let runner: unknown
let jobs: () => Response | Promise<Response>
let chain: () => Response
let record: AuditRecord
let holders: Holders[]
let download: () => Response
let confirm: () => Response
let asked: { url: string; method: string; body?: unknown }[]
let saved: { blob: Blob; name: string }[]
beforeEach(() => {
  asked = []
  saved = []
  runner = { state: 'signed', publicKey, keyId }
  jobs = () => json(200, report())
  chain = () => new Response(served, { status: 200, headers: { 'Content-Type': 'application/jsonl' } })
  record = { state: 'report', runtime: '0.27.1', report: valid }
  holders = [{ holders: [], trail: { identity: trail, sequence: 3 } }]
  download = () => json(404, { error: 'not here' })
  confirm = () => json(404, { error: 'not here' })
  vi.mocked(deskFetch).mockImplementation(async (url, init) => {
    const method = init?.method ?? 'GET'
    asked.push({ url: String(url), method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (String(url) === '/api/runner-key') return json(200, { runnerKey: runner })
    if (String(url) === '/api/audit/jobs-verify') return jobs()
    if (String(url) === '/api/operations/run-chain') return chain()
    if (String(url) === '/api/audit/verify') return json(200, record)
    if (String(url) === '/api/audit/holders' && method === 'GET') return json(200, holders.length > 1 ? holders.shift()! : holders[0]!)
    if (String(url).startsWith('/api/audit/checkpoints?')) return download()
    if (String(url).endsWith('/confirm') && method === 'POST') return confirm()
    return json(404, { error: 'not here' })
  })
  let pending: Blob | undefined
  URL.createObjectURL = (blob: Blob) => { pending = blob; return 'blob:saved' }
  URL.revokeObjectURL = () => undefined
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    if (pending) saved.push({ blob: pending, name: this.download })
  })
})
const createObjectURL = URL.createObjectURL, revokeObjectURL = URL.revokeObjectURL
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.restoreAllMocks(); vi.useRealTimers(); URL.createObjectURL = createObjectURL; URL.revokeObjectURL = revokeObjectURL })

function show(client: QueryClient = testQueryClient(), visible = true) {
  return render(<QueryClientProvider client={client}><JobsRecord visible={visible} /></QueryClientProvider>)
}
const count = (url: string, method = 'GET') => asked.filter(call => call.url === url && call.method === method).length
const panel = async () => within(await screen.findByTestId('jobs-record'))
async function check() {
  fireEvent.click((await panel()).getByRole('button', { name: 'Check the chain of runs' }))
}

describe('the Jobs record', () => {
  it('is not shown where this desk has no Runner, and asks nothing more', async () => {
    runner = null
    show()
    await waitFor(() => expect(count('/api/runner-key')).toBe(1))
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)) })
    expect(screen.queryByRole('region', { name: 'Jobs record' })).toBeNull()
    expect(count('/api/audit/jobs-verify')).toBe(0)
  })

  it('runs only when the owner asks: never on opening, on a timer, on focus or on a reconnect', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const client = testQueryClient()
    show(client)
    expect(await screen.findByRole('region', { name: 'Jobs record' })).toBeTruthy()
    await act(async () => { await vi.advanceTimersByTimeAsync(6 * 60 * 60 * 1000) })
    window.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('offline'))
    window.dispatchEvent(new Event('online'))
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(count('/api/audit/jobs-verify')).toBe(0)
    expect(count('/api/runner-key')).toBe(1)
    await check()
    expect(await screen.findByText(STATEMENT)).toBeTruthy()
    expect(count('/api/audit/jobs-verify')).toBe(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(6 * 60 * 60 * 1000) })
    // Neither the project's own invalidation nor a blanket one runs it again.
    expect(followsTheProject(client.getQueryCache().find({ queryKey: JOBS_RECORD_KEY })!)).toBe(false)
    await act(async () => { await client.invalidateQueries() })
    expect(count('/api/audit/jobs-verify')).toBe(1)
    expect(count('/api/runner-key')).toBe(1)
  })

  it('shows the runtime’s report, with what it witnessed, each finding by name, and its sentences verbatim, in English', async () => {
    jobs = () => json(200, report({ report: witnessed }))
    show()
    await check()
    const shown = await panel()
    expect((await shown.findByText('The trail failed a check the runtime made.')).getAttribute('data-status')).toBe('invalid')
    expect(shown.getByText(STATEMENT)).toBeTruthy()
    const facts = within(shown.getByLabelText('Coverage'))
    expect(facts.getByText('Witnessed through record 2')).toBeTruthy()
    const term = (name: string) => facts.getByText(name).nextElementSibling?.textContent
    expect(term('Records a held checkpoint witnesses')).toBe('2')
    expect(term('Records no held checkpoint witnesses')).toBe('1')
    expect(term('Chained lines')).toBe('3')
    expect(term('Lines nothing commits to')).toBe('0')
    expect(term('Unchained lines a later chained line commits to')).toBe('0')
    expect(term('Lines a repair names as damaged')).toBe('0')
    expect(facts.getByText('Not checked: Desk gave no public key')).toBeTruthy()
    const findings = within(shown.getByRole('region', { name: 'Findings' }))
    expect(findings.getByText('checkpoint-record-mismatch')).toBeTruthy()
    for (const [region, sentences] of [['What this establishes', establishes], ['What this does not establish', doesNotEstablish]] as const) {
      const list = within(shown.getByRole('region', { name: region })).getByRole('list')
      expect([...list.querySelectorAll('li')].map(item => item.textContent)).toEqual(sentences)
      expect(list.getAttribute('lang')).toBe('en')
    }
    expect(shown.getByText('Lines in Desk’s copy of the chain: 3')).toBeTruthy()
    expect(shown.getByText('Checked by jpack 0.27.1.')).toBeTruthy()
  })

  it('says Runner’s key in the Gates paragraph’s words, and where each run’s signature is checked, after it', async () => {
    show()
    await check()
    const signatures = within(await (await panel()).findByRole('region', { name: 'Signatures' }))
    const paragraphs = signatures.getAllByText((_, element) => element?.tagName === 'P')
    expect(paragraphs[0]!.textContent).toMatch(/^Jobs signatures\. Runner signs the record of each Jobs run on this desk with a key of its own/)
    expect(signatures.getByLabelText(`Runner’s public key, keyId ${keyId}`).textContent).toBe(publicKey)
    expect(paragraphs.at(-1)!.textContent).toBe(SIGNATURES)
    cleanup()
    jobs = () => json(200, report({ runnerKey: { state: 'unsigned', reason: 'in-use' } }))
    show()
    await check()
    const unsigned = within(await (await panel()).findByRole('region', { name: 'Signatures' }))
    expect(unsigned.getByText(/^Jobs runs on this desk are not signed, and run as before\./)).toBeTruthy()
    expect(unsigned.getByText(SIGNATURES)).toBeTruthy()
  })

  it('makes no claim of Desk’s own: the runtime’s sentences and the coverage are the only ones', async () => {
    jobs = () => json(200, report({ report: witnessed }))
    show()
    await check()
    const region = await screen.findByRole('region', { name: 'Jobs record' })
    await within(region).findByText(STATEMENT)
    const own = region.cloneNode(true) as HTMLElement
    for (const element of own.querySelectorAll('[aria-label="Coverage"], [aria-label="What this establishes"], [aria-label="What this does not establish"], [aria-label="Findings"]')) element.remove()
    for (const element of own.querySelectorAll('p')) if (element.textContent === STATEMENT) element.remove()
    expect(own.textContent).not.toMatch(/verified|witnessed|proof|evidence/i)
  })

  it('says where this desk has no Runner after all, and where Runner is not running, and offers nothing more', async () => {
    for (const [answer, words] of [[{ state: 'no-runner' }, 'This desk has no Runner, so it has no chain of runs to check.'],
      [{ state: 'not-running', runnerKey: { state: 'not-running', detail: 'runner unavailable' } }, 'Runner is not running on this desk now, so Desk could not read its chain of runs.']] as const) {
      jobs = () => json(200, answer)
      show()
      await check()
      const shown = await panel()
      expect(await shown.findByText(words)).toBeTruthy()
      expect(shown.queryByText(STATEMENT)).toBeNull()
      expect(shown.queryByRole('region', { name: 'Signatures' })).toBeNull()
      cleanup()
    }
  })

  it('says the decision record’s sentence with an older runtime, and nothing else of a check', async () => {
    jobs = () => json(200, { state: 'older-runtime', runtime: '0.25.0', floor: '0.26.0' })
    show()
    await check()
    const shown = await panel()
    expect(await shown.findByText('This runtime (jpack 0.25.0) writes an unchained trail and has no audit commands. Chaining, checkpoints, signing and stamping need jpack 0.26.0 or later.')).toBeTruthy()
    expect(shown.queryByText(STATEMENT)).toBeNull()
    expect(shown.queryByLabelText('Coverage')).toBeNull()
  })

  it('shows the runtime’s refusal to check, in its words, and why Desk passed no checkpoint', async () => {
    const problem = 'Desk could not tell which trail the checkpoints it handed over belong to, so it passed none of them to the check: The runtime gives no checkpoint of this trail now: The trail .desk-private/handover/jobs-chain.jsonl could not be opened as one regular file.'
    jobs = () => json(200, { state: 'unverified', runtime: '0.27.1', diagnostics: [{ code: 'JPS-AUDIT-TRAIL-READ', message: 'The trail .desk-private/handover/jobs-chain.jsonl could not be opened as one regular file.' }], chainLines: 2, handoverProblem: problem })
    show()
    await check()
    const shown = await panel()
    expect(await shown.findByText('The runtime did not check the chain of runs.')).toBeTruthy()
    const list = shown.getByRole('list', { name: 'What the runtime said' })
    expect(list.textContent).toBe('JPS-AUDIT-TRAIL-READ The trail .desk-private/handover/jobs-chain.jsonl could not be opened as one regular file.')
    expect(shown.getByText(problem).getAttribute('role')).toBe('alert')
  })

  it('names the holders whose checkpoints of the chain it could not read', async () => {
    jobs = () => json(200, report({ expectUnread: ['Auditor', 'Regulator'] }))
    show()
    await check()
    expect((await (await panel()).findByText('Desk could not read the checkpoints it keeps as handed over to Auditor, Regulator, so the check ran without them.')).getAttribute('role')).toBe('alert')
  })

  it('says a refusal that is not JSON as one, and Desk’s refusal in its words', async () => {
    jobs = () => new Response('upstream broke', { status: 502, headers: { 'Content-Type': 'text/plain' } })
    show()
    await check()
    expect((await (await panel()).findByRole('alert')).textContent).toBe('The chain of runs could not be checked. Please try again.')
    cleanup()
    const said = 'Desk could not take a copy of the runner\'s chain of runs, and nothing was checked: the runner\'s answer did not complete.'
    jobs = () => json(502, { error: said, code: 'bad-request' })
    show()
    await check()
    expect((await (await panel()).findByRole('alert')).textContent).toBe(said)
    cleanup()
    jobs = () => json(200, { state: 'report', report: valid })
    show()
    await check()
    expect((await (await panel()).findByRole('alert')).textContent).toBe('The chain of runs could not be checked. Please try again.')
  })

  it('shows an error in place of an earlier report when the check is asked again, and nothing of that report', async () => {
    show()
    await check()
    const shown = await panel()
    expect(await shown.findByText(STATEMENT)).toBeTruthy()
    const said = 'Desk could not take a copy of the runner\'s chain of runs, and nothing was checked: the runner\'s answer did not complete.'
    jobs = () => json(502, { error: said, code: 'bad-request' })
    await check()
    expect((await shown.findByRole('alert')).textContent).toBe(said)
    expect(count('/api/audit/jobs-verify')).toBe(2)
    expect(shown.queryByText(STATEMENT)).toBeNull()
    expect(shown.queryByLabelText('Coverage')).toBeNull()
    expect(shown.queryByRole('region', { name: 'Signatures' })).toBeNull()
  })

  it('saves the chain as Desk passed it on: a Blob, never read as text, under run-chain.jsonl', async () => {
    show()
    fireEvent.click((await panel()).getByRole('button', { name: 'Download the chain' }))
    expect(await (await panel()).findByText('Saved run-chain.jsonl.')).toBeTruthy()
    expect(count('/api/operations/run-chain')).toBe(1)
    expect(saved).toHaveLength(1)
    expect(saved[0]!.name).toBe('run-chain.jsonl')
    expect(new Uint8Array(await saved[0]!.blob.arrayBuffer())).toEqual(served)
    // The download checks nothing.
    expect(count('/api/audit/jobs-verify')).toBe(0)
    chain = () => json(503, { error: 'The local runner is unavailable. Check its installation and private state directory.' })
    fireEvent.click((await panel()).getByRole('button', { name: 'Download the chain' }))
    expect((await (await panel()).findByRole('alert')).textContent).toBe('The local runner is unavailable. Check its installation and private state directory.')
    expect(saved).toHaveLength(1)
    expect((await panel()).queryByText('Saved run-chain.jsonl.')).toBeNull()
  })

  it('offers no repair, though its report names an incomplete last line: the chain of runs is Runner’s', async () => {
    const torn = { ...witnessed, findings: [{ name: 'incomplete-last-line', line: 4, detail: 'the trail ends in 8 bytes with no newline: a write that did not complete' }] }
    // Even an answer that carried an offer is shown with none.
    jobs = () => json(200, { ...report({ report: torn }), repair: { state: 'available', line: 4, token: 'ab'.repeat(48) } })
    show()
    await check()
    expect(await (await panel()).findByText('incomplete-last-line')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Repairing the trail' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Repair the trail' })).toBeNull()
    expect(count('/api/audit/repair', 'POST')).toBe(0)
  })
})

/* The chain of runs in the hand-over ------------------------------------------- */

const auditor: Holder = { id: 'a1b2c3d4e5f60718', label: 'Auditor', channel: 'e-mail to records@example.com', addedAt: 1791201600, trails: {} }
const jobsName = `jobs-checkpoints-${chainId}-4-5.jsonl`
const jobsCheckpoints = (headers: Record<string, string> = {}) => new Response(served, { status: 200, headers: {
  'Content-Type': 'application/jsonl', 'Content-Disposition': `attachment; filename="${jobsName}"`, 'Desk-Checkpoints-Trail': chainId,
  'Desk-Checkpoints-From': '3', 'Desk-Checkpoints-Through': '5', 'Desk-Checkpoints-Digest': fileDigest, 'Desk-Checkpoints-More': 'false', ...headers } })

function showBoth(client: QueryClient = testQueryClient()) {
  render(<QueryClientProvider client={client}><DecisionRecord /><JobsRecord /></QueryClientProvider>)
  return client
}
const handover = async () => within(await screen.findByRole('region', { name: 'Hand-over' }))
const row = async (name: string) => within(await (await handover()).findByRole('group', { name }))

describe('the chain of runs in the hand-over', () => {
  it('shows each holder two rows where this desk has a Runner, each with what Desk recorded of it', async () => {
    holders = [{ holders: [{ ...auditor, trails: { [trail]: { through: 3, confirmedAt: 1791205200, digest, unwitnessed: 0 } }, jobs: { [chainId]: { through: 3, confirmedAt: 1791205200, digest: fileDigest, unwitnessed: 2 } } }],
      trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    showBoth()
    const desk = await row('Decision record'), runs = await row('Jobs runs')
    expect(desk.getByText('Records since: 0')).toBeTruthy()
    expect(desk.getByLabelText('SHA-256 of what was handed over').textContent).toBe(digest)
    expect(runs.getByText('Records since: 2')).toBeTruthy()
    expect(runs.getByLabelText('SHA-256 of what was handed over').textContent).toBe(fileDigest)
    expect(desk.getAllByRole('button', { name: 'Download checkpoints' })).toHaveLength(1)
    expect(runs.getAllByRole('button', { name: 'Download checkpoints' })).toHaveLength(1)
  })

  it('says where Runner is not running, no run is chained yet, or the chain could not be read, and then offers nothing', async () => {
    for (const [jobsChain, words] of [
      [{ state: 'not-running' }, 'Runner is not running on this desk now, so Desk could not read its chain of runs.'],
      [{ state: 'empty' }, 'No run is chained yet, so there is nothing to hand over.'],
      [{ state: 'unread', problem: 'Desk could not take a copy of the runner\'s chain of runs: the runner\'s answer did not complete.' }, 'Desk could not read the runner’s chain of runs now, so nothing can be handed over from it.']
    ] as const) {
      holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 }, jobs: jobsChain }]
      showBoth()
      const runs = await row('Jobs runs')
      expect(runs.getByText(words)).toBeTruthy()
      expect(runs.queryByRole('button', { name: 'Download checkpoints' })).toBeNull()
      expect((await row('Decision record')).getByRole('button', { name: 'Download checkpoints' })).toBeTruthy()
      cleanup()
    }
  })

  it('shows one row for each holder, and says so once, where this desk has no Runner', async () => {
    holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 }, jobs: { state: 'no-runner' } }]
    showBoth()
    const section = await handover()
    expect(await section.findByText('This desk has no Runner, so it has no chain of runs to hand over.')).toBeTruthy()
    expect(section.queryByRole('group', { name: 'Jobs runs' })).toBeNull()
    expect(section.getAllByRole('button', { name: 'Download checkpoints' })).toHaveLength(1)
  })

  it('says a holder of a chain that has another identity now starts at 0 for it', async () => {
    holders = [{ holders: [{ ...auditor, jobs: { [moved]: { through: 9, confirmedAt: 1791201000, digest } }, otherJobsChain: true }],
      trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    showBoth()
    expect((await row('Jobs runs')).getByText('The chain of runs has another identity now: this holder starts at 0 for it')).toBeTruthy()
    expect((await row('Decision record')).getByText('Nothing handed over yet')).toBeTruthy()
  })

  it('downloads the chain’s checkpoints as served, confirms them as the chain’s, and then checks the Jobs record again', async () => {
    const recorded = { ...auditor, jobs: { [chainId]: { through: 5, confirmedAt: 1791205200, digest: fileDigest, unwitnessed: 0 } } }
    holders = [{ holders: [{ ...auditor, jobs: { [chainId]: { through: 3, confirmedAt: 1791201000, digest } } }], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } },
      { holders: [recorded], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    download = () => jobsCheckpoints()
    confirm = () => json(200, recorded)
    jobs = () => json(200, report({ report: { ...valid, coverage: { ...valid.coverage, checkpointed: { status: 'through', through: 5 }, witnessed: 5, unwitnessed: 0 } } }))
    showBoth()
    fireEvent.click((await row('Jobs runs')).getByRole('button', { name: 'Download checkpoints' }))
    expect(await (await row('Jobs runs')).findByText(`Saved ${jobsName}: the checkpoints after record 3, through record 5.`)).toBeTruthy()
    expect(asked.some(call => call.url === '/api/audit/checkpoints?holder=a1b2c3d4e5f60718&chain=jobs')).toBe(true)
    expect(saved.map(file => file.name)).toEqual([jobsName])
    expect(new Uint8Array(await saved[0]!.blob.arrayBuffer())).toEqual(served)
    expect((await row('Decision record')).queryByRole('button', { name: 'Confirm' })).toBeNull()
    expect(count('/api/audit/jobs-verify')).toBe(0)
    fireEvent.click((await row('Jobs runs')).getByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Recorded: the file went to Auditor, through record 5.')).toBeTruthy()
    expect(asked.filter(call => call.url.endsWith('/confirm'))).toEqual([{ url: '/api/audit/holders/a1b2c3d4e5f60718/confirm', method: 'POST', body: { chain: 'jobs', trail: chainId, from: 3, through: 5, digest: fileDigest } }])
    // The Jobs record is checked again, and shows what the runtime found.
    await waitFor(() => expect(count('/api/audit/jobs-verify')).toBe(1))
    expect(await (await panel()).findByText('Witnessed through record 5')).toBeTruthy()
  })

  /** One holder handed the chain through record 3, with records 4 and 5 to download and confirm. */
  function handingOverThroughFive() {
    const recorded = { ...auditor, jobs: { [chainId]: { through: 5, confirmedAt: 1791205200, digest: fileDigest, unwitnessed: 0 } } }
    holders = [{ holders: [{ ...auditor, jobs: { [chainId]: { through: 3, confirmedAt: 1791201000, digest } } }], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } },
      { holders: [recorded], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    download = () => jobsCheckpoints()
    confirm = () => json(200, recorded)
  }

  it('checks the chain again after a confirmation of it, though a check asked before is still in flight, and shows the later one', async () => {
    handingOverThroughFive()
    const before = deferred()
    const answers = [() => before.response, () => witnessedThrough(5)]
    jobs = () => answers.length > 1 ? answers.shift()!() : answers[0]!()
    showBoth()
    // The owner checks, and the check is still running when the hand-over is confirmed.
    await check()
    await waitFor(() => expect(count('/api/audit/jobs-verify')).toBe(1))
    fireEvent.click((await row('Jobs runs')).getByRole('button', { name: 'Download checkpoints' }))
    fireEvent.click(await (await row('Jobs runs')).findByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Recorded: the file went to Auditor, through record 5.')).toBeTruthy()
    // A check of its own, after the confirmation: not the one asked before it.
    await waitFor(() => expect(count('/api/audit/jobs-verify')).toBe(2))
    expect(await (await panel()).findByText('Witnessed through record 5')).toBeTruthy()
    // The earlier check's answer, arriving now, shows nothing.
    await act(async () => { before.release(witnessedThrough(3)); await new Promise(resolve => setTimeout(resolve, 20)) })
    expect((await panel()).getByText('Witnessed through record 5')).toBeTruthy()
    expect((await panel()).queryByText('Witnessed through record 3')).toBeNull()
  })

  it('keeps the check after a confirmation of the chain through a change to the project while it runs', async () => {
    handingOverThroughFive()
    const after = deferred()
    jobs = () => after.response
    const client = showBoth()
    fireEvent.click((await row('Jobs runs')).getByRole('button', { name: 'Download checkpoints' }))
    fireEvent.click(await (await row('Jobs runs')).findByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(count('/api/audit/jobs-verify')).toBe(1))
    // What McpProvider does on desk/fileChanged, and after a reconnect.
    await act(async () => {
      await client.cancelQueries({ predicate: followsTheProject })
      await client.invalidateQueries({ predicate: followsTheProject })
    })
    expect(client.getQueryState(JOBS_RECORD_KEY)?.fetchStatus).toBe('fetching')
    await act(async () => { after.release(witnessedThrough(5)) })
    expect(await (await panel()).findByText('Witnessed through record 5')).toBeTruthy()
    expect(count('/api/audit/jobs-verify')).toBe(1)
  })

  it('does not check the Jobs record again after a hand-over of the desk’s trail', async () => {
    holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    download = () => new Response(served, { status: 200, headers: {
      'Content-Type': 'application/jsonl', 'Content-Disposition': `attachment; filename="checkpoints-${trail}-1-3.jsonl"`, 'Desk-Checkpoints-Trail': trail,
      'Desk-Checkpoints-From': '0', 'Desk-Checkpoints-Through': '3', 'Desk-Checkpoints-Digest': fileDigest, 'Desk-Checkpoints-More': 'false' } })
    confirm = () => json(200, auditor)
    showBoth()
    fireEvent.click((await row('Decision record')).getByRole('button', { name: 'Download checkpoints' }))
    fireEvent.click(await (await row('Decision record')).findByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Recorded: the file went to Auditor, through record 3.')).toBeTruthy()
    expect(asked.filter(call => call.url.endsWith('/confirm'))[0]!.body).toEqual({ trail, from: 0, through: 3, digest: fileDigest })
    expect(asked.some(call => call.url === `/api/audit/checkpoints?holder=${auditor.id}`)).toBe(true)
    await waitFor(() => expect(count('/api/audit/verify')).toBe(2))
    expect(count('/api/audit/jobs-verify')).toBe(0)
  })

  it('keeps the chain’s download and its notices apart from the decision record’s', async () => {
    holders = [{ holders: [auditor], trail: { identity: trail, sequence: 3 }, jobs: { state: 'chain', chain: { identity: chainId, sequence: 5 } } }]
    download = () => new Response(null, { status: 204, headers: { 'Desk-Checkpoints-Trail': chainId } })
    showBoth()
    fireEvent.click((await row('Jobs runs')).getByRole('button', { name: 'Download checkpoints' }))
    expect(await (await row('Jobs runs')).findByText('Nothing new to hand over to this holder.')).toBeTruthy()
    expect((await row('Decision record')).queryByText('Nothing new to hand over to this holder.')).toBeNull()
  })
})

describe('the Jobs record client', () => {
  it('reads the Jobs record, and refuses what is not one', () => {
    expect(isJobsRecord(report())).toBe(true)
    expect(isJobsRecord({ state: 'not-running', runnerKey: { state: 'not-running' } })).toBe(true)
    expect(isJobsRecord({ state: 'no-runner' })).toBe(true)
    expect(isJobsRecord({ state: 'older-runtime', runtime: '0.25.0', floor: '0.26.0' })).toBe(true)
    for (const value of [{ ...report(), chainLines: undefined }, { ...report(), chainLines: -1 }, { ...report(), runnerKey: { state: 'signed', publicKey: 'x', keyId } },
      { ...report(), expected: -1 }, { ...report(), expectUnread: [''] }, { ...report(), handoverProblem: '' }, { state: 'unverified', diagnostics: [], chainLines: 0 },
      { state: 'no-runner', report: valid }, { state: 'not-running', chainLines: 3 }, { state: 'older-runtime' }, { state: 'chained' }, { ...report(), report: { ...valid, findingsTotal: 1 } }]) {
      expect(isJobsRecord(value), JSON.stringify(value)).toBe(false)
    }
  })

  it('reads the chain beside the holders, and refuses what is not it', () => {
    expect(isHolders({ holders: [], trail: null, jobs: { state: 'chain', chain: { identity: chainId, sequence: 1 } } })).toBe(true)
    expect(isHolders({ holders: [{ ...auditor, jobs: { [chainId]: { through: 1, confirmedAt: 1, digest } }, otherJobsChain: false }], trail: null })).toBe(true)
    for (const value of [{ state: 'chain' }, { state: 'chain', chain: { identity: chainId, sequence: 0 } }, { state: 'unread' }, { state: 'unread', problem: '' },
      { state: 'unread', diagnostics: [] }, { state: 'empty', chain: { identity: chainId, sequence: 1 } }, { state: 'running' }]) {
      expect(isJobsChain(value), JSON.stringify(value)).toBe(false)
    }
    expect(isHolders({ holders: [{ ...auditor, jobs: { 'not-a-chain': { through: 1, confirmedAt: 1, digest } } }], trail: null })).toBe(false)
    expect(isHolders({ holders: [{ ...auditor, otherJobsChain: 'yes' }], trail: null })).toBe(false)
  })

  it('takes a download of the chain only under the chain’s name, and the trail’s only under the trail’s', async () => {
    download = () => jobsCheckpoints({ 'Content-Disposition': `attachment; filename="checkpoints-${chainId}-4-5.jsonl"` })
    await expect(downloadCheckpoints(auditor.id, 'jobs')).rejects.toThrow('The checkpoints could not be downloaded. Please try again.')
    download = () => jobsCheckpoints()
    await expect(downloadCheckpoints(auditor.id)).rejects.toThrow('The checkpoints could not be downloaded. Please try again.')
    expect((await downloadCheckpoints(auditor.id, 'jobs'))?.chain).toBe('jobs')
  })

  it('runs the check after a confirmation only on request, so a change to the project does not cancel it', async () => {
    // The fetch's own options, with no panel mounted to set them again.
    const client = testQueryClient()
    const after = deferred()
    jobs = () => after.response
    const checking = checkJobsRecordAgain(client)
    await waitFor(() => expect(count('/api/audit/jobs-verify')).toBe(1))
    expect(followsTheProject(client.getQueryCache().find({ queryKey: JOBS_RECORD_KEY })!)).toBe(false)
    // What McpProvider does on desk/fileChanged, and after a reconnect.
    await client.cancelQueries({ predicate: followsTheProject })
    await client.invalidateQueries({ predicate: followsTheProject })
    after.release(witnessedThrough(5))
    await checking
    expect(client.getQueryData<JobsAnswer>(JOBS_RECORD_KEY)?.state).toBe('report')
    expect(count('/api/audit/jobs-verify')).toBe(1)
  })

  it('names, as Desk’s own sentences, only sentences the chassis says', () => {
    const source = readFileSync(join(import.meta.dirname, '../../../internal/desk/jobs_record.go'), 'utf8')
    expect(JOBS_REASONS).toHaveLength(4)
    for (const reason of JOBS_REASONS) expect(source.includes(reason) ? reason : `missing: ${reason}`).toBe(reason)
  })
})

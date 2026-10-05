/**
 * A job's Activity tab (#213), rendered from records shaped as Runner v0.5.0
 * serves them: the table, its named times, who initiated each record, what a
 * run's record holds, the filters, paging, and the detail pane.
 */
import { createHash } from 'node:crypto'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DetailsSlotContext } from '../shell/DetailsSlot'
import { JobsContent } from './JobsView'
import { jobsAPI } from './client'
import { when } from './RunRecord'
import { at, JOB, occurrences, RECORD_LINE, runner, runs, summary, SIDECAR_LINES } from './__fixtures__/activity'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
beforeEach(() => { vi.mocked(jobsAPI).mockImplementation(runner() as never) })
afterEach(() => { cleanup(); vi.resetAllMocks(); document.body.innerHTML = '' })

function show(slot?: React.ContextType<typeof DetailsSlotContext>) {
  const page = <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}?tab=activity`]}><Routes><Route path="/jobs/:jobId" element={<JobsContent />} /><Route path="/jobs/:jobId/runs/:runId" element={<p>run page</p>} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>
  return render(slot ? <DetailsSlotContext.Provider value={slot}>{page}</DetailsSlotContext.Provider> : page)
}
const table = () => screen.findByRole('table')
const row = (what: string) => screen.getByRole('button', { name: what }).closest('tr')!
const cells = (what: string) => [...row(what).querySelectorAll('td')]
const whats = async () => within(await table()).getAllByRole('button').map(button => button.textContent)
async function pick(label: string, option: string) {
  fireEvent.keyDown(screen.getByRole('combobox', { name: label }), { key: 'Enter' })
  fireEvent.click(await screen.findByRole('option', { name: option }))
}
const asked = (path: string) => vi.mocked(jobsAPI).mock.calls.some(([called]) => called === path)
/** What a reader reads: everything but raw values in code and pre. */
function sentences(element: Element) {
  const copy = element.cloneNode(true) as Element
  copy.querySelectorAll('pre, code').forEach(node => node.remove())
  return copy.textContent ?? ''
}

describe('the Activity tab', () => {
  it('comes after Release, and shows each record once, newest first by the time Runner first recorded it', async () => {
    show()
    const tabs = within(await screen.findByRole('navigation', { name: 'Job' })).getAllByRole('button')
    expect(tabs.map(tab => tab.textContent)).toEqual(['Runs', 'Triggers', 'Release', 'Activity'])
    expect(tabs[3]!.getAttribute('aria-current')).toBe('page')
    const head = within(await table()).getAllByRole('columnheader').map(th => th.textContent)
    expect(head).toEqual(['When', 'What', 'By', 'Release', 'Evidence'])
    expect(await whats()).toEqual([
      'Run queued', 'Run started', 'Occurrence received', 'Occurrence skipped', 'Occurrence expired', 'Occurrence failed', 'Occurrence canceled',
      'Run completed: accept', 'Run completed: refer', 'Run failed', 'Run interrupted', 'Run completed: accept',
      'Source preparation waiting for sources', 'Source preparation needs attention'
    ])
    // Read only, and no brief: nothing but Runner's own lists and records was asked for.
    expect(vi.mocked(jobsAPI).mock.calls.every(([path, body]) => body === undefined && !String(path).includes('brief'))).toBe(true)
  })

  it('opens from the tab switch', async () => {
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}`]}><Routes><Route path="/jobs/:jobId" element={<JobsContent />} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
    fireEvent.click(await screen.findByRole('button', { name: 'Activity' }))
    expect(await screen.findByRole('columnheader', { name: 'Evidence' })).toBeTruthy()
  })

  it('names every time it shows for what Runner recorded at it', async () => {
    show(); await table()
    const stamps = (what: string, index = 0) => [...screen.getAllByRole('button', { name: what })[index]!.closest('tr')!.querySelectorAll('td')[0]!.querySelectorAll(':scope > div > div')].map(line => line.textContent)
    expect(stamps('Run queued')).toEqual([`Submitted ${when(runs.queued.createdAt)}`])
    expect(stamps('Run started')).toEqual([`Submitted ${when(runs.running.createdAt)}`, `Started ${when(runs.running.startedAt!)}`])
    expect(stamps('Run completed: refer')).toEqual([`Submitted ${when(runs.unsigned.createdAt)}`, `Started ${when(runs.unsigned.startedAt!)}`, `Finished ${when(runs.unsigned.finishedAt!)}`])
    // Expired in the queue: never started, and not said to have.
    expect(stamps('Run failed')).toEqual([`Submitted ${when(runs.failed.createdAt)}`, `Finished ${when(runs.failed.finishedAt!)}`])
    expect(stamps('Occurrence skipped')).toEqual([`Received ${when(occurrences.skipped.receivedAt)}`, `Scheduled for ${when(occurrences.skipped.scheduledAt!)}`])
    expect(stamps('Source preparation waiting for sources')).toEqual([`Received ${when(occurrences.waiting.receivedAt)}`, `Preparation started ${when(occurrences.waiting.preparation!.startedAt)}`])
    for (const time of screen.getAllByRole('table')[0]!.querySelectorAll('time')) expect(Date.parse(time.getAttribute('datetime')!)).not.toBeNaN()
  })

  it('shows an interrupted run’s finish as the time the runner recorded the interruption', async () => {
    show(); await table()
    const [when_] = cells('Run interrupted')
    expect(when_!.textContent).toContain(`Interruption recorded by the runner ${when(runs.interrupted.finishedAt!)}`)
    expect(when_!.textContent).not.toContain('Finished')
  })

  // A run without its submission time is held by the row tests in
  // activity.test.ts: the Runs tab, which stays mounted beside this one, does
  // not render such a run at all.
  it('makes no row for a record without the time that orders it', async () => {
    const unreceived = { ...occurrences.skipped, id: 'occ_' + '9'.repeat(32), receivedAt: '' }
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [runs.signed], occurrences: [unreceived, occurrences.expired] }) as never)
    show()
    expect(await whats()).toEqual(['Occurrence expired', 'Run completed: accept'])
    expect((await table()).querySelectorAll('tbody tr')).toHaveLength(2)
  })

  it('says who initiated each record: this installation, or a trigger by name and revision; never a person', async () => {
    show(); await table()
    expect(cells('Run queued')[2]!.textContent).toBe('This installation')
    expect(cells('Run started')[2]!.textContent).toBe('Nightly intake, trigger revision 2')
    expect(cells('Occurrence skipped')[2]!.textContent).toBe('Nightly intake, trigger revision 3')
    // A run recorded before Runner kept who asked for it.
    expect(screen.getAllByRole('button', { name: 'Run completed: accept' })[1]!.closest('tr')!.querySelectorAll('td')[2]!.textContent).toBe('Not recorded')
  })

  it('names the fixed release, linking to the Release tab', async () => {
    show(); await table()
    const link = within(cells('Run queued')[3]!).getByRole('link', { name: 'Fixed version 1.2.0' })
    expect(link.textContent).toBe('1.2.0')
    expect(link.getAttribute('href')).toBe(`/jobs/${JOB}?tab=release`)
  })

  it('says what each run’s record holds, present or absent, read from the run itself', async () => {
    show(); await table()
    const evidence = (what: string) => cells(what)[4]!
    await waitFor(() => expect(evidence('Run completed: refer').textContent).not.toContain('Loading'))
    expect([...evidence('Run completed: refer').querySelectorAll('span')].map(span => span.textContent)).toEqual(['Present: Retained inputs, Exact record bytes', 'Absent: Signature sidecar, Acquisition receipts'])
    await waitFor(() => expect([...evidence('Run queued').querySelectorAll('span')].map(span => span.textContent)).toEqual(['Present: Retained inputs', 'Absent: Exact record bytes, Signature sidecar, Acquisition receipts']))
    const signed = screen.getAllByRole('button', { name: 'Run completed: accept' })[0]!.closest('tr')!.querySelectorAll('td')[4]!
    await waitFor(() => expect([...signed.querySelectorAll('span')].map(span => span.textContent)).toEqual(['Present: Retained inputs, Exact record bytes, Signature sidecar', 'Absent: Acquisition receipts']))
    expect(asked(`runs/${runs.signed.id}`)).toBe(true)
    // An occurrence is no run record, and claims nothing.
    expect(evidence('Occurrence skipped').textContent).toBe('—')
  })

  it('shows a failed run’s problem by its first line, and occurrences and preparations with their reasons', async () => {
    show(); await table()
    expect(cells('Run failed')[1]!.textContent).toContain('The automatic run expired in the queue before evaluation.')
    expect(cells('Run failed')[1]!.textContent).not.toContain('Second line')
    expect(cells('Occurrence received')[1]!.textContent).toContain('Submitted')
    expect(cells('Occurrence skipped')[1]!.textContent).toContain('Another run was pending')
    expect(cells('Occurrence expired')[1]!.textContent).toContain('Queue expired')
    expect(cells('Occurrence failed')[1]!.textContent).toContain('Source acquisition was interrupted.')
    expect(cells('Occurrence canceled')[1]!.textContent).toContain('Cancelled locally.')
    expect(cells('Source preparation needs attention')[1]!.textContent).toContain('The acquisition worker stopped before retaining the response.')
  })

  it('links each run row, and an occurrence that became a run, to the run page', async () => {
    show(); await table()
    expect(within(cells('Run queued')[1]!).getByRole('link').getAttribute('href')).toBe(`/jobs/${JOB}/runs/${runs.queued.id}`)
    expect(within(cells('Occurrence received')[1]!).getByRole('link').getAttribute('href')).toBe(`/jobs/${JOB}/runs/${runs.running.id}`)
    fireEvent.click(within(cells('Run queued')[1]!).getByRole('link'))
    expect(await screen.findByText('run page')).toBeTruthy()
  })

  it('says nothing is checked: no sentence says verified, valid, witnessed or audit trail', async () => {
    show(); await table()
    fireEvent.click(screen.getAllByRole('button', { name: 'Run completed: accept' })[0]!)
    await screen.findByText('Exact record bytes: SHA-256')
    const page = screen.getByRole('region', { name: 'Activity' })
    expect(sentences(page)).not.toMatch(/verified|valid|witnessed|audit trail/i)
    expect(sentences(page)).toContain('nothing here checks it')
  })
})

describe('the filters', () => {
  it('keep one kind, and ask Runner to filter runs by state before it pages them', async () => {
    show(); await table()
    await pick('Kind', 'Runs')
    await waitFor(async () => expect((await whats()).every(what => what!.startsWith('Run '))).toBe(true))
    await pick('State', 'Completed')
    await waitFor(() => expect(asked(`jobs/${JOB}/runs?state=completed&after=0`)).toBe(true))
    await waitFor(async () => expect(await whats()).toEqual(['Run completed: accept', 'Run completed: refer', 'Run completed: accept']))
  })

  it('offer each kind its own states, and keep occurrences and preparations apart', async () => {
    show(); await table()
    await pick('Kind', 'Trigger occurrences')
    await waitFor(async () => expect(await whats()).toEqual(['Occurrence received', 'Occurrence skipped', 'Occurrence expired', 'Occurrence failed', 'Occurrence canceled']))
    await pick('State', 'Skipped')
    await waitFor(async () => expect(await whats()).toEqual(['Occurrence skipped']))
    await pick('Kind', 'Source preparations')
    // Skipped is no preparation's state: the filter starts again from all states.
    await waitFor(async () => expect(await whats()).toEqual(['Source preparation waiting for sources', 'Source preparation needs attention']))
    fireEvent.keyDown(screen.getByRole('combobox', { name: 'State' }), { key: 'Enter' })
    expect((await screen.findAllByRole('option')).map(option => option.textContent)).toEqual(['All states', 'Waiting for sources', 'Needs attention'])
  })

  it('keep what one trigger initiated, or what this installation did', async () => {
    show(); await table()
    await pick('Trigger', 'Nightly intake')
    await waitFor(async () => expect((await whats()).filter(what => what!.startsWith('Run '))).toEqual(['Run started']))
    await pick('Trigger', 'Manual / API')
    await waitFor(async () => expect(await whats()).toEqual(['Run queued', 'Run completed: accept', 'Run completed: refer', 'Run failed', 'Run interrupted', 'Run completed: accept']))
  })

  it('say when nothing matches', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [runs.queued], occurrences: [] }) as never)
    show(); await table()
    await pick('Kind', 'Source preparations')
    expect(await screen.findByText('Nothing the runner recorded matches these filters.')).toBeTruthy()
  })
})

describe('paging', () => {
  it('reads Runner’s next page through its cursor, and never shows a row that page could put above one shown', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ pages: {
      [`jobs/${JOB}/runs?after=0`]: { items: [runs.queued, runs.running].map(summary), next: 7 },
      [`jobs/${JOB}/runs?after=7`]: { items: [runs.signed, runs.unsigned].map(summary), next: 0 }
    } }) as never)
    show()
    // The oldest loaded run was submitted at :30; every occurrence is older.
    expect(await whats()).toEqual(['Run queued', 'Run started'])
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }))
    await waitFor(() => expect(asked(`jobs/${JOB}/runs?after=7`)).toBe(true))
    await waitFor(async () => expect(await whats()).toEqual([
      'Run queued', 'Run started', 'Occurrence received', 'Occurrence skipped', 'Occurrence expired', 'Occurrence failed', 'Occurrence canceled',
      'Run completed: accept', 'Run completed: refer', 'Source preparation waiting for sources', 'Source preparation needs attention'
    ]))
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull()
  })

  it('shows no row until every list it reads has answered', async () => {
    let answer: (value: unknown) => void = () => {}
    const occurrencesLater = new Promise(resolve => { answer = resolve })
    const served = runner()
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => path === `jobs/${JOB}/occurrences?after=0` ? occurrencesLater : served(path)) as never)
    show()
    await waitFor(() => expect(asked(`jobs/${JOB}/runs?after=0`)).toBe(true))
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(screen.queryByRole('table')).toBeNull()
    answer({ items: Object.values(occurrences), next: 0 })
    expect((await whats())[2]).toBe('Occurrence received')
  })

  it('says the runner has recorded nothing yet', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [] }) as never)
    show()
    expect(await screen.findByText('The runner has recorded nothing for this job yet.')).toBeTruthy()
  })
})

describe('the detail pane', () => {
  it('shows a run with the run page’s fields and disclosures, and technical details with the record and the bytes’ size and SHA-256', async () => {
    show(); await table()
    fireEvent.click(screen.getAllByRole('button', { name: 'Run completed: accept' })[0]!)
    const pane = await screen.findByRole('region', { name: 'Run completed: accept' })
    const fact = (term: string) => within(pane).getByText(term, { selector: 'dt' }).nextElementSibling!.textContent
    expect(fact('Started')).toBe(when(runs.signed.startedAt!))
    expect(fact('Requested by')).toBe('This installation')
    expect(fact('Signature sidecar')).toBe('Present in the record')
    expect(within(pane).getByText('Runtime audit record')).toBeTruthy()
    expect(within(pane).getByRole('link', { name: 'View run' }).getAttribute('href')).toBe(`/jobs/${JOB}/runs/${runs.signed.id}`)
    const digest = (text: string) => 'sha256:' + createHash('sha256').update(text).digest('hex')
    await waitFor(() => expect(fact('Exact record bytes: SHA-256')).toBe(digest(RECORD_LINE)))
    expect(fact('Exact record bytes: size in bytes')).toBe(String(new TextEncoder().encode(RECORD_LINE).length))
    expect(fact('Signature sidecar: SHA-256')).toBe(digest(SIDECAR_LINES))
    expect(fact('Signature sidecar: size in bytes')).toBe(String(SIDECAR_LINES.length))
    // The record itself, as Runner returned it, once opened.
    const record = within(pane).getByText('Run record (JSON)').closest('details')!
    expect(record.querySelector('pre')).toBeNull()
    record.open = true; fireEvent(record, new Event('toggle'))
    expect(JSON.parse(record.querySelector('pre')!.textContent!)).toEqual(runs.signed)
  })

  it('says a run without a sidecar holds none, and has no bytes to measure', async () => {
    show(); await table()
    fireEvent.click(screen.getByRole('button', { name: 'Run queued' }))
    const pane = await screen.findByRole('region', { name: 'Run queued' })
    const fact = (term: string) => within(pane).getByText(term, { selector: 'dt' }).nextElementSibling!.textContent
    await waitFor(() => expect(fact('Exact record bytes')).toBe('Absent from the record'))
    expect(fact('Signature sidecar')).toBe('Absent from the record')
    expect(fact('Exact record bytes: SHA-256')).toBe('Absent from the record')
  })

  it('shows an occurrence’s own record, read only', async () => {
    show(); await table()
    fireEvent.click(screen.getByRole('button', { name: 'Source preparation needs attention' }))
    const pane = await screen.findByRole('region', { name: 'Source preparation needs attention' })
    const fact = (term: string) => within(pane).getByText(term, { selector: 'dt' }).nextElementSibling!.textContent
    expect(fact('Received')).toBe(when(occurrences.attention.receivedAt))
    expect(fact('Preparation started')).toBe(when(occurrences.attention.preparation!.startedAt))
    expect(fact('Trigger')).toBe('Nightly intake, trigger revision 3')
    expect(within(pane).getByText('Occurrence ID', { selector: 'dt' }).nextElementSibling!.textContent).toBe(occurrences.attention.id)
    // Read only: the Triggers tab's cancel and check actions are not here.
    expect(within(pane).queryByRole('button', { name: 'Cancel' })).toBeNull()
    expect(within(pane).queryByRole('button', { name: 'Check status' })).toBeNull()
  })

  it('opens in the shell’s right pane when it offers one', async () => {
    const target = document.createElement('div'); document.body.append(target)
    const reveal = vi.fn(), release = vi.fn()
    show({ target, open: true, claim: () => release, reveal })
    await table()
    fireEvent.click(screen.getByRole('button', { name: 'Run interrupted' }))
    expect(reveal).toHaveBeenCalled()
    await waitFor(() => expect(within(target).getByRole('region', { name: 'Run interrupted' })).toBeTruthy())
    expect(within(target).getByText('Interruption recorded by the runner', { selector: 'dt' }).nextElementSibling!.textContent).toBe(when(runs.interrupted.finishedAt!))
    expect(screen.getAllByRole('region', { name: 'Run interrupted' })).toHaveLength(1)
    expect(at(50)).toBe(runs.interrupted.finishedAt)
  })
})

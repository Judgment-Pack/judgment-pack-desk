/**
 * The Activity tab's reading of a large journal (#218): Runner pages it oldest
 * first and cannot be read from its newest entries, so the tab reads forward,
 * one page at a time with a turn for the browser between pages, says how far
 * it has read and shows no entry until it is read to its end, renders the
 * newest entries in steps, keeps a bounded number in memory, and continues
 * from its cursor after a failure. The stand-in journals here are generated,
 * several thousand entries long; the tests compare counts and short sentences,
 * never the entries themselves.
 */
import { cleanup, configure, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ActivityView } from './ActivityView'
import { JobsContent } from './JobsView'
import { jobsAPI, JobsRequestError } from './client'
import { JOURNAL_LIMITS, type JournalLimits } from './journal'
import { JOB, release, runner, runs, type StandInJournal } from './__fixtures__/activity'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
configure({ asyncUtilTimeout: 20000 })
beforeEach(() => { vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [] }) as never) })
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); document.body.innerHTML = '' })

const page = (ui: React.ReactNode) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}?tab=activity`]}><Routes><Route path="/jobs/:jobId" element={ui} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>
/** The whole job page, with the tab's own bounds. */
const showPage = () => render(page(<JobsContent />))
/** The tab alone, with bounds small enough to reach. */
const showTab = (limits: JournalLimits = JOURNAL_LIMITS) => render(page(<ActivityView jobId={JOB} release={release} limits={limits} />))

const OTHER_RUN = 'run_' + 'f'.repeat(32)
/** `count` entries of a busy journal, sequences 1 to count, each a second apart. */
function busy(count: number, first = 1): { sequence: number }[] {
  return Array.from({ length: count }, (_, index) => ({
    sequence: first + index, entryVersion: '1', kind: 'run.started', at: new Date(Date.UTC(2026, 9, 3, 0, 0, first + index)).toISOString(),
    by: { kind: 'runner' }, concerns: { job: JOB, release: release.id, run: OTHER_RUN }, from: 'queued', to: 'running'
  }))
}
const events = (after: number) => `jobs/${JOB}/events?after=${after}`
const asked = (path: string) => vi.mocked(jobsAPI).mock.calls.filter(([called]) => called === path).length
// The job page keeps its other tabs mounted: only the Activity tab's rows count.
const bodyRows = () => [...(screen.queryByRole('region', { name: 'Activity' })?.querySelectorAll('tbody tr') ?? [])]
const journalRows = () => bodyRows().filter(row => row.getAttribute('data-kind') === 'journal')
/** The first and last journal row shown, by the time Runner recorded each. */
const span = () => { const rows = journalRows(); return [rows[0], rows.at(-1)].map(row => row?.querySelector('time')?.getAttribute('datetime')).join(' to ') }
const second = (sequence: number) => new Date(Date.UTC(2026, 9, 3, 0, 0, sequence)).toISOString()
/** A stand-in Runner that holds the answer to one journal page until released. */
function holding(journal: StandInJournal, held: string, records: Parameters<typeof runner>[0] = { runs: [], occurrences: [] }) {
  let release: () => void = () => {}
  const released = new Promise<void>(resolve => { release = resolve })
  const served = runner({ ...records, journal })
  let waited = false
  vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === held && !waited) { waited = true; await released } return served(path) }) as never)
  return () => release()
}

describe('reading a large journal', () => {
  it('says how far it has read while it reads, and shows no entry until it has read to the end', async () => {
    const answer = holding({ entries: busy(3000) }, events(1500), { runs: [runs.signed, runs.queued], occurrences: [] })
    showPage()
    expect(await screen.findByText('Reading the runner’s journal. Entries read so far: 1,500. Its entries are shown once it is read to its end. Until then, records stand by when the runner first recorded each.')).toBeTruthy()
    // The records, newest first by their time; no entry of a journal read in part.
    await waitFor(() => expect(bodyRows().length).toBe(2))
    expect(journalRows().length).toBe(0)
    expect(screen.queryByText(/read to its end as of the last request/)).toBeNull()
    answer()
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 3,000.')).toBeTruthy()
    expect(screen.queryByText(/Entries read so far/)).toBeNull()
    expect(asked(events(0))).toBe(1)
  })

  // It renders 1,500 rows in all, which takes a loaded host more than the default 20 seconds.
  it('renders the newest 500 entries, says so, and shows earlier ones 500 at a time', { timeout: 60_000 }, async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [], journal: { entries: busy(3000) } }) as never)
    showPage()
    expect(await screen.findByText('Showing the newest 500 of 3,000 journal entries read.')).toBeTruthy()
    expect(journalRows().length).toBe(500)
    expect(span()).toBe(`${second(3000)} to ${second(2501)}`)
    fireEvent.click(screen.getByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing the newest 1,000 of 3,000 journal entries read.')).toBeTruthy()
    expect(journalRows().length).toBe(1000)
    expect(span()).toBe(`${second(3000)} to ${second(2001)}`)
  })

  it('keeps at most its bound in memory, says how many earlier entries were read and not kept, and offers no step past them', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [], journal: { entries: busy(3000) } }) as never)
    showTab({ keep: 300, window: 100, step: 100 })
    expect(await screen.findByText('Earlier entries read and not kept, to bound what this page holds: 2,700. Only the newest 300 are kept.')).toBeTruthy()
    expect(screen.getByText('The runner’s journal is read to its end as of the last request. Entries read: 3,000.')).toBeTruthy()
    expect(screen.getByText('Showing the newest 100 of 3,000 journal entries read.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing the newest 200 of 3,000 journal entries read.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing the newest 300 of 3,000 journal entries read.')).toBeTruthy()
    expect([journalRows().length, span()].join(' / ')).toBe(`300 / ${second(3000)} to ${second(2701)}`)
    // Everything kept is shown: the entries before it were read and are not kept.
    expect(screen.queryByRole('button', { name: 'Show earlier journal entries' })).toBeNull()
  })

  it('asks for one page at a time, and gives the browser a turn between pages', async () => {
    const served = runner({ runs: [], occurrences: [], journal: { entries: busy(2000) } })
    let inFlight = 0, most = 0, turned = true, withoutATurn = 0
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      if (!path.startsWith(`jobs/${JOB}/events`)) return served(path)
      if (!turned) withoutATurn++
      inFlight++; most = Math.max(most, inFlight)
      await new Promise(resolve => setTimeout(resolve, 0))
      inFlight--
      // A task queued with the answer runs before the next page is asked for only if the reader yields to one.
      turned = false; setTimeout(() => { turned = true }, 0)
      return served(path)
    }) as never)
    showPage()
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 2,000.')).toBeTruthy()
    expect([asked(events(0)), most, withoutATurn].join(' ')).toBe('1 1 0')
    expect(vi.mocked(jobsAPI).mock.calls.filter(([called]) => String(called).startsWith(`jobs/${JOB}/events`)).length).toBe(40)
  })

  it('stops where a page fails, says after which entry, and continues from that cursor, never from 0', async () => {
    const served = runner({ runs: [], occurrences: [], journal: { entries: busy(3000) } })
    let failed = false
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      if (path === events(1500) && !failed) { failed = true; throw new JobsRequestError('The runner did not respond.', 503) }
      return served(path)
    }) as never)
    showPage()
    const stop = await screen.findByRole('alert')
    expect(stop.textContent).toBe('Reading the runner’s journal stopped after entry 1,500: The runner did not respond. Entries read: 1,500. Its entries are shown once it is read to its end. Until then, records stand by when the runner first recorded each.')
    expect(journalRows().length).toBe(0)
    fireEvent.click(screen.getByRole('button', { name: 'Continue from entry 1,500' }))
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 3,000.')).toBeTruthy()
    expect([asked(events(0)), asked(events(1500)), journalRows().length].join(' ')).toBe('1 2 500')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('says a reading stopped before its first entry, and tries again from the start', async () => {
    const served = runner({ runs: [], occurrences: [], journal: { entries: busy(10) } })
    let failed = false
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      if (path === events(0) && !failed) { failed = true; throw new JobsRequestError('The runner did not respond.', 503) }
      return served(path)
    }) as never)
    showPage()
    expect((await screen.findByRole('alert')).textContent).toMatch(/^Reading the runner’s journal stopped before its first entry: The runner did not respond\. /)
    fireEvent.click(screen.getByRole('button', { name: 'Try again from its start' }))
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 10.')).toBeTruthy()
  })

  it('keeps showing the journal as last read to its end while it reads newer pages, and says so', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    const journal: StandInJournal = { entries: busy(100) }
    const answer = holding(journal, events(1100))
    showPage()
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 100.')).toBeTruthy()
    journal.entries!.push(...busy(2000, 101))
    await vi.advanceTimersByTimeAsync(5000)
    expect(await screen.findByText('Reading the runner’s journal. Entries read so far: 1,100. Until it is read to its end again, the table shows the journal as it was when last read to its end.')).toBeTruthy()
    expect(journalRows().length).toBe(100)
    answer()
    expect(await screen.findByText('The runner’s journal is read to its end as of the last request. Entries read: 2,100.')).toBeTruthy()
    expect(journalRows().length).toBe(500)
  })
})

describe('a record row', () => {
  it('says the same of a run’s state with the whole journal, a bounded one, and none', async () => {
    const run = runs.running
    const about = [
      { sequence: 1, kind: 'run.queued', from: null, to: 'queued' }, { sequence: 2, kind: 'run.started', from: 'queued', to: 'running' },
      { sequence: 3, kind: 'run.completed', from: 'running', to: 'completed', chainSequence: 7 }, { sequence: 4, kind: 'run.interrupted', from: 'running', to: 'interrupted' }
    ].map(entry => ({ ...entry, entryVersion: '1', at: second(entry.sequence), by: { kind: 'runner' }, concerns: { job: JOB, release: release.id, run: run.id } }))
    const journal = { entries: [...about, ...busy(2000, 5)] }
    const said = async (setting: StandInJournal | 'absent', limits?: JournalLimits) => {
      vi.mocked(jobsAPI).mockImplementation(runner({ runs: [run], occurrences: [], journal: setting }) as never)
      const { unmount } = showTab(limits)
      let text: string | null | undefined
      await waitFor(() => { text = bodyRows().find(row => row.getAttribute('data-kind') === 'run')?.textContent; expect(text).toMatch(/Present: /) })
      if (setting !== 'absent') await screen.findByText(/read to its end as of the last request/)
      unmount(); cleanup()
      return text
    }
    const none = await said('absent'), whole = await said(journal), bounded = await said(journal, { keep: 1000, window: 500, step: 500 })
    expect(whole).toBe(none)
    expect(bounded).toBe(none)
    expect(none).toContain('Run started')
    expect(none).not.toMatch(/completed|interrupted/i)
  })
})

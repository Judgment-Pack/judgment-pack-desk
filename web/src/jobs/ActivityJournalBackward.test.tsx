/**
 * The Activity tab reading Runner's journal from its newest entries (#235),
 * from a stand-in that serves Runner v0.8.0's `journal-backward`: the newest
 * entries after one request for them; older pages only when asked for, each
 * held to the record of the page before it; new entries followed forward while
 * older ones load, with no gap and none twice; a page that does not fit
 * refused and said; and every record row standing where it stood.
 */
import { cleanup, configure, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ActivityView } from './ActivityView'
import { jobsAPI, JobsRequestError } from './client'
import { forgetRunnerCapabilities, JOURNAL_LIMITS, type JournalLimits } from './journal'
import { backwardPage, JOB, occurrences, release, runner, runs, type StandInJournal } from './__fixtures__/activity'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
configure({ asyncUtilTimeout: 20000 })
beforeEach(() => { forgetRunnerCapabilities() })
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); document.body.innerHTML = '' })

const show = (limits: JournalLimits = JOURNAL_LIMITS) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}?tab=activity`]}><Routes><Route path="/jobs/:jobId" element={<ActivityView jobId={JOB} release={release} limits={limits} />} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
const OTHER_RUN = 'run_' + 'f'.repeat(32)
const second = (sequence: number) => new Date(Date.UTC(2026, 9, 3, 0, 0, sequence)).toISOString()
/** Entries of a busy journal with sequences `first` to `last`, each a second apart, the second its sequence. */
function busy(last: number, first = 1) {
  return Array.from({ length: last - first + 1 }, (_, index) => ({
    sequence: first + index, entryVersion: '1', kind: 'run.started', at: second(first + index),
    by: { kind: 'runner' }, concerns: { job: JOB, release: release.id, run: OTHER_RUN }, from: 'queued', to: 'running'
  }))
}
const tail = `jobs/${JOB}/events?tail=true`
const before = (sequence: number) => `jobs/${JOB}/events?before=${sequence}`
const after = (sequence: number) => `jobs/${JOB}/events?after=${sequence}`
const eventRequests = () => vi.mocked(jobsAPI).mock.calls.map(([path]) => String(path)).filter(path => path.startsWith(`jobs/${JOB}/events`))
const serve = (journal: StandInJournal, records: { runs?: typeof runs[string][]; occurrences?: typeof occurrences[string][] } = { runs: [], occurrences: [] }) => {
  const stand = runner({ ...records, journal: { ...journal, backward: true } })
  vi.mocked(jobsAPI).mockImplementation(stand as never)
  return stand
}
const region = () => screen.getByRole('region', { name: 'Activity' })
const journalRows = () => [...region().querySelectorAll('tbody tr')].filter(row => row.getAttribute('data-kind') === 'journal')
/** The second each journal row shows, which is the entry's sequence, top to bottom. */
const sequences = () => journalRows().map(row => new Date(row.querySelector('time')!.getAttribute('datetime')!).getUTCSeconds() + 60 * new Date(row.querySelector('time')!.getAttribute('datetime')!).getUTCMinutes())
const lines = () => [...region().querySelectorAll('p.quiet, [role=alert], [role=status]')].map(node => node.textContent)

describe('the journal read from its newest entries', () => {
  it('shows the newest entries after one request for them, from a journal of thousands, and reads no more of it', async () => {
    serve({ entries: busy(3000) })
    show()
    expect(await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    expect(sequences()).toEqual(Array.from({ length: 50 }, (_, index) => 3000 - index))
    expect(eventRequests()).toEqual([tail])
    expect(vi.mocked(jobsAPI).mock.calls.filter(([path]) => path === 'status').length).toBe(1)
    expect(screen.getByRole('button', { name: 'Show earlier journal entries' })).toBeTruthy()
    expect(screen.queryByText(/read to its end/)).toBeNull()
  })

  it('walks back as far as the viewer asks, a step at a time, each entry once and in order', async () => {
    serve({ entries: busy(3000) })
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing the newest 150 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    expect(eventRequests()).toEqual([tail, before(2951), before(2901)])
    expect(sequences()).toEqual(Array.from({ length: 150 }, (_, index) => 3000 - index))
    // The page asks for no more than it was asked to.
    expect(screen.getByRole('button', { name: 'Show earlier journal entries' })).toBeTruthy()
  })

  it('reaches the first entry once each, and then offers nothing older and says the journal is shown whole', async () => {
    serve({ entries: busy(230) })
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing the newest 150 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Showing all 230 journal entries the runner holds for this job, as of the last request.')).toBeTruthy()
    expect(sequences()).toEqual(Array.from({ length: 230 }, (_, index) => 230 - index))
    expect(screen.queryByRole('button', { name: 'Show earlier journal entries' })).toBeNull()
    expect(eventRequests().filter(path => path === before(231) || path.includes('before=1&'))).toEqual([])
  })

  it('follows the journal forward from the newest page, and shows what arrived with none twice', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    const journal: StandInJournal = { entries: busy(200) }
    serve(journal)
    show()
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    journal.entries!.push(...busy(204, 201))
    await vi.advanceTimersByTimeAsync(5000)
    expect(await screen.findByText('Showing the newest 54 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    expect(eventRequests()).toEqual([tail, after(200)])
    expect(sequences()).toEqual(Array.from({ length: 54 }, (_, index) => 204 - index))
    await vi.advanceTimersByTimeAsync(5000)
    expect(eventRequests()).toEqual([tail, after(200), after(204)])
    expect(sequences().length).toBe(54)
  })

  it('merges older pages with entries that arrive while they load: no gap, none twice', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    const journal: StandInJournal = { entries: busy(500) }
    const stand = serve(journal)
    let release!: () => void
    const held = new Promise<void>(resolve => { release = resolve })
    let waited = false
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === before(451) && !waited) { waited = true; await held } return stand(path) }) as never)
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    expect(await screen.findByText('Reading earlier journal entries…')).toBeTruthy()
    // Entries arrive and are followed while the older page is still on its way.
    journal.entries!.push(...busy(503, 501))
    await vi.advanceTimersByTimeAsync(5000)
    await waitFor(() => expect(sequences()[0]).toBe(503))
    expect(sequences().length).toBe(53)
    release()
    expect(await screen.findByText('Showing the newest 153 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    const shown = sequences()
    expect(shown).toEqual(Array.from({ length: 153 }, (_, index) => 503 - index))
    expect(new Set(shown).size).toBe(shown.length)
  })

  it('stands each record at the entry that created it, and a record whose entry is not yet read below every entry', async () => {
    const run = runs.running, old = runs.signed
    const entries = [
      { ...busy(1)[0]!, sequence: 5, at: second(5), kind: 'run.queued', concerns: { job: JOB, release: release.id, run: old.id }, from: null, to: 'queued' },
      ...busy(80, 6),
      { ...busy(1)[0]!, sequence: 81, at: second(81), kind: 'run.queued', concerns: { job: JOB, release: release.id, run: run.id }, from: null, to: 'queued' },
      ...busy(100, 82)
    ]
    serve({ entries }, { runs: [run, old], occurrences: [] })
    show()
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    // The newest page is entries 51 to 100: the entry that queued the running run is in it, the other's is not read.
    const order = [...region().querySelectorAll('tbody tr')].map(row => row.getAttribute('data-kind'))
    // Entries 100 to 82, the run entry 81 queued, entries 81 to 51 ...
    expect(order.length).toBe(52)
    expect(order.indexOf('run')).toBe(19)
    expect(order.slice(0, 19).every(kind => kind === 'journal') && order.slice(20, 51).every(kind => kind === 'journal')).toBe(true)
    // ... and the run whose entry, 5, is not yet read, below every entry.
    expect(order.at(-1)).toBe('run')
  })
})

describe('a page that does not fit what was read', () => {
  const withPage = (journal: StandInJournal, change: (path: string, page: ReturnType<typeof backwardPage>) => unknown) => {
    const stand = runner({ runs: [], occurrences: [], journal: { ...journal, backward: true } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      const answer = await stand(path)
      return path.startsWith(`jobs/${JOB}/events?before=`) ? change(path, answer as ReturnType<typeof backwardPage>) : answer
    }) as never)
  }
  const olderStopped = async (journal: StandInJournal, change: Parameters<typeof withPage>[1]) => {
    withPage(journal, change)
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    return (await screen.findByRole('alert')).textContent
  }
  const REFUSED = 'Reading the runner’s journal stopped: The runner’s journal page did not fit the entries already read, so it was not used. Journal entries read: 50. Earlier entries are not yet read.'

  it('is refused and said, not merged, when it overlaps what is held', async () => {
    expect(await olderStopped({ entries: busy(300) }, (_, page) => ({ ...page, items: busy(251, 200), before: 200, next: 251 }))).toBe(REFUSED)
    expect(sequences().length).toBe(50)
    expect(sequences()[0]).toBe(300)
  })
  it('is refused when its cursor is not the sequence of its first entry', async () => {
    expect(await olderStopped({ entries: busy(300) }, (_, page) => ({ ...page, before: page.before + 1 }))).toBe(REFUSED)
    expect(sequences().length).toBe(50)
  })
  it('is refused when it is from another journal', async () => {
    expect(await olderStopped({ entries: busy(300) }, (_, page) => ({ ...page, journalBegan: '2026-01-01T00:00:00Z' }))).toBe(REFUSED)
  })
  it('is refused when it says older entries exist and holds none, or an entry before the journal’s first', async () => {
    expect(await olderStopped({ entries: busy(300) }, () => ({ journalBegan: second(0), items: [], next: 0, more: true, before: 251 }))).toBe(REFUSED)
  })
  it('is refused when its entries are not in order or have no sequence', async () => {
    expect(await olderStopped({ entries: busy(300) }, (_, page) => ({ ...page, items: [...page.items].reverse() }))).toBe(REFUSED)
    cleanup(); forgetRunnerCapabilities()
    expect(await olderStopped({ entries: busy(300) }, (_, page) => ({ ...page, items: (page.items as unknown as { sequence: number }[]).map(({ sequence: _s, ...rest }) => rest) }))).toBe(REFUSED)
  })
  it('is tried again from the cursor held, which a refused page did not move', async () => {
    let refuse = true
    const stand = runner({ runs: [], occurrences: [], journal: { entries: busy(300), backward: true } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      const answer = await stand(path)
      if (path === before(251) && refuse) { refuse = false; return { ...(answer as object), before: 1 } }
      return answer
    }) as never)
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    await screen.findByRole('alert')
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('Showing the newest 150 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    expect(sequences()).toEqual(Array.from({ length: 150 }, (_, index) => 300 - index))
  })
  it('is refused when the newest page does not carry its own cursor, and nothing is shown', async () => {
    const stand = runner({ runs: [], occurrences: [], journal: { entries: busy(300), backward: true } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { const answer = await stand(path); return path === tail ? { ...(answer as object), before: 0 } : answer }) as never)
    show()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toMatch(/^Reading the runner’s journal stopped before any entry was read: The runner’s journal page did not fit/)
    expect(journalRows().length).toBe(0)
  })
})

describe('what cannot be read now is not what is not there', () => {
  it('stops, and asks again, when Runner’s status cannot be read, and reads no journal in the meantime', async () => {
    const stand = runner({ runs: [], occurrences: [], journal: { entries: busy(100), backward: true } })
    let failed = false
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === 'status' && !failed) { failed = true; throw new JobsRequestError('The runner did not respond.', 503) } return stand(path) }) as never)
    show()
    expect((await screen.findByRole('alert')).textContent).toMatch(/^Reading the runner’s journal stopped before its first entry: The runner did not respond\./)
    expect(eventRequests()).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Try again from its start' }))
    expect(await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')).toBeTruthy()
    expect(eventRequests()).toEqual([tail])
  })
  it('says nothing is shown where the first newest-page request fails, and then reads it', async () => {
    const stand = runner({ runs: [], occurrences: [], journal: { entries: busy(100), backward: true } })
    let failed = false
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === tail && !failed) { failed = true; throw new JobsRequestError('The runner did not respond.', 503) } return stand(path) }) as never)
    show()
    expect((await screen.findByRole('alert')).textContent).toBe('Reading the runner’s journal stopped before any entry was read: The runner did not respond.')
    expect(journalRows().length).toBe(0)
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')).toBeTruthy()
  })
  it('keeps what it shows, and says what is not yet read, when an older page cannot be read', async () => {
    const stand = runner({ runs: [], occurrences: [], journal: { entries: busy(300), backward: true } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === before(251)) throw new JobsRequestError('The runner did not respond.', 503); return stand(path) }) as never)
    show({ keep: 20000, window: 100, step: 100 })
    await screen.findByText('Showing the newest 50 journal entries. Earlier entries are not yet read.')
    fireEvent.click(await screen.findByRole('button', { name: 'Show earlier journal entries' }))
    expect((await screen.findByRole('alert')).textContent).toBe('Reading the runner’s journal stopped: The runner did not respond. Journal entries read: 50. Earlier entries are not yet read.')
    expect(journalRows().length).toBe(50)
    expect(lines().some(line => /Showing all/.test(line ?? ''))).toBe(false)
  })
  it('says a journal the runner serves none of for the job is absent only where the runner said 404', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [], journal: 'absent' }) as never)
    show()
    expect(await screen.findByText(/serves no journal/)).toBeTruthy()
  })
})

/**
 * What the Activity tab says of how far Runner's journal is read, from a
 * Runner that lists no `journal-backward` capability (#235): byte for byte as
 * it said in #228, in every state of a reading. Each case collects every
 * sentence and button of the tab outside the table, and compares them with the
 * lines recorded from the tab before Desk could read a journal from its newest
 * entries.
 */
import { cleanup, configure, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ActivityView } from './ActivityView'
import { jobsAPI, JobsRequestError } from './client'
import { forgetRunnerCapabilities } from './journal'
import { when } from './RunRecord'
import { at, JOB, release, runner } from './__fixtures__/activity'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
configure({ asyncUtilTimeout: 20000 })
beforeEach(() => { forgetRunnerCapabilities() })
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); document.body.innerHTML = '' })

const show = (limits = { keep: 20000, window: 500, step: 500 }) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}?tab=activity`]}><Routes><Route path="/jobs/:jobId" element={<ActivityView jobId={JOB} release={release} limits={limits} />} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
const OTHER_RUN = 'run_' + 'f'.repeat(32)
const busy = (count: number) => Array.from({ length: count }, (_, index) => ({
  sequence: index + 1, entryVersion: '1', kind: 'run.started', at: new Date(Date.UTC(2026, 9, 3, 0, 0, index + 1)).toISOString(),
  by: { kind: 'runner' }, concerns: { job: JOB, release: release.id, run: OTHER_RUN }, from: 'queued', to: 'running'
}))
const events = (after: number) => `jobs/${JOB}/events?after=${after}`
/** Every sentence and button of the tab outside its table and filters. */
const lines = () => [...screen.getByRole('region', { name: 'Activity' }).querySelectorAll('p, button')].filter(node => !node.closest('table') && !node.closest('select') && node.getAttribute('role') !== 'combobox').map(node => node.textContent)
const serve = (journal: NonNullable<Parameters<typeof runner>[0]>['journal']) => vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [], journal }) as never)

describe('the Activity tab’s lines on how far the journal is read, from a Runner without journal-backward', () => {
  it('has read a short journal to its end, and says nothing was filled in before it began', async () => {
    serve({ began: at(30), entries: busy(120) })
    show()
    await screen.findByText(/read to its end as of the last request/)
    await waitFor(() => expect(screen.getAllByRole('row').length).toBeGreaterThan(100))
    expect(lines()).toEqual(GOLDEN.short)
  })
  it('has read a journal past its bound', async () => {
    serve({ entries: busy(900) })
    show({ keep: 300, window: 100, step: 100 })
    await screen.findByText(/Earlier entries read and not kept/)
    expect(lines()).toEqual(GOLDEN.bounded)
  })
  it('stopped after an entry, and before its first', async () => {
    const served = runner({ runs: [], occurrences: [], journal: { entries: busy(300) } })
    let failures = 0
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => {
      if (path === events(100) && failures === 0) { failures++; throw new JobsRequestError('The runner did not respond.', 503) }
      return served(path)
    }) as never)
    show()
    await screen.findByRole('alert')
    expect(lines()).toEqual(GOLDEN.stoppedAfter)
    cleanup()
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === events(0)) throw new JobsRequestError('The runner did not respond.', 503); return served(path) }) as never)
    show()
    await screen.findByRole('alert')
    expect(lines()).toEqual(GOLDEN.stoppedBefore)
  })
  it('is reading', async () => {
    const served = runner({ runs: [], occurrences: [], journal: { entries: busy(300) } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => path === events(100) ? new Promise(() => {}) : served(path)) as never)
    show()
    await screen.findByText(/Entries read so far/)
    expect(lines()).toEqual(GOLDEN.reading)
  })
  it('has no journal to read, and shows it only with no filter set', async () => {
    serve('absent')
    show()
    await screen.findByText(/serves no journal/)
    expect(lines()).toEqual(GOLDEN.absent)
    cleanup()
    serve({ entries: busy(10) })
    show()
    await screen.findByText(/read to its end as of the last request/)
    fireEvent.click(screen.getByRole('combobox', { name: 'Kind' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Runs' }))
    await screen.findByText(/shown when no filter is set/)
    expect(lines()).toEqual(GOLDEN.filtered)
  })
})

/** The tab's first sentence, which these lines follow. */
const INTRO = 'What the local runner recorded about this job, newest first: the entries of its journal of job activity in the order the runner wrote them, and its records, each where the entry that created it stands, or by when the runner first recorded it where the journal has no such entry. Every time shown is one the runner stored, named for what it records. The journal is the runner’s own log, not chained and not signed. Evidence says only whether the runner holds an item; nothing here checks it.'
const BEFORE_FIRST = 'Its entries are shown once it is read to its end. Until then, records stand by when the runner first recorded each.'
const GOLDEN: Record<string, (string | null)[]> = {
  short: [INTRO, 'The runner’s journal is read to its end as of the last request. Entries read: 120.', `No journal before ${when(at(30))}: the runner’s journal of job activity began then. Nothing earlier has an entry, and nothing is filled in from its records.`],
  bounded: [INTRO, 'The runner’s journal is read to its end as of the last request. Entries read: 900.', 'Earlier entries read and not kept, to bound what this page holds: 600. Only the newest 300 are kept.', 'Showing the newest 100 of 900 journal entries read.', 'Show earlier journal entries'],
  stoppedAfter: [INTRO, `Reading the runner’s journal stopped after entry 100: The runner did not respond. Entries read: 100. ${BEFORE_FIRST}`, 'Continue from entry 100', 'The runner has recorded nothing for this job yet.'],
  stoppedBefore: [INTRO, `Reading the runner’s journal stopped before its first entry: The runner did not respond. ${BEFORE_FIRST}`, 'Try again from its start', 'The runner has recorded nothing for this job yet.'],
  reading: [INTRO, `Reading the runner’s journal. Entries read so far: 100. ${BEFORE_FIRST}`, 'The runner has recorded nothing for this job yet.'],
  absent: [INTRO, 'The local runner serves no journal of job activity for this job, so only its records are shown.', 'The runner has recorded nothing for this job yet.'],
  filtered: [INTRO, 'The runner’s journal is read to its end as of the last request. Entries read: 10.', 'The runner’s journal is shown when no filter is set.', 'Nothing the runner recorded matches these filters.']
}

/**
 * The Activity tab reading Runner's journal of job activity (#218), from a
 * stand-in Runner that serves the journal as Runner v0.7.0 does: every kind
 * with its recorded time, source and initiator; record rows at the place of the
 * entry that created them, and saying what they said without the journal;
 * paging through Runner's cursor, and asking again no faster than every five
 * seconds; where the journal begins; a runner that serves none; and entries
 * this Desk does not know or cannot read.
 */
import { cleanup, configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { JobsContent } from './JobsView'
import { jobsAPI } from './client'
import { when } from './RunRecord'
import { at, JOB, occurrences, release, runner, runs, TRIGGER, type StandInJournal } from './__fixtures__/activity'
import everyKind from './__fixtures__/runner-v0.7.0-journal-entries.json'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
configure({ asyncUtilTimeout: 5000 })
beforeEach(() => { vi.mocked(jobsAPI).mockImplementation(runner() as never) })
afterEach(() => { cleanup(); vi.useRealTimers(); vi.resetAllMocks(); document.body.innerHTML = '' })

function show() {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}?tab=activity`]}><Routes><Route path="/jobs/:jobId" element={<JobsContent />} /><Route path="/jobs/:jobId/runs/:runId" element={<p>run page</p>} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
}
function serve(journal: StandInJournal | 'absent', records: { runs?: typeof runs[string][]; occurrences?: typeof occurrences[string][] } = { runs: [], occurrences: [] }) {
  vi.mocked(jobsAPI).mockImplementation(runner({ ...records, journal }) as never)
}
const table = () => screen.findByRole('table')
const bodyRows = async () => [...(await table()).querySelectorAll('tbody tr')]
const journalRows = async () => (await bodyRows()).filter(row => row.getAttribute('data-kind') === 'journal')
const text = (row: Element, column: number) => row.querySelectorAll('td')[column]!.textContent
/** A row in a sentence: its source and what it says, as a reader scans the table. */
const line = (row: Element) => `${text(row, 1)}: ${row.querySelector('button')!.textContent}`
const asked = (path: string) => vi.mocked(jobsAPI).mock.calls.filter(([called]) => called === path).length
const events = (after: number) => `jobs/${JOB}/events?after=${after}`

const run2 = runs.running.id, occ1 = occurrences.submitted.id
const entry = (sequence: number, kind: string, extra: Record<string, unknown> = {}) => ({ sequence, entryVersion: '1', kind, at: at(sequence), by: { kind: 'runner' }, concerns: { job: JOB, release: release.id }, ...extra })

/** Each kind of Runner v0.7.0 as the tab words it. */
const WORDS: Record<string, string> = {
  'trigger.configured': 'Trigger configured', 'trigger.paused': 'Trigger paused', 'trigger.resumed': 'Trigger resumed', 'trigger.key-rotated': 'Trigger key rotated',
  'occurrence.received': 'Occurrence received', 'occurrence.skipped': 'Occurrence skipped', 'occurrence.preparing': 'Occurrence preparing', 'occurrence.ready': 'Occurrence ready',
  'occurrence.submitted': 'Occurrence submitted', 'occurrence.failed': 'Occurrence failed', 'occurrence.expired': 'Occurrence expired', 'occurrence.needs-attention': 'Occurrence needs attention',
  'occurrence.cancelled': 'Occurrence canceled', 'occurrence.reconciled': 'Occurrence reconciled',
  'run.queued': 'Run queued', 'run.started': 'Run started', 'run.expired': 'Run expired in the queue', 'run.completed': 'Run completed', 'run.failed': 'Run failed', 'run.interrupted': 'Run interrupted',
  'release.previewed': 'Release previewed', 'job.created': 'Job created', 'admission.refused': 'Request to start work refused',
  'journal.began': 'Journal began', 'runner.started': 'Runner started', 'runner.stopped': 'Runner stopped'
}
/** Who Runner says initiated an entry, as the tab says it: never a person. */
function initiator(by: { kind: string; trigger?: string; revision?: number; keyRevision?: number | null; connection?: string }) {
  const trigger = `Trigger ${by.trigger?.slice(-8)}`
  return by.kind === 'installation' ? 'This installation'
    : by.kind === 'trigger' ? `${trigger}, trigger revision ${by.revision}`
      : by.kind === 'trigger-credential' ? (by.keyRevision === null ? `Event token of ${trigger}, issued before the journal began` : `Event token of ${trigger}, issued at trigger revision ${by.keyRevision}`)
        : by.kind === 'cloud-connection' ? `Cloud connection ${by.connection}` : 'The runner itself'
}

describe('the journal in the Activity tab', () => {
  it('shows every kind Runner writes, each with the time Runner recorded it, its source, and who Runner says initiated it', async () => {
    // Runner's own fixture of every kind, and the identity forms it leaves out.
    const extra = [
      { ...everyKind[24]!, sequence: 27, at: '2026-10-06T09:27:00.000001Z', by: { kind: 'cloud-connection', connection: 'pubsub-intake' }, request: 'cloud-signal', status: undefined, code: undefined },
      { ...everyKind[8]!, sequence: 28, at: '2026-10-06T09:28:00.000001Z', by: { kind: 'trigger-credential', trigger: (everyKind[8]!.concerns as { trigger: string }).trigger, keyRevision: null } }
    ]
    const served = [...everyKind, ...extra]
    serve({ began: everyKind[0]!.at, entries: served })
    show()
    const rows = await journalRows()
    expect(rows.length).toBe(served.length)
    // Newest first, in Runner's order.
    for (const [index, row] of [...rows].reverse().entries()) {
      const recorded = served[index]!
      expect(line(row)).toBe(`Journal entry: ${WORDS[recorded.kind]}`)
      expect(text(row, 0)).toBe(`Recorded ${when(recorded.at)}`)
      expect(row.querySelector('time')!.getAttribute('datetime')).toBe(recorded.at)
      expect(text(row, 3)).toBe(initiator(recorded.by as never))
    }
    expect(screen.queryByText(/does not know/)).toBeNull()
  })

  it('says what each entry records beside its kind: a trigger’s revision, a refusal’s answer, the reason, the run', async () => {
    serve({ entries: [
      entry(1, 'trigger.configured', { by: { kind: 'installation', owner: 'local-owner' }, concerns: { job: JOB, release: release.id, trigger: TRIGGER }, revision: 4, previousRevision: 3, from: 'enabled', to: 'paused' }),
      entry(2, 'admission.refused', { by: { kind: 'trigger-credential', trigger: TRIGGER, keyRevision: 2 }, concerns: { job: JOB, release: release.id, trigger: TRIGGER }, request: 'deliver-event', status: 409, code: 'trigger_paused', coalescedSeconds: 60 }),
      entry(3, 'occurrence.skipped', { by: { kind: 'trigger', trigger: TRIGGER, revision: 4 }, concerns: { job: JOB, release: release.id, trigger: TRIGGER, occurrence: occ1 }, from: null, to: 'skipped', reason: 'overlap' }),
      entry(4, 'run.failed', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'running', to: 'failed', reason: 'Runtime refused.\nSecond line.' })
    ] })
    show()
    const [failed, skipped, refused, configured] = await journalRows()
    expect(text(configured!, 2)).toBe('Trigger configuredNightly intake, trigger revision 4')
    expect(text(configured!, 3)).toBe('This installation')
    expect(text(refused!, 2)).toBe('Request to start work refuseddeliver-event · 409 · trigger_paused')
    expect(text(refused!, 3)).toBe('Event token of Nightly intake, issued at trigger revision 2')
    expect(text(skipped!, 2)).toBe('Occurrence skippedAnother run was pending')
    expect(text(skipped!, 3)).toBe('Nightly intake, trigger revision 4')
    expect(text(failed!, 2)).toBe(`Run failedRuntime refused.Run ${run2.slice(-8)}`)
    expect(within(failed!.querySelectorAll('td')[2]!).getByRole('link').getAttribute('href')).toBe(`/jobs/${JOB}/runs/${run2}`)
    expect(within(failed!.querySelectorAll('td')[4]!).getByRole('link', { name: 'Fixed version 1.2.0' })).toBeTruthy()
  })

  it('stands each record at the place of the entry that created it, and a record from before the journal below every entry', async () => {
    serve({ began: at(28), entries: [
      entry(1, 'occurrence.received', { concerns: { job: JOB, release: release.id, trigger: TRIGGER, occurrence: occ1 }, from: null, to: 'accepted' }),
      entry(2, 'run.queued', { concerns: { job: JOB, release: release.id, occurrence: occ1, run: run2 }, from: null, to: 'queued' }),
      entry(3, 'occurrence.submitted', { concerns: { job: JOB, release: release.id, occurrence: occ1, run: run2 }, from: 'accepted', to: 'submitted' }),
      entry(4, 'run.started', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'queued', to: 'running' })
    ] }, { runs: [runs.running, runs.signed], occurrences: [occurrences.submitted] })
    show()
    const shown = (await bodyRows()).map(line)
    expect(shown.join(' | ')).toBe([
      'Journal entry: Run started', 'Journal entry: Occurrence submitted',
      'Record: Run started', 'Journal entry: Run queued',
      'Record: Occurrence received', 'Journal entry: Occurrence received',
      // Submitted at :20, before the journal began at :28: it has no entry.
      'Record: Run completed: accept'
    ].join(' | '))
  })

  it('never changes what a record row says about a run’s state', async () => {
    // The whole row, once the run's own record has been read for what it holds.
    const said = async () => {
      let row: Element | undefined
      await waitFor(async () => { row = (await bodyRows()).find(row => row.getAttribute('data-kind') === 'run'); expect(row?.textContent).toMatch(/Present: /) })
      return row!.textContent
    }
    serve({}, { runs: [runs.running], occurrences: [] })
    const { unmount } = show()
    const without = await said()
    unmount(); cleanup()
    serve({ entries: [
      entry(1, 'run.queued', { concerns: { job: JOB, release: release.id, run: run2 }, from: null, to: 'queued' }),
      entry(2, 'run.started', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'queued', to: 'running' }),
      // Entries Runner's record of the run does not reflect yet, or ever.
      entry(3, 'run.completed', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'running', to: 'completed', chainSequence: 7 }),
      entry(4, 'run.interrupted', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'running', to: 'interrupted', interruption: { seen: false, lastKnownRunning: at(30, 5), restart: 5 } })
    ] }, { runs: [runs.running], occurrences: [] })
    show()
    await waitFor(async () => expect((await journalRows()).length).toBe(4))
    expect(await said()).toBe(without)
    expect(without).toContain('Run started')
    expect(without).not.toMatch(/completed|interrupted/i)
  })

  it('pages through Runner’s cursor, and shows no entry until it has caught up', async () => {
    const entries = [1, 2, 3, 4, 5].map(sequence => entry(sequence, 'run.started', { concerns: { job: JOB, release: release.id, run: run2 } }))
    serve({ entries, size: 2 })
    show()
    await waitFor(async () => expect((await journalRows()).length).toBe(5))
    expect([0, 2, 4].map(after => asked(events(after))).join(' ')).toBe('1 1 1')
    expect((await journalRows()).map(row => row.querySelector('time')!.getAttribute('datetime')).join(' ')).toBe([5, 4, 3, 2, 1].map(minute => at(minute)).join(' '))
  })

  it('asks again with the cursor it holds, never from 0, and no faster than every five seconds', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    const times: number[] = []
    const served = runner({ runs: [], occurrences: [], journal: { entries: [entry(1, 'job.created'), entry(2, 'trigger.configured')] } })
    vi.mocked(jobsAPI).mockImplementation((async (path: string) => { if (path === events(2)) times.push(Date.now()); return served(path) }) as never)
    show()
    await waitFor(async () => expect((await journalRows()).length).toBe(2))
    await vi.advanceTimersByTimeAsync(16000)
    expect(asked(events(0))).toBe(1)
    expect(times.length).toBeGreaterThanOrEqual(3)
    const gaps = times.slice(1).map((time, index) => time - times[index]!)
    expect(gaps.filter(gap => gap < 5000).join(' ')).toBe('')
  })
})

describe('where the journal begins', () => {
  it('says there is no journal before it when the job is older', async () => {
    serve({ began: at(5) }, { runs: [runs.signed], occurrences: [] })
    show(); await table()
    expect(screen.getByText(`No journal before ${when(at(5))}: the runner’s journal of job activity began then. Nothing earlier has an entry, and nothing is filled in from its records.`)).toBeTruthy()
  })

  it('says nothing of the kind when the journal began with the job', async () => {
    serve({ began: at(0) }, { runs: [runs.signed], occurrences: [] })
    show(); await table()
    expect(screen.queryByText(/No journal before|when its journal began/)).toBeNull()
  })

  it('says it cannot tell when the runner’s time of it does not read as one', async () => {
    serve({ began: 'soon' }, { runs: [runs.signed], occurrences: [] })
    show(); await table()
    expect(screen.getByText('The runner did not say when its journal began in a time this Desk can read. Nothing before it has an entry.')).toBeTruthy()
  })
})

describe('a runner that serves no journal', () => {
  it('shows the records as before, says the runner serves no journal, and does not ask again', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    serve('absent', {})
    show()
    expect(await screen.findByText('The local runner serves no journal of job activity for this job, so only its records are shown.')).toBeTruthy()
    expect((await bodyRows()).length).toBe(Object.values(runs).length + Object.values(occurrences).length)
    expect((await journalRows()).length).toBe(0)
    expect(screen.queryByRole('alert')).toBeNull()
    await vi.advanceTimersByTimeAsync(11000)
    expect(asked(events(0))).toBe(1)
  })
})

describe('an entry this Desk cannot word or read', () => {
  it('shows a kind it does not know by its name, with its time, and never drops it', async () => {
    serve({ entries: [entry(1, 'run.paused', { by: { kind: 'operator', name: 'someone' } })] })
    show()
    const [row] = await journalRows()
    expect(line(row!)).toBe('Journal entry: An entry of a kind this Desk does not know: run.paused')
    expect(text(row!, 0)).toBe(`Recorded ${when(at(1))}`)
    expect(text(row!, 3)).toBe('operator')
  })

  it('says “Not recorded” for a time it cannot read and an initiator Runner did not name, and keeps an item that is no entry', async () => {
    vi.mocked(jobsAPI).mockImplementation(runner({ runs: [], occurrences: [], pages: {
      [events(0)]: { journalBegan: at(0), items: [{ sequence: 1, kind: 'run.started' }, { sequence: 2, kind: 'run.started', at: 'yesterday', by: { kind: 'runner' } }, null], next: 2, more: false }
    } }) as never)
    show()
    const [nothing, unreadable, absent] = await journalRows()
    expect([text(absent!, 0), text(absent!, 3)].join(' / ')).toBe('Recorded Not recorded / Not recorded')
    expect([text(unreadable!, 0), text(unreadable!, 3)].join(' / ')).toBe('Recorded Not recorded / The runner itself')
    expect(line(nothing!)).toBe('Journal entry: An entry of a kind this Desk does not know: null')
    expect((await table()).querySelectorAll('time').length).toBe(0)
  })
})

describe('a journal row', () => {
  it('opens the entry in the pane: its source, time, initiator and the entry as Runner sent it', async () => {
    const recorded = entry(1, 'run.completed', { concerns: { job: JOB, release: release.id, run: run2 }, from: 'running', to: 'completed', chainSequence: 7, reason: 'Completed.' })
    serve({ entries: [recorded] })
    show(); await table()
    fireEvent.click(screen.getByRole('button', { name: 'Run completed' }))
    const pane = await screen.findByRole('region', { name: 'Run completed' })
    const fact = (term: string) => within(pane).getByText(term, { selector: 'dt' }).nextElementSibling!.textContent
    expect([fact('Source'), fact('Recorded'), fact('By'), fact('Run')].join(' / ')).toBe(`Journal entry / ${when(at(1))} / The runner itself / Run ${run2.slice(-8)}`)
    const json = within(pane).getByText('Journal entry (JSON)').closest('details')!
    json.open = true; fireEvent(json, new Event('toggle'))
    expect(JSON.parse(json.querySelector('pre')!.textContent!)).toEqual(recorded)
  })

  it('is not shown while a filter is set, and the tab says so', async () => {
    serve({ entries: [entry(1, 'run.queued', { concerns: { job: JOB, release: release.id, run: run2 }, from: null, to: 'queued' })] }, { runs: [runs.running], occurrences: [] })
    show()
    await waitFor(async () => expect((await journalRows()).length).toBe(1))
    fireEvent.keyDown(screen.getByRole('combobox', { name: 'Kind' }), { key: 'Enter' })
    fireEvent.click(await screen.findByRole('option', { name: 'Runs' }))
    expect(await screen.findByText('The runner’s journal is shown when no filter is set.')).toBeTruthy()
    await waitFor(async () => expect((await bodyRows()).map(line).join(' | ')).toBe('Record: Run started'))
  })
})

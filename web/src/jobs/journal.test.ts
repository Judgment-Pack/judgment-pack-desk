/**
 * Runner's journal of job activity as the Activity tab reads it (#218): the
 * kinds it words against the pinned Runner's own list, the reading through
 * Runner's cursor, where each record's creation stands, and when the tab says
 * the journal began after the job.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { beforeTheJournal, catchUp, creations, emptyJournal, entryOf, JOURNAL_KINDS, JOURNAL_LIMITS, JOURNAL_REFRESH_MS, JOURNAL_SOURCE, kindText, readCount, WORDED_KINDS, yieldToBrowser, type Journal, type JournalPage } from './journal'
import runnerKinds from './__fixtures__/runner-v0.6.0-journal-kinds.json'
import everyKind from './__fixtures__/runner-v0.6.0-journal-entries.json'

const missingFrom = (list: readonly string[], from: readonly string[]) => list.filter(item => !from.includes(item))

describe('the kinds the tab words', () => {
  it('are exactly the kinds the pinned Runner’s openapi.json lists, at the tag the component lock pins', () => {
    expect(runnerKinds.tag).toBe(JOURNAL_SOURCE)
    expect(missingFrom(runnerKinds.kinds, WORDED_KINDS)).toEqual([])
    expect(missingFrom(WORDED_KINDS, runnerKinds.kinds)).toEqual([])
    expect(JOURNAL_KINDS.length).toBe(runnerKinds.kinds.length)
    // A Desk that pins a later Runner fails here until this list is read again
    // from that Runner's openapi.json and every kind it adds is worded.
    const lock = JSON.parse(readFileSync(join(import.meta.dirname, '../../../internal/releaseplan/components.json'), 'utf8'))
    expect(lock.components.runner.version).toBe(runnerKinds.tag)
  })

  it('cover the fixture of every kind that Runner’s own contract test reads', () => {
    expect(new Set(everyKind.map((entry: { kind: string }) => entry.kind)).size).toBe(runnerKinds.kinds.length)
    expect(missingFrom(everyKind.map((entry: { kind: string }) => entry.kind), WORDED_KINDS)).toEqual([])
  })

  it('name a kind this Desk does not know by Runner’s own string, and never by nothing', () => {
    expect(kindText('run.paused')).toBe('An entry of a kind this Desk does not know: run.paused')
    expect(kindText(undefined)).toBe('An entry of a kind this Desk does not know: null')
    expect(kindText(7)).toBe('An entry of a kind this Desk does not know: 7')
    expect(kindText('toString')).toBe('An entry of a kind this Desk does not know: toString')
    for (const kind of JOURNAL_KINDS) expect(kindText(kind)).not.toContain('does not know')
  })
})

describe('reading the journal', () => {
  const page = (items: number[], next: number, more: boolean): JournalPage => ({ journalBegan: '2026-10-03T09:00:00Z', items: items.map(sequence => ({ sequence, kind: 'run.started' })), next, more })
  const sequences = (journal: Journal) => journal.entries.map(entry => entry.sequence).join(' ')
  const nothing = () => {}, now = async () => {}

  it('follows Runner’s next while another page is there, from 0 the first time', async () => {
    const read = vi.fn(async (after: number) => after === 0 ? page([1, 2], 2, true) : after === 2 ? page([3], 3, false) : page([], after, false))
    const journal = emptyJournal()
    await catchUp(journal, read, 100, nothing, now)
    expect(read.mock.calls.map(([after]) => after).join(' ')).toBe('0 2')
    expect(sequences(journal)).toBe('1 2 3')
    expect([journal.next, journal.began, readCount(journal)]).toEqual([3, '2026-10-03T09:00:00Z', 3])
  })

  it('asks again with the cursor it holds, never from 0, and adds what is new', async () => {
    const journal: Journal = { entries: [{ sequence: 1 }, { sequence: 2 }], dropped: 0, next: 2, began: '2026-10-03T09:00:00Z' }
    const read = vi.fn(async (after: number) => after === 2 ? page([3, 4], 4, false) : page([], after, false))
    await catchUp(journal, read, 100, nothing, now)
    expect(read.mock.calls.map(([after]) => after).join(' ')).toBe('2')
    expect(sequences(journal)).toBe('1 2 3 4')
  })

  it('stops where a page moves the cursor nowhere, whatever it says of more', async () => {
    const read = vi.fn(async (after: number) => page([], after, true))
    const journal = { ...emptyJournal(), next: 9 }
    await catchUp(journal, read, 100, nothing, now)
    expect(read).toHaveBeenCalledTimes(1)
    expect(journal.next).toBe(9)
  })

  it('asks for one page at a time, says how far it has read, and gives the browser a turn between pages', async () => {
    const events: string[] = []
    const read = vi.fn(async (after: number) => { events.push(`page ${after}`); return after < 6 ? page([after + 1, after + 2], after + 2, true) : page([], after, false) })
    const journal = emptyJournal()
    await catchUp(journal, read, 100, () => events.push(`read ${readCount(journal)}`), async () => { events.push('pause') })
    expect(events.join(', ')).toBe('page 0, read 2, pause, page 2, read 4, pause, page 4, read 6, pause, page 6')
  })

  it('keeps at most the bound, dropping the oldest and counting them', async () => {
    const read = vi.fn(async (after: number) => after < 3000 ? page(Array.from({ length: 50 }, (_, index) => after + index + 1), after + 50, after + 50 < 3000) : page([], after, false))
    const journal = emptyJournal()
    await catchUp(journal, read, 1000, nothing, now)
    expect([readCount(journal), journal.entries.length, journal.dropped, journal.next].join(' ')).toBe('3000 1000 2000 3000')
    expect([journal.entries[0]!.sequence, journal.entries.at(-1)!.sequence].join(' ')).toBe('2001 3000')
  })

  it('leaves what it read as it was when a page fails, so that reading continues from the cursor', async () => {
    let fail = true
    const read = vi.fn(async (after: number) => { if (after === 4 && fail) { fail = false; throw new Error('gone') } return after < 6 ? page([after + 1, after + 2], after + 2, true) : page([], after, false) })
    const journal = emptyJournal()
    await expect(catchUp(journal, read, 100, nothing, now)).rejects.toThrow('gone')
    expect([sequences(journal), journal.next].join(' / ')).toBe('1 2 3 4 / 4')
    await catchUp(journal, read, 100, nothing, now)
    expect(read.mock.calls.map(([after]) => after).join(' ')).toBe('0 2 4 4 6')
    expect(sequences(journal)).toBe('1 2 3 4 5 6')
  })

  it('reads nothing from a page answered after the reading was abandoned', async () => {
    const controller = new AbortController()
    const journal = emptyJournal()
    await expect(catchUp(journal, async () => { controller.abort(); return page([1], 1, false) }, 100, nothing, now, controller.signal)).rejects.toBeTruthy()
    expect(readCount(journal)).toBe(0)
  })

  it('yields to the browser through a new task, not a microtask', async () => {
    vi.useFakeTimers()
    try {
      let done = false
      void yieldToBrowser().then(() => { done = true })
      for (let turn = 0; turn < 5; turn++) await Promise.resolve()
      expect(done).toBe(false)
      await vi.advanceTimersByTimeAsync(0)
      expect(done).toBe(true)
    } finally { vi.useRealTimers() }
  })

  it('keeps an item that is not an entry as an entry with nothing recorded', () => {
    expect(entryOf(null)).toEqual({})
    expect(entryOf([1])).toEqual({})
    expect(entryOf({ kind: 'run.started' })).toEqual({ kind: 'run.started' })
  })

  it('asks again no faster than the record lists, every 5 seconds, and holds 20,000 entries at most, rendering 500 at a time', () => {
    expect(JOURNAL_REFRESH_MS).toBe(5000)
    expect(JOURNAL_LIMITS).toEqual({ keep: 20000, window: 500, step: 500 })
  })
})

describe('where a record’s creation stands', () => {
  const run = 'run_' + '1'.repeat(32), occurrence = 'occ_' + '2'.repeat(32)
  it('is a run’s run.queued, and an occurrence’s admission, from null', () => {
    const places = creations([
      { kind: 'occurrence.received', from: null, concerns: { occurrence } },
      { kind: 'occurrence.submitted', from: 'accepted', concerns: { occurrence, run } },
      // A run Runner queued for that occurrence names it too, and is not its admission.
      { kind: 'run.queued', from: null, concerns: { occurrence, run } },
      { kind: 'run.started', from: 'queued', concerns: { occurrence, run } }
    ])
    expect(places.get(`occurrence:${occurrence}`)).toBe(0)
    expect(places.get(`run:${run}`)).toBe(2)
    expect(places.size).toBe(2)
  })

  it('is no place for an entry that changed a record it did not create', () => {
    expect(creations([{ kind: 'occurrence.cancelled', from: 'waiting', concerns: { occurrence } }, { kind: 'run.completed', from: 'running', concerns: { run } }]).size).toBe(0)
  })
})

describe('whether the journal began after the job', () => {
  it('says so when the job was created first, or when either time does not read as one', () => {
    expect(beforeTheJournal('2026-10-03T10:05:00Z', '2026-10-03T10:00:00Z')).toBe(true)
    expect(beforeTheJournal('2026-10-03T10:00:00Z', '2026-10-03T10:00:00Z')).toBe(false)
    expect(beforeTheJournal('2026-10-03T09:00:00Z', '2026-10-03T10:00:00Z')).toBe(false)
    expect(beforeTheJournal('not a time', '2026-10-03T10:00:00Z')).toBe(true)
    expect(beforeTheJournal('2026-10-03T09:00:00Z', undefined)).toBe(true)
  })
})

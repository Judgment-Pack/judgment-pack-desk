/**
 * Runner's journal of job activity as the Activity tab reads it (#218): the
 * kinds it words against the pinned Runner's own list, the reading through
 * Runner's cursor, where each record's creation stands, and when the tab says
 * the journal began after the job.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { beforeTheJournal, creations, entryOf, JOURNAL_KINDS, JOURNAL_REFRESH_MS, JOURNAL_SOURCE, kindText, readJournal, WORDED_KINDS, type JournalPage } from './journal'
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
    expect(new Set(everyKind.map(entry => entry.kind)).size).toBe(runnerKinds.kinds.length)
    expect(missingFrom(everyKind.map(entry => entry.kind), WORDED_KINDS)).toEqual([])
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

  it('follows Runner’s next while another page is there, from 0 the first time', async () => {
    const read = vi.fn(async (after: number) => after === 0 ? page([1, 2], 2, true) : after === 2 ? page([3], 3, false) : page([], after, false))
    const journal = await readJournal(read)
    expect(read.mock.calls.map(([after]) => after).join(' ')).toBe('0 2')
    expect(journal.entries.map(entry => entry.sequence).join(' ')).toBe('1 2 3')
    expect([journal.next, journal.began]).toEqual([3, '2026-10-03T09:00:00Z'])
  })

  it('asks again with the cursor it holds, never from 0, and adds what is new', async () => {
    const prior = { entries: [{ sequence: 1 }, { sequence: 2 }], next: 2, began: '2026-10-03T09:00:00Z' }
    const read = vi.fn(async (after: number) => after === 2 ? page([3, 4], 4, false) : page([], after, false))
    const journal = await readJournal(read, prior)
    expect(read.mock.calls.map(([after]) => after).join(' ')).toBe('2')
    expect(journal.entries.map(entry => entry.sequence).join(' ')).toBe('1 2 3 4')
    // The reading it continued is not changed under the page that shows it.
    expect(prior.entries.length).toBe(2)
  })

  it('stops where a page moves the cursor nowhere, whatever it says of more', async () => {
    const read = vi.fn(async (after: number) => page([], after, true))
    const journal = await readJournal(read, { entries: [], next: 9, began: undefined })
    expect(read).toHaveBeenCalledTimes(1)
    expect(journal.next).toBe(9)
  })

  it('keeps an item that is not an entry as an entry with nothing recorded', () => {
    expect(entryOf(null)).toEqual({})
    expect(entryOf([1])).toEqual({})
    expect(entryOf({ kind: 'run.started' })).toEqual({ kind: 'run.started' })
  })

  it('asks again no faster than the record lists, every 5 seconds', () => {
    expect(JOURNAL_REFRESH_MS).toBe(5000)
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

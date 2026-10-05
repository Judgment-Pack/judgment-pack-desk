/**
 * The Activity tab's rows (#213): one per record, ordered by the time Runner
 * first recorded it, every time a stored one and named; who initiated it; what
 * the record holds; the filters; and the merge of Runner's two paged lists.
 */
import { createHash } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import { decodeBase64, evidenceOf, keeps, mergeActivity, occurrenceRow, requesterOf, runRow, sha256, statesFor, streamsFor, type ActivityFilter } from './activity'
import { at, occurrences, OWNER, RECORD_LINE, runs, summary, TRIGGER } from './__fixtures__/activity'
import type { Run } from './client'

const all: ActivityFilter = { kind: 'all', state: 'all', trigger: 'all' }
const names = (row: ReturnType<typeof runRow>) => row && [row.when.name, ...row.also.map(stamp => stamp.name)]

describe('a run row', () => {
  it('is ordered by when the run was submitted, and names each further time Runner stored', () => {
    expect(names(runRow(runs.queued))).toEqual(['submitted'])
    expect(names(runRow(runs.running))).toEqual(['submitted', 'started'])
    expect(names(runRow(runs.signed))).toEqual(['submitted', 'started', 'finished'])
    expect(runRow(runs.signed)!.also.map(stamp => stamp.at)).toEqual([runs.signed.startedAt, runs.signed.finishedAt])
  })

  it('names an interrupted run’s finish as the time the runner recorded the interruption', () => {
    const row = runRow(runs.interrupted)!
    expect(row.also.map(stamp => [stamp.name, stamp.at])).toEqual([['started', runs.interrupted.startedAt], ['interruption', runs.interrupted.finishedAt]])
    expect(row.also.some(stamp => stamp.name === 'finished')).toBe(false)
  })

  it('shows no start for a run Runner never started', () => {
    expect(names(runRow(runs.failed))).toEqual(['submitted', 'finished'])
  })

  it('is no row at all without the time that orders it, and never supplies one', () => {
    expect(runRow({ ...runs.signed, createdAt: undefined as unknown as string })).toBeUndefined()
    expect(runRow({ ...runs.signed, createdAt: 'not a time' })).toBeUndefined()
    // A later time that is missing or unreadable is left out, not replaced.
    expect(names(runRow({ ...runs.signed, finishedAt: undefined }))).toEqual(['submitted', 'started'])
    expect(names(runRow({ ...runs.signed, startedAt: 'never' }))).toEqual(['submitted', 'finished'])
  })
})

describe('an occurrence row', () => {
  it('is ordered by when Runner received it, and names the slot a schedule was for', () => {
    const row = occurrenceRow(occurrences.skipped)!
    expect([row.kind, row.state, row.when.name, row.when.at]).toEqual(['occurrence', 'skipped', 'received', occurrences.skipped.receivedAt])
    expect(row.also.map(stamp => [stamp.name, stamp.at])).toEqual([['scheduled', occurrences.skipped.scheduledAt]])
  })

  it('reads as received while it is admitted or submitted, and in its own state once it ended', () => {
    expect(occurrenceRow(occurrences.submitted)!.state).toBe('received')
    expect(occurrenceRow({ ...occurrences.submitted, state: 'accepted', runId: undefined })!.state).toBe('received')
    for (const state of ['skipped', 'expired', 'failed', 'cancelled'] as const) expect(occurrenceRow(occurrences[state])!.state).toBe(state)
  })

  it('is a preparation while it waits for sources or needs attention, with the time the preparation started', () => {
    for (const [fixture, state] of [[occurrences.waiting, 'waiting'], [occurrences.attention, 'needs-attention']] as const) {
      const row = occurrenceRow(fixture)!
      expect([row.kind, row.state]).toEqual(['preparation', state])
      expect(row.also.map(stamp => [stamp.name, stamp.at])).toEqual([['preparation-started', fixture.preparation!.startedAt]])
    }
    // A preparation that ended is its occurrence's row again.
    expect(occurrenceRow({ ...occurrences.waiting, state: 'cancelled' })!.kind).toBe('occurrence')
  })

  it('is no row without the time Runner received it', () => {
    expect(occurrenceRow({ ...occurrences.skipped, receivedAt: '' })).toBeUndefined()
    expect(occurrenceRow({ ...occurrences.waiting, receivedAt: undefined as unknown as string })).toBeUndefined()
  })
})

describe('who initiated a run', () => {
  it('is this installation, a trigger with the revision that fired, or not recorded; never a person', () => {
    expect(requesterOf(runs.signed)).toEqual({ kind: 'installation' })
    expect(requesterOf(runs.running)).toEqual({ kind: 'trigger', triggerId: TRIGGER, revision: 2, triggerKind: 'schedule' })
    expect(requesterOf(runs.legacy)).toEqual({ kind: 'unrecorded' })
    // A trigger origin for another trigger says nothing about this one's revision.
    expect(requesterOf({ requestedBy: 'trigger:' + TRIGGER, trigger: { ...runs.running.trigger!, triggerId: 'trg_other' } })).toEqual({ kind: 'trigger', triggerId: TRIGGER, revision: undefined, triggerKind: undefined })
    expect(OWNER.startsWith('local-owner:')).toBe(true)
  })
})

describe('what a run record holds', () => {
  it('is read from the run’s own record: a list strips it', () => {
    expect(evidenceOf(runs.signed)).toEqual({ inputs: true, recordBytes: true, sidecar: true, receipts: false })
    expect(evidenceOf(runs.unsigned)).toEqual({ inputs: true, recordBytes: true, sidecar: false, receipts: false })
    expect(evidenceOf(runs.queued)).toEqual({ inputs: true, recordBytes: false, sidecar: false, receipts: false })
    expect(evidenceOf(summary(runs.signed))).toEqual({ inputs: false, recordBytes: false, sidecar: false, receipts: false })
    // A run recorded before Runner kept the bytes still has its parsed record, and no bytes.
    expect(evidenceOf({ ...runs.signed, auditBytes: undefined, auditSignatures: undefined })).toEqual({ inputs: true, recordBytes: false, sidecar: false, receipts: false })
  })

  it('counts acquisition receipts only where the preparation cites one', () => {
    const preparation = { version: 2 as const, verifiedAt: at(1), mappingDigest: 'sha256:m', verification: 'v', lineage: [], outcomes: [] }
    expect(evidenceOf({ ...runs.signed, input: { facts: {}, preparation: { ...preparation, cites: [{ sessionId: 's', callIndex: 0, signature: 'x' }] } } }).receipts).toBe(true)
    expect(evidenceOf({ ...runs.signed, input: { facts: {}, preparation: { ...preparation, cites: [] } } }).receipts).toBe(false)
  })

  it('measures the bytes Runner sent, decoded from base64, not their encoding', async () => {
    const bytes = decodeBase64(runs.signed.auditBytes!)
    expect(new TextDecoder().decode(bytes)).toBe(RECORD_LINE)
    expect(await sha256(bytes)).toBe('sha256:' + createHash('sha256').update(RECORD_LINE).digest('hex'))
  })
})

describe('the filters', () => {
  it('offer the states of the chosen kind', () => {
    expect(statesFor('run')).toEqual(['queued', 'running', 'completed', 'failed', 'interrupted'])
    expect(statesFor('occurrence')).toEqual(['received', 'skipped', 'expired', 'failed', 'cancelled'])
    expect(statesFor('preparation')).toEqual(['waiting', 'needs-attention'])
    expect(statesFor('all')).toEqual(['queued', 'running', 'completed', 'failed', 'interrupted', 'received', 'skipped', 'expired', 'cancelled', 'waiting', 'needs-attention'])
  })

  it('ask Runner to filter runs by state, and read no list the filter cannot show', () => {
    expect(streamsFor(all)).toEqual({ runs: true, occurrences: true })
    expect(streamsFor({ ...all, state: 'completed' })).toEqual({ runs: true, runState: 'completed', occurrences: false })
    expect(streamsFor({ ...all, state: 'failed' })).toEqual({ runs: true, runState: 'failed', occurrences: true })
    expect(streamsFor({ ...all, state: 'skipped' })).toEqual({ runs: false, occurrences: true })
    expect(streamsFor({ ...all, kind: 'run' })).toEqual({ runs: true, occurrences: false })
    expect(streamsFor({ ...all, kind: 'preparation', state: 'waiting' })).toEqual({ runs: false, occurrences: true })
    expect(streamsFor({ ...all, kind: 'preparation', state: 'skipped' })).toEqual({ runs: false, occurrences: false })
    expect(streamsFor({ ...all, trigger: 'manual' })).toEqual({ runs: true, occurrences: false })
  })

  it('keep rows of the chosen kind, state and initiator', () => {
    const rows = [...Object.values(runs).map(runRow), ...Object.values(occurrences).map(occurrenceRow)].map(row => row!)
    const kept = (filter: Partial<ActivityFilter>) => rows.filter(keeps({ ...all, ...filter })).map(row => row.key)
    expect(kept({ kind: 'preparation' })).toEqual([occurrences.waiting, occurrences.attention].map(o => `occurrence:${o.id}`))
    expect(kept({ state: 'failed' })).toEqual([`run:${runs.failed.id}`, `occurrence:${occurrences.failed.id}`])
    expect(kept({ trigger: 'manual' })).toEqual([runs.queued, runs.signed, runs.unsigned, runs.failed, runs.interrupted, runs.legacy].map(run => `run:${run.id}`))
    expect(kept({ kind: 'run', trigger: TRIGGER })).toEqual([`run:${runs.running.id}`])
    expect(kept({ trigger: 'trg_none' })).toEqual([])
  })
})

describe('merging Runner’s two lists', () => {
  const run = (minute: number): Run => ({ ...runs.signed, id: `run_${String(minute).padStart(32, '0')}`, createdAt: at(minute) })
  const occurrence = (minute: number) => ({ ...occurrences.skipped, id: `occ_${String(minute).padStart(32, '0')}`, receivedAt: at(minute) })

  it('orders every row newest first by when Runner first recorded it', () => {
    const merged = mergeActivity({ runs: { records: [run(30), run(10)], more: false }, occurrences: { records: [occurrence(40), occurrence(20)], more: false } })
    expect(merged.rows.map(row => row.when.at)).toEqual([at(40), at(30), at(20), at(10)])
    expect(merged).toMatchObject({ held: 0, limiting: [] })
  })

  it('holds back a row a list’s next page could put above, and names the list to read next', () => {
    const merged = mergeActivity({ runs: { records: [run(30), run(25)], more: true }, occurrences: { records: [occurrence(40), occurrence(20), occurrence(5)], more: false } })
    expect(merged.rows.map(row => row.when.at)).toEqual([at(40), at(30), at(25)])
    expect(merged.held).toBe(2)
    expect(merged.limiting).toEqual(['runs'])
  })

  it('holds back to the newer floor when both lists have more', () => {
    const merged = mergeActivity({ runs: { records: [run(30), run(25)], more: true }, occurrences: { records: [occurrence(40), occurrence(28)], more: true } })
    expect(merged.rows.map(row => row.when.at)).toEqual([at(40), at(30), at(28)])
    expect(merged.limiting).toEqual(['occurrences'])
  })

  it('holds everything back while a list with more pages has no readable time', () => {
    const merged = mergeActivity({ runs: { records: [{ ...run(30), createdAt: '' }], more: true }, occurrences: { records: [occurrence(40)], more: false } })
    expect(merged.rows).toEqual([])
    expect(merged.limiting).toEqual(['runs'])
  })
})

describe('merging Runner’s lists with its journal (#218)', () => {
  const run = (n: number, minute: number): Run => ({ ...runs.signed, id: `run_${String(n).padStart(32, '0')}`, createdAt: at(minute) })
  const queued = (n: number) => ({ kind: 'run.queued', from: null, concerns: { run: `run_${String(n).padStart(32, '0')}` } })
  const started = (n: number) => ({ kind: 'run.started', from: 'queued', concerns: { run: `run_${String(n).padStart(32, '0')}` } })
  const names = (merged: ReturnType<typeof mergeActivity>) => merged.shown.map(row => row.kind === 'journal' ? `${row.place}:${row.entry.kind}` : `record:${row.key.slice(-1)}`).join(' ')

  it('keeps Runner’s order for entries, puts each record just above the entry that created it, and a record before the journal below every entry', () => {
    // The journal's times run backwards: Runner's clock was set back. Its order stands.
    const entries = [{ ...queued(2), at: at(50) }, { ...started(2), at: at(40) }, { ...queued(3), at: at(30) }]
    const merged = mergeActivity({ runs: { records: [run(3, 5), run(2, 6), run(1, 59)], more: false } }, () => true, { entries, show: true })
    expect(names(merged)).toBe('record:3 2:run.queued 1:run.started record:2 0:run.queued record:1')
    expect(merged.rows.map(row => row.key.slice(-1)).join(' ')).toBe('3 2 1')
  })

  it('holds back every row, entries included, below the oldest loaded record of a list with more pages', () => {
    const entries = [queued(1), started(1), queued(2), started(2), queued(3)]
    const merged = mergeActivity({ runs: { records: [run(3, 30), run(2, 20)], more: true } }, () => true, { entries, show: true })
    // Run 1 is not loaded yet; its record would stand above its run.queued, at place 0, and above nothing shown.
    expect(names(merged)).toBe('record:3 4:run.queued 3:run.started record:2 2:run.queued')
    expect(merged.held).toBe(2)
    expect(merged.limiting).toEqual(['runs'])
  })

  it('shows no entry while the filters keep records only, and still places each record by its creation', () => {
    const entries = [queued(2), queued(1)]
    const merged = mergeActivity({ runs: { records: [run(2, 30), run(1, 20)], more: false } }, () => true, { entries, show: false })
    expect(names(merged)).toBe('record:1 record:2')
  })
})

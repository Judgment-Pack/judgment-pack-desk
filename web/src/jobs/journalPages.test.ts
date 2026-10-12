/**
 * A page of Runner's journal read from its newest entries (#235) is held to the
 * record of the request and of what was read before it, and merged only if it
 * fits: the same journal; a cursor that is its first entry's sequence; entries
 * on the right side of the cursor asked with; `more` only where an older entry
 * can exist. A page that does not fit is refused and changes nothing.
 */
import { describe, expect, it } from 'vitest'
import { emptyHeld, holdPage, mergePage, PageRefused, type Held, type Way } from './journal'

const BEGAN = '2026-10-03T00:00:00Z'
const entry = (sequence: number) => ({ sequence, entryVersion: '1', kind: 'run.started', at: BEGAN, by: { kind: 'runner' }, concerns: {} })
const range = (first: number, last: number) => Array.from({ length: last - first + 1 }, (_, index) => entry(first + index))
const page = (first: number, last: number, extra: Record<string, unknown> = {}) => ({ journalBegan: BEGAN, items: range(first, last), next: last, more: first > 1, before: first, ...extra })
const tail: Way = { kind: 'tail' }
const older = (before: number): Way => ({ kind: 'older', before })
const forward = (after: number): Way => ({ kind: 'forward', after })
function held(first: number, last: number): Held {
  const h = emptyHeld()
  mergePage(h, tail, holdPage(h, tail, page(first, last)), 1000)
  return h
}
const refused = (h: Held, way: Way, answer: unknown) => expect(() => holdPage(h, way, answer)).toThrow(PageRefused)
const fits = (h: Held, way: Way, answer: unknown) => expect(() => holdPage(h, way, answer)).not.toThrow()

describe('holding a page to the record', () => {
  it('takes the newest page, an older page and a forward page that fit, and an empty journal', () => {
    fits(emptyHeld(), tail, page(51, 100))
    fits(emptyHeld(), tail, page(1, 30, { more: false }))
    fits(emptyHeld(), tail, { journalBegan: BEGAN, items: [], next: 0, more: false, before: 0 })
    fits(held(51, 100), older(51), page(1, 50))
    // The journal of a job is not contiguous: a page may end well before the cursor.
    fits(held(51, 100), older(51), page(3, 20, { next: 20, more: true }))
    fits(held(51, 100), forward(100), { journalBegan: BEGAN, items: range(101, 103), next: 103, more: false })
    fits(held(51, 100), forward(100), { journalBegan: BEGAN, items: [], next: 100, more: false })
  })
  it('refuses a page of another journal', () => {
    refused(held(51, 100), older(51), page(1, 50, { journalBegan: '2026-01-01T00:00:00Z' }))
    refused(held(51, 100), forward(100), { journalBegan: '2026-01-01T00:00:00Z', items: [], next: 100, more: false })
  })
  it('refuses a page whose form is not Runner’s', () => {
    refused(emptyHeld(), tail, null)
    refused(emptyHeld(), tail, { ...page(51, 100), items: 'x' })
    refused(emptyHeld(), tail, { ...page(51, 100), more: 'yes' })
    refused(emptyHeld(), tail, { ...page(51, 100), next: '100' })
    refused(emptyHeld(), tail, { ...page(51, 100), before: -1 })
    refused(emptyHeld(), tail, { ...page(51, 100), journalBegan: 5 })
    refused(emptyHeld(), tail, { ...page(51, 100), items: [...range(51, 99), 'x'] })
    refused(emptyHeld(), tail, { ...page(51, 100), items: [...range(51, 99), { ...entry(100), sequence: '100' }] })
    refused(emptyHeld(), tail, { ...page(51, 100), items: [...range(51, 99), entry(99)] })
    refused(emptyHeld(), tail, { ...page(51, 100), items: [entry(0), ...range(51, 99)], before: 0 })
  })
  it('refuses a backward page whose cursors do not name its entries', () => {
    refused(emptyHeld(), tail, page(51, 100, { before: 52 }))
    refused(emptyHeld(), tail, page(51, 100, { next: 99 }))
    refused(emptyHeld(), tail, { ...page(51, 100), before: undefined })
    refused(emptyHeld(), tail, { journalBegan: BEGAN, items: [], next: 0, more: true, before: 0 })
    refused(emptyHeld(), tail, { journalBegan: BEGAN, items: [], next: 0, more: false, before: 5 })
    refused(held(51, 100), older(51), page(1, 50, { before: 2 }))
    refused(held(51, 100), older(51), page(1, 50, { next: 49 }))
    refused(held(51, 100), older(51), { ...page(1, 50), before: undefined })
    refused(held(51, 100), older(51), { journalBegan: BEGAN, items: [], next: 0, more: false, before: 52 })
  })
  it('refuses an older page that reaches the entries held or past them: a duplicate', () => {
    refused(held(51, 100), older(51), page(40, 51))
    refused(held(51, 100), older(51), page(40, 60))
    refused(held(51, 100), older(51), page(60, 100))
  })
  it('refuses `more` where no older entry can exist, or where the page holds none', () => {
    refused(emptyHeld(), tail, page(1, 50, { more: true }))
    refused(emptyHeld(), tail, { journalBegan: BEGAN, items: [], next: 0, more: true, before: 0 })
    refused(held(51, 100), older(51), page(1, 50, { more: true }))
    refused(held(51, 100), older(51), { journalBegan: BEGAN, items: [], next: 0, more: true, before: 51 })
  })
  it('refuses a forward page that names an older entry, repeats one held, or carries a backward cursor', () => {
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: range(100, 102), next: 102, more: false })
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: range(95, 102), next: 102, more: false })
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: range(101, 102), next: 101, more: false })
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: range(101, 102), next: 102, more: false, before: 101 })
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: [], next: 0, more: false })
    refused(held(51, 100), forward(100), { journalBegan: BEGAN, items: [], next: 100, more: true })
  })
})

describe('merging a page that was held', () => {
  it('keeps the entries in journal order, each once, from the newest page, older pages and forward pages', () => {
    const h = held(101, 150)
    mergePage(h, older(101), holdPage(h, older(101), page(51, 100)), 1000)
    mergePage(h, forward(150), holdPage(h, forward(150), { journalBegan: BEGAN, items: range(151, 153), next: 153, more: false }), 1000)
    mergePage(h, older(51), holdPage(h, older(51), page(1, 50)), 1000)
    expect(h.places).toEqual(Array.from({ length: 153 }, (_, index) => index + 1))
    expect(h.entries.map(e => e.sequence)).toEqual(h.places)
    expect(h.older).toEqual({ more: false, before: 1 })
    expect(h.next).toBe(153)
  })
  it('does not merge an older page asked for before the cursor moved, and changes nothing', () => {
    const h = held(101, 150)
    const answer = holdPage(h, older(101), page(51, 100))
    expect(mergePage(h, older(120), answer, 1000)).toBe(false)
    expect(h.places.length).toBe(50)
    expect(h.older).toEqual({ more: true, before: 101 })
  })
  it('drops the oldest beyond its bound from a forward arrival, and says older entries exist from the first kept', () => {
    const h = held(101, 150)
    mergePage(h, forward(150), holdPage(h, forward(150), { journalBegan: BEGAN, items: range(151, 160), next: 160, more: false }), 55)
    expect(h.places).toEqual(Array.from({ length: 55 }, (_, index) => 106 + index))
    expect(h.older).toEqual({ more: true, before: 106 })
    expect(h.next).toBe(160)
    // The entries dropped are read again from the cursor, and are not held twice.
    mergePage(h, older(106), holdPage(h, older(106), page(101, 105)), 55)
    expect(h.places).toEqual(Array.from({ length: 60 }, (_, index) => 101 + index))
  })
})

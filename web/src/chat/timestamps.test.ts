import { describe, expect, it } from 'vitest'
import { dayBoundaries, formatMessageTime, timestampDate } from './timestamps'

const time = (at: string, locale = 'en-US', zone = 'America/Toronto') => formatMessageTime(at, locale, zone)!
describe('message instants', () => {
  it.each(['', 'not a date', '2026-09-21', '2026-09-21T12:00:00', '2026-02-30T12:00:00Z', '2025-02-29T12:00:00Z', '2026-13-01T00:00:00Z', '2026-01-00T00:00:00Z', '2026-01-01T24:00:00Z', '2026-01-01T00:60:00Z', '2026-01-01T00:00:60Z', '2026-01-01T00:00:00+24:00'])('does not invent a time for %s', at => {
    expect(timestampDate(at)).toBeUndefined()
    expect(formatMessageTime(at, 'en', 'UTC')).toBeUndefined()
  })
  it('accepts explicit offsets, fractional seconds and leap days without changing the instant', () => {
    expect(timestampDate('2024-02-29T12:00:00.123-05:00')?.toISOString()).toBe('2024-02-29T17:00:00.123Z')
    expect(timestampDate('2026-09-21T12:00:00.123456789Z')?.toISOString()).toBe('2026-09-21T12:00:00.123Z')
  })
  it('follows regional 12/24-hour conventions and retains seconds and offset in details', () => {
    const at = '2026-09-21T18:14:37.000Z'
    expect(time(at).short).toBe('2:14 PM')
    expect(time(at, 'en-GB').short).toBe('14:14')
    expect(time(at).full).toContain('2:14:37 PM')
    expect(time(at).full).toContain('GMT-04:00')
  })
  it.each(['en', 'fr', 'es', 'de', 'it', 'pt-PT', 'pt-BR', 'ko', 'zh-Hans', 'zh-Hant', 'yue-Hant', 'ja'])('formats %s using Intl and the viewer timezone', locale => {
    const at = '2026-09-21T18:14:37.000Z'
    expect(time(at, locale, 'Asia/Seoul').short).toBe(new Intl.DateTimeFormat(locale, { timeZone: 'Asia/Seoul', hour: 'numeric', minute: '2-digit' }).format(new Date(at)))
    expect(time(at, locale, 'Asia/Seoul').dayKey).toBe('month:09/day:22/year:2026')
  })
  it('distinguishes repeated local times during the autumn clock change', () => {
    const first = time('2026-11-01T05:30:00Z'), second = time('2026-11-01T06:30:00Z')
    expect(first.short).toBe(second.short)
    expect(first.full).toContain('GMT-04:00')
    expect(second.full).toContain('GMT-05:00')
    expect(dayBoundaries([first, second])).toEqual([undefined, undefined])
  })
  it('groups by local calendar day, not UTC midnight or elapsed 24 hours', () => {
    const utcMidnight = [time('2026-09-21T23:59:00Z'), time('2026-09-22T00:01:00Z')]
    expect(dayBoundaries(utcMidnight)).toEqual([undefined, undefined])
    const localMidnight = [time('2026-09-22T03:59:00Z'), time('2026-09-22T04:01:00Z')]
    expect(dayBoundaries(localMidnight)).toEqual(['Sep 21, 2026', 'Sep 22, 2026'])
    expect(dayBoundaries(localMidnight.map((_, i) => time(i ? '2026-09-22T04:01:00Z' : '2026-09-22T03:59:00Z', 'en-US', 'Europe/Berlin')))).toEqual([undefined, undefined])
  })
  it('keeps duplicate, regressed, future and unknown times in their stored order', () => {
    const a = time('2026-09-21T12:00:00Z'), b = time('2026-09-22T12:00:00Z'), future = time('2099-09-21T12:00:00Z')
    const ordered = [a, a, b, undefined, a, future]
    expect(dayBoundaries(ordered)).toEqual([a.day, undefined, b.day, undefined, a.day, future.day])
    expect(ordered).toEqual([a, a, b, undefined, a, future])
  })
})

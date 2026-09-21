/** Accept explicit instants only. Date.parse alone also accepts ambiguous local
 * dates and normalizes impossible dates, which must not become invented times. */
export function timestampDate(value: string): Date | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-](\d{2}):(\d{2}))$/.exec(value)
  if (!match) return
  const [, year, month, day, hour, minute, second, , offsetHour, offsetMinute] = match
  const y = Number(year), m = Number(month), d = Number(day)
  const days = [31, y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (m < 1 || m > 12 || d < 1 || d > days[m - 1]! || Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59 || Number(offsetHour ?? 0) > 23 || Number(offsetMinute ?? 0) > 59) return
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date : undefined
}

export interface MessageTimeFormat { short: string; full: string; day: string; dayKey: string }
/** Reuse Intl formatters across a transcript, including after new turns arrive. */
export function messageTimeFormatter(locale: string, timeZone: string) {
  const day = new Intl.DateTimeFormat('en', { timeZone, calendar: 'gregory', numberingSystem: 'latn', year: 'numeric', month: '2-digit', day: '2-digit' })
  const short = new Intl.DateTimeFormat(locale, { timeZone, hour: 'numeric', minute: '2-digit' })
  const full = new Intl.DateTimeFormat(locale, { timeZone, year: 'numeric', month: 'long', day: 'numeric', hour: 'numeric', minute: '2-digit', second: '2-digit', timeZoneName: 'longOffset' })
  const label = new Intl.DateTimeFormat(locale, { timeZone, dateStyle: 'medium' })
  return (value: string): MessageTimeFormat | undefined => {
    const date = timestampDate(value)
    if (!date) return
    // Local calendar day (including DST), not an elapsed 24-hour period.
    const dayKey = day.formatToParts(date).filter(part => ['year', 'month', 'day'].includes(part.type))
      .map(part => `${part.type}:${part.value}`).join('/')
    return { short: short.format(date), full: full.format(date), day: label.format(date), dayKey }
  }
}
export function formatMessageTime(value: string, locale: string, timeZone: string): MessageTimeFormat | undefined {
  return messageTimeFormatter(locale, timeZone)(value)
}
/** Keep stored order, even if a device's clock moved backward. Invalid legacy
 * times remain in the transcript and interrupt grouping without inventing a day. */
export function dayBoundaries(times: readonly (MessageTimeFormat | undefined)[]): (string | undefined)[] {
  if (new Set(times.flatMap(time => time ? [time.dayKey] : [])).size < 2) return times.map(() => undefined)
  return times.map((time, index) => time && time.dayKey !== times[index - 1]?.dayKey ? time.day : undefined)
}

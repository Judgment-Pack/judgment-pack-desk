import { msg } from '../i18n'
import { parseJsonText } from '../research/verify/canon'

/** Raw signed JSON must not pass through JavaScript floating-point numbers. */
export class RetainedJSON {
  constructor(readonly text: string) { parseJsonText(text) }
}
export function jobsRequest(value: unknown): string {
  const encode = (v: unknown): string | undefined => {
    if (v instanceof RetainedJSON) return v.text
    if (Array.isArray(v)) return `[${v.map(item => encode(item) ?? 'null').join(',')}]`
    if (v !== null && typeof v === 'object') return `{${Object.entries(v).flatMap(([k, item]) => { const text = encode(item); return text === undefined ? [] : [`${JSON.stringify(k)}:${text}`] }).join(',')}}`
    return JSON.stringify(v)
  }
  const text = encode(value)
  if (text === undefined || new TextEncoder().encode(text).length > (2 << 20)) throw new Error(msg('Jobs requests are limited to 2 MiB.'))
  return text
}

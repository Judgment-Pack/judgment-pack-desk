/** The two canonical forms of spec/dialect.md §§1 and 9.5. No schema interpretation. */
export type JSONValue = null | boolean | number | string | JSONValue[] | { [name: string]: JSONValue }

const sections = {
  invalid_unicode: '§1', integer_literal: '§1', integer_range: '§1',
  nesting_limit: '§12', invalid_json: '§1', duplicate_member: '§1',
  non_json_object: '§1', non_string_member: '§1', accessor_member: '§1', non_json_value: '§1',
} as const
export type CanonErrorCode = keyof typeof sections

/** Machine-readable rule failure. Presentation and translation belong to callers. */
export class CanonError extends Error {
  readonly section: typeof sections[CanonErrorCode]
  constructor(readonly code: CanonErrorCode, readonly details: { offset?: number; member?: string } = {}) {
    super(`${sections[code]}: ${code}`)
    this.name = 'CanonError'
    this.section = sections[code]
  }
}

function unicode(value: string): void {
  for (const ch of value) {
    const point = ch.codePointAt(0)!
    if (point >= 0xd800 && point <= 0xdfff) throw new CanonError('invalid_unicode')
  }
}

/** Validate the literal before conversion: JSON.parse alone loses 1.0 and 1e2. */
function integer(literal: string): number {
  if (!/^-?(0|[1-9][0-9]*)$/.test(literal)) throw new CanonError('integer_literal')
  const value = Number(literal)
  if (!Number.isSafeInteger(value)) throw new CanonError('integer_range')
  return value // All accepted literals are exact in JS, including -0.
}

function nesting(depth: number): void {
  if (depth >= 64) throw new CanonError('nesting_limit')
}

/** Read one canonical-domain JSON value, refusing duplicates before they can be lost. */
export function parseJSON(text: string): JSONValue {
  let at = 0
  const fail = (): never => { throw new CanonError('invalid_json', { offset: at }) }
  const space = () => { while (/[\x20\t\r\n]/.test(text[at] ?? '') && at < text.length) at++ }
  const string = (): string => {
    const start = at++
    while (at < text.length) {
      const ch = text[at++]
      if (ch === '\\') { at++; continue }
      if (ch === '"') {
        let value: string
        try { value = JSON.parse(text.slice(start, at)) as string } catch { return fail() }
        unicode(value)
        return value
      }
    }
    return fail()
  }
  const read = (depth: number): JSONValue => {
    space()
    const ch = text[at]
    if (ch === '"') return string()
    if (ch === '{' || ch === '[') {
      nesting(depth)
      at++
      const object = ch === '{', end = object ? '}' : ']'
      const entries: [string, JSONValue][] = [], items: JSONValue[] = [], names = new Set<string>()
      space()
      if (text[at] !== end) {
        for (;;) {
          space()
          if (object) {
            if (text[at] !== '"') return fail()
            const name = string()
            if (names.has(name)) throw new CanonError('duplicate_member', { member: name })
            names.add(name)
            space()
            if (text[at++] !== ':') return fail()
            entries.push([name, read(depth + 1)])
          } else items.push(read(depth + 1))
          space()
          if (text[at] !== ',') break
          at++
        }
      }
      if (text[at++] !== end) return fail()
      return object ? Object.fromEntries(entries) : items
    }
    for (const [token, value] of [['true', true], ['false', false], ['null', null]] as const) {
      if (text.startsWith(token, at)) { at += token.length; return value }
    }
    const literal = /^[0-9eE+.\-]+/.exec(text.slice(at))?.[0]
    if (literal) { at += literal.length; return integer(literal) }
    return fail()
  }
  const value = read(0)
  space()
  if (at !== text.length) return fail()
  return value
}

function compareNames(a: string, b: string): number {
  const left = Array.from(a), right = Array.from(b)
  for (let i = 0; i < Math.min(left.length, right.length); i++) {
    const difference = left[i].codePointAt(0)! - right[i].codePointAt(0)!
    if (difference) return difference
  }
  return left.length - right.length
}

function render(value: unknown, preparation: boolean, depth = 0): string {
  if (value === null) return 'null'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value)) throw new CanonError('integer_range')
    return preparation && Object.is(value, -0) ? '-0' : String(value)
  }
  if (typeof value === 'string') {
    unicode(value)
    const quoted = JSON.stringify(value)
    return preparation ? quoted.replace(/[&<>\u2028\u2029]/g, ch => '\\u' + ch.charCodeAt(0).toString(16).padStart(4, '0')) : quoted
  }
  if (typeof value === 'object') {
    nesting(depth)
    if (Array.isArray(value)) return '[' + Array.from(value, item => render(item, preparation, depth + 1)).join(',') + ']'
    const proto = Object.getPrototypeOf(value)
    if (proto !== Object.prototype && proto !== null) throw new CanonError('non_json_object')
    if (Object.getOwnPropertySymbols(value).length) throw new CanonError('non_string_member')
    return '{' + Object.keys(value).sort(compareNames).map(name => {
      const descriptor = Object.getOwnPropertyDescriptor(value, name)!
      if (!('value' in descriptor)) throw new CanonError('accessor_member')
      return render(name, preparation, depth + 1) + ':' + render(descriptor.value, preparation, depth + 1)
    }).join(',') + '}'
  }
  throw new CanonError('non_json_value')
}

/** §1 gateway form: code point order, raw Unicode, -0 becomes 0. */
export const canonicalize = (value: unknown): string => render(value, false)
/** §9.5 preparation form: HTML/line-separator escapes and -0 retained. */
export const canonicalPreparation = (value: unknown): string => render(value, true)

/** SHA-256 of bytes, named as §1 requires; Web Crypto performs no I/O. */
export async function digestBytes(bytes: Uint8Array): Promise<`sha256:${string}`> {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', new Uint8Array(bytes))
  return 'sha256:' + Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('') as `sha256:${string}`
}
export const dialectDigest = (value: unknown) => digestBytes(new TextEncoder().encode(canonicalize(value)))
export const preparationDigest = (value: unknown) => digestBytes(new TextEncoder().encode(canonicalPreparation(value)))

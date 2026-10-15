/** The two canonical forms of spec/dialect.md §§1 and 9.5. No schema interpretation. */
export type JSONValue = null | boolean | number | string | JSONValue[] | { [name: string]: JSONValue }

export class CanonError extends Error {
  constructor(rule: string) { super(rule); this.name = 'CanonError' }
}

function unicode(value: string): void {
  for (const ch of value) {
    const point = ch.codePointAt(0)!
    if (point >= 0xd800 && point <= 0xdfff) throw new CanonError('§1: strings must be well-formed Unicode (no lone surrogate)')
  }
}

/** Validate the literal before conversion: JSON.parse alone loses 1.0 and 1e2. */
function integer(literal: string): number {
  if (!/^-?(0|[1-9][0-9]*)$/.test(literal)) throw new CanonError('§1: an integer has no fraction or exponent')
  const value = Number(literal)
  if (!Number.isSafeInteger(value)) throw new CanonError('§1: an integer must be within ±(2^53−1)')
  return value // All accepted literals are exact in JS, including -0.
}

function nesting(depth: number): void {
  if (depth >= 64) throw new CanonError('§12: document nesting exceeds 64 containers')
}

/** Read one canonical-domain JSON value, refusing duplicates before they can be lost. */
export function parseJSON(text: string): JSONValue {
  let at = 0
  const fail = (): never => { throw new CanonError(`§1: expected exactly one RFC 8259 JSON value (offset ${at})`) }
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
            if (names.has(name)) throw new CanonError('§1: duplicate member name ' + JSON.stringify(name))
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
    if (!Number.isSafeInteger(value)) throw new CanonError('§1: an integer must be within ±(2^53−1), with no fraction or exponent')
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
    if (proto !== Object.prototype && proto !== null) throw new CanonError('§1: only JSON objects have a canonical form')
    if (Object.getOwnPropertySymbols(value).length) throw new CanonError('§1: JSON member names must be strings')
    return '{' + Object.keys(value).sort(compareNames).map(name => {
      const descriptor = Object.getOwnPropertyDescriptor(value, name)!
      if (!('value' in descriptor)) throw new CanonError('§1: a JSON member must hold a value, not an accessor')
      return render(name, preparation, depth + 1) + ':' + render(descriptor.value, preparation, depth + 1)
    }).join(',') + '}'
  }
  throw new CanonError('§1: value is outside the JSON canonical domain')
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

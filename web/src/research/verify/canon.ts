/**
 * The gateway's canonical form (judgment-pack-gateway SPEC.md §1.1), over JSON
 * **text** rather than parsed values.
 *
 * Text, because a verifier has to tell the integer `1` from the number `1.0`
 * and refuse a lone surrogate escape, and neither survives `JSON.parse`. So
 * this is a small strict parser that keeps every number as the literal it was
 * written as, refuses a duplicate member name where a browser's parser keeps
 * the last, and a renderer that writes exactly the bytes §1.1 states: member
 * names sorted by **code point** (not by UTF-16 code unit, which is what an
 * RFC 8785 canonicalizer does and what one vector exists to catch), arrays in
 * order, no whitespace, raw UTF-8, the short escapes and lowercase `\u00xx` for
 * the other C0 controls, integers only and within ±(2^53−1).
 *
 * It answers to the frozen vectors in `fixtures/canon.json`, copied from the
 * gateway's corpus at the commit `fixtures/GATEWAY-COMMIT` names, and is what
 * the receipt and seal signing inputs and the artifact re-digest are built on.
 */

export type JsonNode =
  | { kind: 'object'; members: { name: string; value: JsonNode }[] }
  | { kind: 'array'; items: JsonNode[] }
  | { kind: 'string'; value: string }
  | { kind: 'number'; literal: string }
  | { kind: 'boolean'; value: boolean }
  | { kind: 'null' }

export class CanonError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'CanonError'
  }
}

/** The containers a value may nest: what the gateway's own decoder reads. */
const MAX_NESTING = 10_000
const MAX_INTEGER = 2n ** 53n - 1n
const INTEGER_LITERAL = /^-?(0|[1-9][0-9]*)$/
const NUMBER_LITERAL = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$/

/** Parse one JSON document strictly, keeping number literals as written. */
export function parseJsonText(text: string): JsonNode {
  const parser = new Parser(text)
  parser.skipWhitespace()
  const node = parser.value(0)
  parser.skipWhitespace()
  if (parser.at < text.length) throw new CanonError('trailing content after the document')
  return node
}

class Parser {
  at = 0
  constructor(private readonly text: string) {}

  skipWhitespace(): void {
    for (;;) {
      const c = this.text[this.at]
      if (c === ' ' || c === '\t' || c === '\n' || c === '\r') this.at += 1
      else return
    }
  }

  value(depth: number): JsonNode {
    const c = this.text[this.at]
    if (c === undefined) throw new CanonError('unexpected end of the document')
    if (c === '{') return this.object(depth)
    if (c === '[') return this.array(depth)
    if (c === '"') return { kind: 'string', value: this.string() }
    if (c === 't') return this.literal('true', { kind: 'boolean', value: true })
    if (c === 'f') return this.literal('false', { kind: 'boolean', value: false })
    if (c === 'n') return this.literal('null', { kind: 'null' })
    if (c === '-' || (c >= '0' && c <= '9')) return this.number()
    throw new CanonError(`unexpected character ${JSON.stringify(c)} at ${this.at}`)
  }

  private literal(word: string, node: JsonNode): JsonNode {
    if (this.text.startsWith(word, this.at)) {
      this.at += word.length
      return node
    }
    throw new CanonError(`unexpected token at ${this.at}`)
  }

  private number(): JsonNode {
    const start = this.at
    while (this.at < this.text.length && /[0-9eE+\-.]/.test(this.text[this.at]!)) this.at += 1
    const literal = this.text.slice(start, this.at)
    if (!NUMBER_LITERAL.test(literal)) throw new CanonError(`not a JSON number: ${literal}`)
    return { kind: 'number', literal }
  }

  private object(depth: number): JsonNode {
    if (depth >= MAX_NESTING) throw new CanonError('nesting exceeds the depth a decoder reads')
    this.at += 1
    const members: { name: string; value: JsonNode }[] = []
    const seen = new Set<string>()
    this.skipWhitespace()
    if (this.text[this.at] === '}') {
      this.at += 1
      return { kind: 'object', members }
    }
    for (;;) {
      this.skipWhitespace()
      if (this.text[this.at] !== '"') throw new CanonError(`member name expected at ${this.at}`)
      const name = this.string()
      if (seen.has(name)) throw new CanonError(`duplicate member name ${JSON.stringify(name)}`)
      seen.add(name)
      this.skipWhitespace()
      if (this.text[this.at] !== ':') throw new CanonError(`":" expected at ${this.at}`)
      this.at += 1
      this.skipWhitespace()
      members.push({ name, value: this.value(depth + 1) })
      this.skipWhitespace()
      const c = this.text[this.at]
      if (c === ',') {
        this.at += 1
        continue
      }
      if (c === '}') {
        this.at += 1
        return { kind: 'object', members }
      }
      throw new CanonError(`"," or "}" expected at ${this.at}`)
    }
  }

  private array(depth: number): JsonNode {
    if (depth >= MAX_NESTING) throw new CanonError('nesting exceeds the depth a decoder reads')
    this.at += 1
    const items: JsonNode[] = []
    this.skipWhitespace()
    if (this.text[this.at] === ']') {
      this.at += 1
      return { kind: 'array', items }
    }
    for (;;) {
      this.skipWhitespace()
      items.push(this.value(depth + 1))
      this.skipWhitespace()
      const c = this.text[this.at]
      if (c === ',') {
        this.at += 1
        continue
      }
      if (c === ']') {
        this.at += 1
        return { kind: 'array', items }
      }
      throw new CanonError(`"," or "]" expected at ${this.at}`)
    }
  }

  /** A JSON string, escapes decoded, surrogate escapes paired or refused. */
  private string(): string {
    this.at += 1
    let out = ''
    for (;;) {
      const c = this.text[this.at]
      if (c === undefined) throw new CanonError('unterminated string')
      const code = c.charCodeAt(0)
      if (c === '"') {
        this.at += 1
        return out
      }
      if (code < 0x20) throw new CanonError('a control character in a string must be escaped')
      if (c !== '\\') {
        // A raw surrogate in the source text: a lone one is not a character.
        if (code >= 0xd800 && code <= 0xdbff) {
          const next = this.text.charCodeAt(this.at + 1)
          if (!(next >= 0xdc00 && next <= 0xdfff)) throw new CanonError('unpaired surrogate')
          out += c + this.text[this.at + 1]
          this.at += 2
          continue
        }
        if (code >= 0xdc00 && code <= 0xdfff) throw new CanonError('unpaired surrogate')
        out += c
        this.at += 1
        continue
      }
      const e = this.text[this.at + 1]
      this.at += 2
      switch (e) {
        case '"':
          out += '"'
          break
        case '\\':
          out += '\\'
          break
        case '/':
          out += '/'
          break
        case 'b':
          out += '\b'
          break
        case 'f':
          out += '\f'
          break
        case 'n':
          out += '\n'
          break
        case 'r':
          out += '\r'
          break
        case 't':
          out += '\t'
          break
        case 'u': {
          const high = this.hex4()
          if (high >= 0xd800 && high <= 0xdbff) {
            if (this.text[this.at] !== '\\' || this.text[this.at + 1] !== 'u') {
              throw new CanonError('unpaired surrogate escape')
            }
            this.at += 2
            const low = this.hex4()
            if (!(low >= 0xdc00 && low <= 0xdfff)) throw new CanonError('unpaired surrogate escape')
            out += String.fromCharCode(high, low)
          } else if (high >= 0xdc00 && high <= 0xdfff) {
            throw new CanonError('unpaired surrogate escape')
          } else {
            out += String.fromCharCode(high)
          }
          break
        }
        default:
          throw new CanonError(`invalid escape at ${this.at - 2}`)
      }
    }
  }

  private hex4(): number {
    const hex = this.text.slice(this.at, this.at + 4)
    if (!/^[0-9a-fA-F]{4}$/.test(hex)) throw new CanonError(`invalid \\u escape at ${this.at}`)
    this.at += 4
    return parseInt(hex, 16)
  }
}

/** Ascending by Unicode code point, which is not JavaScript's `<` on strings. */
export function compareCodePoints(a: string, b: string): number {
  const aa = Array.from(a)
  const bb = Array.from(b)
  const n = Math.min(aa.length, bb.length)
  for (let i = 0; i < n; i += 1) {
    const x = aa[i]!.codePointAt(0)!
    const y = bb[i]!.codePointAt(0)!
    if (x !== y) return x < y ? -1 : 1
  }
  return aa.length === bb.length ? 0 : aa.length < bb.length ? -1 : 1
}

function canonicalString(s: string): string {
  let out = '"'
  for (const ch of s) {
    switch (ch) {
      case '"':
        out += '\\"'
        break
      case '\\':
        out += '\\\\'
        break
      case '\b':
        out += '\\b'
        break
      case '\f':
        out += '\\f'
        break
      case '\n':
        out += '\\n'
        break
      case '\r':
        out += '\\r'
        break
      case '\t':
        out += '\\t'
        break
      default: {
        const code = ch.codePointAt(0)!
        if (code < 0x20) out += `\\u${code.toString(16).padStart(4, '0')}`
        else out += ch
      }
    }
  }
  return out + '"'
}

function render(node: JsonNode, out: string[]): void {
  switch (node.kind) {
    case 'object': {
      const sorted = [...node.members].sort((a, b) => compareCodePoints(a.name, b.name))
      out.push('{')
      sorted.forEach((member, index) => {
        if (index > 0) out.push(',')
        out.push(canonicalString(member.name), ':')
        render(member.value, out)
      })
      out.push('}')
      return
    }
    case 'array':
      out.push('[')
      node.items.forEach((item, index) => {
        if (index > 0) out.push(',')
        render(item, out)
      })
      out.push(']')
      return
    case 'string':
      out.push(canonicalString(node.value))
      return
    case 'number': {
      if (!INTEGER_LITERAL.test(node.literal)) {
        throw new CanonError(`number ${node.literal} is outside the canonical domain`)
      }
      const magnitude = BigInt(node.literal)
      if (magnitude > MAX_INTEGER || magnitude < -MAX_INTEGER) {
        throw new CanonError(`integer ${node.literal} is outside ±(2^53−1)`)
      }
      out.push(node.literal)
      return
    }
    case 'boolean':
      out.push(node.value ? 'true' : 'false')
      return
    case 'null':
      out.push('null')
  }
}

/** The canonical bytes of a parsed document. */
export function canonicalize(node: JsonNode): Uint8Array {
  const out: string[] = []
  render(node, out)
  return new TextEncoder().encode(out.join(''))
}

/** The canonical bytes of a JSON text. */
export function canonicalText(text: string): Uint8Array {
  return canonicalize(parseJsonText(text))
}

/** The named member of an object node, or undefined. */
export function memberOf(node: JsonNode, name: string): JsonNode | undefined {
  if (node.kind !== 'object') return undefined
  return node.members.find((member) => member.name === name)?.value
}

/** A string member's value, or undefined where it is absent or not a string. */
export function stringMember(node: JsonNode, name: string): string | undefined {
  const value = memberOf(node, name)
  return value?.kind === 'string' ? value.value : undefined
}

/** A plain JavaScript value for rendering, with number literals as numbers. */
export function toPlain(node: JsonNode): unknown {
  switch (node.kind) {
    case 'object':
      return Object.fromEntries(node.members.map((member) => [member.name, toPlain(member.value)]))
    case 'array':
      return node.items.map(toPlain)
    case 'string':
      return node.value
    case 'number':
      return Number(node.literal)
    case 'boolean':
      return node.value
    case 'null':
      return null
  }
}

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

export function hexToBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0 || !/^[0-9a-f]*$/.test(hex)) throw new CanonError('not lowercase hex')
  const out = new Uint8Array(hex.length / 2)
  for (let i = 0; i < out.length; i += 1) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  return out
}

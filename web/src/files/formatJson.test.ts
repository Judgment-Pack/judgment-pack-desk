import { describe, expect, it } from 'vitest'
import { formatJson } from './formatJson'

describe('whitespace-only JSON formatting', () => {
  it('preserves precision, numeric spelling, key order, duplicate keys and escapes', () => {
    const source = String.raw`{"z":9007199254740993,"10":1.00e+3,"2":-0,"z":"\u0061\"\\","space":" a b ","list":[{},[],true,null]}`
    const result = formatJson(source)
    expect(result.ok).toBe(true)
    if (!result.ok) return
    const tokens = (text: string) => [...text.matchAll(/"(?:[^"\\]|\\.)*"|[^\s{}\[\],:]+|[{}\[\],:]/g)].map(m => m[0])
    expect(tokens(result.content)).toEqual(tokens(source))
    expect(result.content).toContain('  "z": 9007199254740993,\n')
    expect(result.content).toContain('  "list": [\n    {},\n    [],\n    true,\n    null\n  ]\n}\n')
    expect(formatJson(result.content)).toEqual(result)
  })
  it.each(['', '{"a":}', '{"a":1,}', '{"a":NaN}', '{"a":1}\n{"b":2}', '// note\n{}'])('rejects invalid JSON without replacing it: %s', source => {
    expect(formatJson(source)).toEqual({ ok: false, reason: 'invalid' })
  })
  it.each(['{}', '[]', 'true', 'null', '42', '"hello"'])('formats valid root value %s', source => {
    expect(formatJson(source)).toEqual({ ok: true, content: source + '\n' })
  })
  it('bounds indentation expansion before it exceeds the writable file size', () => {
    expect(formatJson('['.repeat(2000) + '0' + ']'.repeat(2000))).toEqual({ ok: false, reason: 'too-large' })
  })
  // The files API writes at most 4 MiB, counted in bytes: the formatter never
  // offers a buffer that save would refuse. One string, so the output is the
  // input and a newline; at the bound it is offered, one byte past it is not.
  it('offers formatted JSON of exactly the files API bound and refuses one byte more, counted in UTF-8 bytes', () => {
    const limit = 4 << 20
    const ascii = (bytes: number) => '"' + 'a'.repeat(bytes - 3) + '"'
    const at = formatJson(ascii(limit))
    expect(at.ok && new TextEncoder().encode(at.content).length).toBe(limit)
    expect(formatJson(ascii(limit + 1))).toEqual({ ok: false, reason: 'too-large' })
    // Two bytes a character: 2,097,151 characters, 4,194,305 bytes once formatted.
    const wide = '"' + 'é'.repeat((limit - 2) / 2) + '"'
    expect(wide.length).toBeLessThan(limit)
    expect(formatJson(wide)).toEqual({ ok: false, reason: 'too-large' })
    const fits = '"' + 'é'.repeat((limit - 4) / 2) + '"'
    const formatted = formatJson(fits)
    expect(formatted.ok && new TextEncoder().encode(formatted.content).length).toBe(limit - 1)
  })
})

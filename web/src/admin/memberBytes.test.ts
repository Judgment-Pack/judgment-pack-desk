/**
 * The member slicer, held to the one property it exists for: what comes back
 * is what is in the file, not what a round trip through `JSON.parse` would
 * produce.
 */
import { describe, expect, it } from 'vitest'
import { memberBytes } from './memberBytes'

describe('one member, by its own bytes', () => {
  it('returns the member exactly as it is written', () => {
    const text = '{\n  "a": 1,\n  "panes": {\n      "left": {"width":  248}\n  }\n}\n'
    expect(memberBytes(text, 'panes')).toBe('{\n      "left": {"width":  248}\n  }')
  })

  it('keeps a number in the spelling the file used', () => {
    // The whole reason this is a slice: `JSON.stringify(JSON.parse(…))` turns
    // `1e2` into `100` and rounds an integer past a float64's precision, so a
    // page that re-serialised would show a value the file does not carry.
    const text = '{"a": {"n": 1e2, "big": 9007199254740993}}'
    expect(memberBytes(text, 'a')).toBe('{"n": 1e2, "big": 9007199254740993}')
  })

  it('reads a string, a null, a boolean and an array', () => {
    const text = '{"s":"x","n":null,"b":true,"l":[1, 2]}'
    expect(memberBytes(text, 's')).toBe('"x"')
    expect(memberBytes(text, 'n')).toBe('null')
    expect(memberBytes(text, 'b')).toBe('true')
    expect(memberBytes(text, 'l')).toBe('[1, 2]')
  })

  it('is not confused by a brace or a comma inside a string', () => {
    const text = '{"a":"} , {","b":2}'
    expect(memberBytes(text, 'a')).toBe('"} , {"')
    expect(memberBytes(text, 'b')).toBe('2')
  })

  it('is not confused by an escaped quote', () => {
    const text = String.raw`{"a":"he said \"no\"","b":2}`
    expect(memberBytes(text, 'a')).toBe(String.raw`"he said \"no\""`)
  })

  it('reads a member whose name carries an escape', () => {
    const text = String.raw`{"a\nb":1,"c":2}`
    expect(memberBytes(text, 'a\nb')).toBe('1')
  })

  it('answers nothing for an absent member', () => {
    expect(memberBytes('{"a":1}', 'b')).toBeUndefined()
  })

  it('answers nothing for a member written twice', () => {
    // Two readers of that file would not agree what it holds, so this page
    // quotes neither value — the same answer the chassis gives a write.
    expect(memberBytes('{"a":1,"a":2}', 'a')).toBeUndefined()
  })

  it('answers nothing for text that is not one JSON object', () => {
    for (const text of ['', '   ', '[1]', '{', '{"a"', '{"a":}', '{"a":1', 'null']) {
      expect(memberBytes(text, 'a'), text).toBeUndefined()
    }
  })

  it('answers nothing for an empty object', () => {
    expect(memberBytes('{}', 'a')).toBeUndefined()
  })
})

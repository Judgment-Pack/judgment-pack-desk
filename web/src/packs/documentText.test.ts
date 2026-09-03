/**
 * The byte-preservation proof.
 *
 * The fixtures are read with `readFileSync` rather than written as template
 * literals, so no editor and no formatter in this file can launder the bytes
 * the assertion is about: the indentation under test is the indentation on
 * disk.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  agreesWithParse,
  indexDocument,
  removeMember,
  replaceValue,
  spanAt
} from './documentText'

const FIXTURES = join(import.meta.dirname, '__fixtures__')
const read = (name: string) => readFileSync(join(FIXTURES, name), 'utf8')

describe('the span index', () => {
  it('addresses every value by its pointer, down to a condition operand', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    expect(index.parseError).toBeUndefined()
    const operand = spanAt(index, '/rules/1/when/conditions/0/value')!
    expect(text.slice(operand.valueStart, operand.valueEnd)).toBe('"5000"')
    const rule = spanAt(index, '/rules/1')!
    expect(text.slice(rule.valueStart, rule.valueStart + 1)).toBe('{')
    // The document itself is addressed too, and its pointer is the empty one.
    expect(spanAt(index, '')!.valueStart).toBe(0)
  })
})

describe('a one-field edit', () => {
  it('leaves every byte outside the span byte-identical', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const span = spanAt(index, '/rules/0/description')!
    const edited = replaceValue(text, index, '/rules/0/description', '"A different sentence."')

    // Before the span: byte for byte.
    expect(edited.slice(0, span.valueStart)).toBe(text.slice(0, span.valueStart))
    // After the span: byte for byte, measured from each string's own end so a
    // length change cannot hide a shifted byte.
    const tail = text.length - span.valueEnd
    expect(edited.slice(edited.length - tail)).toBe(text.slice(span.valueEnd))
    // And exactly one member changed.
    const before = JSON.parse(text)
    const after = JSON.parse(edited)
    expect(after.rules[0].description).toBe('A different sentence.')
    before.rules[0].description = 'A different sentence.'
    expect(after).toEqual(before)
  })

  it('edits the span and not the first text that matches it', () => {
    // The one case that discriminates a positional splice from a string
    // replace. `/rules/0/outcome` holds `"decline"`, and the *first* occurrence
    // of those bytes in the file is `/outcomes/1/id` — the outcome declaration
    // the whole document points at. A `text.replace(...)` writer passes every
    // other assertion in this file, because the two pointers those exercise
    // hold text that is unique, and silently renames the outcome instead.
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const span = spanAt(index, '/rules/0/outcome')!
    expect(text.indexOf('"decline"')).toBeLessThan(span.valueStart)

    const edited = replaceValue(text, index, '/rules/0/outcome', '"approve"')
    const before = JSON.parse(text)
    const after = JSON.parse(edited)
    expect(after.outcomes[1].id).toBe('decline')
    expect(after.rules[0].outcome).toBe('approve')
    before.rules[0].outcome = 'approve'
    expect(after).toEqual(before)

    // And the bytes outside the span are the file's own, on both sides.
    expect(edited.slice(0, span.valueStart)).toBe(text.slice(0, span.valueStart))
    const tail = text.length - span.valueEnd
    expect(edited.slice(edited.length - tail)).toBe(text.slice(span.valueEnd))
  })

  it('is not a re-serialization: the file’s own shape survives', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const edited = replaceValue(text, index, '/version', '"1.3.0"')
    // A re-serializer would collapse this line, which the fixture writes on one.
    expect(edited).toContain('{ "id": "approve", "label": "Approve"')
    expect(edited.endsWith('}\n')).toBe(true)
  })
})

describe('removing a member', () => {
  const cases: [string, string, string][] = [
    ['at the head', '{\n  "a": 1,\n  "b": 2,\n  "c": 3\n}', '/a'],
    ['in the middle', '{\n  "a": 1,\n  "b": 2,\n  "c": 3\n}', '/b'],
    ['at the tail', '{\n  "a": 1,\n  "b": 2,\n  "c": 3\n}', '/c'],
    ['when it is the only one', '{\n  "a": 1\n}', '/a']
  ]

  it.each(cases)('leaves JSON that still parses %s', (_where, text, pointer) => {
    const result = removeMember(text, indexDocument(text), pointer)
    expect(() => JSON.parse(result)).not.toThrow()
    const name = pointer.slice(1)
    expect(Object.keys(JSON.parse(result))).not.toContain(name)
    expect(result).not.toMatch(/,\s*[,}]/)
    expect(result).not.toMatch(/[{[]\s*,/)
  })

  it('is what blanking a nonEmptyString does, rather than writing ""', () => {
    // A `""` here is a document the runtime refuses by name. A document
    // without the member is merely smaller — and where the member was
    // required, the refusal names it at its own pointer.
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const smaller = removeMember(text, index, '/rules/0/rationale')
    const parsed = JSON.parse(smaller)
    expect('rationale' in parsed.rules[0]).toBe(false)
    expect(smaller).not.toContain('"rationale": ""')
    expect(JSON.stringify(parsed.rules[1])).toBe(JSON.stringify(JSON.parse(text).rules[1]))
  })
})

describe('the two readings of one file', () => {
  it('reports a duplicated member, naming the pointer and the name', () => {
    const text = read('duplicate-member.pack.json')
    const index = indexDocument(text)
    const problems = agreesWithParse(text, index)
    expect(problems.length).toBeGreaterThan(0)
    expect(problems.some((problem) => problem.pointer === '/title')).toBe(true)
    expect(problems.some((problem) => problem.reason.includes('"title"'))).toBe(true)
    // JSON.parse takes the last; this scanner takes the first. That is the
    // disagreement, and it is why form mode is withheld on this document.
    expect(JSON.parse(text).title).toBe('Another title')
    expect((index.value as { title: string }).title).toBe('One title')
  })

  it('reports none for a document the two readings agree about', () => {
    for (const name of ['full.pack.json', 'minimal.pack.json', 'reordered.pack.json']) {
      const text = read(name)
      expect(agreesWithParse(text, indexDocument(text)), name).toEqual([])
    }
  })

  it('says so where the bytes are not JSON at all', () => {
    const text = '{ "a": '
    const index = indexDocument(text)
    expect(index.parseError).toBeDefined()
    expect(agreesWithParse(text, index).length).toBeGreaterThan(0)
  })
})

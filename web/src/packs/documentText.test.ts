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
  insertMember,
  moveElement,
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

describe('removal keeps every surviving byte of layout', () => {
  // Taking the whitespace *after* the forward comma looks equivalent to taking
  // the whitespace before the member and is not: that whitespace belongs to the
  // member that follows. A document whose members are not indented identically
  // was silently reformatted by a delete.
  it('leaves the next member’s own indentation alone', () => {
    const text = '{\n\t"a":1,\n      "b":2\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/a')).toBe('{\n      "b":2\n}')
  })

  it('does the same with CRLF endings', () => {
    const text = '{\r\n\t"a":1,\r\n      "b":2\r\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/a')).toBe('{\r\n      "b":2\r\n}')
  })

  it('keeps the layout of a member in the middle of three', () => {
    const text = '{\n  "a": 1,\n\t\t"b": 2,\n      "c": 3\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/b')).toBe('{\n  "a": 1,\n      "c": 3\n}')
  })

  it('takes the comma before it when it is the last member', () => {
    const text = '{\n  "a": 1,\n      "b": 2\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/b')).toBe('{\n  "a": 1\n}')
  })

  it('leaves an empty object when it was the only member', () => {
    const text = '{\n  "a": 1\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/a')).toBe('{\n}')
  })

  it('removes a nested member without touching its siblings’ layout', () => {
    const text = '{\n  "r": {\n\t"x": 1,\n        "y": 2\n  }\n}'
    const index = indexDocument(text)
    expect(removeMember(text, index, '/r/x')).toBe('{\n  "r": {\n        "y": 2\n  }\n}')
  })

  it('still parses to the document the removal describes', () => {
    // The bytes are the point, and so is the result still being JSON.
    const text = '{\r\n\t"a":1,\r\n      "b":2\r\n}'
    const after = removeMember(text, indexDocument(text), '/a')
    expect(JSON.parse(after)).toEqual({ b: 2 })
  })
})

describe('a member named __proto__ is a member like any other', () => {
  // The scanner assigned dynamic keys into `{}`, so a literal `__proto__`
  // invoked the prototype setter and stored nothing — and the gate that exists
  // to catch a reading that disagrees with `JSON.parse` compared with `in`,
  // which found the inherited one and reported agreement.
  it('is carried by the scanner’s own reading', () => {
    const text = '{"__proto__":{"x":1},"a":2}'
    const index = indexDocument(text)
    expect(spanAt(index, '/__proto__')).toBeDefined()
    expect(spanAt(index, '/__proto__/x')).toBeDefined()
  })

  it('is not reported as a disagreement when both readings carry it', () => {
    const text = '{"__proto__":{"x":1},"a":2}'
    expect(agreesWithParse(text, indexDocument(text))).toEqual([])
  })

  it('is edited at its own span, like any other member', () => {
    const text = '{"__proto__":{"x":1},"a":2}'
    const index = indexDocument(text)
    const after = replaceValue(text, index, '/__proto__/x', '9')
    expect(after).toBe('{"__proto__":{"x":9},"a":2}')
  })

  it('is the same member under its escaped spelling', () => {
    // `_` is `_`. A document may spell the name either way and both are
    // one member; a reading that saw only one of them would edit the wrong one.
    const escaped = '{"\\u005f\\u005fproto\\u005f\\u005f":{"x":1},"a":2}'
    const index = indexDocument(escaped)
    expect(spanAt(index, '/__proto__')).toBeDefined()
    expect(agreesWithParse(escaped, index)).toEqual([])
  })

  it('is not carried by a reading that only inherits it', () => {
    // **The gate's own half of the pair, on a reading the fixed scanner cannot
    // produce.** With `Object.create(null)` above, every member the scanner has
    // is its own, so nothing this file can write from bytes tells `in` from
    // `Object.hasOwn` — and the gate is exactly what would catch a scanner that
    // went back to `{}`. The reading is built directly instead: a member that
    // is inherited and not owned is a member this reading does not carry, and
    // saying otherwise is how the one comparison against `JSON.parse` reported
    // agreement about a document it disagreed on.
    const text = '{"a":1}'
    const inherited = Object.create({ a: 1 }) as Record<string, unknown>
    const problems = agreesWithParse(text, { ...indexDocument(text), value: inherited })
    expect(problems).toHaveLength(1)
    expect(problems[0]!.reason).toContain('only one reading carries the member')
    expect(problems[0]!.pointer).toBe('/a')
  })

  it('still reports a duplicate of it, so the gate has not merely stopped firing', () => {
    // The point is not that `__proto__` is now quiet — it is that the reading
    // sees it. A document declaring it twice is a document `JSON.parse` reads
    // last-wins and this reads first-wins, and that disagreement must survive.
    const text = '{"__proto__":1,"__proto__":2}'
    const problems = agreesWithParse(text, indexDocument(text))
    expect(problems.map((problem) => problem.pointer)).toContain('/__proto__')
  })
})

/**
 * Adding a member, in the layout the document already uses.
 *
 * The house style is the container's own: a neighbour's leading run, taken
 * verbatim. That is the same rule the delete holds in the other direction, and
 * it is the rule that keeps a one-member addition from reformatting a document
 * whose indentation is not uniform.
 */
describe('insertMember', () => {
  it('takes a neighbour’s own layout run rather than inventing one', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const added = insertMember(text, index, '/decision', 'extensions', '{}', { last: true })
    // The document's members sit at four spaces inside `decision`; the added
    // member sits at four spaces too, and nobody wrote that number here.
    expect(added).toContain('\n    "extensions": {}')
    expect(JSON.parse(added).decision.extensions).toEqual({})
  })

  it('never inherits a neighbour’s indentation where the two differ', () => {
    // Unequal indentation on purpose: `a` is written with a tab and `b` with
    // six spaces. A writer with a house style levels one of them on the first
    // edit; this one takes the layout of the member it is written beside.
    const text = '{\n\t"a": 1,\n      "b": 2\n}'
    const index = indexDocument(text)
    const afterA = insertMember(text, index, '', 'c', '3', { after: '/a' })
    expect(afterA).toBe('{\n\t"a": 1,\n      "c": 3,\n      "b": 2\n}')
    const atEnd = insertMember(text, index, '', 'c', '3', { last: true })
    expect(atEnd).toBe('{\n\t"a": 1,\n      "b": 2,\n      "c": 3\n}')
  })

  it('writes exactly one adjacent comma, wherever the member lands', () => {
    const text = '{\n  "a": 1,\n  "b": 2\n}'
    const index = indexDocument(text)
    for (const placement of [{ first: true } as const, { last: true } as const, { after: '/a' } as const]) {
      const added = insertMember(text, index, '', 'c', '3', placement)
      expect(added.match(/,/g)?.length).toBe(2)
      expect(added).not.toContain(',,')
      expect(() => JSON.parse(added)).not.toThrow()
    }
  })

  it('claims no style for a container that has shown none', () => {
    const text = '{"a": {}}'
    const index = indexDocument(text)
    expect(insertMember(text, index, '/a', 'b', '1')).toBe('{"a": { "b": 1 }}')
  })

  it('leaves every byte outside the insertion identical', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const span = spanAt(index, '/rules/1')!
    const added = insertMember(text, index, '/rules/1', 'rationale', '"Another sentence."', {
      last: true
    })
    expect(added.slice(0, span.memberStart)).toBe(text.slice(0, span.memberStart))
    const tail = text.length - span.memberEnd
    expect(added.slice(added.length - tail)).toBe(text.slice(span.memberEnd))
  })

  it('adds an array element with the elements’ own layout', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const added = insertMember(text, index, '/outcomes', undefined, '{ "id": "defer", "label": "Defer" }')
    expect(JSON.parse(added).outcomes.map((outcome: { id: string }) => outcome.id)).toEqual([
      'approve',
      'decline',
      'defer'
    ])
  })
})

/**
 * Moving a rule, which is an edit to what the pack decides.
 *
 * Order is §7-significant, so this is not a tidy: the bytes move, and each
 * element keeps the layout it was written with.
 */
describe('moveElement', () => {
  it('exchanges two element spans, each keeping its own layout', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const moved = moveElement(text, index, '/rules', 1, 0)
    const after = JSON.parse(moved)
    expect(after.rules.map((rule: { id: string }) => rule.id)).toEqual([
      'approve-when-clear',
      'screen-first'
    ])
    // Every other member of the document is untouched, member for member.
    const before = JSON.parse(text)
    before.rules = [before.rules[1], before.rules[0]]
    expect(after).toEqual(before)
  })

  it('leaves every byte outside the array identical', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    const span = spanAt(index, '/rules')!
    const moved = moveElement(text, index, '/rules', 0, 1)
    expect(moved.slice(0, span.valueStart)).toBe(text.slice(0, span.valueStart))
    const tail = text.length - span.valueEnd
    expect(moved.slice(moved.length - tail)).toBe(text.slice(span.valueEnd))
  })

  it('does not level unequal indentation', () => {
    const text = '[\n\t1,\n      2\n]'
    const index = indexDocument(text)
    // Each element carries the layout of the position it lands in, and no
    // position gains or loses one: the array is the two runs it always had.
    const moved = moveElement(text, index, '', 0, 1)
    expect(moved).toBe('[\n\t2,\n      1\n]')
  })

  it('keeps the whitespace a position had in front of its comma', () => {
    // `{ "id": "one" } ,` is legal JSON and that space is a byte the author
    // wrote. Joining the rebuilt bodies with a bare comma dropped it — an edit
    // changing a byte it did not name, in the one module whose whole claim is
    // that it does not.
    const text = '{\n  "rules": [\n    { "id": "one" } ,\n    { "id": "two" }\n  ]\n}'
    const index = indexDocument(text)
    const moved = moveElement(text, index, '/rules', 0, 1)
    expect(moved).toBe('{\n  "rules": [\n    { "id": "two" } ,\n    { "id": "one" }\n  ]\n}')
  })

  it('changes nothing for a move that goes nowhere or off the end', () => {
    const text = read('full.pack.json')
    const index = indexDocument(text)
    expect(moveElement(text, index, '/rules', 1, 1)).toBe(text)
    expect(moveElement(text, index, '/rules', 0, 9)).toBe(text)
    expect(moveElement(text, index, '/rules', -1, 0)).toBe(text)
    // Not an array at all.
    expect(moveElement(text, index, '/decision', 0, 1)).toBe(text)
  })
})

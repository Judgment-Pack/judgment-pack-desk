/**
 * Every form edit is a splice, and the buffer is what it splices into.
 *
 * The fixtures are read off disk, in `documentText.test.ts`'s idiom, so the
 * indentation these assertions are about is the indentation on disk rather
 * than whatever this file's own formatter would produce.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { spanAt } from '../documentText'
import {
  addElement,
  addMember,
  buffered,
  moveRule,
  removeAt,
  setBoolean,
  setEnum,
  setRawJson,
  setString,
  setStringList
} from './writes'

const FIXTURES = join(import.meta.dirname, '..', '__fixtures__')
const read = (name: string) => readFileSync(join(FIXTURES, name), 'utf8')
const FULL = read('full.pack.json')

describe('writing one member', () => {
  it('replaces the span and leaves every byte outside it identical', () => {
    const before = buffered(FULL)
    const span = spanAt(before.index, '/rules/0/description')!
    const after = setString(before, '/rules/0/description', 'Another sentence.')
    expect(after.text.slice(0, span.valueStart)).toBe(FULL.slice(0, span.valueStart))
    const tail = FULL.length - span.valueEnd
    expect(after.text.slice(after.text.length - tail)).toBe(FULL.slice(span.valueEnd))
  })

  it('re-indexes rather than doing arithmetic on the old spans', () => {
    // A write moves every byte after it. The index that comes back must
    // describe the *new* text, or the next write splices into the middle of
    // another member.
    const one = setString(buffered(FULL), '/decision/intent', 'Much, much longer than it was.')
    const span = spanAt(one.index, '/decision/question')!
    // The span the *new* index reports really is the question's own bytes.
    expect(one.text.slice(span.valueStart, span.valueEnd)).toBe(
      JSON.stringify(JSON.parse(FULL).decision.question)
    )
    const two = setString(one, '/decision/question', 'A different question?')
    expect(JSON.parse(two.text).decision.question).toBe('A different question?')
    expect(JSON.parse(two.text).decision.intent).toBe('Much, much longer than it was.')
  })

  it('removes a blanked nonEmptyString rather than writing an empty one', () => {
    const after = setString(buffered(FULL), '/rules/1/rationale', '')
    expect(after.text).not.toContain('"rationale": ""')
    expect(JSON.parse(after.text).rules[1].rationale).toBeUndefined()
    // And the member beside it is untouched, comma and all.
    expect(() => JSON.parse(after.text)).not.toThrow()
  })

  it('writes an empty string where the schema does not say nonEmptyString', () => {
    // `publishedAt` is a `date`-formatted string. Blanking it is a diagnostic
    // the runtime issues, not an instruction to the desk to drop the member.
    const after = setString(buffered(FULL), '/sources/0/publishedAt', '')
    expect(JSON.parse(after.text).sources[0].publishedAt).toBe('')
  })

  it('writes enums, booleans and lists as the values they are', () => {
    let current = buffered(FULL)
    current = setEnum(current, '/rules/0/onUnknown', 'ignore')
    current = setBoolean(current, '/evidenceRequirements/1/required', true)
    current = setStringList(current, '/rules/0/sourceRefs', ['insurance-rule'])
    const after = JSON.parse(current.text)
    expect(after.rules[0].onUnknown).toBe('ignore')
    expect(after.evidenceRequirements[1].required).toBe(true)
    expect(after.rules[0].sourceRefs).toEqual(['insurance-rule'])
  })

  it('writes an emptied list rather than removing the member', () => {
    const after = setStringList(buffered(FULL), '/rules/0/evidenceRequirementRefs', [])
    expect(JSON.parse(after.text).rules[0].evidenceRequirementRefs).toEqual([])
  })

  it('writes bytes the author shaped, including bytes that do not parse', () => {
    // The form never refuses. `5000` where the schema wants `"5000"` is
    // writable, and `validate` is what names it at its own pointer.
    const after = setRawJson(buffered(FULL), '/rules/1/when/conditions/0/value', '5000')
    expect(after.text).toContain('"value": 5000')
    // And bytes that are not JSON at all reach the buffer, where the parse
    // error is reported rather than swallowed by a writer with an opinion.
    const broken = setRawJson(buffered(FULL), '/rules/1/when/conditions/0/value', '{oops')
    expect(broken.index.parseError).toBeDefined()
    expect(broken.text).toContain('{oops')
  })
})

describe('adding and removing', () => {
  it('adds a member in the container’s own layout', () => {
    const after = addMember(buffered(FULL), '/rules/0', 'extensions', '{ "example.note": "x" }')
    expect(JSON.parse(after.text).rules[0].extensions).toEqual({ 'example.note': 'x' })
    expect(after.text).toContain('\n      "extensions": { "example.note": "x" }')
  })

  it('adds an array element and addresses it at once', () => {
    const after = addElement(buffered(FULL), '/outcomes', '{ "id": "defer", "label": "Defer" }')
    expect(spanAt(after.index, '/outcomes/2')).toBeDefined()
    expect(JSON.parse(after.text).outcomes[2].id).toBe('defer')
  })

  it('takes a member out with exactly one comma', () => {
    const after = removeAt(buffered(FULL), '/rules/0/sourceRefs')
    expect(JSON.parse(after.text).rules[0].sourceRefs).toBeUndefined()
    expect(after.text).not.toContain(',,')
  })
})

describe('moving a rule', () => {
  it('moves the bytes and re-addresses what follows', () => {
    const after = moveRule(buffered(FULL), '/rules', 0, 1)
    expect(JSON.parse(after.text).rules.map((rule: { id: string }) => rule.id)).toEqual([
      'approve-when-clear',
      'screen-first'
    ])
    // `/rules/0` is now a different rule, which is exactly why the caller
    // marks the check stale rather than re-anchoring what it already had.
    const span = spanAt(after.index, '/rules/0/id')!
    expect(after.text.slice(span.valueStart, span.valueEnd)).toBe('"approve-when-clear"')
  })
})

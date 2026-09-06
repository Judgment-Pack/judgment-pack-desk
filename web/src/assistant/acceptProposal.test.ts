/**
 * Accepting a proposal, measured on the bytes.
 *
 * Two claims are asserted on every case that can carry them, because they are
 * the whole of what makes an accept reviewable: the buffer afterwards **parses
 * to the proposal's document**, and every member the proposal did not move is
 * **byte-identical** — the author's own indentation, spacing and member order
 * included.
 *
 * The drafts here are written with four spaces and with tabs on purpose. A
 * fixture already shaped like `JSON.stringify(x, null, 2)` cannot tell a
 * splice from a re-serialization, and the second claim would pass for a writer
 * that rewrote the whole file.
 */
import { describe, expect, it } from 'vitest'
import scenario from './conformance/scenario.json'
import { acceptState, applyProposal, writable } from './acceptProposal'
import { bytesAt, buffered } from '../packs/edit/writes'

const DRAFT_V1 = scenario.documents.DRAFT_V1 as Record<string, unknown>
const DRAFT_V2 = scenario.documents.DRAFT_V2 as Record<string, unknown>

/**
 * The draft as an author's file: four spaces, a trailing newline, and one
 * member the author collapsed onto a single line.
 *
 * The flourish is the point. A fixture whose every member is exactly what
 * `JSON.stringify` would produce cannot tell "the member was not written" from
 * "the member was written again and came back the same", and the byte claim
 * this file exists for would pass for a writer that rewrote the whole file.
 */
const fileText = (value: unknown) => collapse(`${JSON.stringify(value, null, 4)}\n`)

/** `decision`, as somebody who liked it on one line left it. */
function collapse(text: string): string {
  const held = bytesAt(buffered(text), '/decision')
  if (held === undefined) return text
  return text.replace(held, JSON.stringify(JSON.parse(held)))
}

describe('the scenario’s own accept', () => {
  const before = buffered(fileText(DRAFT_V1))
  const after = applyProposal(before, DRAFT_V2)

  it('leaves a buffer that parses to the proposal', () => {
    expect(JSON.parse(after.text)).toEqual(DRAFT_V2)
  })

  it('leaves every unchanged member byte-identical', () => {
    const moved = new Set(['version', 'rules', 'exceptions'])
    for (const name of Object.keys(DRAFT_V1)) {
      if (moved.has(name)) continue
      expect(bytesAt(after, `/${name}`)).toBe(bytesAt(before, `/${name}`))
    }
    // Including the one the author wrote their own way, which is the member a
    // re-serialization would quietly reformat.
    expect(bytesAt(after, '/decision')).toBe(JSON.stringify(DRAFT_V1.decision))
    expect(after.text).toContain(`\n    "decision": ${JSON.stringify(DRAFT_V1.decision)},`)
  })

  it('leaves the rules it did not touch byte-identical, in their new places', () => {
    // Three of four rules survive; the fourth is dropped. Each survivor's bytes
    // are the bytes it had, at whatever index it now holds.
    for (let index = 0; index < 3; index += 1) {
      expect(bytesAt(after, `/rules/${index}`)).toBe(bytesAt(before, `/rules/${index}`))
    }
    expect(bytesAt(after, '/rules/3')).toBeUndefined()
  })

  it('keeps the file’s own layout for what it wrote', () => {
    // The member the proposal adds is written at the indentation the document
    // uses, and with the step the document is written in — four spaces, not
    // this module's own two.
    expect(after.text).toContain('\n    "exceptions": [')
    expect(after.text).toContain('\n        {\n            "id": "large-claim-to-finance"')
    expect(after.text.endsWith('\n')).toBe(true)
    // And nothing was re-indented: the draft's four spaces survive.
    expect(after.text).not.toContain('\n  "specVersion"')
  })
})

describe('members', () => {
  const draft = fileText({ a: 1, b: { keep: true }, c: [1, 2] })

  it('adds a member the draft does not carry, after the one before it', () => {
    const after = applyProposal(buffered(draft), { a: 1, b: { keep: true }, d: 'new', c: [1, 2] })
    expect(JSON.parse(after.text)).toEqual({ a: 1, b: { keep: true }, d: 'new', c: [1, 2] })
    expect(after.text.indexOf('"d"')).toBeGreaterThan(after.text.indexOf('"b"'))
    expect(after.text.indexOf('"d"')).toBeLessThan(after.text.indexOf('"c"'))
  })

  it('adds a member the proposal puts first', () => {
    const after = applyProposal(buffered(draft), { z: 0, a: 1, b: { keep: true }, c: [1, 2] })
    expect(JSON.parse(after.text)).toEqual({ z: 0, a: 1, b: { keep: true }, c: [1, 2] })
    expect(after.text.indexOf('"z"')).toBeLessThan(after.text.indexOf('"a"'))
  })

  it('removes a member the proposal does not carry, and one comma with it', () => {
    const after = applyProposal(buffered(draft), { a: 1, c: [1, 2] })
    expect(JSON.parse(after.text)).toEqual({ a: 1, c: [1, 2] })
    expect(after.text).not.toContain('keep')
  })

  it('tells a member written as null from a member that is absent', () => {
    const nulled = applyProposal(buffered(draft), { a: 1, b: null, c: [1, 2] })
    expect(JSON.parse(nulled.text)).toEqual({ a: 1, b: null, c: [1, 2] })
    const added = applyProposal(buffered(fileText({ a: 1 })), { a: 1, b: null })
    expect(JSON.parse(added.text).b).toBeNull()
    expect(Object.hasOwn(JSON.parse(added.text) as object, 'b')).toBe(true)
  })

  it('writes a member that is an array over one that was an object', () => {
    const before = buffered(fileText({ rules: { one: 1 }, title: 'kept' }))
    const after = applyProposal(before, { rules: [{ id: 'a' }], title: 'kept' })
    expect(JSON.parse(after.text)).toEqual({ rules: [{ id: 'a' }], title: 'kept' })
    expect(bytesAt(after, '/title')).toBe(bytesAt(before, '/title'))
  })

  it('escapes a member name that is a pointer trap', () => {
    const before = buffered(fileText({ 'a/b': 1, 'c~d': 2 }))
    const after = applyProposal(before, { 'a/b': 9, 'c~d': 2 })
    expect(JSON.parse(after.text)).toEqual({ 'a/b': 9, 'c~d': 2 })
    expect(bytesAt(after, '/c~0d')).toBe(bytesAt(before, '/c~0d'))
  })

  it('writes a member into a document indented with tabs, in tabs', () => {
    const before = buffered('{\n\t"a": 1\n}\n')
    const after = applyProposal(before, { a: 1, b: { deep: [1] } })
    expect(JSON.parse(after.text)).toEqual({ a: 1, b: { deep: [1] } })
    expect(after.text).toContain('\n\t"b": {')
    // The step is the document's own, read off its first member.
    expect(after.text).toContain('\n\t\t"deep"')
  })
})

describe('the elements of an array member', () => {
  const rules = [
    { id: 'a', when: 'first' },
    { id: 'b', when: 'second' },
    { id: 'c', when: 'third' }
  ]
  const draft = buffered(fileText({ rules, title: 'kept' }))

  it('reorders by id without rewriting an element', () => {
    const after = applyProposal(draft, { rules: [rules[2], rules[0], rules[1]], title: 'kept' })
    expect(JSON.parse(after.text)).toEqual({
      rules: [rules[2], rules[0], rules[1]],
      title: 'kept'
    })
    expect(bytesAt(after, '/rules/0')).toBe(bytesAt(draft, '/rules/2'))
    expect(bytesAt(after, '/rules/1')).toBe(bytesAt(draft, '/rules/0'))
    expect(bytesAt(after, '/rules/2')).toBe(bytesAt(draft, '/rules/1'))
  })

  it('writes only the element that changed', () => {
    const after = applyProposal(draft, {
      rules: [rules[0], { id: 'b', when: 'CHANGED' }, rules[2]],
      title: 'kept'
    })
    expect(JSON.parse(after.text).rules[1]).toEqual({ id: 'b', when: 'CHANGED' })
    expect(bytesAt(after, '/rules/0')).toBe(bytesAt(draft, '/rules/0'))
    expect(bytesAt(after, '/rules/2')).toBe(bytesAt(draft, '/rules/2'))
  })

  it('inserts an element in the middle and at the front', () => {
    const middle = applyProposal(draft, {
      rules: [rules[0], { id: 'new' }, rules[1], rules[2]],
      title: 'kept'
    })
    expect(JSON.parse(middle.text).rules.map((rule: { id: string }) => rule.id)).toEqual([
      'a',
      'new',
      'b',
      'c'
    ])
    const front = applyProposal(draft, { rules: [{ id: 'new' }, ...rules], title: 'kept' })
    expect(JSON.parse(front.text).rules.map((rule: { id: string }) => rule.id)).toEqual([
      'new',
      'a',
      'b',
      'c'
    ])
    expect(bytesAt(front, '/rules/1')).toBe(bytesAt(draft, '/rules/0'))
  })

  it('removes an element from the middle', () => {
    const after = applyProposal(draft, { rules: [rules[0], rules[2]], title: 'kept' })
    expect(JSON.parse(after.text).rules.map((rule: { id: string }) => rule.id)).toEqual(['a', 'c'])
    expect(bytesAt(after, '/rules/1')).toBe(bytesAt(draft, '/rules/2'))
  })

  it('empties an array without leaving a comma behind', () => {
    const after = applyProposal(draft, { rules: [], title: 'kept' })
    expect(JSON.parse(after.text)).toEqual({ rules: [], title: 'kept' })
  })

  it('fills an array the draft left empty', () => {
    const before = buffered(fileText({ rules: [], title: 'kept' }))
    const after = applyProposal(before, { rules: [{ id: 'a' }, { id: 'b' }], title: 'kept' })
    expect(JSON.parse(after.text)).toEqual({ rules: [{ id: 'a' }, { id: 'b' }], title: 'kept' })
  })

  it('does one removal and one addition for an id carried twice', () => {
    const before = buffered(fileText({ rules: [{ id: 'a', n: 1 }, { id: 'a', n: 2 }] }))
    const after = applyProposal(before, { rules: [{ id: 'a', n: 3 }] })
    expect(JSON.parse(after.text)).toEqual({ rules: [{ id: 'a', n: 3 }] })
  })

  it('matches unkeyed elements by position', () => {
    const before = buffered(fileText({ tags: ['one', 'two', 'three'] }))
    const after = applyProposal(before, { tags: ['one', 'TWO', 'three'] })
    expect(JSON.parse(after.text)).toEqual({ tags: ['one', 'TWO', 'three'] })
    expect(bytesAt(after, '/tags/0')).toBe(bytesAt(before, '/tags/0'))
    expect(bytesAt(after, '/tags/2')).toBe(bytesAt(before, '/tags/2'))
  })

  it('reorders, rewrites, drops and inserts in one accept', () => {
    const wanted = [
      { id: 'new-first' },
      { id: 'c', when: 'third' },
      { id: 'a', when: 'REWRITTEN' },
      { id: 'tail' }
    ]
    const after = applyProposal(draft, { rules: wanted, title: 'kept' })
    expect(JSON.parse(after.text)).toEqual({ rules: wanted, title: 'kept' })
    // `c` did not change, so its bytes are the bytes it had.
    expect(bytesAt(after, '/rules/1')).toBe(bytesAt(draft, '/rules/2'))
    expect(bytesAt(after, '/title')).toBe(bytesAt(draft, '/title'))
  })
})

describe('a draft there is nothing to splice into', () => {
  it('writes the whole document where the bytes do not scan', () => {
    const before = buffered('{ "specVersion": ')
    const after = applyProposal(before, DRAFT_V2)
    expect(JSON.parse(after.text)).toEqual(DRAFT_V2)
  })

  it('writes the whole document where the desk’s reading is not JSON.parse’s', () => {
    // A duplicated member. Splicing into it would edit one of two members the
    // runtime refuses the document for carrying.
    const before = buffered('{"title": "one", "title": "two"}')
    const after = applyProposal(before, { title: 'three' })
    expect(JSON.parse(after.text)).toEqual({ title: 'three' })
    expect(after.text.match(/"title"/g)).toHaveLength(1)
  })

  it('replaces a document that is JSON and not an object, in place', () => {
    const after = applyProposal(buffered('[1, 2, 3]\n'), { title: 'a pack' })
    expect(JSON.parse(after.text)).toEqual({ title: 'a pack' })
    expect(after.text.endsWith('\n')).toBe(true)
  })

  it('writes a proposal that is not an object over a draft that is', () => {
    const after = applyProposal(buffered(fileText({ a: 1 })), [1, 2])
    expect(JSON.parse(after.text)).toEqual([1, 2])
  })

  it('writes nothing at all for a proposal that is not JSON data', () => {
    const before = buffered(fileText({ a: 1 }))
    const cyclic: Record<string, unknown> = {}
    cyclic.self = cyclic
    expect(applyProposal(before, cyclic).text).toBe(before.text)
    expect(writable(cyclic)).toBe(false)
    expect(writable({ a: 1 })).toBe(true)
  })

  it('starts an empty buffer off with the document and a newline', () => {
    const after = applyProposal(buffered(''), { a: 1 })
    expect(after.text).toBe('{\n  "a": 1\n}\n')
  })
})

describe('a proposal read through a mutable object', () => {
  it('writes what was diffed, not what the object answers next', () => {
    // The ToolGate's lesson, one layer up: a getter that changes its answer
    // between the diff and the write would put a document on disk that nobody
    // was shown. Canonicalizing first is what makes the two the same reading.
    let answers = 0
    const document = {
      get title() {
        answers += 1
        return `title ${answers}`
      }
    }
    const after = applyProposal(buffered(fileText({ title: 'old' })), document)
    expect(JSON.parse(after.text)).toEqual({ title: 'title 1' })
    expect(answers).toBe(1)
  })
})

describe('when Accept may be pressed', () => {
  const open = {
    editing: true,
    proposal: true,
    running: false,
    saving: false,
    disposition: 'open' as const,
    writable: true
  }

  it('is enabled on ?edit, with a proposal, after the run has ended', () => {
    expect(acceptState(open)).toEqual({ enabled: true, why: '' })
  })

  it('is refused on the reading route, and says where to go', () => {
    expect(acceptState({ ...open, editing: false })).toEqual({
      enabled: false,
      why: 'Open Edit to accept.'
    })
  })

  it('is refused while the session is still running', () => {
    const state = acceptState({ ...open, running: true })
    expect(state.enabled).toBe(false)
    expect(state.why).toContain('still running')
  })

  it('is refused with no proposal, once accepted, once rejected, and mid-save', () => {
    expect(acceptState({ ...open, proposal: false }).why).toContain('no proposal')
    expect(acceptState({ ...open, disposition: 'accepted' }).why).toContain('already in the draft')
    expect(acceptState({ ...open, disposition: 'rejected' }).why).toContain('rejected')
    expect(acceptState({ ...open, saving: true }).why).toContain('being saved')
  })

  it('is refused for a proposal this desk could not write', () => {
    expect(acceptState({ ...open, writable: false }).why).toContain('not JSON data')
  })

  it('gives one reason at a time, in the order a reader would ask', () => {
    // Every reason is a real state, and the first one is the one that answers
    // "why can I not press this": the route before the run, the run before the
    // save.
    expect(acceptState({ ...open, editing: false, running: true }).why).toBe('Open Edit to accept.')
    expect(acceptState({ ...open, running: true, saving: true }).why).toContain('still running')
  })
})

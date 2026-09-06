/**
 * The diff, over the fixture documents and over the shapes that break it.
 *
 * `DRAFT_V1 → DRAFT_V2` is the scenario the conformance session runs, so the
 * case a reader actually sees is measured here rather than a pair of documents
 * written to make a diff look tidy. Everything else in this file is a shape the
 * comparison has to survive: a reorder, an id carried twice, a document that is
 * not an object, a member that is an array on one side and an object on the
 * other, and a member whose value is `null`.
 */
import { describe, expect, it } from 'vitest'
import scenario from './conformance/scenario.json'
import {
  diffProposal,
  matchElements,
  plain,
  readDraft,
  sameValue,
  type DiffEntry
} from './proposalDiff'

const DRAFT_V1 = scenario.documents.DRAFT_V1 as unknown
const DRAFT_V2 = scenario.documents.DRAFT_V2 as unknown

const text = (value: unknown) => JSON.stringify(value, null, 2)
const at = (entries: readonly DiffEntry[], pointer: string) =>
  entries.find((entry) => entry.pointer === pointer)

describe('the scenario’s own two drafts', () => {
  const diff = diffProposal(text(DRAFT_V1), DRAFT_V2)

  it('compares against the draft, member by member', () => {
    expect(diff.against).toBe('the draft')
    expect(diff.reason).toBeUndefined()
    expect(diff.problem).toBeUndefined()
    expect(diff.counts).toEqual({ added: 1, removed: 0, changed: 2, unchanged: 11 })
  })

  it('names the two members that moved and the one that arrived', () => {
    expect(at(diff.entries, '/version')?.status).toBe('changed')
    expect(at(diff.entries, '/rules')?.status).toBe('changed')
    expect(at(diff.entries, '/exceptions')?.status).toBe('added')
    // The eleven that did not move are reported as unchanged rather than
    // omitted: the pane collapses them under one line with a count, and a
    // member missing from the list would read as a member the diff lost.
    expect(at(diff.entries, '/title')?.status).toBe('unchanged')
    expect(at(diff.entries, '/metadata')?.status).toBe('unchanged')
  })

  it('carries the old and the new text of a changed member', () => {
    const version = at(diff.entries, '/version')!
    expect(JSON.parse(version.before!)).toBe('0.0.1')
    expect(JSON.parse(version.after!)).toBe(JSON.parse(text(DRAFT_V2)).version)
  })

  it('descends into rules and matches them by id', () => {
    const rules = at(diff.entries, '/rules')!
    const children = rules.children!
    // Three rules survive by id, one is dropped, none is added.
    expect(children.filter((entry) => entry.status === 'removed').map((entry) => entry.label)).toEqual([
      'large-claim'
    ])
    expect(children.filter((entry) => entry.status === 'added')).toEqual([])
    expect(children.filter((entry) => entry.status !== 'removed').map((entry) => entry.label)).toEqual([
      'small-claim',
      'receipted-claim',
      'unreceipted-claim'
    ])
  })

  it('is the same diff whichever way the proposal object was built', () => {
    // The proposal arrives as an engine's parsed object; the same document
    // reached through a live object with a `toJSON` must diff identically,
    // because both are canonicalized before anything is compared.
    const live = { toJSON: () => DRAFT_V2 }
    expect(diffProposal(text(DRAFT_V1), live)).toEqual(diff)
  })
})

describe('canonicalizing before anything is compared', () => {
  it('reads a value through a getter once, and compares the reading', () => {
    // The ToolGate's own lesson: a getter can answer one thing while the diff
    // is looking and another when the writer serializes. Whatever it answers
    // first is what is diffed and what would be written — one reading, not two.
    let answers = 0
    const document = {
      title: 'steady',
      get version() {
        answers += 1
        return `v${answers}`
      }
    }
    const diff = diffProposal(text({ title: 'steady', version: 'v1' }), document)
    expect(answers).toBe(1)
    expect(at(diff.entries, '/version')?.status).toBe('unchanged')
  })

  it('refuses a proposal that is not JSON data at all', () => {
    const cyclic: Record<string, unknown> = { title: 'a pack' }
    cyclic.self = cyclic
    const diff = diffProposal(text({ title: 'a pack' }), cyclic)
    expect(diff.problem).toContain('not JSON data')
    expect(diff.entries).toEqual([])
  })

  it('drops what JSON has no word for', () => {
    expect(plain({ a: 1, b: undefined, c: () => 1 })).toEqual({ a: 1 })
    expect(plain(undefined)).toBeUndefined()
  })

  it('calls two objects the same only where their member order is the same', () => {
    expect(sameValue({ a: 1, b: 2 }, { a: 1, b: 2 })).toBe(true)
    // The same JSON value, two documents: accepting one over the other moves
    // bytes, so the diff has to say so.
    expect(sameValue({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(false)
  })
})

describe('where there is no draft to compare with', () => {
  it('says so, and reports the whole proposal as added, when the bytes do not parse', () => {
    const diff = diffProposal('{ "specVersion": ', DRAFT_V2)
    expect(diff.against).toBe('nothing')
    expect(diff.reason).toContain('not JSON')
    expect(diff.entries.every((entry) => entry.status === 'added')).toBe(true)
    expect(diff.entries).toHaveLength(Object.keys(DRAFT_V2 as object).length)
  })

  it('withholds the comparison where the desk’s reading is not JSON.parse’s', () => {
    // A duplicated member: the scanner keeps the first, `JSON.parse` keeps the
    // last, and the runtime refuses the document. Nothing may be diffed
    // through a reading nobody else shares.
    const diff = diffProposal('{"title": "one", "title": "two"}', { title: 'three' })
    expect(diff.against).toBe('nothing')
    expect(diff.reason).toContain('appears more than once')
    expect(diff.entries.map((entry) => entry.status)).toEqual(['added'])
  })

  it('says so where the draft is JSON and not an object', () => {
    const diff = diffProposal('[1, 2, 3]', { title: 'a pack' })
    expect(diff.against).toBe('nothing')
    expect(diff.reason).toContain('not an object')
  })

  it('says so where there are no bytes at all', () => {
    expect(diffProposal(undefined, { title: 'a pack' }).reason).toContain('no draft')
    expect(diffProposal('   ', { title: 'a pack' }).reason).toContain('no draft')
  })
})

describe('a proposal that is not an object', () => {
  it('is one thing to accept or reject, whole', () => {
    const diff = diffProposal('{"title": "a pack"}', [1, 2])
    expect(diff.entries).toHaveLength(1)
    expect(diff.entries[0]!.pointer).toBe('')
    expect(diff.entries[0]!.status).toBe('changed')
    expect(JSON.parse(diff.entries[0]!.after!)).toEqual([1, 2])
  })

  it('is unchanged where the draft is the very same value', () => {
    // Not reachable from the pack routes, and stated anyway: "the draft is not
    // an object" and "the proposal repeats it" are different answers.
    const diff = diffProposal('{"a": 1}', { a: 1 })
    expect(diff.counts.unchanged).toBe(1)
  })
})

describe('members of the wrong shape, and members that are null', () => {
  it('reports an array-vs-object member as one changed member, with no elements', () => {
    const diff = diffProposal('{"rules": {"one": 1}}', { rules: [{ id: 'a' }] })
    const rules = at(diff.entries, '/rules')!
    expect(rules.status).toBe('changed')
    expect(rules.children).toBeUndefined()
  })

  it('tells a member written as null from a member that is absent', () => {
    const added = diffProposal('{"title": "a"}', { title: 'a', escalation: null })
    expect(at(added.entries, '/escalation')?.status).toBe('added')
    expect(at(added.entries, '/escalation')?.after).toBe('null')

    const removed = diffProposal('{"title": "a", "escalation": null}', { title: 'a' })
    expect(at(removed.entries, '/escalation')?.status).toBe('removed')
    expect(at(removed.entries, '/escalation')?.before).toBe('null')

    const kept = diffProposal('{"escalation": null}', { escalation: null })
    expect(at(kept.entries, '/escalation')?.status).toBe('unchanged')
  })

  it('escapes a member name that is a pointer trap', () => {
    const diff = diffProposal('{"a/b": 1}', { 'a/b': 2, 'c~d': 3 })
    expect(at(diff.entries, '/a~1b')?.status).toBe('changed')
    expect(at(diff.entries, '/c~0d')?.status).toBe('added')
  })
})

describe('matching an array’s elements', () => {
  it('matches by id, and reports a move rather than four rewrites', () => {
    const before = [{ id: 'a', n: 1 }, { id: 'b', n: 2 }, { id: 'c', n: 3 }]
    const after = [{ id: 'c', n: 3 }, { id: 'a', n: 1 }, { id: 'b', n: 2 }]
    const diff = diffProposal(text({ rules: before }), { rules: after })
    const children = at(diff.entries, '/rules')!.children!
    expect(children.map((entry) => [entry.label, entry.status, entry.moved === true])).toEqual([
      ['c', 'unchanged', true],
      ['a', 'unchanged', true],
      ['b', 'unchanged', true]
    ])
  })

  it('matches unkeyed elements by position', () => {
    const { pairs, dropped, byId } = matchElements([1, 2, 3], [1, 9])
    expect(pairs).toEqual([0, 1])
    expect(dropped).toEqual([2])
    expect(byId).toBe(false)
  })

  it('never pairs a keyed element with an element of another id', () => {
    // A new rule at index 0 must not be reported as an edit of the rule that
    // happened to sit there: one rule's text printed as another's old value
    // looks exactly like an answer.
    const before = [{ id: 'a', n: 1 }]
    const after = [{ id: 'z', n: 1 }]
    const { pairs, dropped } = matchElements(before, after)
    expect(pairs).toEqual([undefined])
    expect(dropped).toEqual([0])
  })

  it('uses an id that names two elements for nothing at all', () => {
    const before = [{ id: 'a', n: 1 }, { id: 'a', n: 2 }, { id: 'b', n: 3 }]
    const after = [{ id: 'a', n: 1 }, { id: 'b', n: 3 }]
    const { pairs, dropped } = matchElements(before, after)
    // `b` matches; both `a`s are a removal and the proposal's `a` an addition.
    expect(pairs).toEqual([undefined, 2])
    expect(dropped).toEqual([0, 1])
  })

  it('reports a collision as a removal and an addition, not as a change', () => {
    const diff = diffProposal(
      text({ rules: [{ id: 'a', n: 1 }, { id: 'a', n: 2 }] }),
      { rules: [{ id: 'a', n: 1 }] }
    )
    const children = at(diff.entries, '/rules')!.children!
    expect(children.map((entry) => entry.status)).toEqual(['added', 'removed', 'removed'])
  })

  it('matches an element added at the end without disturbing the others', () => {
    const before = [{ id: 'a' }, { id: 'b' }]
    const after = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]
    const diff = diffProposal(text({ rules: before }), { rules: after })
    const children = at(diff.entries, '/rules')!.children!
    expect(children.map((entry) => [entry.label, entry.status])).toEqual([
      ['a', 'unchanged'],
      ['b', 'unchanged'],
      ['c', 'added']
    ])
  })

  it('anchors every element on its own pointer', () => {
    const diff = diffProposal(text({ rules: [{ id: 'a' }] }), { rules: [{ id: 'a' }, { id: 'b' }] })
    expect(at(diff.entries, '/rules')!.children!.map((entry) => entry.pointer)).toEqual([
      '/rules/0',
      '/rules/1'
    ])
  })
})

describe('reading the draft', () => {
  it('is one rule, and the diff and the writer share it', () => {
    expect(readDraft('{"a": 1}')).toEqual({ value: { a: 1 } })
    expect('problem' in readDraft('{"a": 1, "a": 2}')).toBe(true)
    expect('problem' in readDraft('nonsense')).toBe(true)
    expect('problem' in readDraft(undefined)).toBe(true)
  })
})

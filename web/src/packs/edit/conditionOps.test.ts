/**
 * The builder's edits, as the splices they are.
 *
 * Every case asserts two things: that the intended member changed, and that
 * **every byte outside the touched span is identical**. The second is the whole
 * mechanism — a writer that re-serialized would pass the first and fail the
 * second, and ADR-0019 makes a human read the diff either way.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  addChild,
  changeKind,
  conditionKind,
  removeNode,
  setOperator,
  wrapInGroup
} from './conditionOps'
import { buffered } from './writes'

const PACK = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)

/** `/rules/1/when` — an `all` of a `fact` and an `any`. */
const WHEN = '/rules/1/when'
const start = () => buffered(PACK)

/** Everything before and after one span, which a splice must not touch. */
function outside(text: string, from: number, to: number): [string, string] {
  return [text.slice(0, from), text.slice(to)]
}

describe('what kind a node is', () => {
  it('reads the schema’s five, and calls anything else other', () => {
    expect(conditionKind({ op: 'all', conditions: [] })).toBe('all')
    expect(conditionKind({ op: 'fact' })).toBe('fact')
    expect(conditionKind({ op: 'evidence-present' })).toBe('evidence-present')
    expect(conditionKind({ op: 'not' })).toBe('not')
    expect(conditionKind({ op: 'literal', value: true })).toBe('literal')
    // A runtime may grow a sixth. It is printed, never guessed at.
    expect(conditionKind({ op: 'between', low: 1 })).toBe('other')
    expect(conditionKind('yes')).toBe('other')
    expect(conditionKind(null)).toBe('other')
    expect(conditionKind([{ op: 'all' }])).toBe('other')
  })
})

describe('adding a condition', () => {
  it('writes one child in the layout its siblings already use', () => {
    const next = addChild(start(), WHEN)
    const before = start()
    const span = before.index.spans.get(`${WHEN}/conditions`)!
    const [head, tail] = outside(PACK, span.valueStart, span.valueEnd)
    expect(next.text.startsWith(head)).toBe(true)
    expect(next.text.endsWith(tail)).toBe(true)
    const children = next.index.value as { rules: { when: { conditions: unknown[] } }[] }
    expect(children.rules[1]!.when.conditions).toHaveLength(3)
    // The placeholder is the simplest node the schema declares, which is the
    // one the builder itself can then edit.
    expect(children.rules[1]!.when.conditions[2]).toEqual({ op: 'literal', value: true })
  })

  it('takes a sibling’s own indentation rather than inventing one', () => {
    const next = addChild(start(), WHEN)
    // The fixture indents a group's children by ten spaces. The added child
    // sits on a line of its own with exactly that run in front of it.
    expect(next.text).toContain('\n          {\n            "op": "literal",')
  })
})

describe('wrapping a node in a group', () => {
  it('keeps the child’s own bytes and moves them as a block', () => {
    const child = `${WHEN}/conditions/0`
    const before = start()
    const span = before.index.spans.get(child)!
    const raw = PACK.slice(span.valueStart, span.valueEnd)
    const next = wrapInGroup(before, child, 'any')
    const [head, tail] = outside(PACK, span.valueStart, span.valueEnd)
    expect(next.text.startsWith(head)).toBe(true)
    expect(next.text.endsWith(tail)).toBe(true)
    // Every line of the child survives, shifted by two levels and not
    // re-serialized: the author's own member order and spacing are still there.
    for (const line of raw.split('\n').map((entry) => entry.trim())) {
      expect(next.text).toContain(line)
    }
    const value = next.index.value as {
      rules: { when: { conditions: { op: string; conditions: unknown[] }[] } }[]
    }
    expect(value.rules[1]!.when.conditions[0]!.op).toBe('any')
    expect(value.rules[1]!.when.conditions[0]!.conditions).toHaveLength(1)
  })
})

describe('removing a node', () => {
  it('takes the element and exactly one adjacent comma', () => {
    const next = removeNode(start(), `${WHEN}/conditions/0`)
    const value = next.index.value as { rules: { when: { conditions: { op: string }[] } }[] }
    expect(value.rules[1]!.when.conditions).toHaveLength(1)
    expect(value.rules[1]!.when.conditions[0]!.op).toBe('any')
    expect(next.text).not.toContain(',,')
    // The remaining sibling keeps its own indentation: the removal takes the
    // deleted node's layout, never its neighbour's.
    expect(next.text).toContain('\n          {\n            "op": "any",')
  })

  it('leaves the text alone where the pointer names nothing', () => {
    expect(removeNode(start(), `${WHEN}/conditions/9`).text).toBe(PACK)
  })
})

describe('changing a node’s kind', () => {
  it('carries the author’s own operand into the new kind', () => {
    const at = `${WHEN}/conditions/0`
    const next = changeKind(start(), at, 'literal')
    const value = next.index.value as {
      rules: { when: { conditions: { op: string; value: unknown }[] } }[]
    }
    const node = value.rules[1]!.when.conditions[0]!
    expect(node.op).toBe('literal')
    // `"5000"` was the fact's operand and is still `"5000"`: a form that
    // retyped it would be deciding what the rule means.
    expect(node.value).toBe('5000')
  })

  it('keeps the group’s children when all becomes any', () => {
    const next = changeKind(start(), WHEN, 'any')
    const value = next.index.value as { rules: { when: { op: string; conditions: unknown[] } }[] }
    expect(value.rules[1]!.when.op).toBe('any')
    expect(value.rules[1]!.when.conditions).toHaveLength(2)
  })

  it('moves one word when all becomes any, and no other byte', () => {
    // The change most often wanted, and the one that needs no new bytes at
    // all: every member the new kind declares is already there. Carried
    // through the serializer it re-emitted the whole subtree — every nested
    // condition re-indented, and `"5000"` and `5.0` re-printed by
    // `JSON.stringify` — so a one-word edit arrived as the whole-subtree diff
    // a reviewer has to read.
    const before = start()
    const span = before.index.spans.get(`${WHEN}/op`)!
    const next = changeKind(before, WHEN, 'any')
    const [head, tail] = outside(PACK, span.valueStart, span.valueEnd)
    expect(next.text.startsWith(head)).toBe(true)
    expect(next.text.endsWith(tail)).toBe(true)
    // Which is to say: exactly the four characters of the word changed.
    expect(next.text.length).toBe(PACK.length)
    expect(next.text).toBe(`${head}"any"${tail}`)
  })

  it('writes the new kind’s members empty where the old node had none', () => {
    const next = changeKind(start(), `${WHEN}/conditions/0`, 'evidence-present')
    const value = next.index.value as {
      rules: { when: { conditions: Record<string, unknown>[] } }[]
    }
    const node = value.rules[1]!.when.conditions[0]!
    expect(node).toEqual({ op: 'evidence-present', evidenceRequirement: '' })
  })

  it('starts the replacement on the line the node started on', () => {
    const at = `${WHEN}/conditions/0`
    const before = start()
    const span = before.index.spans.get(at)!
    const next = changeKind(before, at, 'literal')
    const [head, tail] = outside(PACK, span.valueStart, span.valueEnd)
    expect(next.text.startsWith(head)).toBe(true)
    expect(next.text.endsWith(tail)).toBe(true)
    // The document's own indentation unit, and the node's own base: a one-word
    // change that reformatted the subtree would put the whole condition in the
    // diff.
    expect(next.text).toContain('{\n            "op": "literal",\n            "value": "5000"\n          }')
  })

  it('leaves a kind it has never heard of alone', () => {
    expect(changeKind(start(), WHEN, 'between').text).toBe(PACK)
  })
})

describe('changing the operator', () => {
  it('writes the operator and leaves the operand exactly as it was', () => {
    const at = `${WHEN}/conditions/0`
    const before = start()
    const next = setOperator(before, at, 'in')
    const value = next.index.value as {
      rules: { when: { conditions: { operator: string; value: unknown }[] } }[]
    }
    const node = value.rules[1]!.when.conditions[0]!
    expect(node.operator).toBe('in')
    // `in` wants a list and this is a string. The runtime names that; the form
    // does not retype it, because retyping would be an edit nobody asked for.
    expect(node.value).toBe('5000')
    // The operand's own bytes, in the buffer as it now is: the write shortened
    // the text ahead of it, and the span is re-read rather than arithmetic'd.
    const operand = next.index.spans.get(`${at}/value`)!
    expect(next.text.slice(operand.valueStart, operand.valueEnd)).toBe('"5000"')
  })
})

/**
 * The mirrored schema, held in `fixtures.test.ts`'s idiom.
 *
 * There is no schema file in this repository to diff against — the runtime
 * bundles it — so what is held here is the same thing that file holds: the
 * fixtures are documents `jpack spec validate` reads, and every enum-valued
 * member of each of them must be a member of the list `shape.ts` offers. A
 * list that lost a value would stop offering something a conformant document
 * uses, and the fixtures are where that shows.
 *
 * The other half is the rule that is *not* a list: the operand control, which
 * decides whether an author is offered a decimal string, a list, or any JSON
 * at all — and which the schema states as three `if`/`then` clauses inside the
 * `fact` node.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  CONDITION_MEMBERS,
  DECIMAL_STRING,
  ENUMS,
  LOCAL_ID,
  isNonEmptyString,
  operandControl
} from './shape'

const FIXTURES = join(import.meta.dirname, '..', '__fixtures__')
const load = (name: string) =>
  JSON.parse(readFileSync(join(FIXTURES, name), 'utf8')) as Record<string, unknown>

const CONFORMANT = ['full.pack.json', 'minimal.pack.json', 'reordered.pack.json', 'exceptions.pack.json']

describe('the closed lists this desk offers', () => {
  it.each(CONFORMANT)('offers every value %s actually uses', (name) => {
    const doc = load(name)
    for (const rule of list(doc.rules)) {
      expect(ENUMS.onUnknown).toContain(at(rule, 'onUnknown'))
    }
    for (const exception of list(doc.exceptions)) {
      expect(ENUMS.effect).toContain(at(exception, 'effect'))
      expect(ENUMS.onUnknown).toContain(at(exception, 'onUnknown'))
    }
    for (const trigger of list(at(doc.escalation, 'triggers'))) {
      expect(ENUMS.triggers).toContain(trigger)
    }
    const targetKind = at(at(doc.escalation, 'target'), 'kind')
    if (targetKind !== undefined) expect(ENUMS.targetKind).toContain(targetKind)
    for (const source of list(doc.sources)) {
      expect(ENUMS.locatorKind).toContain(at(at(source, 'locator'), 'kind'))
    }
    for (const requirement of list(doc.evidenceRequirements)) {
      const kind = at(requirement, 'kind')
      if (kind !== undefined) expect(ENUMS.evidenceKind).toContain(kind)
    }
    for (const review of list(at(doc.metadata, 'reviews'))) {
      expect(ENUMS.reviewDisposition).toContain(at(review, 'disposition'))
    }
    for (const node of conditions(doc)) {
      expect(ENUMS.conditionOp).toContain(node.op)
      if (node.op === 'fact') expect(ENUMS.factOperator).toContain(node.operator)
    }
  })

  it('names every condition kind it offers, with the members that kind carries', () => {
    // The five `oneOf` branches, each with its own required members. A kind the
    // builder offers and has no member list for would write `{"op": "not"}`
    // and nothing else.
    expect(Object.keys(CONDITION_MEMBERS).sort()).toEqual([...ENUMS.conditionOp].sort())
    expect(CONDITION_MEMBERS.fact).toEqual(['op', 'path', 'operator', 'value'])
    expect(CONDITION_MEMBERS.all).toEqual(['op', 'conditions'])
  })
})

describe('the operand rule', () => {
  it('gives the four ordered comparisons a decimal string and nothing else', () => {
    for (const operator of [
      'greater-than',
      'greater-than-or-equal',
      'less-than',
      'less-than-or-equal'
    ]) {
      expect(operandControl(operator)).toBe('decimal')
    }
  })

  it('gives `in` a list and the two equalities any JSON', () => {
    expect(operandControl('in')).toBe('list')
    expect(operandControl('equals')).toBe('json')
    expect(operandControl('not-equals')).toBe('json')
  })

  it('gives an operator this desk has never seen the widest control', () => {
    // `"value": true` is the fact node's base: any JSON. An operator a later
    // spec adds is offered that rather than a shape this desk guessed.
    expect(operandControl('sounds-like')).toBe('json')
  })
})

describe('the shapes offered, which are never gates', () => {
  it('knows which members are removed by blanking rather than written empty', () => {
    expect(isNonEmptyString('/rules/0/description')).toBe(true)
    expect(isNonEmptyString('/decision/intent')).toBe(true)
    expect(isNonEmptyString('/sources/1/citation/excerpt')).toBe(true)
    expect(isNonEmptyString('/escalation/target/name')).toBe(true)
    // `publishedAt` is a `date`-formatted string, not a `nonEmptyString`: an
    // empty one is a diagnostic, and dropping the member instead would be the
    // desk deciding the author meant to remove it.
    expect(isNonEmptyString('/sources/0/publishedAt')).toBe(false)
    // An id is a `localId`, not a `nonEmptyString`.
    expect(isNonEmptyString('/rules/0/id')).toBe(false)
    expect(isNonEmptyString('/rules/0/outcome')).toBe(false)
  })

  it('holds the id and decimal patterns the schema spells', () => {
    expect(LOCAL_ID.test('approve-when-clear')).toBe(true)
    expect(LOCAL_ID.test('Approve')).toBe(false)
    expect(DECIMAL_STRING.test('5000')).toBe(true)
    expect(DECIMAL_STRING.test('-12.5')).toBe(true)
    expect(DECIMAL_STRING.test('05')).toBe(false)
    expect(DECIMAL_STRING.test('5e3')).toBe(false)
  })
})

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function at(value: unknown, name: string): unknown {
  return typeof value === 'object' && value !== null
    ? (value as Record<string, unknown>)[name]
    : undefined
}

function conditions(document: Record<string, unknown>): Record<string, unknown>[] {
  const found: Record<string, unknown>[] = []
  const walk = (node: unknown) => {
    if (typeof node !== 'object' || node === null) return
    const value = node as Record<string, unknown>
    found.push(value)
    for (const child of list(value.conditions)) walk(child)
    if (value.condition !== undefined) walk(value.condition)
  }
  walk(document.applicability)
  for (const rule of list(document.rules)) walk(at(rule, 'when'))
  for (const exception of list(document.exceptions)) walk(at(exception, 'when'))
  return found
}

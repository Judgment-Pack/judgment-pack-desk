/**
 * What the builder does to a condition, as text edits — and the one place that
 * decides what kind a node is.
 *
 * **The kind discrimination is here rather than in the renderer**, and the
 * renderer reads it from here. Two copies of "what makes a node a `fact`" is
 * two chances for the tree to draw one thing and the builder to edit another,
 * which for a condition is the difference between the policy on screen and the
 * policy on disk.
 *
 * Every function below is a splice through `documentText`. Nothing here
 * re-serializes a document, and the two edits that must write new bytes — a
 * kind change and a group wrap — take their layout from **the node's own
 * first line** rather than from a house style. A one-word change that
 * reformatted a subtree would put the whole condition in the diff ADR-0019
 * makes a human read.
 *
 * **A new node is `{"op": "literal", "value": true}`.** It is a placeholder,
 * and it is chosen because it is one the builder can then edit: an empty
 * object is a node this desk prints as JSON and offers no controls for, which
 * would leave an author who pressed *add* with something they cannot change.
 * The runtime's own answer about it is whatever `validate` says.
 */
import { indexDocument, insertMember, removeMember, spanAt } from '../documentText'
import { CONDITION_MEMBERS, ENUMS } from './shape'
import { buffered, type Buffered } from './writes'

/** The five kinds the schema declares, plus what a sixth reads as here. */
export type ConditionKind = 'literal' | 'all' | 'any' | 'not' | 'fact' | 'evidence-present' | 'other'

/**
 * What kind of node this is.
 *
 * A node whose `op` is not one of the schema's five is `other` — printed as
 * the JSON it is and not editable, exactly as the reading tree already holds.
 * A runtime may grow a sixth kind, and a desk that guessed at its shape would
 * be editing a condition it does not understand.
 */
export function conditionKind(condition: unknown): ConditionKind {
  if (typeof condition !== 'object' || condition === null || Array.isArray(condition)) return 'other'
  const op = (condition as { op?: unknown }).op
  if (typeof op !== 'string') return 'other'
  return (ENUMS.conditionOp as readonly string[]).includes(op) ? (op as ConditionKind) : 'other'
}

/** The node the builder writes when it is asked for one it has no bytes for. */
export const NEW_NODE = { op: 'literal', value: true }

/** Add one condition to an `all` or `any` group, in the layout it already uses. */
export function addChild(current: Buffered, groupPointer: string): Buffered {
  const at = `${groupPointer}/conditions`
  if (spanAt(current.index, at) === undefined) {
    // The group carries no `conditions` yet. It is written as a one-element
    // array rather than an empty one: an author who pressed *add* asked for a
    // condition, and an empty array is a different document.
    const text = insertMember(
      current.text,
      current.index,
      groupPointer,
      'conditions',
      serialize([NEW_NODE], layoutOf(current, groupPointer))
    )
    return buffered(text)
  }
  // **The layout is a sibling's, not the array's.** `insertMember` puts the
  // new element behind a neighbour's own leading run, so the bytes written have
  // to be indented for where that run puts them — indenting for the `conditions`
  // member itself leaves every continuation line two columns short of the brace
  // above it. Where there is no sibling to learn from, one step in from the
  // group is the only position that follows from the document.
  const first = spanAt(current.index, `${at}/0`)
  const layout =
    first === undefined ? deeper(layoutOf(current, groupPointer)) : layoutOf(current, `${at}/0`)
  return buffered(
    insertMember(current.text, current.index, at, undefined, serialize(NEW_NODE, layout))
  )
}

/** One level further in than a layout, for a container with nothing inside it. */
function deeper(layout: Layout): Layout {
  return layout.step === '' ? layout : { base: layout.base + layout.step, step: layout.step }
}

/**
 * Put a node inside a new group, keeping the node's **own bytes**.
 *
 * The child is moved as text and indented as a block, so the author's spacing,
 * their member order and their own line breaks survive being wrapped. A wrap
 * that re-serialized the child would rewrite a subtree to say what it already
 * said.
 */
export function wrapInGroup(current: Buffered, pointer: string, op: 'all' | 'any'): Buffered {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return current
  const raw = current.text.slice(span.valueStart, span.valueEnd)
  const layout = layoutOf(current, pointer)
  const written =
    layout.step === ''
      ? `{"op": ${JSON.stringify(op)}, "conditions": [${raw}]}`
      : [
          '{',
          `${layout.base}${layout.step}"op": ${JSON.stringify(op)},`,
          `${layout.base}${layout.step}"conditions": [`,
          `${layout.base}${layout.step}${layout.step}${shift(raw, `${layout.step}${layout.step}`)}`,
          `${layout.base}${layout.step}]`,
          `${layout.base}}`
        ].join('\n')
  return buffered(
    current.text.slice(0, span.valueStart) + written + current.text.slice(span.valueEnd)
  )
}

/**
 * Take one node out.
 *
 * A member of an object — a `not`'s `condition` — and an element of an array —
 * a group's child — are the same removal, and `removeMember` is what draws the
 * span for both, with exactly one adjacent comma and the node's own layout.
 */
export function removeNode(current: Buffered, pointer: string): Buffered {
  if (spanAt(current.index, pointer) === undefined) return current
  return buffered(removeMember(current.text, current.index, pointer))
}

/**
 * Make this node another kind, carrying every value the new kind has somewhere
 * to put.
 *
 * The author's `value` survives a `fact` becoming a `literal`, their `path`
 * survives a round trip through another kind and back, and their `conditions`
 * survive `all` becoming `any` — which is the change most often wanted and the
 * one a form that retyped would make destructive. What the new kind requires
 * and the old node had nothing for is written empty, and `validate` is what
 * says whether the result is a document.
 *
 * **Where the new kind needs no member the old node lacks, only the one word
 * moves.** `all` → `any` is the change most often wanted and it needs no new
 * bytes at all: carrying the subtree through the serializer would re-emit
 * every nested condition, re-indent them, and turn the author's `5.0` into
 * `5` — a one-word edit arriving as the whole-subtree diff ADR-0019 makes a
 * human read. So that case splices `op` the way `setOperator` splices
 * `operator`, and the serializer is reached only where the node has no bytes
 * for a member the new kind requires.
 */
export function changeKind(current: Buffered, pointer: string, kind: string): Buffered {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return current
  const members = CONDITION_MEMBERS[kind]
  if (members === undefined) return current
  const before = current.index.value === undefined ? undefined : nodeAt(current, pointer)
  const held = (name: string): unknown =>
    typeof before === 'object' && before !== null && !Array.isArray(before)
      ? (before as Record<string, unknown>)[name]
      : undefined

  if (carriesExactly(before, members)) return setOp(current, pointer, kind)

  const next: Record<string, unknown> = {}
  for (const name of members) {
    if (name === 'op') {
      next.op = kind
      continue
    }
    const carried = held(name)
    if (carried !== undefined) {
      next[name] = carried
      continue
    }
    next[name] = blank(name)
  }
  const layout = layoutOf(current, pointer)
  const written = serialize(next, layout)
  return buffered(
    current.text.slice(0, span.valueStart) + written + current.text.slice(span.valueEnd)
  )
}

/**
 * Whether this node already carries exactly the members the new kind declares
 * — no more and no fewer, so nothing is added and nothing is dropped.
 *
 * The *names* are compared and never the values: a node's own `conditions` are
 * whatever the author wrote, and this is the question of whether any byte
 * outside `op` has to move.
 */
function carriesExactly(node: unknown, members: readonly string[]): boolean {
  if (typeof node !== 'object' || node === null || Array.isArray(node)) return false
  const names = Object.keys(node as Record<string, unknown>)
  if (names.length !== members.length) return false
  return names.every((name) => members.includes(name))
}

/** Write this node's `op` and leave every other byte where it is. */
function setOp(current: Buffered, pointer: string, kind: string): Buffered {
  const at = `${pointer}/op`
  const span = spanAt(current.index, at)
  if (span === undefined) return current
  return buffered(
    current.text.slice(0, span.valueStart) + JSON.stringify(kind) + current.text.slice(span.valueEnd)
  )
}

/** What a member the old node had nothing for is written as. */
function blank(name: string): unknown {
  switch (name) {
    case 'conditions':
      return []
    case 'condition':
      return { ...NEW_NODE }
    case 'operator':
      return 'equals'
    case 'value':
      return ''
    default:
      // `path` and `evidenceRequirement`. An empty string is a member the
      // runtime names by code; an absent one is a member it names by code too,
      // and the empty one is the one a control can be pointed at.
      return ''
  }
}

/**
 * Set a `fact` node's operator and **leave the operand exactly as it is**.
 *
 * The four ordered comparisons take a decimal string and `in` takes a list, so
 * changing the operator can make the operand wrong. That is the author's to
 * fix and the runtime's to name: a form that retyped `"5000"` into `["5000"]`
 * on the way to `in` would be deciding what the rule means. The operand
 * control changes shape underneath, which is what tells the author the
 * operand is now being read differently.
 */
export function setOperator(current: Buffered, pointer: string, operator: string): Buffered {
  const at = `${pointer}/operator`
  if (spanAt(current.index, at) === undefined) {
    return buffered(
      insertMember(current.text, current.index, pointer, 'operator', JSON.stringify(operator))
    )
  }
  const span = spanAt(current.index, at)!
  return buffered(
    current.text.slice(0, span.valueStart) +
      JSON.stringify(operator) +
      current.text.slice(span.valueEnd)
  )
}

/** The value one pointer names inside the buffer's own reading of the bytes. */
function nodeAt(current: Buffered, pointer: string): unknown {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return undefined
  // Read back through this desk's own scanner rather than `JSON.parse`, so the
  // bytes a kind change carries forward are the bytes the page is drawing.
  const scanned = indexDocument(current.text.slice(span.valueStart, span.valueEnd))
  return scanned.value
}

/** Where a node sits, in the terms new bytes have to be written in. */
interface Layout {
  /** The indentation of the line the node begins on. */
  base: string
  /** One level of this document's own indentation, or `''` for inline. */
  step: string
}

/**
 * The layout this node is written in, read off the document.
 *
 * `base` is the run in front of the node on its own line, so a replacement
 * starts where the node started. `step` is the document's own indentation
 * unit, taken from the first indented line in the file — never a house style,
 * because a document indented with tabs and one indented with four spaces are
 * two documents and a writer with a preference would convert one of them on
 * the first edit.
 *
 * A node written **inline** — no newline in front of it — gets `''`, and the
 * replacement is written inline too. That is what keeps a one-line condition
 * from becoming six lines because its operator changed.
 */
function layoutOf(current: Buffered, pointer: string): Layout {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return { base: '', step: '' }
  let back = span.memberStart - 1
  while (back >= 0 && /[ \t\n\r]/.test(current.text[back]!)) back -= 1
  const leading = current.text.slice(back + 1, span.memberStart)
  const newline = leading.lastIndexOf('\n')
  if (newline < 0) return { base: '', step: '' }
  return { base: leading.slice(newline + 1), step: indentUnit(current.text) }
}

/** This document's own indentation, from its first indented line. */
function indentUnit(text: string): string {
  const found = /\n([ \t]+)\S/.exec(text)
  return found === null ? '  ' : found[1]!
}

/**
 * One value, written in this document's layout.
 *
 * `JSON.stringify` with an indent produces the shape, and every line after the
 * first is moved to where the node sits. There is no other serializer in this
 * desk and this one is reached only by the two edits that have no bytes to
 * splice: everything else moves the author's own.
 */
function serialize(value: unknown, layout: Layout): string {
  if (layout.step === '') return JSON.stringify(value)
  return shift(JSON.stringify(value, null, layout.step), layout.base)
}

/** Every line after the first, moved right by one prefix. */
function shift(text: string, prefix: string): string {
  return text.split('\n').join(`\n${prefix}`)
}

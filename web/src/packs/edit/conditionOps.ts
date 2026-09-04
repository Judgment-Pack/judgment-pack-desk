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
import { pointer as pointerOf, parsePointer } from '../pointers'
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
  const held = spanAt(current.index, at) === undefined ? undefined : nodeAt(current, at)
  if (held !== undefined && !Array.isArray(held)) {
    // **`"conditions": {}` is valid JSON and not a list.** `insertMember`
    // refuses a container that is not an array, so the button drew, took the
    // click, and changed nothing at all. An author who pressed *add* asked for
    // a condition: this is the one edit that says so out loud, and it is
    // theirs to undo — the bytes it replaces are one action back on the stack.
    const span = spanAt(current.index, at)!
    const written = serialize([NEW_NODE], layoutOf(current, at))
    return buffered(
      current.text.slice(0, span.valueStart) + written + current.text.slice(span.valueEnd)
    )
  }
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
  return layout.step === ''
    ? layout
    : { base: layout.base + layout.step, step: layout.step, eol: layout.eol }
}

/**
 * The author's own bytes, moved right — **without touching their line endings**.
 *
 * A CRLF document's lines end `\r\n`, and splitting on `\n` alone would leave
 * the `\r` at the end of each line and put the indentation after it. So the
 * split is on the document's own ending.
 */
function indent(text: string, prefix: string, eol: string): string {
  return text.split(eol).join(`${eol}${prefix}`)
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
          `${layout.base}${layout.step}${layout.step}${indent(raw, `${layout.step}${layout.step}`, layout.eol)}`,
          `${layout.base}${layout.step}]`,
          `${layout.base}}`
        ].join(layout.eol)
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
 * **Nothing the author wrote is re-serialized, in any case.** This used to
 * rebuild the whole node from JavaScript values wherever the member sets
 * differed, and a rebuild goes through `JSON.stringify` over values the scanner
 * produced with `Number()`: `9007199254740993` came back as
 * `9007199254740992`, `5.0` as `5`, an escaped `\u00e9` as the character, and
 * every line as an LF in a document written with CRLF. It is three splices
 * instead — the word, the members the new kind has nowhere to put, and the ones
 * it requires that the node has nothing for — so a member both kinds carry
 * keeps its own bytes because nothing ever reads them.
 */
export function changeKind(current: Buffered, pointer: string, kind: string): Buffered {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return current
  const members = CONDITION_MEMBERS[kind]
  if (members === undefined) return current
  const before = current.index.value === undefined ? undefined : nodeAt(current, pointer)

  // Not an object at all — a node whose bytes are `null`, a list, a number.
  // There is nothing to splice into and nothing of the author's to carry, so
  // this is the one path that writes a node from values, and every value in it
  // is this desk's own.
  if (typeof before !== 'object' || before === null || Array.isArray(before)) {
    const written = serialize(skeleton(kind, members), layoutOf(current, pointer))
    return buffered(
      current.text.slice(0, span.valueStart) + written + current.text.slice(span.valueEnd)
    )
  }

  const present = Object.keys(before as Record<string, unknown>)
  let next = current

  // 1. The word itself.
  next = present.includes('op')
    ? setOp(next, pointer, kind)
    : buffered(
        insertMember(next.text, next.index, pointer, 'op', JSON.stringify(kind), { first: true })
      )

  // 2. The members the new kind has nowhere to put. Only those: a `value` that
  //    both kinds carry keeps **its own bytes**, which is the whole point —
  //    `9007199254740993` is not `9007199254740992`, `5.0` is not `5`, and a
  //    `\u00e9` an author escaped is not the character it stands for.
  for (const name of present) {
    if (name === 'op' || members.includes(name)) continue
    next = buffered(removeMember(next.text, next.index, memberPointer(pointer, name)))
  }

  // 3. The members the new kind requires and this node has nothing for. Written
  //    empty, because what they should say is the author's to decide and the
  //    runtime's to judge.
  for (const name of members) {
    if (name === 'op' || present.includes(name)) continue
    next = buffered(
      insertMember(
        next.text,
        next.index,
        pointer,
        name,
        serialize(blank(name), deeper(layoutOf(next, pointer))),
        placeAfter(next, pointer, members, name)
      )
    )
  }
  return next
}

/** One member of a node, addressed. */
function memberPointer(pointer: string, name: string): string {
  const parts = parsePointer(pointer)
  return parts === undefined ? `${pointer}/${name}` : pointerOf([...parts, name])
}

/**
 * Where a member this kind requires goes: after the nearest earlier member the
 * kind declares that the node actually carries, and first where there is none.
 *
 * The kind's own order is asked rather than the node's, because the node has
 * nothing to say about a member it does not have.
 */
function placeAfter(
  current: Buffered,
  pointer: string,
  members: readonly string[],
  name: string
): { after: string } | { first: true } {
  const at = members.indexOf(name)
  for (let earlier = at - 1; earlier >= 0; earlier -= 1) {
    const sibling = memberPointer(pointer, members[earlier]!)
    if (spanAt(current.index, sibling) !== undefined) return { after: sibling }
  }
  return { first: true }
}

/** A whole node of one kind, from this desk's own values and nobody else's. */
function skeleton(kind: string, members: readonly string[]): Record<string, unknown> {
  const written: Record<string, unknown> = {}
  for (const name of members) written[name] = name === 'op' ? kind : blank(name)
  return written
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
  /**
   * This document's own line ending.
   *
   * `JSON.stringify(value, null, step)` emits LF, and a document written with
   * CRLF got LF lines spliced into it — a file with two kinds of line ending,
   * from one form control, in a diff a human is required to read.
   */
  eol: string
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
  const eol = lineEnding(current.text)
  const span = spanAt(current.index, pointer)
  if (span === undefined) return { base: '', step: '', eol }
  let back = span.memberStart - 1
  while (back >= 0 && /[ \t\n\r]/.test(current.text[back]!)) back -= 1
  const leading = current.text.slice(back + 1, span.memberStart)
  const newline = leading.lastIndexOf('\n')
  if (newline < 0) return { base: '', step: '', eol }
  return { base: leading.slice(newline + 1), step: indentUnit(current.text), eol }
}

/** This document's own line ending, from the first one it uses. */
function lineEnding(text: string): string {
  const found = text.indexOf('\n')
  if (found < 0) return '\n'
  return text[found - 1] === '\r' ? '\r\n' : '\n'
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
  return shift(JSON.stringify(value, null, layout.step), layout.base, layout.eol)
}

/** Every line after the first, moved right by one prefix and ended as the document ends its lines. */
function shift(text: string, prefix: string, eol: string): string {
  return text.split('\n').join(`${eol}${prefix}`)
}

/**
 * The one place a form edit becomes text.
 *
 * Every function here takes the buffer and a pointer, and returns the buffer
 * with exactly that member's bytes replaced, removed, added or moved. Nothing
 * here serializes a document and nothing here parses one in order to write:
 * the buffer *is* the document, and a form that re-serialized would hand a
 * reviewer a diff of every line for a one-word change (ADR-0019).
 *
 * **The text is re-indexed after every write.** Spans are byte offsets, and
 * an edit moves every byte after it, so the alternative is span arithmetic —
 * a second model of the file, free to disagree with the first. Indexing is a
 * single pass over a document of a few kilobytes; a wrong offset is a splice
 * into the middle of another member.
 *
 * `shape.ts` says what a member may hold. This says how it is written.
 */
import {
  indexDocument,
  insertMember,
  moveElement,
  removeMember,
  replaceValue,
  type DocumentIndex,
  type Placement
} from '../documentText'
import { isNonEmptyString } from './shape'

/** The buffer and this desk's reading of it, always in step. */
export interface Buffered {
  text: string
  index: DocumentIndex
}

/** Read a buffer once. Every write below returns one of these. */
export function buffered(text: string): Buffered {
  return { text, index: indexDocument(text) }
}

function rewritten(text: string): Buffered {
  return buffered(text)
}

/**
 * Write one string member, or remove it where blanking means removal.
 *
 * `""` is a document the runtime refuses by name where the schema says
 * `nonEmptyString`; the member's absence is a document that is merely smaller,
 * and where the member was required the refusal names it at its own pointer —
 * an address this form already has, so the diagnostic lands on the field that
 * is now empty. Which members those are is `shape.ts`'s, from the schema.
 */
export function setString(current: Buffered, pointer: string, value: string): Buffered {
  if (value === '' && isNonEmptyString(pointer)) {
    return rewritten(removeMember(current.text, current.index, pointer))
  }
  return rewritten(replaceValue(current.text, current.index, pointer, JSON.stringify(value)))
}

/**
 * Write one enum member.
 *
 * A separate function from `setString` because the empty rule is different: a
 * closed enum has no empty member, so blanking one — which only happens when a
 * caller offers a blank option — removes it rather than writing `""`.
 */
export function setEnum(current: Buffered, pointer: string, value: string): Buffered {
  if (value === '') return rewritten(removeMember(current.text, current.index, pointer))
  return rewritten(replaceValue(current.text, current.index, pointer, JSON.stringify(value)))
}

export function setBoolean(current: Buffered, pointer: string, value: boolean): Buffered {
  return rewritten(replaceValue(current.text, current.index, pointer, value ? 'true' : 'false'))
}

/**
 * Write a list of strings — `evidenceRequirementRefs`, `sourceRefs`,
 * `escalation.triggers`.
 *
 * An empty list is written as `[]` and the member is **not** removed: an
 * author who cleared every reference has said the rule cites nothing, and
 * `escalation.triggers` has `minItems: 1`, which is a diagnostic the runtime
 * issues by name rather than an omission the desk decides on their behalf.
 */
export function setStringList(current: Buffered, pointer: string, values: readonly string[]): Buffered {
  return rewritten(replaceValue(current.text, current.index, pointer, JSON.stringify(values)))
}

/**
 * Write bytes the caller has already shaped.
 *
 * The escape hatch every honest form needs: an operand that may be any JSON,
 * a condition subtree, a raw value an author typed. It is written in verbatim,
 * including bytes that do not parse — `validate` is what names those, and a
 * writer that refused them would be a second validator with an opinion the
 * runtime does not share.
 */
export function setRawJson(current: Buffered, pointer: string, json: string): Buffered {
  return rewritten(replaceValue(current.text, current.index, pointer, json))
}

/** Take one member out, with exactly one adjacent comma. */
export function removeAt(current: Buffered, pointer: string): Buffered {
  return rewritten(removeMember(current.text, current.index, pointer))
}

/** Add one member to an object, in the layout that object already uses. */
export function addMember(
  current: Buffered,
  containerPointer: string,
  name: string,
  json: string,
  placement?: Placement
): Buffered {
  return rewritten(
    insertMember(current.text, current.index, containerPointer, name, json, placement)
  )
}

/** Add one element to an array, in the layout its elements already use. */
export function addElement(
  current: Buffered,
  arrayPointer: string,
  json: string,
  placement?: Placement
): Buffered {
  return rewritten(
    insertMember(current.text, current.index, arrayPointer, undefined, json, placement)
  )
}

/**
 * Move one rule, or one exception, to another position.
 *
 * Order is §7-significant — the first matching rule is the one that fires — so
 * this is an edit to what the pack decides. The caller marks the check stale
 * afterwards: every `/rules/N` pointer past the smaller of the two indices now
 * names a different rule, and a diagnostic re-anchored across that lands on a
 * rule it is not about.
 */
export function moveRule(
  current: Buffered,
  arrayPointer: string,
  from: number,
  to: number
): Buffered {
  return rewritten(moveElement(current.text, current.index, arrayPointer, from, to))
}

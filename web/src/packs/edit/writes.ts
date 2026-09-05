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
import { pointer as pointerOf, parsePointer } from '../pointers'
import {
  indexDocument,
  insertMember,
  moveElement,
  removeMember,
  replaceValue,
  spanAt,
  type DocumentIndex,
  type Placement
} from '../documentText'
import { isNonEmptyString, memberOrder } from './shape'

/** The buffer and this desk's reading of it, always in step. */
export interface Buffered {
  text: string
  index: DocumentIndex
}

/** Read a buffer once. Every write below returns one of these. */
export function buffered(text: string): Buffered {
  return { text, index: indexDocument(text) }
}

/**
 * The **exact bytes** one member is written with, or undefined where the
 * document does not carry it.
 *
 * A control over a value that may be any JSON — a `fact` node's operand under
 * `equals` — has to show what is on disk rather than a re-serialization of it,
 * or the author's own `5000`, `"5000"` and `5.0` all display alike and the
 * first keystroke rewrites bytes nobody touched.
 */
export function bytesAt(current: Buffered, pointer: string): string | undefined {
  const span = spanAt(current.index, pointer)
  if (span === undefined) return undefined
  return current.text.slice(span.valueStart, span.valueEnd)
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
    if (spanAt(current.index, pointer) === undefined) return current
    return rewritten(removeMember(current.text, current.index, pointer))
  }
  return place(current, pointer, JSON.stringify(value))
}

/**
 * Write one member's bytes, wherever it is — including where it is **not**.
 *
 * `replaceValue` splices a span, and a member the document does not declare
 * has no span: it returned the text unchanged, which is a form field that
 * silently does nothing. That is the ordinary case for the fields this editor
 * has to show, because the ones an author reaches for are the ones their draft
 * omitted — `onUnknown` on an exception is required and is the member most
 * drafts of one leave out, and the diagnostic that names it anchors on a field
 * that has to be writable or the diagnostic is a dead end.
 *
 * So an absent member is **inserted**, at the position the schema's own
 * property order gives it, in the layout the container already uses. Nothing
 * else moves: `insertMember` takes a neighbour's own leading run verbatim, and
 * every byte outside the inserted span is untouched.
 */
function place(current: Buffered, pointer: string, json: string): Buffered {
  if (spanAt(current.index, pointer) !== undefined) {
    return rewritten(replaceValue(current.text, current.index, pointer, json))
  }
  const parts = parsePointer(pointer)
  if (parts === undefined || parts.length === 0) return current
  const name = parts[parts.length - 1]!
  const container = pointerOf(parts.slice(0, -1))
  const containerSpan = spanAt(current.index, container)
  // No container either — an absent rule's absent member. One missing member is
  // a field; a missing object is a different edit, and inventing the object
  // around it would write members the author never asked for.
  if (containerSpan === undefined) return current
  if (current.text[containerSpan.valueStart] !== '{') return current
  return rewritten(
    insertMember(current.text, current.index, container, name, json, placeFor(current, container, name))
  )
}

/**
 * Where a member the document does not declare goes: after the nearest earlier
 * member the **schema** lists that this document actually has.
 *
 * The schema's order is asked rather than the document's, because the document
 * has nothing to say about a member it does not carry. Where the container
 * declares no order this desk knows — an `extensions` object, whose keys are
 * the author's own — the member goes last, which is the only position that
 * asserts nothing.
 */
function placeFor(current: Buffered, container: string, name: string): Placement {
  const order = memberOrder(container)
  const at = order.indexOf(name)
  if (at < 0) return { last: true }
  const parts = parsePointer(container) ?? []
  for (let earlier = at - 1; earlier >= 0; earlier -= 1) {
    const sibling = pointerOf([...parts, order[earlier]!])
    if (spanAt(current.index, sibling) !== undefined) return { after: sibling }
  }
  return { first: true }
}

/**
 * Write one enum member.
 *
 * A separate function from `setString` because the empty rule is different: a
 * closed enum has no empty member, so blanking one — which only happens when a
 * caller offers a blank option — removes it rather than writing `""`.
 */
export function setEnum(current: Buffered, pointer: string, value: string): Buffered {
  if (value === '') {
    if (spanAt(current.index, pointer) === undefined) return current
    return rewritten(removeMember(current.text, current.index, pointer))
  }
  return place(current, pointer, JSON.stringify(value))
}

export function setBoolean(current: Buffered, pointer: string, value: boolean): Buffered {
  return place(current, pointer, value ? 'true' : 'false')
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
  return place(current, pointer, JSON.stringify(values))
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
  return place(current, pointer, json)
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

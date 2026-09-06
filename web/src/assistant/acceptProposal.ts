/**
 * Accepting a proposal: the diff, turned into the writers a form edit uses.
 *
 * ADR-0001 again — "the desk renders the diff and applies an accepted proposal
 * through the span-preserving writer. No engine event writes anything." So
 * there is no serializer here and no second model of the document: every change
 * is a splice at a pointer, exactly as `writes.ts` performs one for a field, and
 * **every byte the proposal does not move survives**, including the author's own
 * indentation and the spacing they left around a member.
 *
 * That is not tidiness. ADR-0019 makes a human read the diff of a save, and an
 * accept that re-serialized the document would hand that human every line of
 * the file for a two-member proposal — which is exactly the review nobody can
 * do.
 *
 * **One call, one buffer.** This returns the whole edit as a single `Buffered`,
 * so the pane makes exactly one `write` and the author's Undo takes the whole
 * accept back in one step. The pane never reaches past `write`: the editing
 * session exposes no `commit`, and this function is the only thing it hands it.
 *
 * The order the operations are applied in is the whole trick, because each
 * write re-indexes the text and array pointers are positions:
 *
 * 1. members the proposal does not carry, removed;
 * 2. members that changed — element by element where both sides are arrays;
 * 3. members the draft does not carry, added, in the proposal's own order so
 *    each one has the member before it to sit after.
 *
 * And inside one array: removals from the back, then the moves that put the
 * survivors in the proposal's order, then the survivors that changed, then the
 * new elements in ascending order — so that when an element is inserted at
 * index j, positions 0…j-1 already hold what the proposal says they hold.
 */
import { agreesWithParse, spanAt, type Placement } from '../packs/documentText'
import {
  addElement,
  addMember,
  buffered,
  moveRule,
  removeAt,
  setRawJson,
  type Buffered
} from '../packs/edit/writes'
import { pointer as pointerOf } from '../packs/pointers'
import { isObject, matchElements, plain, sameValue } from './proposalDiff'

/**
 * The whole accept, as one buffer.
 *
 * The document is canonicalized **first**, on the ToolGate's reasoning: a value
 * with a getter or a `toJSON` can answer one thing while the diff is computed
 * and another while a writer serializes it, so what is written would not be
 * what was shown. What is diffed and what is written are the same plain data.
 *
 * A proposal that is not JSON data at all writes nothing: there is nothing to
 * write, and inventing a document from it would be this desk having an opinion
 * about bytes it could not read.
 */
export function applyProposal(current: Buffered, document: unknown): Buffered {
  const proposed = plain(document)
  if (proposed === undefined) return current

  // **The draft has to be a document this desk and `JSON.parse` agree about.**
  // The form editor holds the same rule, and for the same reason: a splice
  // through a reading nobody else shares edits a document nobody has. Where
  // they disagree — a duplicated member, bytes that do not scan — there is
  // nothing to splice into, and the proposal is written whole.
  const readable =
    current.index.parseError === undefined &&
    agreesWithParse(current.text, current.index).length === 0
  const draft = readable ? current.index.value : undefined
  if (!isObject(draft) || !isObject(proposed)) {
    if (!readable || spanAt(current.index, '') === undefined) return whole(current, proposed)
    // The bytes scan and are not an object, or the proposal is not one: one
    // value replaces another, in place, and the file's own leading and
    // trailing bytes stay where they are.
    return setRawJson(current, '', text(proposed, ''))
  }

  let next = current
  for (const name of Object.keys(draft)) {
    if (Object.hasOwn(proposed, name)) continue
    next = removeAt(next, pointerOf([name]))
  }
  for (const name of Object.keys(proposed)) {
    if (!Object.hasOwn(draft, name)) continue
    const at = pointerOf([name])
    const before = draft[name]
    const after = proposed[name]
    // Untouched: no write at all, so the member's bytes are the bytes it had.
    if (sameValue(before, after)) continue
    if (Array.isArray(before) && Array.isArray(after) && isArrayAt(next, at)) {
      next = applyArray(next, at, before, after)
      continue
    }
    next = setRawJson(next, at, text(after, indentOf(next, at)))
  }
  for (const name of Object.keys(proposed)) {
    if (Object.hasOwn(draft, name)) continue
    const value = proposed[name]
    next = addMember(next, '', name, text(value, childIndent(next, '')), placeAfter(next, proposed, name))
  }
  return next
}

/** Whether the pointer names an array in the text as it stands. */
function isArrayAt(current: Buffered, at: string): boolean {
  const span = spanAt(current.index, at)
  return span !== undefined && current.text[span.valueStart] === '['
}

/**
 * One array member, element by element.
 *
 * The point of doing it this way rather than writing the whole array: an
 * element the proposal did not touch keeps its own bytes, so a proposal that
 * edits one rule out of nine produces a diff of one rule.
 */
function applyArray(
  current: Buffered,
  at: string,
  before: readonly unknown[],
  after: readonly unknown[]
): Buffered {
  const { pairs } = matchElements(before, after)
  const kept = new Map<number, number>()
  for (let to = 0; to < pairs.length; to += 1) {
    const from = pairs[to]
    if (from !== undefined) kept.set(from, to)
  }
  let next = current

  // Removals from the back: taking element 1 out renumbers element 2, and a
  // pointer is a position.
  for (let from = before.length - 1; from >= 0; from -= 1) {
    if (kept.has(from)) continue
    next = removeAt(next, `${at}/${from}`)
  }

  // What is left, in the order the document holds it, each labelled with the
  // position the proposal gives it.
  const order = [...kept.keys()].sort((left, right) => left - right).map((from) => kept.get(from)!)
  const wanted = [...order].sort((left, right) => left - right)
  for (let position = 0; position < wanted.length; position += 1) {
    if (order[position] === wanted[position]) continue
    const found = order.indexOf(wanted[position]!, position)
    next = moveRule(next, at, found, position)
    const [moved] = order.splice(found, 1)
    order.splice(position, 0, moved!)
  }

  // The survivors that changed, where they now sit.
  for (let position = 0; position < wanted.length; position += 1) {
    const to = wanted[position]!
    const from = pairs[to]!
    if (sameValue(before[from], after[to])) continue
    const pointer = `${at}/${position}`
    next = setRawJson(next, pointer, text(after[to], indentOf(next, pointer)))
  }

  // The new elements, in ascending order — so positions 0…j-1 are already
  // what the proposal says they are when the element at j is inserted.
  for (let to = 0; to < after.length; to += 1) {
    if (pairs[to] !== undefined) continue
    const placement: Placement = to === 0 ? { first: true } : { after: `${at}/${to - 1}` }
    const neighbour = to === 0 ? `${at}/0` : `${at}/${to - 1}`
    next = addElement(next, at, text(after[to], indentOf(next, neighbour)), placement)
  }
  return next
}

/**
 * The whole buffer, replaced.
 *
 * The one case with nothing to splice into: bytes that do not scan, or two
 * readings that disagree about them. The trailing newline of the file that was
 * there is kept, because a file that ended in one is a file that ends in one.
 */
function whole(current: Buffered, proposed: unknown): Buffered {
  const tail = current.text === '' || current.text.endsWith('\n') ? '\n' : ''
  return buffered(`${text(proposed, '')}${tail}`)
}

/**
 * One value as the bytes to write, laid out where it is going.
 *
 * Two spaces per level, which is the layout of every fixture and of everything
 * `jpack` writes — and then every line after the first carries the indentation
 * the member itself sits at, so a rule written into a rules array four columns
 * in is not left hanging at column zero. The first line is not indented: the
 * writer is splicing it in after `"name": ` or after a neighbour's own leading
 * run.
 */
function text(value: unknown, indent: string): string {
  const written = JSON.stringify(value, null, 2) ?? 'null'
  return indent === '' ? written : written.split('\n').join(`\n${indent}`)
}

/** The whitespace one member sits behind on its own line. */
function indentOf(current: Buffered, at: string): string {
  const span = spanAt(current.index, at)
  return span === undefined ? '' : indentAt(current.text, span.memberStart)
}

/** The layout one container's members are written with, read off the first. */
function childIndent(current: Buffered, container: string): string {
  const prefix = container === '' ? '/' : `${container}/`
  for (const [pointer, span] of current.index.spans) {
    if (!pointer.startsWith(prefix)) continue
    if (pointer.slice(prefix.length).includes('/')) continue
    return indentAt(current.text, span.memberStart)
  }
  return ''
}

function indentAt(text: string, offset: number): string {
  let start = offset
  while (start > 0 && text[start - 1] !== '\n') start -= 1
  let end = start
  while (end < offset && (text[end] === ' ' || text[end] === '\t')) end += 1
  return text.slice(start, end)
}

/**
 * Where a member the draft does not carry goes: after the nearest member
 * before it in the **proposal's** order that the document actually has.
 *
 * The proposal's order is asked because the proposal is the document being
 * accepted; the document's own order decides nothing about a member it does not
 * carry. Where nothing before it is there, it goes first, which is the position
 * that says the least.
 */
function placeAfter(
  current: Buffered,
  proposed: Record<string, unknown>,
  name: string
): Placement {
  const names = Object.keys(proposed)
  for (let index = names.indexOf(name) - 1; index >= 0; index -= 1) {
    const earlier = pointerOf([names[index]!])
    if (spanAt(current.index, earlier) !== undefined) return { after: earlier }
  }
  return { first: true }
}

/** Which proposals this desk can put into the draft at all. */
export function writable(document: unknown): boolean {
  return plain(document) !== undefined
}

/** What the proposal is called once a person has acted on it. */
export type Disposition = 'open' | 'accepted' | 'rejected'

export interface AcceptState {
  enabled: boolean
  /** Why not, in the words the button's `title` carries. Empty where enabled. */
  why: string
}

/**
 * Whether Accept may be pressed, and the one sentence saying why not.
 *
 * A pure function because every reason is a state somebody can be in and the
 * table of them is what has to be right — a control that is enabled while a run
 * is still going would apply a document the session is about to replace, and a
 * control enabled on the reading route would write to a buffer no save can
 * reach.
 */
export function acceptState(input: {
  /** True on `?edit`, which is the only route with a draft to accept into. */
  editing: boolean
  /** True where a proposal has arrived. */
  proposal: boolean
  /** True while the session is still running, in either of its phases. */
  running: boolean
  /** True while a save is in flight. */
  saving: boolean
  disposition: Disposition
  /** True where the proposal is JSON data this desk could write. */
  writable: boolean
}): AcceptState {
  if (!input.editing) return { enabled: false, why: 'Open Edit to accept.' }
  if (!input.proposal) return { enabled: false, why: 'There is no proposal to accept yet.' }
  if (input.running) {
    return { enabled: false, why: 'The session is still running. Stop it or wait for it to end.' }
  }
  if (input.disposition === 'accepted') {
    return { enabled: false, why: 'This proposal is already in the draft.' }
  }
  if (input.disposition === 'rejected') return { enabled: false, why: 'This proposal was rejected.' }
  if (input.saving) return { enabled: false, why: 'This draft is being saved.' }
  if (!input.writable) {
    return { enabled: false, why: 'The proposal is not JSON data, so there is nothing to write.' }
  }
  return { enabled: true, why: '' }
}

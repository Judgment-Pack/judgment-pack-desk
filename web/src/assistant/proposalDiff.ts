/**
 * The proposal as a diff on the draft — **computed here, never quoted**.
 *
 * ADR-0001: "the proposal is the only sink. The desk renders the diff and
 * applies an accepted proposal through the span-preserving writer." What a
 * model says it changed is a sentence about its own work; what this desk shows
 * is the comparison it made itself, member by member, between the bytes in the
 * editor and the document that arrived. Nothing here reads a summary, a
 * changelog, or any member of the proposal other than the document itself.
 *
 * **Everything is canonicalized before it is compared.** `JSON.parse(
 * JSON.stringify(x))` first, on the ToolGate's own reasoning: a value with a
 * getter or a `toJSON` can answer one thing while the diff is looking and
 * another when the writer serializes it, so the diff would be about a document
 * nobody is accepting. What comes out has no getters, no `toJSON`, no
 * functions, no symbol keys and no prototype left, and it is the only thing
 * this module and `acceptProposal.ts` ever read.
 *
 * **Granularity is the pack's top-level members, and the elements of array
 * members.** Elements that carry an `id` are matched by it — so a reordered
 * `rules` array reports a move rather than four rewrites — and elements without
 * one are matched by position. An id that appears twice in one array names
 * nothing: it is not used for matching, and the elements carrying it are
 * reported as a removal and an addition rather than paired with each other.
 *
 * Pure functions, no React. `ProposalDiff.tsx` renders what they return and
 * `acceptProposal.ts` writes it.
 */
import { agreesWithParse, indexDocument } from '../packs/documentText'
import { pointer as pointerOf } from '../packs/pointers'

export type DiffStatus = 'added' | 'removed' | 'changed' | 'unchanged'

/** One member of the document, or one element of an array member. */
export interface DiffEntry {
  /** The RFC 6901 pointer this entry is about, in the desk's one address space. */
  pointer: string
  /** What to call it: the member's name, an element's id, or its position. */
  label: string
  status: DiffStatus
  /** The draft's value as JSON text, where the draft carries one. */
  before?: string
  /** The proposal's value as JSON text, where the proposal carries one. */
  after?: string
  /**
   * True for an element whose value is identical and whose position is not.
   *
   * It is reported rather than folded into `unchanged`, because accepting it
   * moves the element's bytes — and rule order is §7-significant, so a move is
   * an edit to what the pack decides.
   */
  moved?: boolean
  /** Element entries, for an array member compared element by element. */
  children?: DiffEntry[]
}

export interface ProposalDiff {
  /** What the proposal was compared with. */
  against: 'the draft' | 'nothing'
  /** Why there was nothing to compare with, where there was not. */
  reason?: string
  /** Why there is no diff at all: a proposal that is not JSON data. */
  problem?: string
  /** Top-level entries, in the proposal's own member order, removals last. */
  entries: DiffEntry[]
  counts: Record<DiffStatus, number>
}

export function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * One value as plain JSON data, or undefined where it is not JSON data at all.
 *
 * The canonicalization the whole module rests on. A cycle, a `BigInt` and a
 * `toJSON` that throws all end here as `undefined` — a proposal this desk will
 * not diff and will not write — rather than as an exception thrown at a render.
 */
export function plain(value: unknown): unknown {
  let text: string | undefined
  try {
    text = JSON.stringify(value)
  } catch {
    return undefined
  }
  if (text === undefined) return undefined
  return JSON.parse(text) as unknown
}

/**
 * Whether two canonical values are the same value, **member order included**.
 *
 * Two objects with the same members in a different order are the same JSON
 * value and are not the same document: accepting one over the other rewrites
 * bytes. So the comparison is over the serialization, which keeps the order.
 */
export function sameValue(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

function jsonText(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? 'null'
}

/** What the draft is, or why it cannot be compared with. */
export type DraftReading = { value: Record<string, unknown> } | { problem: string }

/**
 * Read the bytes in the editor as a document to compare against.
 *
 * **The desk's own reading has to be the reading `JSON.parse` has**, which is
 * the rule the form editor already holds: a duplicated member name is read one
 * way by the scanner, another by `JSON.parse`, and refused outright by the
 * runtime, so a diff computed through it would be about a document nobody has.
 * Where the two disagree there is nothing to compare with, and the proposal is
 * offered whole.
 */
export function readDraft(text: string | undefined): DraftReading {
  if (text === undefined || text.trim() === '') {
    return { problem: 'there is no draft on this page to compare with' }
  }
  const index = indexDocument(text)
  const disagreement = agreesWithParse(text, index)
  const first = disagreement[0]
  if (first !== undefined) {
    return {
      problem: `${first.reason}${first.pointer === '' ? '' : ` at ${first.pointer}`}`
    }
  }
  if (!isObject(index.value)) {
    return { problem: 'the bytes in the editor are JSON, but not an object' }
  }
  return { value: index.value }
}

/** How the elements of two arrays line up. */
export interface Matching {
  /** For each proposal index, the draft index it was matched to. */
  pairs: (number | undefined)[]
  /** Draft indices the proposal carries no counterpart for. */
  dropped: number[]
  /** True where an id decided any pairing. */
  byId: boolean
}

/** The `id` an element may be matched by, or undefined. */
function idOf(item: unknown): string | undefined {
  if (!isObject(item)) return undefined
  const id = item.id
  return typeof id === 'string' && id !== '' ? id : undefined
}

/**
 * The ids that name exactly one element, by id.
 *
 * An id carried by two elements of one array names neither of them: pairing on
 * it would report one rule as an edit of another, and which one is a coin
 * toss. Both are dropped from the table, so the elements carrying that id fall
 * through to the unkeyed rules below — which, for a keyed element, means it is
 * reported as its own removal and its own addition.
 */
function unique(list: readonly unknown[]): Map<string, number> {
  const found = new Map<string, number>()
  const collided = new Set<string>()
  for (let index = 0; index < list.length; index += 1) {
    const id = idOf(list[index])
    if (id === undefined) continue
    if (found.has(id)) {
      collided.add(id)
      continue
    }
    found.set(id, index)
  }
  for (const id of collided) found.delete(id)
  return found
}

/**
 * Line two arrays' elements up: by id where both sides carry one, by position
 * where neither does.
 *
 * **A keyed element is never paired positionally.** A proposal that adds a rule
 * with a new id would otherwise be reported as an edit of whichever rule
 * happened to sit at that index — one rule's description printed as another's
 * old value, which looks exactly like an answer.
 */
export function matchElements(before: readonly unknown[], after: readonly unknown[]): Matching {
  const beforeIds = unique(before)
  const afterIds = unique(after)
  const pairs: (number | undefined)[] = new Array<number | undefined>(after.length).fill(undefined)
  const taken = new Set<number>()
  let byId = false

  for (let to = 0; to < after.length; to += 1) {
    const id = idOf(after[to])
    if (id === undefined || afterIds.get(id) !== to) continue
    const from = beforeIds.get(id)
    if (from === undefined || taken.has(from)) continue
    pairs[to] = from
    taken.add(from)
    byId = true
  }

  // What is left, in order — and only where neither side is keyed.
  const spare: number[] = []
  for (let from = 0; from < before.length; from += 1) {
    if (taken.has(from) || idOf(before[from]) !== undefined) continue
    spare.push(from)
  }
  let next = 0
  for (let to = 0; to < after.length; to += 1) {
    if (pairs[to] !== undefined || idOf(after[to]) !== undefined) continue
    const from = spare[next]
    if (from === undefined) break
    next += 1
    pairs[to] = from
    taken.add(from)
  }

  const dropped: number[] = []
  for (let from = 0; from < before.length; from += 1) if (!taken.has(from)) dropped.push(from)
  return { pairs, dropped, byId }
}

function labelOf(item: unknown, index: number): string {
  return idOf(item) ?? `[${index}]`
}

/** One array member's elements, compared. */
function compareElements(at: string, before: readonly unknown[], after: readonly unknown[]): DiffEntry[] {
  const { pairs, dropped } = matchElements(before, after)
  const children: DiffEntry[] = []
  for (let to = 0; to < after.length; to += 1) {
    const pointer = `${at}/${to}`
    const from = pairs[to]
    if (from === undefined) {
      children.push({
        pointer,
        label: labelOf(after[to], to),
        status: 'added',
        after: jsonText(after[to])
      })
      continue
    }
    const same = sameValue(before[from], after[to])
    children.push({
      pointer,
      label: labelOf(after[to], to),
      status: same ? 'unchanged' : 'changed',
      ...(same && from !== to ? { moved: true } : {}),
      before: jsonText(before[from]),
      after: jsonText(after[to])
    })
  }
  for (const from of dropped) {
    children.push({
      pointer: `${at}/${from}`,
      label: labelOf(before[from], from),
      status: 'removed',
      before: jsonText(before[from])
    })
  }
  return children
}

function counted(entries: readonly DiffEntry[]): Record<DiffStatus, number> {
  const counts: Record<DiffStatus, number> = { added: 0, removed: 0, changed: 0, unchanged: 0 }
  for (const entry of entries) counts[entry.status] += 1
  return counts
}

/**
 * The proposal, as a diff against the bytes in the editor.
 *
 * Where there is no draft to compare with — no bytes, bytes that are not JSON,
 * bytes two readers disagree about, or bytes that are JSON and not an object —
 * the whole proposal is reported as added and the reason travels with it, so
 * the pane can say why rather than showing a comparison it did not make.
 */
export function diffProposal(draftText: string | undefined, document: unknown): ProposalDiff {
  const proposed = plain(document)
  if (proposed === undefined) {
    return {
      against: 'nothing',
      problem: 'the proposal is not JSON data, so there is nothing to compare or to write',
      entries: [],
      counts: counted([])
    }
  }
  const draft = readDraft(draftText)
  if ('problem' in draft) {
    const entries = isObject(proposed)
      ? Object.keys(proposed).map((name) => ({
          pointer: pointerOf([name]),
          label: name,
          status: 'added' as const,
          after: jsonText(proposed[name])
        }))
      : [
          {
            pointer: '',
            label: 'the whole document',
            status: 'added' as const,
            after: jsonText(proposed)
          }
        ]
    return { against: 'nothing', reason: draft.problem, entries, counts: counted(entries) }
  }

  // A proposal that is not an object is one thing to accept or reject, whole.
  if (!isObject(proposed)) {
    const entry: DiffEntry = {
      pointer: '',
      label: 'the whole document',
      status: sameValue(draft.value, proposed) ? 'unchanged' : 'changed',
      before: jsonText(draft.value),
      after: jsonText(proposed)
    }
    return { against: 'the draft', entries: [entry], counts: counted([entry]) }
  }

  const entries: DiffEntry[] = []
  for (const name of Object.keys(proposed)) {
    const pointer = pointerOf([name])
    const after = proposed[name]
    // `hasOwn`, so a member the proposal writes as `null` is a member it
    // carries — and a member neither side carries is not invented here.
    if (!Object.hasOwn(draft.value, name)) {
      entries.push({ pointer, label: name, status: 'added', after: jsonText(after) })
      continue
    }
    const before = draft.value[name]
    if (sameValue(before, after)) {
      entries.push({
        pointer,
        label: name,
        status: 'unchanged',
        before: jsonText(before),
        after: jsonText(after)
      })
      continue
    }
    entries.push({
      pointer,
      label: name,
      status: 'changed',
      before: jsonText(before),
      after: jsonText(after),
      ...(Array.isArray(before) && Array.isArray(after)
        ? { children: compareElements(pointer, before, after) }
        : {})
    })
  }
  for (const name of Object.keys(draft.value)) {
    if (Object.hasOwn(proposed, name)) continue
    entries.push({
      pointer: pointerOf([name]),
      label: name,
      status: 'removed',
      before: jsonText(draft.value[name])
    })
  }
  return { against: 'the draft', entries, counts: counted(entries) }
}

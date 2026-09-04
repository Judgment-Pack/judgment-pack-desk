/**
 * What a block needs to know to be a form, and nothing else.
 *
 * A context beside `SelectionContext` rather than a prop threaded through
 * twelve components: a block that is not editing reads the default and renders
 * exactly what it rendered before, so the reading view is not rewritten to
 * carry an editor it does not use.
 *
 * **Every write goes through one function.** `write` takes an edit expressed
 * as a splice over the buffer — `writes.ts`' functions are exactly that shape
 * — so no block builds a document, serializes one, or knows what a save is.
 * The pointer a field is addressed by is the pointer the splice uses, which is
 * the pointer a diagnostic anchors on: one address space, phase 1's rule, now
 * with a writer on it.
 */
import { createContext, useContext } from 'react'
import type { AnchoredDiagnostic } from '../checks'
import type { Buffered } from './writes'
import { parsePointer, pointer as pointerOf } from '../pointers'

export interface DeclaredIds {
  /** `outcomes[].id`, for the Select a rule's outcome is chosen from. */
  outcomes: string[]
  /** `evidenceRequirements[].id`. */
  evidence: string[]
  /** `sources[].id`. */
  sources: string[]
  /** `rules[].id`, for an exception's `targetRule`. */
  rules: string[]
  /**
   * The fact paths this project's packs already consult, as `list_packs`
   * reports them. A suggestion list and never a closed one.
   */
  factPaths: string[]
}

export interface EditingSession {
  /** True while the document is being edited as a form. */
  editing: boolean
  /** The bytes and this desk's reading of them. */
  buffer: Buffered
  /**
   * Apply one splice to the buffer.
   *
   * `coalesceKey` is the field being typed into: consecutive writes with the
   * same key are one undo entry, so typing a sentence is one action.
   */
  write: (edit: (current: Buffered) => Buffered, options?: { coalesceKey?: string }) => void
  /** The diagnostics anchored exactly on one pointer, for the field to print. */
  diagnosticsAt: (pointer: string) => AnchoredDiagnostic[]
  /** The ids the document itself declares, for the Selects over them. */
  ids: DeclaredIds
  /**
   * Text an author has typed into an operand that is not JSON yet, by pointer.
   *
   * It lives here rather than in the field because the two ways out of a form
   * — switching to the JSON view, and saving — both leave the field behind. A
   * draft held in `useState` was silently gone on the first and silently
   * absent from the second, so the count in the toolbar and the text itself
   * both survive the mode toggle. Nothing here is written to the buffer and
   * nothing here is refused: it is the author's own bytes, waiting to parse.
   */
  pending: ReadonlyMap<string, PendingText>
  /** Hold what was typed at one pointer, or let it go once it is written. */
  hold: (pointer: string, draft: PendingText | null) => void
}

/** One unwritten operand: what was typed, and the bytes it started from. */
export interface PendingText {
  text: string
  /**
   * The member's own bytes when the draft began.
   *
   * A draft is only about the bytes it started from: undo, the JSON view and a
   * kind change all move them underneath it, and a draft that outlived that
   * would be a stale word over a member it is no longer about.
   */
  from: string
  /**
   * The bytes of the card this member sits in — the rule or the exception.
   *
   * **A pointer is a position, not an identity.** Moving rule 1 above rule 0
   * leaves `/rules/0/when/value` naming a different rule's operand, and where
   * the two operands read the same the byte comparison above sees nothing move:
   * the draft stayed, and finishing it wrote the *other* rule. The owner's own
   * bytes change when a move exchanges them, which is what says the member this
   * draft is about has gone somewhere else.
   */
  owner: string
}

/**
 * The bytes of the card one pointer sits inside — `/rules/N`, `/exceptions/N` —
 * or the empty string where it sits in neither.
 */
export function ownerOf(
  buffer: { text: string; index: { spans: ReadonlyMap<string, { valueStart: number; valueEnd: number }> } },
  pointer: string
): string {
  const parts = parsePointer(pointer)
  if (parts === undefined || parts.length < 2) return ''
  if (parts[0] !== 'rules' && parts[0] !== 'exceptions') return ''
  const span = buffer.index.spans.get(pointerOf([parts[0]!, parts[1]!]))
  return span === undefined ? '' : buffer.text.slice(span.valueStart, span.valueEnd)
}

const NOT_EDITING: EditingSession = {
  editing: false,
  buffer: { text: '', index: { spans: new Map(), duplicates: [] } },
  write: () => {},
  diagnosticsAt: () => [],
  ids: { outcomes: [], evidence: [], sources: [], rules: [], factPaths: [] },
  pending: new Map(),
  hold: () => {}
}

export const EditingContext = createContext<EditingSession>(NOT_EDITING)

export function useEditing(): EditingSession {
  return useContext(EditingContext)
}

/**
 * The ids one document declares, read off the document being rendered.
 *
 * **Every member is read as whatever it is.** This runs over the buffer, and
 * the buffer is a document somebody is in the middle of writing: `rules` can be
 * an object, `outcomes` a number, `sources` a string, for exactly as long as it
 * takes to finish a paste. `?? []` catches only null and undefined, so an
 * object here threw out of a memo the route computes in every mode and took the
 * whole page down — with the unsaved buffer, which is the one thing on the page
 * that exists nowhere else. Reading defensively is what `draftRequirements`,
 * `childCount` and `listAt` already do; this was the one reader that did not.
 */
export function declaredIds(
  document: {
    outcomes?: unknown
    evidenceRequirements?: unknown
    sources?: unknown
    rules?: unknown
  },
  factPaths: readonly string[] = []
): DeclaredIds {
  const ids = (entries: unknown) =>
    (Array.isArray(entries) ? (entries as { id?: unknown }[]) : [])
      .map((entry) => (typeof entry === 'object' && entry !== null ? entry.id : undefined))
      .filter((id): id is string => typeof id === 'string' && id !== '')
  return {
    outcomes: ids(document.outcomes),
    evidence: ids(document.evidenceRequirements),
    sources: ids(document.sources),
    rules: ids(document.rules),
    factPaths: [...factPaths]
  }
}

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
}

const NOT_EDITING: EditingSession = {
  editing: false,
  buffer: { text: '', index: { spans: new Map(), duplicates: [] } },
  write: () => {},
  diagnosticsAt: () => [],
  ids: { outcomes: [], evidence: [], sources: [], rules: [], factPaths: [] }
}

export const EditingContext = createContext<EditingSession>(NOT_EDITING)

export function useEditing(): EditingSession {
  return useContext(EditingContext)
}

/** The ids one document declares, read off the document being rendered. */
export function declaredIds(
  document: {
    outcomes?: { id?: string }[]
    evidenceRequirements?: { id?: string }[]
    sources?: { id?: string }[]
    rules?: { id?: string }[]
  },
  factPaths: readonly string[] = []
): DeclaredIds {
  const ids = (entries: { id?: string }[] | undefined) =>
    (entries ?? [])
      .map((entry) => entry?.id)
      .filter((id): id is string => typeof id === 'string' && id !== '')
  return {
    outcomes: ids(document.outcomes),
    evidence: ids(document.evidenceRequirements),
    sources: ids(document.sources),
    rules: ids(document.rules),
    factPaths: [...factPaths]
  }
}

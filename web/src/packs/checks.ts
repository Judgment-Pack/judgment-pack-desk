/**
 * Reading a `validate` answer, without saying anything it did not.
 *
 * Four pure functions, no React and no colour. The desk never issues a verdict
 * of its own here: it quotes the runtime's `status`, names the layer rows the
 * payload actually lists, counts the diagnostics, and puts each one on a block
 * the reader can see. Everything below is written against the runtime's own
 * ladder (`internal/validation/validator.go:158-289`) and against the five
 * payloads the PR body pastes in.
 *
 * **The rule that governs all of it: a layer the payload does not list is one
 * that did not run.** The ladder short-circuits — a carrier failure returns
 * `[carrier failed]` alone, a structural failure appends `structural failed`
 * and returns before semantic — so the rows are the whole of what may be said
 * about which layers ran, and nothing may be inferred from `status` alone.
 */
import type { Diagnostic, ValidationReport } from '../mcp/types'
import { parentPointers } from './pointers'

/** The ladder, in the order the runtime runs it. */
const LADDER = ['carrier', 'structural', 'semantic'] as const

/**
 * The runtime's own limit on how many diagnostics one answer carries
 * (`internal/validation/validator.go:21`, `MaxDiagnostics = 100`). Named here
 * only so the sentence about truncation can say a number the runtime chose.
 */
export const RUNTIME_DIAGNOSTIC_LIMIT = 100

/** What the check strip says, and what the Checks tab repeats. */
export interface LayerSentence {
  /** The runtime's own `status` word, verbatim, or undefined where it sent none. */
  status?: string
  /** The whole sentence, ready to print. */
  text: string
}

/**
 * Describe which layers ran, from the payload's own rows.
 *
 * Three shapes have to come out right and each has a test:
 *
 * - `[carrier passed, structural failed]` — every row is printed with the
 *   status the payload gave it, the report's own status leads the sentence,
 *   and the semantic layer is named as not having run.
 * - `status: "unsupported"` with `[carrier passed]` and a `capability`-layer
 *   diagnostic at `/specVersion` (`validator.go:203-210`) — a diagnostic whose
 *   layer appears in **no** layer row. The sentence quotes the status and does
 *   not invent a capability row.
 * - `status: "unsupported"` with all three layers `passed`
 *   (`validator.go:268-285`) — an unsupported *required extension*. All three
 *   ran, and saying otherwise would describe the wrong refusal.
 */
export function layersReached(report: ValidationReport | undefined): LayerSentence {
  if (report === undefined) return { text: 'Not checked.' }
  const rows = report.layers ?? []
  const count = report.diagnostics?.length ?? 0
  const diagnostics = `${count} diagnostic${count === 1 ? '' : 's'}`
  const named = rows
    .map((row) => row.name)
    .filter((name): name is string => typeof name === 'string')

  // **One shape, whatever happened.** There were two, and the failed-layer one
  // dropped both the report's own status and every row that was not the
  // failure: a payload saying `invalid` with `carrier passed, structural
  // failed` printed `structural — 1 diagnostic`, which names neither the
  // verdict the runtime reached nor the layer that did run. Every supplied row
  // is spelled with the status the payload gave it, in the payload's order,
  // and the status is quoted rather than translated — `valid`, `unsupported`,
  // or a word a later runtime uses that this desk has never seen.
  const status = report.status ?? 'no status'
  const spelled = rows
    .map((row) => `${row.name ?? 'an unnamed layer'} ${row.status ?? 'with no status'}`)
    .join(', ')
  const missed = LADDER.filter((layer) => !named.includes(layer))
  const tail =
    missed.length === 0
      ? ''
      : ` The ${listOf(missed)} layer${missed.length === 1 ? '' : 's'} did not run.`
  if (rows.length === 0) {
    return {
      status: report.status,
      text: `${status} — ${diagnostics}. This answer lists no layer.`
    }
  }
  return { status: report.status, text: `${status} — ${spelled}, ${diagnostics}.${tail}` }
}

function listOf(words: readonly string[]): string {
  if (words.length === 0) return ''
  if (words.length === 1) return words[0]!
  return `${words.slice(0, -1).join(', ')} and ${words[words.length - 1]}`
}

/** One diagnostic, and the block it is printed on. */
export interface AnchoredDiagnostic {
  diagnostic: Diagnostic
  /** The rendered block this is printed under; `''` is the document strip. */
  anchor: string
  /**
   * The diagnostic's own `instancePath`, printed beside the block whenever the
   * two differ — so `/rules/0/when` on a `when` that is not rendered still
   * names the member it is about.
   */
  named: string
  /** True where the anchor is not the diagnostic's own pointer. */
  approximate: boolean
}

/**
 * Put each diagnostic on a block that exists.
 *
 * Exact `instancePath` match first; otherwise the nearest **rendered**
 * ancestor, with the diagnostic's own pointer printed verbatim beside it;
 * otherwise the document strip.
 *
 * The ancestor walk is what makes a *missing* member reportable. The runtime
 * reports one at the pointer **including the absent name**
 * (`validator.go:319`, `append(location, missing)` through `carrier.Pointer`),
 * so `/rules/0/when` on a rule with no `when` anchors on `/rules/0`'s card and
 * prints `/rules/0/when`. The probe in the PR body shows exactly that payload.
 *
 * An empty `instancePath` — the root type failure at `validator.go:188` — goes
 * to the strip, which is the block whose pointer is the empty string.
 */
export function anchor(
  report: ValidationReport | undefined,
  rendered: ReadonlySet<string>
): AnchoredDiagnostic[] {
  if (report === undefined) return []
  return (report.diagnostics ?? []).map((diagnostic) => {
    const named = diagnostic.instancePath ?? ''
    if (named !== '' && rendered.has(named)) {
      return { diagnostic, anchor: named, named, approximate: false }
    }
    for (const ancestor of parentPointers(named)) {
      if (rendered.has(ancestor)) {
        return { diagnostic, anchor: ancestor, named, approximate: true }
      }
    }
    return { diagnostic, anchor: '', named, approximate: named !== '' }
  })
}

/**
 * Whether a check describes bytes other than the ones on screen.
 *
 * A diagnostic computed against different bytes is **never re-anchored**:
 * deleting `rules[0]` moves every `/rules/N` pointer, so an anchor computed
 * from the old text would print a real diagnostic on the wrong rule. The check
 * is marked stale and its anchors are dropped wholesale — the diagnostics are
 * still shown, and shown as being about bytes that have moved.
 */
export function isStale(checkedBytes: string | undefined, currentBytes: string | undefined): boolean {
  if (checkedBytes === undefined || currentBytes === undefined) return false
  return checkedBytes !== currentBytes
}

/**
 * What the tab may say about a member no diagnostic named.
 *
 * With a truncated list, "no other diagnostic names this member" is a claim
 * about diagnostics the runtime did not send. The list was cut, and the tab
 * says that instead.
 */
export function truncationNote(report: ValidationReport | undefined): string | undefined {
  if (report?.diagnosticsTruncated !== true) return undefined
  return `The runtime stopped at ${RUNTIME_DIAGNOSTIC_LIMIT} diagnostics, so this list is not all of them.`
}

/**
 * The diagnostics anchored at or under one pointer, for the Checks tab.
 *
 * **Under, and not only at.** A rule card is the block a reader selects; the
 * runtime names `/rules/0/when/value`. Filtering on equality alone showed that
 * reader "No other diagnostic names this member." over a rule the runtime had
 * just refused — a clean bill this desk has no business issuing, and the one
 * thing the Checks tab exists to never say. Each row still prints the
 * diagnostic's own `instancePath` at its foot, so a diagnostic about a
 * descendant is addressed to the descendant and not to the member selected.
 *
 * The descendant test is `anchor + "/"` and not a bare string prefix, because
 * `/rules/10` begins with `/rules/1` and is a different rule. The empty pointer
 * is the document itself, and every diagnostic is under the document.
 */
export function diagnosticsFor(
  anchored: readonly AnchoredDiagnostic[],
  pointer: string
): AnchoredDiagnostic[] {
  return anchored.filter(
    (entry) => entry.anchor === pointer || entry.anchor.startsWith(`${pointer}/`)
  )
}

/**
 * What the strip says about a report the editor has typed past.
 *
 * Distinct from the other stale sentence, because the two are different
 * events: a report about a revision the page never showed is a disagreement
 * between two sources, and this one is a check that is simply behind somebody's
 * hands. Both drop every anchor, for the one reason — a pointer computed
 * against other bytes names a member that has moved.
 */
export const CHECK_BEHIND_BUFFER =
  'This check ran over bytes the editor has moved past, so nothing it found is placed on this document.'

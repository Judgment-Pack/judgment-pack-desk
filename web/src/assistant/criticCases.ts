import { readCriticCases, type CriticCase } from '@judgment-pack/harness/refutation'
import { executable, matrix, type TestSuite } from '../packs/test-workspace/model'
import type { AuthoringCase } from '../research/checkCandidate'

/**
 * The cases whose outcomes the Desk declares to the critic
 * (`@judgment-pack/harness` v0.2.0, `criticCases`): the pack's saved tests and
 * the cases the request declares, each a row of the Runtime's matrix and its
 * `source`. The critic compares the proposal's outcome for each with the
 * expected one, and marks its critique `incomplete` while a declared case is
 * uncompared, so a wrong outcome in a declared case is refuted here and not
 * left to a reader.
 *
 * **A case the harness would refuse is left out, never sent.** The harness
 * reads the list closed and ends the run before any request when one case is
 * refused, so a saved test with facts that are not an object, or an
 * expectation that states no kind, would stop every critic pass on the pack.
 * Each case is read alone by the harness's own reader and the ones it accepts
 * are declared: a test that cannot be compared is a test the saved-tests view
 * already shows the person. Only cases that state an expectation are sent,
 * as the matrix sends them.
 */
const MEMBERS = ['id', 'facts', 'evidenceAvailability', 'supportedExtensions', 'expectedDisposition', 'expectedErrorClass', 'expectedErrorPhase', 'expectedHandoffTarget', 'origin', 'focus', 'specSection', 'cites'] as const

/** The harness's own limit on the declared list. */
export const MAX_CRITIC_CASES = 200

function declare(row: Record<string, unknown>, source: CriticCase['source']): CriticCase | undefined {
  const held: Record<string, unknown> = { source }
  for (const member of MEMBERS) if (row[member] !== undefined) held[member] = row[member]
  try {
    return readCriticCases([held])[0]
  } catch {
    return undefined
  }
}

/** A pack's saved tests that state an expectation, as `source: 'test'`. */
export function savedTestCases(suite: TestSuite): CriticCase[] {
  const kept = suite.cases.filter((item) => !suite.deleted?.includes(item.id) && executable(item))
  return matrix(kept).cases.flatMap((row) => declare(row as Record<string, unknown>, 'test') ?? [])
}

/** The cases a conversation has established for its draft, as `source: 'declared'`. */
export function declaredCases(cases: readonly AuthoringCase[]): CriticCase[] {
  return cases.flatMap((item) =>
    declare({ id: item.id, facts: item.facts, evidenceAvailability: item.evidenceAvailability, expectedDisposition: item.expectedDisposition, expectedHandoffTarget: item.expectedHandoffTarget, focus: item.rationale }, 'declared') ?? []
  )
}

/**
 * One list for a run: the saved tests first, then the declared cases, an id
 * once (a declared case that repeats a saved test's id is the saved test), and
 * at most the harness's limit.
 */
export function criticCases(...lists: readonly (readonly CriticCase[])[]): CriticCase[] {
  const seen = new Set<string>()
  const all: CriticCase[] = []
  for (const item of lists.flat()) {
    if (seen.has(item.id) || all.length >= MAX_CRITIC_CASES) continue
    seen.add(item.id)
    all.push(item)
  }
  return all
}

/**
 * The runtime's verdicts on a candidate: is it a pack, and what does it decide
 * for each test case. Both through the runtime's own tools, in rehearsal, and
 * nothing here is a verdict of the desk's — a case passes where the runtime's
 * disposition equals the expectation the reviewer established from the
 * sources, using the runtime’s canonical disposition (including reason and
 * trigger sets), with all other JSON arrays compared in order.
 *
 * Adapted from desk PR #73's proof (`docs/adr/0002-iterative-authoring.md`):
 * the same binding of a check to exact candidate bytes and the same rule that
 * a transport failure stops the run rather than becoming a policy disagreement.
 */
import type { CallTool, McpToolResult } from '../assistant/engine'
import { expectationSpec, validateExpectations } from './expectations'

export interface AuthoringCase {
  id: string
  facts: unknown
  evidenceAvailability?: unknown
  expectedDisposition: unknown
  expectedHandoffTarget?: unknown
  /** The excerpt id the expectation was established from. */
  expectationSource: string
  rationale: string
}

export interface CaseResult {
  id: string
  passed: boolean
  expected: unknown
  actual: unknown
  /** The runtime's own refusal, where the evaluation did not complete. */
  refused?: string
}

export interface CandidateCheck {
  documentDigest: string
  valid: boolean
  diagnostics: unknown[]
  cases: CaseResult[]
}

export async function digestOf(text: string): Promise<string> {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))
  return Array.from(new Uint8Array(bytes), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/** Key order is immaterial; array order and exact JSON values are preserved. */
export function jsonIdentity(value: unknown): string {
  const sort = (input: unknown): unknown => {
    if (Array.isArray(input)) return input.map(sort)
    if (input !== null && typeof input === 'object') {
      return Object.fromEntries(
        Object.entries(input as Record<string, unknown>)
          .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
          .map(([key, member]) => [key, sort(member)])
      )
    }
    return input
  }
  return JSON.stringify(sort(value))
}

function payload(result: McpToolResult, validation = false): Record<string, unknown> {
  const value =
    result.structuredContent ??
    JSON.parse(
      (result.content ?? [])
        .filter((item) => item.type === 'text')
        .map((item) => item.text ?? '')
        .join('\n')
    )
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('the runtime answered with something other than a report')
  if (result.isError && !(validation && (value as { status?: unknown }).status === 'invalid')) {
    const text = (result.content ?? []).map((item) => item.text ?? '').join(' ')
    throw new Error(`the runtime refused: ${text || 'no reason given'}`)
  }
  return value as Record<string, unknown>
}

/** Validate the candidate, then rehearse every case; a refused case is a failed one, named. */
export async function checkCandidate(
  document: string,
  cases: AuthoringCase[],
  callTool: CallTool,
  signal: AbortSignal
): Promise<CandidateCheck> {
  const expectations = await validateExpectations(expectationSpec(JSON.parse(document)), cases.map(row => row.expectedDisposition), callTool, signal)
  const invalid = expectations.findIndex(row => row.status === 'invalid')
  if (invalid !== -1) throw new Error(`Invalid expectation for ${cases[invalid]!.id}: ${(expectations[invalid] as { message: string }).message}`)
  const report: CandidateCheck = { documentDigest: await digestOf(document), valid: false, diagnostics: [], cases: [] }
  if (signal.aborted) throw new DOMException('stopped', 'AbortError')
  const validation = payload(await callTool('validate', { document }), true)
  if (validation.status !== 'valid' && validation.status !== 'invalid') throw new Error('the runtime did not return a validation result')
  report.valid = validation.status === 'valid'
  report.diagnostics = Array.isArray(validation.diagnostics) ? validation.diagnostics : []
  if (!report.valid) return report
  for (const [index, row] of cases.entries()) {
    if (signal.aborted) throw new DOMException('stopped', 'AbortError')
    const args: Record<string, unknown> = { pack: document, facts: JSON.stringify(row.facts), rehearsal: true }
    if (row.evidenceAvailability !== undefined) args.evidence = JSON.stringify(row.evidenceAvailability)
    const expected = {
      disposition: JSON.parse((expectations[index] as { canonical: string }).canonical),
      ...(row.expectedHandoffTarget !== undefined ? { handoffTarget: row.expectedHandoffTarget } : {})
    }
    let evaluated: Record<string, unknown>
    try {
      evaluated = payload(await callTool('experimental_evaluate', args))
    } catch (cause) {
      if ((cause as Error)?.name === 'AbortError') throw cause
      report.cases.push({ id: row.id, passed: false, expected, actual: null, refused: (cause as Error).message })
      continue
    }
    if (evaluated.status !== 'evaluated' || evaluated.rehearsal !== true || !evaluated.disposition) {
      report.cases.push({
        id: row.id,
        passed: false,
        expected,
        actual: evaluated.disposition ?? null,
        refused: `no completed rehearsal: status ${String(evaluated.status)}`
      })
      continue
    }
    const actual = {
      disposition: evaluated.disposition,
      ...(row.expectedHandoffTarget !== undefined ? { handoffTarget: evaluated.handoffTarget ?? null } : {})
    }
    report.cases.push({ id: row.id, expected, actual, passed: jsonIdentity(expected) === jsonIdentity(actual) })
  }
  return report
}

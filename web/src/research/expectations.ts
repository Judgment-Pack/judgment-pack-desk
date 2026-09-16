import type { CallTool } from '../assistant/engine'
import { withAbort } from '../assistant/engines/contract'

export const EXPECTATION_TOOL = 'experimental_validate_expectations'

/**
 * The JPS version an expectation is judged against.
 *
 * An expectation describes a disposition an evaluator produces, not a member of
 * the draft, so it is judged against the version the evaluator implements and
 * never against the version the draft happens to declare. A draft that declares
 * the wrong one is a draft defect: `validate` and `experimental_evaluate` say so,
 * and the repair loop fixes it, which is what a run did before this check existed.
 */
export const EVALUATOR_SPEC = '0.2.0-draft'

/** How many expectations one call carries; the tool is stateless, so a larger suite is chunked. */
const PER_CALL = 256

export type CheckedExpectation =
  | { status: 'valid'; canonical: string }
  | { status: 'invalid'; message: string; admitted: boolean }

/** Runtime-owned §8.3 checks; a missing or incomplete report is never acceptance. */
export async function validateExpectations(dispositions: readonly unknown[], callTool: CallTool, signal: AbortSignal): Promise<CheckedExpectation[]> {
  const checked: CheckedExpectation[] = []
  for (let start = 0; start < dispositions.length; start += PER_CALL) {
    checked.push(...await validateBatch(dispositions.slice(start, start + PER_CALL), callTool, signal))
  }
  return checked
}

async function validateBatch(dispositions: readonly unknown[], callTool: CallTool, signal: AbortSignal): Promise<CheckedExpectation[]> {
  if (dispositions.length === 0) return []
  const result = await withAbort(() => callTool(EXPECTATION_TOOL, {
    spec_version: EVALUATOR_SPEC, expectations: dispositions.map(value => JSON.stringify(value ?? null))
  }), signal)
  const text = (result.content ?? []).filter(item => item.type === 'text').map(item => item.text ?? '').join('\n')
  if (result.isError) throw new Error(`Expectations could not be validated: ${text || 'the runtime refused the request'}`)
  const report = result.structuredContent ?? JSON.parse(text)
  if (!record(report) || report.specVersion !== EVALUATOR_SPEC || !Array.isArray(report.results) || report.results.length !== dispositions.length) {
    throw new Error('The runtime returned an incomplete expectation validation report.')
  }
  const checked = report.results.map((row: unknown, index: number): CheckedExpectation => {
    if (!record(row) || row.index !== index) throw new Error('The runtime did not account for every expectation in order.')
    if (row.status === 'valid' && typeof row.canonical === 'string' && record(JSON.parse(row.canonical))) return { status: 'valid', canonical: row.canonical }
    // A limit finding says the runtime did not admit the input, not that Core
    // prohibits its meaning, so it is carried as what it is and never handed to
    // a reviewer as a §8.3 defect to correct.
    if (row.status === 'invalid' && typeof row.message === 'string' && row.message.trim()) {
      return { status: 'invalid', message: row.message, admitted: row.code !== 'JPS-EXPECTATION-LIMIT' }
    }
    throw new Error('The runtime returned an unreadable expectation finding.')
  })
  if (report.status !== (checked.some(row => row.status === 'invalid') ? 'invalid' : 'valid')) throw new Error('The runtime returned an inconsistent expectation validation report.')
  return checked
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

/**
 * What an invalid finding is: a §8.3 defect a reviewer can correct, or an input
 * the runtime would not admit at all. The wording of the second is the runtime's
 * own, so a person reads why it was refused rather than a claim Core makes.
 */
export function findingSummary(finding: { message: string; admitted: boolean }): string {
  return finding.admitted ? finding.message : `The runtime did not admit this expectation: ${finding.message}`
}

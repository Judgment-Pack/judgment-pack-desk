import type { CallTool, McpToolResult } from '../engine'
import { withAbort } from '../engines/contract'
import { documentDigest, jsonIdentity, type AuthoringCase, type CandidateCheck } from './run'

function payload(result: McpToolResult, validation = false): Record<string, unknown> {
  const value = result.structuredContent ?? JSON.parse((result.content ?? [])
    .filter(item => item.type === 'text').map(item => item.text ?? '').join('\n'))
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Malformed runtime report.')
  if (result.isError && !(validation && value.status === 'invalid')) throw new Error('The runtime refused the check request.')
  return value as Record<string, unknown>
}

/** Inline rehearsals, not a replacement for the runtime's full matrix report. */
export async function checkCandidate(document: string, cases: AuthoringCase[], callTool: CallTool,
  runtimeIdentity: string, signal: AbortSignal): Promise<CandidateCheck> {
  const report: CandidateCheck = {
    documentDigest: await documentDigest(document), runtimeIdentity, valid: false, diagnostics: [], cases: []
  }
  const validation = payload(await withAbort(() => callTool('validate', { document }), signal), true)
  if (validation.status !== 'valid' && validation.status !== 'invalid') throw new Error('The runtime did not return a validation result.')
  report.valid = validation.status === 'valid'
  report.diagnostics = Array.isArray(validation.diagnostics) ? validation.diagnostics : []
  if (!report.valid) return report
  for (const row of cases) {
    const args: Record<string, unknown> = { pack: document, facts: JSON.stringify(row.facts), rehearsal: true }
    if (row.evidenceAvailability !== undefined) args.evidenceAvailability = JSON.stringify(row.evidenceAvailability)
    const evaluated = payload(await withAbort(() => callTool('experimental_evaluate', args), signal))
    if (evaluated.status !== 'evaluated' || evaluated.rehearsal !== true || !evaluated.disposition) {
      throw new Error(`No completed rehearsal for test ${row.id}.`)
    }
    const expected = { disposition: row.expectedDisposition,
      ...(row.expectedHandoffTarget !== undefined ? { handoffTarget: row.expectedHandoffTarget } : {}) }
    const actual = { disposition: evaluated.disposition,
      ...(row.expectedHandoffTarget !== undefined ? { handoffTarget: evaluated.handoffTarget ?? null } : {}) }
    report.cases.push({ id: row.id, expected, actual, passed: jsonIdentity(expected) === jsonIdentity(actual) })
  }
  return report
}

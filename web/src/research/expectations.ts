import type { CallTool } from '../assistant/engine'
import { withAbort } from '../assistant/engines/contract'

export const EXPECTATION_TOOL = 'experimental_validate_expectations'
export type CheckedExpectation = { status: 'valid'; canonical: string } | { status: 'invalid'; message: string }

/** Runtime-owned §8.3 checks; a missing or incomplete report is never acceptance. */
export async function validateExpectations(specVersion: string, dispositions: readonly unknown[], callTool: CallTool, signal: AbortSignal): Promise<CheckedExpectation[]> {
  if (dispositions.length === 0) return []
  if (dispositions.length > 256) throw new Error('At most 256 expectations can be validated in one authoring run.')
  const result = await withAbort(() => callTool(EXPECTATION_TOOL, {
    spec_version: specVersion, expectations: dispositions.map(value => JSON.stringify(value ?? null))
  }), signal)
  const text = (result.content ?? []).filter(item => item.type === 'text').map(item => item.text ?? '').join('\n')
  if (result.isError) throw new Error(`Expectations could not be validated: ${text || 'the runtime refused the request'}`)
  const report = result.structuredContent ?? JSON.parse(text)
  if (!record(report) || report.specVersion !== specVersion || !Array.isArray(report.results) || report.results.length !== dispositions.length) {
    throw new Error('The runtime returned an incomplete expectation validation report.')
  }
  const checked = report.results.map((row: unknown, index: number): CheckedExpectation => {
    if (!record(row) || row.index !== index) throw new Error('The runtime did not account for every expectation in order.')
    if (row.status === 'valid' && typeof row.canonical === 'string' && record(JSON.parse(row.canonical))) return { status: 'valid', canonical: row.canonical }
    if (row.status === 'invalid' && typeof row.message === 'string' && row.message.trim()) return { status: 'invalid', message: row.message }
    throw new Error('The runtime returned an unreadable expectation finding.')
  })
  if (report.status !== (checked.some(row => row.status === 'invalid') ? 'invalid' : 'valid')) throw new Error('The runtime returned an inconsistent expectation validation report.')
  return checked
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function expectationSpec(document: unknown): string {
  const version = record(document) ? document.specVersion : undefined
  if (typeof version !== 'string') throw new Error('The draft must declare its JPS specVersion before expectations can be validated.')
  return version
}

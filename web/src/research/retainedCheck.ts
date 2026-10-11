/** Caller-owned rehearsal history. No saved UI flag grants finalization authority. */
import type { CallTool, McpToolResult } from '../assistant/engine'
import { digestOf, jsonIdentity, type AuthoringCase, type CandidateCheck } from './checkCandidate'

export interface RetainedCheck {
  version: 1
  at: string
  documentDigest: string
  inputs: AuthoringCase[]
  runtime: string
  context: string
  /** Only statements cited by these cases, not the conversation transcript. */
  statements: [string, string][]
  validation: McpToolResult
  matrix?: { text: string; response: McpToolResult }
  digest: string
}
const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const hash = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
export function checkPayload(reply: McpToolResult): Record<string, unknown> {
  const text = (reply.content ?? []).filter(c => c.type === 'text').map(c => c.text ?? '').join('\n')
  let value: unknown
  try { value = reply.structuredContent ?? JSON.parse(text) } catch { throw Error(text || 'The runtime did not return a check report.') }
  if (!object(value) || (reply.isError && value.status !== 'invalid')) throw Error(text || 'The runtime refused the check.')
  return value
}
export async function runtimeCheckIdentity(call: CallTool): Promise<string> {
  const descriptor = checkPayload(await call('describe_runtime', {}))
  if (!object(descriptor.tool) || typeof descriptor.tool.name !== 'string' || typeof descriptor.tool.version !== 'string' || !descriptor.tool.version)
    throw Error('The runtime did not identify its version.')
  return jsonIdentity(descriptor)
}
export function reusableRuntime(identity: string): boolean {
  try { const v = JSON.parse(identity).tool?.version; return typeof v === 'string' && !/dev|unknown|^0\.0\.0/i.test(v) } catch { return false }
}
export function readRetainedCheck(value: unknown): RetainedCheck | undefined {
  if (value === undefined) return undefined
  if (!object(value) || value.version !== 1) return undefined
  if (!hash(value.documentDigest) || !hash(value.context) || !hash(value.digest) || typeof value.runtime !== 'string' ||
      typeof value.at !== 'string' || !Number.isFinite(Date.parse(value.at)) || !Array.isArray(value.inputs) || value.inputs.length > 256 ||
      value.inputs.some(r => !object(r) || typeof r.id !== 'string' || !object(r.facts) || typeof r.expectationSource !== 'string' || typeof r.rationale !== 'string' || !object(r.expectedDisposition)) ||
      new Set(value.inputs.map(r => r.id)).size !== value.inputs.length || !Array.isArray(value.statements) || value.statements.length > 256 ||
      value.statements.some(r => !Array.isArray(r) || r.length !== 2 || r.some(x => typeof x !== 'string')) ||
      new Set(value.statements.map(r => r[0])).size !== value.statements.length || !object(value.validation) ||
      (value.matrix !== undefined && (!object(value.matrix) || typeof value.matrix.text !== 'string' || !object(value.matrix.response))))
    throw Error('Invalid saved draft check record.')
  return value as unknown as RetainedCheck
}
export function caseMatrix(inputs: AuthoringCase[]): string {
  return JSON.stringify({matrixVersion:'3', cases:inputs.map(({id,facts,evidenceAvailability,expectedDisposition,expectedHandoffTarget}) => ({
    id,facts,expectedDisposition,...(evidenceAvailability === undefined ? {} : {evidenceAvailability}),
    ...(expectedHandoffTarget === undefined ? {} : {expectedHandoffTarget})
  }))})
}
/** Display the runtime's comparison; do not compare dispositions again in Desk. */
export function checkFromRecord(record: RetainedCheck): CandidateCheck {
  const validation = checkPayload(record.validation)
  const identity = JSON.parse(record.runtime)
  if (jsonIdentity(validation.tool) !== jsonIdentity(identity.tool)) throw Error('The validation report names a different runtime.')
  if (!['valid','invalid'].includes(String(validation.status))) throw Error('The runtime did not return a validation result.')
  const result: CandidateCheck = {documentDigest:record.documentDigest, valid:validation.status === 'valid', diagnostics:Array.isArray(validation.diagnostics) ? validation.diagnostics : [], cases:[], record}
  if (!result.valid || record.inputs.length === 0) return result
  if (!record.matrix || record.matrix.text !== caseMatrix(record.inputs)) throw Error('The saved test inputs do not match the checked matrix.')
  const report = checkPayload(record.matrix.response)
  if (jsonIdentity(report.tool) !== jsonIdentity(identity.tool) || typeof report.evaluatorSpecVersion !== 'string') throw Error('The test report did not identify the checked runtime and evaluator.')
  const packs = report.packs
  if (!Array.isArray(packs) || packs.length !== 1 || !object(packs[0]) || !Array.isArray(packs[0].rows) || !object(report.summary) ||
      report.summary.total !== record.inputs.length || packs[0].rows.length !== record.inputs.length ||
      !['passed','mismatch'].includes(String(report.status))) throw Error('The runtime returned an incomplete test report.')
  result.cases = packs[0].rows.map((row: unknown, i: number) => {
    const input = record.inputs[i]!
    if (!object(row) || row.id !== input.id || !['passed','mismatch'].includes(String(row.status)) || typeof row.expected !== 'string' || typeof row.actual !== 'string')
      throw Error('The runtime returned an incomplete test row.')
    const value = (text: string) => { try { return JSON.parse(text) as unknown } catch { return text } }
    return {id:input.id,passed:row.status === 'passed',expected:{disposition:value(row.expected),...(input.expectedHandoffTarget === undefined ? {} : {handoffTarget:input.expectedHandoffTarget})},
      actual:{disposition:value(row.actual),...(input.expectedHandoffTarget === undefined ? {} : {handoffTarget:typeof row.actualHandoffTarget === 'string' ? value(row.actualHandoffTarget) : null})}}
  })
  if ((report.status === 'passed') !== result.cases.every(r => r.passed) || report.summary.passed !== result.cases.filter(r => r.passed).length)
    throw Error('The runtime returned an inconsistent test report.')
  return result
}
export async function verifyRetainedCheck(record: RetainedCheck): Promise<CandidateCheck> {
  const {digest,...held} = record
  if (await digestOf(jsonIdentity(held)) !== digest) throw Error('The saved check record failed integrity verification.')
  return checkFromRecord(record)
}
/** End local verification promptly even when a transport does not support cancellation. */
export async function whileChecking<T>(signal: AbortSignal, work: () => Promise<T>): Promise<T> {
  if (signal.aborted) throw new DOMException('stopped','AbortError')
  return new Promise<T>((resolve,reject) => {
    const stopped = () => reject(new DOMException('stopped','AbortError'))
    signal.addEventListener('abort',stopped,{once:true})
    Promise.resolve().then(() => { if (signal.aborted) throw new DOMException('stopped','AbortError'); return work() }).then(resolve,reject).finally(() => signal.removeEventListener('abort',stopped))
  })
}
export async function runSnapshotCheck(document: string, inputs: AuthoringCase[], call: CallTool, signal: AbortSignal,
  identity: () => Promise<string>, context: () => Promise<string>, statements: [string,string][], at: string): Promise<CandidateCheck> {
  const check = () => { if (signal.aborted) throw new DOMException('stopped','AbortError') }
  check()
  const runtime = await whileChecking(signal,identity), basis = await whileChecking(signal,context), frozen = structuredClone(inputs)
  check()
  const validation = await whileChecking(signal,()=>call('validate',{document}))
  check()
  const valid = checkPayload(validation).status
  if (valid !== 'valid' && valid !== 'invalid') throw Error('The runtime did not return a validation result.')
  let matrix: RetainedCheck['matrix']
  if (valid === 'valid' && frozen.length) {
    const text = caseMatrix(frozen)
    matrix = {text,response:await whileChecking(signal,()=>call('experimental_test_cases',{pack:document,matrix:text}))}
  }
  check()
  if (runtime !== await whileChecking(signal,identity) || basis !== await whileChecking(signal,context)) throw Error('The runtime or source context changed during the checks. Run checks again.')
  check()
  const held = {version:1 as const,at,documentDigest:await digestOf(document),inputs:frozen,runtime,context:basis,statements:structuredClone(statements),validation,...(matrix ? {matrix} : {})}
  return checkFromRecord({...held,digest:await digestOf(jsonIdentity(held))})
}

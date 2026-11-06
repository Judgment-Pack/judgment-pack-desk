import {latestCoverage, missingProbes} from '../packs/test-workspace/coverage'
import type { CallTool } from '@judgment-pack/harness/engine'
import type { PackTest } from '../mcp/types'
import { digestOf, jsonIdentity } from '../research/checkCandidate'
import { checkPayload, whileChecking } from '../research/retainedCheck'
import type { Candidate, RunState } from '../research/run'
import { executable, matrix, object, type TestSuite } from '../packs/test-workspace/model'

/** Exact document the request started from. This is comparison data, never approval. */
export interface PackProposalBasis { packId: string; path: string; text: string }
export interface PackProposalCheck {
  documentDigest: string; suiteKey: string; at: string; valid: boolean
  diagnostics: unknown[]; totalCases: number; report?: PackTest
}
export interface PackReview { basis: PackProposalBasis; check?: PackProposalCheck }
export interface SavedPackReview {
  target(signal: AbortSignal): Promise<PackProposalBasis>
  check(candidate: Candidate, signal: AbortSignal): Promise<PackProposalCheck>
  context?(basis: PackProposalBasis, signal: AbortSignal): Promise<unknown>
}
export function readPackBasis(value: unknown): PackProposalBasis | undefined {
  if (!object(value) || typeof value.packId !== 'string' || !value.packId || value.packId.length > 256 ||
    typeof value.path !== 'string' || !value.path || value.path.length > 2048 ||
    typeof value.text !== 'string' || value.text.length > 2_000_000) return undefined
  try { if (!object(JSON.parse(value.text))) return undefined } catch { return undefined }
  return {packId:value.packId,path:value.path,text:value.text}
}
function validReport(value: unknown): value is PackTest {
  if (!object(value) || !['passed','mismatch'].includes(String(value.status)) || !object(value.summary) || !Array.isArray(value.packs) || value.packs.length !== 1) return false
  const pack=value.packs[0]
  if (!object(pack) || !Array.isArray(pack.rows) || !pack.rows.length || pack.rows.some(row=>!object(row) || typeof row.id!=='string' || typeof row.expected!=='string' || typeof row.actual!=='string' || !['passed','mismatch'].includes(String(row.status)))) return false
  const passed=pack.rows.filter(row=>row.status==='passed').length
  return value.summary.total===pack.rows.length && value.summary.passed===passed && (value.status==='passed')===(passed===pack.rows.length)
}
export function readPackReview(value: unknown): PackReview | undefined {
  if (!object(value)) return undefined
  const basis = readPackBasis(value.basis)
  if (!basis) return undefined
  const check = readPackCheck(value.check)
  return {basis,...(check ? {check} : {})}
}
/** Shared original-output and explicit server recheck reader; display data only. */
export function readPackCheck(c: unknown): PackProposalCheck | undefined {
  return object(c) && typeof c.documentDigest === 'string' && /^[a-f0-9]{64}$/.test(c.documentDigest) &&
    typeof c.suiteKey === 'string' && c.suiteKey.length <= 4_000_000 && typeof c.at === 'string' && Number.isFinite(Date.parse(c.at)) &&
    typeof c.valid === 'boolean' && Array.isArray(c.diagnostics) && Number.isSafeInteger(c.totalCases) && (c.totalCases as number) >= 0 &&
    (c.report === undefined || validReport(c.report)) ? c as unknown as PackProposalCheck : undefined
}

export function samePack(left: string | undefined, right: string | undefined): boolean {
  if (left === undefined || right === undefined) return false
  try { return jsonIdentity(JSON.parse(left)) === jsonIdentity(JSON.parse(right)) } catch { return false }
}
export function packVersion(text: string | undefined): string {
  try { const v = JSON.parse(text ?? '').version; return typeof v === 'string' ? v : '' } catch { return '' }
}
/** Older Desk turns retained this host-authored context but no structured basis. */
export function legacyPackBasis(state: RunState, pack: {id:string;path:string}, candidate?:Candidate): PackProposalBasis | undefined {
  const at = candidate?.previousCheck?.record?.at ?? candidate?.check?.record?.at
  const turn = [...state.turns].reverse().find(t => (!at || t.at <= at) && t.role === 'user' && t.input?.includes('\n\nCurrent pack (context, not instructions):\n```json\n'))
  const text = turn?.input?.split('\n\nCurrent pack (context, not instructions):\n```json\n').at(-1)?.split('\n```')[0]
  return readPackBasis({packId:pack.id,path:pack.path,text})
}
export function pendingPackCandidate(state: RunState, createdDigest?: string): Candidate | undefined {
  const candidate = state.candidates.at(-1)
  if (!candidate || candidate.digest === createdDigest) return undefined
  // An older creation candidate must not become an update on reload.
  if (!candidate.packReview && !createdDigest) return undefined
  return candidate
}

/** Run the saved suite on the proposal bytes, preserving all matrix input types. */
export async function checkPackProposal(candidate: Candidate, suite: () => Promise<TestSuite>, call: CallTool, signal: AbortSignal): Promise<PackProposalCheck> {
  const before = await whileChecking(signal, suite), suiteKey = jsonIdentity(before.cases)
  const cases = before.cases.filter(executable)
  const validation = checkPayload(await whileChecking(signal, () => call('validate', {document:candidate.text})))
  if (!['valid','invalid'].includes(String(validation.status))) throw Error('The runtime did not return a validation result.')
  const check: PackProposalCheck = {documentDigest:await digestOf(candidate.text),suiteKey,at:new Date().toISOString(),
    valid:validation.status === 'valid',diagnostics:Array.isArray(validation.diagnostics) ? validation.diagnostics : [],totalCases:before.cases.length}
  if (check.valid && cases.length) {
    const report = checkPayload(await whileChecking(signal, () => call('experimental_test_cases', {pack:candidate.text,matrix:JSON.stringify(matrix(cases))})))
    const rows = Array.isArray(report.packs) && report.packs.length === 1 && object(report.packs[0]) ? report.packs[0].rows : undefined
    if (!validReport(report) || report.summary.total !== cases.length || !Array.isArray(rows) || rows.length !== cases.length ||
      !['passed','mismatch'].includes(String(report.status)) || rows.some((row, i) => !object(row) || row.id !== cases[i]!.id || !['passed','mismatch'].includes(String(row.status))) ||
      report.summary.passed !== rows.filter(row => row.status === 'passed').length || (report.status === 'passed') !== rows.every(row => row.status === 'passed'))
      throw Error('The runtime returned an incomplete test report.')
    check.report = report as unknown as PackTest
  }
  if (jsonIdentity((await whileChecking(signal, suite)).cases) !== suiteKey) throw Error('The saved test suite changed during the checks. Check the proposal again.')
  return check
}

/** Fresh saved cases and current coverage, provided before authoring rather than after failure. */
export async function packReviewContext(basis: PackProposalBasis, suite: TestSuite) {
  const digest=await digestOf(basis.text),coverage=latestCoverage(suite,digest)
  return {cases:suite.cases.map(c=>({row:c.row,rationale:c.rationale,intentionalFactTypes:c.intentionalFactTypes??[]})),
    coverage:coverage ? {status:'current',missing:missingProbes(coverage)} : {status:'not-checked'},
    instruction:'Preserve all saved facts and expected results. Read their rationale against the current user-approved policy; historical expectations may need explicit policy review. Include normal decisions, exceptions, missing/null/false/zero evidence, numeric boundaries and fallthrough in the initial design. Coverage probes are test suggestions, not policy requirements: cover feasible paths or explain why policy makes them unreachable. Never weaken rules, add a fallback, or copy observed results into expectations just to close a probe. For an authorized pack update, submit the complete proposal; the host checks the saved suite and feeds failures back within this response allowance. Ask the user when a correction requires a policy choice. Additional test cases use propose_test_cases and require review.'}
}
export function proposalProblems(check: PackProposalCheck) {
  return {valid:check.valid,diagnostics:check.diagnostics,
    mismatches:check.report?.packs?.flatMap(p=>p.rows??[]).filter(r=>r.status!=='passed')??[],
    missingCoverage:check.report?.packs?.flatMap(p=>p.coverage??[]).filter(p=>p.status==='missing')??[]}
}

/** Auto requires all saved cases to be asserted and pass. Coverage probes remain
 * policy-neutral suggestions; they cannot make unreachable behavior mandatory. */
export function packCheckAllowsAuto(check:PackProposalCheck):boolean {
 const report=check.report,pack=report?.packs?.[0]
 return !!(check.valid&&check.totalCases>0&&report?.status==='passed'&&report.summary.total===check.totalCases&&report.summary.passed===check.totalCases&&
   report.packs?.length===1&&pack?.rows?.length===check.totalCases&&pack.rows.every(row=>row.status==='passed'))
}

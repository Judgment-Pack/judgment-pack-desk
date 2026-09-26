import { additionFindings } from './coverage'
import type { CallTool } from '../../assistant/engine'
import type { ProposalFinding, ProposalProgress } from '../../assistant/proposalWorkflow'
import { withAbort } from '../../assistant/engines/contract'
import type { ChatAttachment } from '../../chat/store'
import { jsonIdentity } from '../../research/checkCandidate'
import { importMatrix, object, type TestCase, type TestSuite } from './model'

export const MATRIX_CONTRACT_TOOL = 'experimental_get_test_matrix_contract'
export const MATRIX_VALIDATION_TOOL = 'experimental_validate_test_matrix'
export interface TestProposal extends ProposalProgress {
  id: string
  at: string
  request: string
  packDigest: string
  model: string
  contractVersion: string
  sources: ChatAttachment[]
  caseSnapshots: Record<string, string>
  savedCases?: Record<string, string>
  additionsOnly?: boolean
  coverageRunId?: string
}
async function payload(name: string, args: Record<string, unknown>, call: CallTool, signal: AbortSignal) {
  const result = await withAbort(() => call(name, args), signal)
  const text = (result.content ?? [])
    .filter((c) => c.type === 'text')
    .map((c) => c.text ?? '')
    .join('\n')
  if (result.isError)
    throw Error(
      text || 'Test proposal validation is unavailable. Update the connected runtime and try again.',
    )
  const value = result.structuredContent ?? JSON.parse(text)
  if (!object(value) || value.contractVersion !== '1' || value.specVersion !== '0.2.0-draft')
    throw Error('The runtime returned an unsupported test-matrix contract.')
  return value
}
export async function testMatrixContract(call: CallTool, signal: AbortSignal) {
  const report = await payload(MATRIX_CONTRACT_TOOL, {}, call, signal)
  if (
    !object(report.contract) ||
    report.contract.matrixVersion !== '3' ||
    !Array.isArray(report.contract.rowMembers)
  )
    throw Error('The runtime returned an incomplete test-matrix contract.')
  return report.contract
}
function findings(value: unknown): ProposalFinding[] {
  if (
    !Array.isArray(value) ||
    value.some(
      (f) =>
        !object(f) ||
        typeof f.code !== 'string' ||
        typeof f.path !== 'string' ||
        typeof f.message !== 'string',
    )
  )
    throw Error('The runtime returned incomplete matrix findings.')
  return value as ProposalFinding[]
}
export async function validateTestProposal(
  document: unknown,
  sources: ChatAttachment[],
  call: CallTool,
  signal: AbortSignal,
): Promise<ProposalFinding[]> {
  // sourceMappings is explicitly Desk-owned metadata; no other root field is erased.
  const matrix = object(document)
    ? Object.fromEntries(Object.entries(document).filter(([key]) => key !== 'sourceMappings'))
    : document
  const report = await payload(MATRIX_VALIDATION_TOOL, { matrix: JSON.stringify(matrix) }, call, signal)
  const errors = findings(report.findings)
  if (!Array.isArray(report.results)) throw Error('The runtime omitted the matrix case results.')
  if (
    !errors.length &&
    (!object(matrix) || !Array.isArray(matrix.cases) || report.results.length !== matrix.cases.length)
  )
    throw Error('The runtime did not account for every proposed case.')
  report.results.forEach((row, index) => {
    if (!object(row) || row.index !== index || !['valid', 'invalid'].includes(String(row.status)))
      throw Error('The runtime returned unordered matrix findings.')
    const proposed = object(matrix) && Array.isArray(matrix.cases) ? matrix.cases[index] : undefined
    if (object(proposed) && typeof proposed.id === 'string' && proposed.id && row.id !== proposed.id)
      throw Error('The runtime returned findings for a different case.')
    const held = findings(row.findings)
    if ((row.status === 'valid') !== (held.length === 0))
      throw Error('The runtime returned inconsistent case findings.')
    errors.push(...held)
  })
  if (
    (report.status === 'valid') !== (errors.length === 0) ||
    !['valid', 'invalid'].includes(String(report.status))
  )
    throw Error('The runtime returned an inconsistent matrix validation report.')
  if (errors.some((f) => f.code === 'MATRIX-LIMIT'))
    throw Error('The proposal exceeds runtime limits. Reduce the requested suite size; no cases were saved.')
  if (object(document) && document.sourceMappings !== undefined) {
    const mappings = document.sourceMappings
    if (!object(mappings))
      errors.push({
        code: 'SOURCE-MAPPING',
        path: '/sourceMappings',
        message: 'Source mappings must be an object keyed by case id.',
      })
    else
      for (const [id, fields] of Object.entries(mappings)) {
        if (
          !Array.isArray(document.cases) ||
          !document.cases.some((c) => object(c) && c.id === id) ||
          !object(fields) ||
          Object.entries(fields).some(
            ([path, source]) => !path.startsWith('/') || !sources.some((s) => s.id === source),
          )
        )
          errors.push({
            code: 'SOURCE-MAPPING',
            path: '/sourceMappings/' + id,
            message: 'Map only existing case fact pointers to supplied source IDs.',
          })
      }
  }
  if (!errors.length) {
    try {
      importMatrix(document, 'ai')
    } catch (e) {
      errors.push({ code: 'DESK-MATRIX', path: '/cases', message: (e as Error).message })
    }
  }
  return errors
}

/** Corrections can add missing disposition members, but cannot change authored
 * assertions or factual inputs, lose cases, reorder cases, or invent sources. */
export function preserveTestMeaning(original: unknown, corrected: unknown): ProposalFinding[] {
  const fail = (path: string, message: string): ProposalFinding[] => [
    { code: 'CORRECTION-MEANING', path, message },
  ]
  if (
    !object(original) ||
    !Array.isArray(original.cases) ||
    !object(corrected) ||
    !Array.isArray(corrected.cases)
  )
    return fail('/cases', 'A repair must preserve the original case list.')
  if (original.cases.length !== corrected.cases.length)
    return fail('/cases', 'A repair cannot add or remove cases.')
  if (jsonIdentity(original.sourceMappings ?? {}) !== jsonIdentity(corrected.sourceMappings ?? {}))
    return fail('/sourceMappings', 'A repair cannot change source associations.')
  for (let i = 0; i < original.cases.length; i++) {
    const before = original.cases[i],
      after = corrected.cases[i]
    if (!object(before) || !object(after))
      return fail(`/cases/${i}`, 'The original case needs manual review.')
    const unique =
      typeof before.id === 'string' &&
      before.id.length > 0 &&
      original.cases.filter((c) => object(c) && c.id === before.id).length === 1
    if (unique && before.id !== after.id) return fail(`/cases/${i}/id`, 'Preserve case IDs and order.')
    for (const key of [
      'facts',
      'evidenceAvailability',
      'supportedExtensions',
      'expectedErrorClass',
      'expectedErrorPhase',
      'expectedHandoffTarget',
      'cites',
    ]) {
      if (
        Object.hasOwn(before, key) !== Object.hasOwn(after, key) ||
        jsonIdentity(before[key] ?? null) !== jsonIdentity(after[key] ?? null)
      )
        return fail(
          `/cases/${i}/${key}`,
          'A repair cannot invent or change inputs, evidence or assertions. This case needs review.',
        )
    }
    if (!retains(before.expectedDisposition, after.expectedDisposition))
      return fail(
        `/cases/${i}/expectedDisposition`,
        'A repair cannot change an existing expected answer. Resolve this expectation through review.',
      )
  }
  return []
}
function retains(before: unknown, after: unknown): boolean {
  if (object(before) && object(after))
    return Object.entries(before).every(
      ([key, value]) => Object.hasOwn(after, key) && retains(value, after[key]),
    )
  return jsonIdentity(before ?? null) === jsonIdentity(after ?? null)
}
export function proposalCases(record: TestProposal): TestCase[] {
  if (record.state !== 'ready') return []
  const document = record.attempts.at(-1)?.document
  return importMatrix(document, 'ai').map((c) => ({
    ...c,
    sources: record.sources,
    sourceMappings:
      object(document) && object(document.sourceMappings) && object(document.sourceMappings[c.id])
        ? (document.sourceMappings[c.id] as Record<string, string>)
        : {},
  }))
}

/** Historical saved counts are explicit receipts, never inferred from matching IDs. */
export function saveReviewedCases(
  suite: TestSuite,
  proposal: TestProposal,
  cases: TestCase[],
  digest: string,
): TestSuite {
  if (proposal.state !== 'ready' || proposal.packDigest !== digest)
    throw Error('The pack changed. Request new suggestions for this revision.')
  const record = suite.proposals?.find((p) => p.id === proposal.id)
  if (
    !record ||
    record.state !== 'ready' ||
    jsonIdentity(record.attempts) !== jsonIdentity(proposal.attempts)
  )
    throw Error('The proposal changed. Reopen it before saving.')
  if (record.additionsOnly && additionFindings({ cases: cases.map(c => c.row) }, [...Object.keys(record.caseSnapshots), ...suite.cases.map(c => c.id)]).length)
    throw Error('Design missing tests cannot replace existing cases. Request new unique IDs.')
  const ids = new Set(proposalCases(record).map((c) => c.id))
  if (
    !cases.length ||
    new Set(cases.map((c) => c.id)).size !== cases.length ||
    cases.some((c) => !ids.has(c.id))
  )
    throw Error('Select distinct cases from this proposal.')
  const savedCases: Record<string, string> = Object.assign(Object.create(null), record.savedCases)
  const next = [...suite.cases]
  for (const test of cases) {
    if (Object.hasOwn(savedCases, test.id))
      throw Error('This proposed case was already saved. Edit it in Cases.')
    const index = next.findIndex((c) => c.id === test.id)
    const current = next[index]
    if (
      (current ? jsonIdentity(current) : undefined) !==
      (Object.hasOwn(record.caseSnapshots, test.id) ? record.caseSnapshots[test.id] : undefined)
    )
      throw Error('This case changed after the request. Ask for a new proposal.')
    const saved = { ...structuredClone(test), revision: (current?.revision ?? 0) + 1 }
    if (index < 0) next.push(saved)
    else next[index] = saved
    savedCases[test.id] = jsonIdentity(saved)
  }
  if (next.length > 256) throw Error('This suite has reached its 256-case limit.')
  return {
    ...suite,
    cases: next,
    deleted: suite.deleted?.filter((id) => !cases.some((c) => c.id === id)),
    proposals: suite.proposals?.map((p) => (p.id === record.id ? { ...p, savedCases } : p)),
  }
}

/** Recover older ownership only when the adjacent recorded request is an exact match. */
export function proposalForMessage(suite: TestSuite, index: number): TestProposal | undefined {
  const message = suite.messages?.[index]
  if (message?.role !== 'assistant') return undefined
  if (message.proposalId) return suite.proposals?.find((p) => p.id === message.proposalId)
  const request = suite.messages?.[index - 1]
  if (request?.role !== 'user') return undefined
  const matching = suite.proposals?.filter((p) => p.at === request.at && p.request === request.text) ?? []
  return matching.length === 1 ? matching[0] : undefined
}

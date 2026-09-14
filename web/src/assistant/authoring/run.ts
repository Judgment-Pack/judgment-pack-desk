/**
 * Headless authoring proof. The host owns persistence; the engine only proposes.
 * No framework types, project writes or model credentials cross this boundary.
 * This controller is not connected to the shipping Create route yet.
 */
import { isCancelled, withAbort } from '../engines/contract'

export interface AuthoringCase {
  id: string
  facts: unknown
  evidenceAvailability?: unknown
  expectedDisposition: unknown
  expectedHandoffTarget?: unknown
  expectationSource: string
}

export interface CaseResult {
  id: string
  passed: boolean
  expected: unknown
  actual: unknown
}

export interface CandidateCheck {
  documentDigest: string
  runtimeIdentity: string
  valid: boolean
  diagnostics: unknown[]
  cases: CaseResult[]
}

export interface Revision {
  document: string
  digest: string
  summary: string
  check?: CandidateCheck
}

export interface AuthoringCheckpoint {
  formatVersion: 1
  id: string
  baseline: string
  brief: string
  cases: AuthoringCase[]
  revisions: Revision[]
  stage: 'draft' | 'check' | 'review' | 'done'
  status: 'running' | 'ready' | 'needs-input' | 'budget' | 'stalled' | 'interrupted' | 'failed' | 'stale'
  detail: string
  reviewSummaries: string[]
}

export type AuthorTurn =
  | { type: 'candidate'; document: string; summary: string }
  | { type: 'question'; text: string }

export interface AuthoringPorts {
  propose(context: AuthoringCheckpoint, signal: AbortSignal): Promise<AuthorTurn>
  check(document: string, cases: AuthoringCase[], signal: AbortSignal): Promise<CandidateCheck>
  /** Fresh reviewer context. Cases need expectations grounded outside the candidate. */
  review(context: AuthoringCheckpoint, signal: AbortSignal): Promise<{
    cases: AuthoringCase[]; questions: string[]; summary: string
  }>
  /** Must durably replace the previous checkpoint before resolving. */
  save(checkpoint: AuthoringCheckpoint): Promise<void>
}

export async function documentDigest(document: string): Promise<string> {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(document))
  return Array.from(new Uint8Array(bytes), byte => byte.toString(16).padStart(2, '0')).join('')
}

/** Key order is immaterial; array order and exact JSON values are preserved. */
export function jsonIdentity(value: unknown): string {
  const sort = (input: unknown): unknown => {
    if (Array.isArray(input)) return input.map(sort)
    if (input !== null && typeof input === 'object') {
      return Object.fromEntries(Object.entries(input).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)
        .map(([key, member]) => [key, sort(member)]))
    }
    return input
  }
  return JSON.stringify(sort(value))
}

function checkCases(cases: AuthoringCase[]): void {
  const ids = new Set<string>()
  for (const row of cases) {
    if (!row.id.trim() || ids.has(row.id) || !row.expectationSource.trim() || row.expectedDisposition == null) {
      throw new Error('Test cases need unique IDs, independent expectations and their source.')
    }
    ids.add(row.id)
  }
}

export function newAuthoringRun(id: string, baseline: string, brief: string, cases: AuthoringCase[]): AuthoringCheckpoint {
  checkCases(cases)
  return structuredClone({ formatVersion: 1, id, baseline, brief, cases, revisions: [],
    stage: 'draft', status: 'running', detail: '', reviewSummaries: [] })
}

/**
 * Resume uses the last completed stage, not an old model stream. Checkpoints are
 * host-owned trusted state, not a public JSON import format. Runtime failures do
 * not become policy failures. Saving errors reject instead of claiming recovery.
 */
export async function runAuthoring(
  input: AuthoringCheckpoint,
  ports: AuthoringPorts,
  options: { baseline: string; runtimeIdentity: string; maxRevisions: number; maxReviewPasses?: number; signal: AbortSignal }
): Promise<AuthoringCheckpoint> {
  const state = structuredClone(input)
  const { signal, maxRevisions } = options
  const maxReviewPasses = options.maxReviewPasses ?? 3
  if (!Number.isInteger(maxRevisions) || maxRevisions < 1 || maxRevisions > 100) {
    throw new Error('The revision budget must be an integer from 1 to 100.')
  }
  if (!Number.isInteger(maxReviewPasses) || maxReviewPasses < 1 || maxReviewPasses > 100) {
    throw new Error('The review budget must be an integer from 1 to 100.')
  }
  if (state.formatVersion !== 1) throw new Error('Unsupported authoring checkpoint version.')
  checkCases(state.cases)
  const save = () => ports.save(structuredClone(state))
  const finish = async (status: AuthoringCheckpoint['status'], detail: string) => {
    state.status = status
    state.detail = detail
    await save()
    return state
  }
  if (state.baseline !== options.baseline) return finish('stale', 'The saved pack changed. Start from its current revision.')
  const latest = state.revisions.at(-1)
  if (latest?.check && latest.check.runtimeIdentity !== options.runtimeIdentity) {
    delete latest.check
    state.stage = 'check'
    state.status = 'running'
  }
  if (state.status === 'ready' || state.status === 'needs-input' || state.status === 'stalled') return state
  state.status = 'running'
  state.detail = ''
  await save()
  while (state.stage !== 'done') {
    try {
      if (signal.aborted) return await finish('interrupted', 'Stopped. The last completed stage is saved.')
      if (state.stage === 'draft') {
        if (state.revisions.length >= maxRevisions) return await finish('budget', 'Revision budget reached. Continue with a larger budget.')
        const turn = await withAbort(() => ports.propose(structuredClone(state), signal), signal)
        if (turn.type === 'question') return await finish('needs-input', turn.text)
        const identity = jsonIdentity(JSON.parse(turn.document))
        if (state.revisions.some(item => jsonIdentity(JSON.parse(item.document)) === identity)) {
          return await finish('stalled', 'The agent repeated an earlier candidate. Review the unresolved findings.')
        }
        const digest = await documentDigest(turn.document)
        state.revisions.push({ document: turn.document, digest, summary: turn.summary })
        state.stage = 'check'
      } else if (state.stage === 'check') {
        const revision = state.revisions.at(-1)
        if (!revision) throw new Error('A candidate is required before checking.')
        const check = await withAbort(() => ports.check(revision.document, structuredClone(state.cases), signal), signal)
        if (check.documentDigest !== revision.digest || check.runtimeIdentity !== options.runtimeIdentity) {
          throw new Error('Checks do not belong to this candidate and runtime.')
        }
        if (check.valid && (check.cases.length !== state.cases.length ||
          check.cases.some((row, index) => row.id !== state.cases[index]?.id))) {
          throw new Error('The check did not account for every test case.')
        }
        revision.check = structuredClone(check)
        state.stage = check.valid && check.cases.length > 0 && check.cases.every(row => row.passed) ? 'review' : 'draft'
        if (check.valid && state.cases.length === 0) return await finish('needs-input', 'Add test cases with independently established expectations.')
      } else {
        if (state.reviewSummaries.length >= maxReviewPasses) return await finish('budget', 'Review budget reached. Continue with a larger budget.')
        const review = await withAbort(() => ports.review(structuredClone(state), signal), signal)
        state.reviewSummaries.push(review.summary)
        if (review.questions.length) return await finish('needs-input', review.questions.join('\n'))
        checkCases(review.cases)
        const added: AuthoringCase[] = []
        for (const row of review.cases) {
          const previous = state.cases.find(item => item.id === row.id)
          if (previous && jsonIdentity(previous) !== jsonIdentity(row)) {
            throw new Error('A reviewer attempted to change an established test case.')
          }
          if (!previous) added.push(structuredClone(row))
        }
        state.cases.push(...added)
        state.stage = added.length ? 'check' : 'done'
      }
    } catch (error) {
      return await finish(isCancelled(error) ? 'interrupted' : 'failed',
        isCancelled(error) ? 'Stopped. The last completed stage is saved.' :
          error instanceof Error ? error.message : 'Authoring failed.')
    }
    await save()
  }
  return finish('ready', 'The candidate passed these checks and is ready for your review.')
}

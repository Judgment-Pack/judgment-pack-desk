import { object, type TestCase, type TestRun, type TestSuite } from '../packs/test-workspace/model'
import { jsonIdentity } from '../research/checkCandidate'
export interface Snapshot { documents?: { id: string; context: string }[]; kind: 'case' | 'job' | 'run'; title: string; pack: string; record: Record<string, unknown> }
export interface BriefText { context: string; findings: string; uncertainty: string; nextAction: string }
export interface Revision { id: string; revision: number; at: string; snapshot: Snapshot; text: BriefText; model: string; method: string }
export interface History { revisions: Revision[]; pending?: { token: string; expires: string; snapshot: Snapshot; model: string } }
export interface Reply { project?: string; content: {version: 1; subjects: Record<string, History>}; snapshot?: Snapshot }
export function caseSnapshot(pack: string, c: TestCase, suite: TestSuite): Snapshot {
  const run = [...suite.runs].reverse().find(r => r.packText === pack && r.cases.some(saved => jsonIdentity(saved) === jsonIdentity(c)))
  // Keep this case's result, not the other cases' private inputs or unrelated reports.
  const result = run?.report?.packs?.flatMap(p => p.rows ?? []).find(row => row.id === c.id)
  return { kind: 'case', title: c.name, pack, record: { case: c, ...(suite.research ? { research: suite.research } : {}), ...(run ? { run: { id: run.id, at: run.at, packDigest: run.packDigest, result, trial: run.trial, error: run.error } } : {}) } }
}
export function briefText(value: unknown): BriefText {
  if (!object(value)) throw Error('The model did not return a brief. The saved version is unchanged.')
  const fields = ['context', 'findings', 'uncertainty', 'nextAction'] as const
  if (fields.some(k => typeof value[k] !== 'string' || !(value[k] as string).trim() || (value[k] as string).length > 3000) || fields.reduce((n,k) => n + [...String(value[k])].length, 0) > 4500) throw Error('The brief must contain four concise sections, within 4,500 characters.')
  return Object.fromEntries(fields.map(k => [k, value[k]])) as unknown as BriefText
}
export function evidenceFor(snapshot: Snapshot) {
  const pack = JSON.parse(snapshot.pack) as Record<string, unknown>
  const c = snapshot.record.case as TestCase | undefined
  const run = snapshot.record.run as { input?: { evidence?: Record<string, string> } } | undefined
  const sample = snapshot.record.sample as { evidence?: Record<string, string> } | undefined
  const availability = snapshot.kind === 'case' ? c?.row.evidenceAvailability : snapshot.kind === 'run' ? run?.input?.evidence : sample?.evidence
  return (Array.isArray(pack.evidenceRequirements) ? pack.evidenceRequirements.filter(object) : []).map(e => ({ id: String(e.id), label: String(e.description || e.id), required: e.required !== false, state: object(availability) && ['present', 'absent'].includes(String(availability[String(e.id)])) ? String(availability[String(e.id)]) : 'unknown' }))
}
export function decisionText(value: unknown): string {
  if (typeof value === 'string') { try { return decisionText(JSON.parse(value)) } catch { return value } }
  if (!object(value)) return '—'
  const d = object(value.disposition) ? value.disposition : value
  const reasons = Array.isArray(d.reasons) && d.reasons.length ? ` · ${d.reasons.join(', ')}` : ''
  const target = object(value.handoffTarget) ? value.handoffTarget.name : undefined
  return String(d.outcomeId ?? d.kind ?? '—') + reasons + (target ? ` · ${String(target)}` : '')
}
export function briefResults(s: Snapshot) {
  const c = s.record.case as TestCase | undefined
  const run = s.record.run as (TestRun & { result?: Record<string, unknown>; state?: string }) | undefined
  const result = run?.result
  return { expected: c ? decisionText(c.row.expectedDisposition) : '', actual: s.kind === 'job' ? decisionText(s.record.preview) : s.kind === 'run' ? decisionText(result) : decisionText(result?.actual ?? run?.trial), state: run?.error ?? run?.state ?? (result?.status as string | undefined) }
}

export function sourceIdentity(s: Snapshot): string { const { documents: _documents, ...basis } = s; return jsonIdentity(basis) }

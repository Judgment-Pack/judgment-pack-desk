import type {TriggerOrigin} from './triggerTypes'
import { jobsRequest } from './wire'
import type { MappingV2, SourceV2, Preparation, InputProfile } from './mappingTypes'
import type { DocumentObject } from '../documents/client'
import type { PackTest } from '../mcp/types'
import { deskFetch } from '../files/client'

export interface InputMapping { version: 1; provider: 'local-file' | 'google-drive'; facts: { target: string; source: string }[]; evidence: { requirement: string; source: string }[] }
export interface SourceInput { mapping: InputMapping; snapshot: DocumentObject & { selectedAt?: string }; mappingDigest?: string }
export interface InputPreview { input: JobInput; factsText: string; evidenceText: string }
export interface JobInput { source?: SourceInput | SourceV2; preparation?: Preparation; facts?: unknown; evidence?: Record<string, 'present' | 'absent' | 'unknown'> }
export interface Decision { disposition: { kind: string; outcomeId?: string; reasons: string[]; handoff: { state: string; triggeredBy?: string[] } }; handoffTarget?: { kind?: string; name?: string } }
export interface Release { inputMapping?: InputMapping | MappingV2; inputProfiles?: InputProfile[]; mappingWarnings?: string[]; id: string; title: string; packId: string; packVersion: string; packDigest: string; runtimeDigest: string; createdAt: string; pack: string; sample: JobInput; preview: Decision; tests: 'not-run' | 'passed' | 'failed' | 'error'; testEvidence?: ReleaseTests }
export interface Job { initialTriggerId?:string; triggers?:{id:string;kind:string;paused:boolean;nextAt?:string}[]; id: string; name: string; releaseId: string; revision: number; createdAt: string; packTitle?: string; packVersion?: string; recentRuns?: Pick<Run, 'id' | 'state' | 'createdAt'>[] }
/**
 * A run as Runner (v0.5.0, `internal/runner/model.go`) returns it. `startedAt`
 * is absent until execution starts, and absent for a run that expired in the
 * queue. `requestedBy` is the installation's owner or `trigger:<id>`; a run
 * recorded before Runner kept it has none. `auditBytes` (the record exactly as
 * the runtime wrote it) and `auditSignatures` (the attempt's signature sidecar)
 * are base64, only on `GET runs/{run}`: every list strips them, with `input`
 * and `audit`.
 */
export interface Run { trigger?:TriggerOrigin; jobName?: string; id: string; jobId: string; releaseId: string; revision: number; state: 'queued' | 'running' | 'completed' | 'failed' | 'interrupted'; createdAt: string; startedAt?: string; finishedAt?: string; requestedBy?: string; attempt: number; problem?: string; input?: JobInput; result?: Decision; audit?: unknown; auditBytes?: string; auditSignatures?: string }
export interface Page<T> { items: T[]; next: number }
/** A refusal in the runner's words, with its code for the few refusals Desk shows differently. */
export class JobsRequestError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) { super(message); this.name = 'JobsRequestError' }
}
export async function jobsAPI<T>(path: string, body?: unknown, key?: string, signal?: AbortSignal): Promise<T> {
  const response = await deskFetch(`/api/operations/${path}`, body === undefined ? { signal } : { signal, method: 'POST', headers: { 'Content-Type': 'application/json', ...(key ? { 'Idempotency-Key': key } : {}) }, body: jobsRequest(body) })
  const result = await response.json()
  if (!response.ok) throw new JobsRequestError(result.error?.message ?? result.message ?? 'The local runner could not complete this request.', response.status, typeof result.error?.code === 'string' ? result.error.code : undefined)
  return result as T
}

export interface ReleaseTests {
 status: 'passed' | 'failed' | 'error'; matrix: string; matrixDigest: string; packDigest: string; runtimeDigest: string; checkedAt: string;
 source?: { packKey: string; suiteRevision: number; caseNames?: Record<string, string>; exploratoryCount?: number };
 report?: PackTest; problem?: string;
}

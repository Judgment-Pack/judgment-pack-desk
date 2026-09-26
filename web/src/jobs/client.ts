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
export interface Job { id: string; name: string; releaseId: string; revision: number; createdAt: string }
export interface Run { id: string; jobId: string; releaseId: string; revision: number; state: 'queued' | 'running' | 'completed' | 'failed' | 'interrupted'; createdAt: string; startedAt?: string; finishedAt?: string; attempt: number; problem?: string; input?: JobInput; result?: Decision; audit?: unknown }
export interface Page<T> { items: T[]; next: number }
export async function jobsAPI<T>(path: string, body?: unknown, key?: string, signal?: AbortSignal): Promise<T> {
  const response = await deskFetch(`/api/operations/${path}`, body === undefined ? { signal } : { signal, method: 'POST', headers: { 'Content-Type': 'application/json', ...(key ? { 'Idempotency-Key': key } : {}) }, body: jobsRequest(body) })
  const result = await response.json()
  if (!response.ok) throw new Error(result.error?.message ?? result.message ?? 'The local runner could not complete this request.')
  return result as T
}

export interface ReleaseTests {
 status: 'passed' | 'failed' | 'error'; matrix: string; matrixDigest: string; packDigest: string; runtimeDigest: string; checkedAt: string;
 source?: { packKey: string; suiteRevision: number; caseNames?: Record<string, string>; exploratoryCount?: number };
 report?: PackTest; problem?: string;
}

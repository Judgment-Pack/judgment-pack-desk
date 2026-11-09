import { AIFailureError } from '@judgment-pack/harness/failures'
import { deskFetch } from '../../files/client'
import { discardBody, sessionBearer, whenSessionEnds } from '../../mcp/session'
import { RunRequestError } from '../serverRuns/client'
import { MAX_SOURCE_CATALOG_BYTES, readSourceCatalog, readSourceRevocation, type SourceCatalog, type SourceRevocation } from './catalog'
import { encodeSourceDecisionRequest, type SourceApproveRequest, type SourceRevokeRequest } from './requests'

/** A review belongs to one chat and the session that opened it. */
export async function sourceReviewClient(chat: string): Promise<SourceReviewClient> {
  if (!/^[a-zA-Z0-9_-]{1,160}$/.test(chat)) throw new RunRequestError('not-sent', undefined, 'invalid-request')
  return new SourceReviewClient(chat, await sessionBearer())
}
export class SourceReviewClient {
  private observed?: SourceCatalog
  private refreshRequired = true
  private busy = false
  constructor(private readonly chat: string, private readonly session: string) {}

  /** Explicit observation: GET may retain a review; never poll or auto-retry it. */
  async review(signal?: AbortSignal): Promise<SourceCatalog> {
    if (this.busy) throw new RunRequestError('not-sent', undefined, 'stale-state')
    this.busy = true; this.refreshRequired = true
    try {
      const result = readSourceCatalog(await this.request('', undefined, signal))
      this.observed = result; this.refreshRequired = false
      return result
    } finally { this.busy = false }
  }
  async approve(request: SourceApproveRequest, signal?: AbortSignal): Promise<SourceCatalog> {
    const body = this.decision('approve', request)
    const selector = JSON.parse(body) as SourceApproveRequest
    const row = this.observed?.sources.find(row => row.reviewId === selector.reviewId && row.reviewRevision === selector.reviewRevision)
    if (!row) throw new RunRequestError('not-sent', undefined, 'stale-state')
    this.busy = true; this.refreshRequired = true
    try {
      const result = readSourceCatalog(await this.request('/approve', body, signal))
      const selected = result.sources.find(value => value.snapshot.adapterId === row.snapshot.adapterId)
      if (selected?.status !== 'approved' || selected.approval?.revision !== 1 || JSON.stringify(selected.snapshot) !== JSON.stringify(row.snapshot)) throw new RunRequestError('uncertain', undefined, 'invalid-response')
      this.observed = result; this.refreshRequired = false
      return result
    } catch (cause) { throw this.mutationFailure(cause) }
    finally { this.busy = false }
  }
  async revoke(request: SourceRevokeRequest, signal?: AbortSignal): Promise<SourceRevocation> {
    const body = this.decision('revoke', request)
    const selector = JSON.parse(body) as SourceRevokeRequest
    const previous = this.observed?.sources.find(row => row.approval?.id === selector.approvalId && row.approval.revision === selector.approvalRevision)?.approval
    if (!previous) throw new RunRequestError('not-sent', undefined, 'stale-state')
    this.busy = true; this.refreshRequired = true
    try {
      const result = readSourceRevocation(await this.request('/revoke', body, signal))
      if (result.approval.id !== selector.approvalId || result.approval.approvedAt !== previous.approvedAt) throw new RunRequestError('uncertain', undefined, 'invalid-response')
      // Receipt only: the next explicit review obtains a complete fresh catalog.
      return result
    } catch (cause) { throw this.mutationFailure(cause) }
    finally { this.busy = false }
  }
  private decision(kind: 'approve' | 'revoke', request: unknown): string {
    if (this.busy || this.refreshRequired) throw new RunRequestError('not-sent', undefined, 'stale-state')
    try { return encodeSourceDecisionRequest(kind, request) }
    catch { throw new RunRequestError('not-sent', undefined, 'invalid-request') }
  }
  private mutationFailure(cause: unknown): RunRequestError {
    return cause instanceof RunRequestError ? cause : new RunRequestError('uncertain', undefined, cause instanceof AIFailureError ? 'invalid-response' : 'connection-lost')
  }
  private async request(suffix: '' | '/approve' | '/revoke', body?: string, signal?: AbortSignal): Promise<string> {
    if (!/^[a-zA-Z0-9_-]{1,160}$/.test(this.chat)) throw new RunRequestError('not-sent', undefined, 'invalid-request')
    const controller = new AbortController(), abort = () => controller.abort()
    const unsubscribe = whenSessionEnds(abort)
    signal?.addEventListener('abort', abort, {once: true})
    let answer: Response | undefined, sent = false
    const current = async () => {
      if (signal?.aborted || controller.signal.aborted || await sessionBearer() !== this.session) throw new RunRequestError(sent ? 'uncertain' : 'not-sent', undefined, 'permission')
    }
    try {
      await current()
      sent = true
      answer = await deskFetch(`/api/assistant-source-approvals/${this.chat}${suffix}`, {method: body === undefined ? 'GET' : 'POST', redirect: 'error', cache: 'no-store', signal: controller.signal,
        ...(body === undefined ? {} : {headers: {'Content-Type': 'application/json'}, body})})
      await current()
      if (!answer.ok) throw new RunRequestError(answer.status >= 400 && answer.status < 500 && answer.status !== 408 ? 'refused' : 'uncertain', answer.status,
        answer.status === 409 ? 'stale-state' : answer.status === 401 ? 'authentication' : answer.status === 403 ? 'permission' : answer.status === 400 ? 'invalid-request' : 'unavailable')
      const raw = await readText(answer, controller.signal)
      await current()
      return raw
    } catch (cause) {
      if (cause instanceof RunRequestError) throw cause
      throw new RunRequestError(sent ? 'uncertain' : 'not-sent', undefined, cause instanceof AIFailureError ? 'invalid-response' : 'connection-lost')
    } finally {
      if (answer) await discardBody(answer)
      unsubscribe(); signal?.removeEventListener('abort', abort)
    }
  }
}
async function readText(answer: Response, signal: AbortSignal): Promise<string> {
  if (answer.headers.get('Content-Type')?.split(';')[0].trim() !== 'application/json' || !answer.body) throw new RunRequestError('uncertain', undefined, 'invalid-response')
  const reader = answer.body.getReader(), decoder = new TextDecoder('utf-8', {fatal: true})
  const abort = () => { void reader.cancel().catch(() => {}) }
  signal.addEventListener('abort', abort, {once: true})
  let bytes = 0, raw = ''
  try {
    for (;;) {
      if (signal.aborted) throw new RunRequestError('uncertain')
      const part = await reader.read()
      if (signal.aborted) throw new RunRequestError('uncertain')
      if (part.done) return raw + decoder.decode()
      bytes += part.value.byteLength
      if (bytes > MAX_SOURCE_CATALOG_BYTES) throw new RunRequestError('uncertain', undefined, 'invalid-response')
      raw += decoder.decode(part.value, {stream: true})
    }
  } finally {
    signal.removeEventListener('abort', abort)
    try { await reader.cancel() } catch { /* The transport already ended. */ }
    reader.releaseLock()
  }
}

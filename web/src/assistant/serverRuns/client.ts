import { validQuestionDraft } from '@judgment-pack/harness/questions'
import { AIFailureError, failure, normalizeFailure, type FailureCode } from '@judgment-pack/harness/failures'
import { deskFetch } from '../../files/client'
import { discardBody, sessionBearer, whenSessionEnds } from '../../mcp/session'
import { cursorParts, decodeDiscovery, decodeSnapshot, digest, exact, invalid, MAX_RUN_BYTES, object, ownerID, parseRunJSON, pendingQuestion, revision, runID, text } from './decode'
import { readFrames, RunObservations } from './stream'
import type { RunAction, RunBinding, RunDiscovery, RunObservation, RunReference, RunSnapshot, RunStart } from './types'

export type Delivery = 'not-sent' | 'refused' | 'uncertain'
/** Safe catalogue failure plus explicit mutation delivery knowledge, never caught prose. */
export class RunRequestError extends AIFailureError {
  constructor(readonly delivery: Delivery, readonly status?: number, code: FailureCode = 'connection-lost') {
    super({...failure(code,'desk',delivery === 'not-sent' ? 'not-started' : delivery === 'refused' ? 'refused' : 'unknown'), ...(status === undefined ? {} : {status})})
    this.name = 'RunRequestError'
  }
}
const ROOT = '/api/assistant-runs'
/** Capture the document's current authenticated session once; this client cannot follow a replacement. */
export async function serverRunClient(): Promise<ServerRunClient> { return new ServerRunClient(await sessionBearer()) }
export class ServerRunClient {
  constructor(private readonly session: string) {}
  private async scope(signal?: AbortSignal) {
    const controller = new AbortController()
    const abort = () => controller.abort()
    const unsubscribe = whenSessionEnds(abort)
    signal?.addEventListener('abort',abort,{once:true})
    const close = () => {unsubscribe(); signal?.removeEventListener('abort',abort)}
    try {
      if (signal?.aborted || await sessionBearer() !== this.session) controller.abort()
      if (controller.signal.aborted) throw new RunRequestError('not-sent')
      return {signal:controller.signal, close}
    } catch (cause) { close(); throw cause instanceof RunRequestError ? cause : new RunRequestError('not-sent',undefined,'permission') }
  }
  private async current(signal: AbortSignal): Promise<void> {
    if (signal.aborted || await sessionBearer() !== this.session) throw new RunRequestError('uncertain',undefined,'permission')
  }
  private async json(path: string, expected: RunBinding & {id?:string}, body?: RunStart | RunAction, signal?: AbortSignal): Promise<RunSnapshot> {
    const scope = await this.scope(signal)
    let sent = false, answer: Response | undefined
    try {
      sent = true
      answer = await deskFetch(path,{method:body ? 'POST' : 'GET',redirect:'error',signal:scope.signal, headers:body ? {'Content-Type':'application/json'} : {}, ...(body ? {body:JSON.stringify(body)} : {})})
      await this.current(scope.signal)
      if (!answer.ok) { await discardBody(answer); throw new RunRequestError(answer.status >= 400 && answer.status < 500 && answer.status !== 408 ? 'refused' : 'uncertain',answer.status,statusCode(answer.status)) }
      const snapshot = decodeSnapshot(await readJSON(answer,scope.signal),expected)
      await this.current(scope.signal)
      return snapshot
    } catch (cause) {
      if (cause instanceof RunRequestError) throw cause
      throw new RunRequestError(sent ? 'uncertain' : 'not-sent',undefined,cause instanceof AIFailureError && cause.failure.code === 'invalid-response' ? 'invalid-response' : 'connection-lost')
    } finally { if (answer) await discardBody(answer); scope.close() }
  }
  async start(start: RunStart, signal?: AbortSignal): Promise<RunSnapshot> {
    if (!object(start) || !exact(start,['admissionId','purpose','ownerId','revision','requestId']) || !text(start.admissionId,128) || start.purpose !== 'chat' || !ownerID(start.ownerId) || !digest(start.revision) || !text(start.requestId,128)) throw new RunRequestError('not-sent',undefined,'invalid-response')
    const body: RunStart = {admissionId:start.admissionId,purpose:'chat',ownerId:start.ownerId,revision:start.revision,requestId:start.requestId}
    return this.json(ROOT,{owner:{kind:'chat',id:body.ownerId,revision:body.revision},requestId:body.requestId},body,signal)
  }
  read(ref: RunReference, signal?: AbortSignal): Promise<RunSnapshot> { const bound = bind(ref); return this.json(`${ROOT}/${bound.id}`,bound,undefined,signal) }
  action(snapshot: RunSnapshot, action: RunAction, signal?: AbortSignal): Promise<RunSnapshot> {
    const bound = bind(snapshot)
    if (snapshot.historical || !object(action) || !text(action.id,128) || !revision(action.revision) || action.revision !== snapshot.revision || !['cancel','approval','abandon','answer','extend-budget'].includes(action.kind) || !exact(action,action.kind === 'approval' ? ['id','kind','revision','callId','argsDigest','approved','note'] : action.kind === 'answer' ? ['id','kind','revision','questionId','questionDigest','answer'] : ['id','kind','revision'])) throw new RunRequestError('not-sent',undefined,'permission')
    const body: RunAction = action.kind === 'approval' ? {id:action.id,kind:'approval',revision:action.revision,callId:action.callId,argsDigest:action.argsDigest,approved:action.approved,...(action.note === undefined ? {} : {note:action.note})} : action.kind === 'answer' ? {id:action.id,kind:'answer',revision:action.revision,questionId:action.questionId,questionDigest:action.questionDigest,answer:action.answer} : {id:action.id,kind:action.kind,revision:action.revision}
    if (body.kind === 'approval' && (!text(body.callId,256) || !digest(body.argsDigest) || typeof body.approved !== 'boolean' || body.note !== undefined && (typeof body.note !== 'string' || body.note.length > 2000))) throw new RunRequestError('not-sent',undefined,'invalid-response')
    if(body.kind==='answer'){
      const q=pendingQuestion(snapshot)
      if(snapshot.state!=='awaiting-input'||!q||body.questionId!==q.id||body.questionDigest!==q.digest||!validQuestionDraft(q.question,body.answer)||!body.answer.selected.length&&!body.answer.text.trim())throw new RunRequestError('not-sent',undefined,'invalid-request')
      try { parseRunJSON(JSON.stringify(body.answer.text)); if(body.answer.text.includes('\0'))throw Error() } catch {throw new RunRequestError('not-sent',undefined,'invalid-request')}
    }
    if(body.kind==='extend-budget'&&(snapshot.state!=='paused-budget'||pendingQuestion(snapshot)))throw new RunRequestError('not-sent',undefined,'permission')
    return this.json(`${ROOT}/${bound.id}/actions`,bound,body,signal)
  }
  async discover(binding: RunDiscovery, signal?: AbortSignal): Promise<readonly RunSnapshot[]> {
    if (!object(binding) || !object(binding.owner) || !ownerID(binding.owner.id) || binding.owner.kind !== 'chat' || !(typeof binding.requestId === 'string' && /^[A-Za-z0-9-]{1,100}$/.test(binding.requestId))) throw new RunRequestError('not-sent',undefined,'invalid-response')
    const bound: RunDiscovery = {owner:{kind:binding.owner.kind,id:binding.owner.id},requestId:binding.requestId}
    const scope = await this.scope(signal)
    let answer: Response | undefined
    try {
      answer = await deskFetch(`${ROOT}/owners/${bound.owner.id}/requests/${encodeURIComponent(bound.requestId)}`,{redirect:'error',signal:scope.signal})
      await this.current(scope.signal)
      if (!answer.ok) { await discardBody(answer); throw new RunRequestError('refused',answer.status,statusCode(answer.status)) }
      const value = await readJSON(answer,scope.signal)
      await this.current(scope.signal)
      return decodeDiscovery(value,bound)
    } catch (cause) { if (cause instanceof RunRequestError) throw cause; throw new AIFailureError(normalizeFailure(cause,'desk')) }
    finally { if (answer) await discardBody(answer); scope.close() }
  }
  async *events(observations: RunObservations, signal?: AbortSignal): AsyncGenerator<RunObservation> {
    validateReference(observations.reference)
    const cursor = observations.cursor
    if (cursor !== undefined) cursorParts(cursor)
    const scope = await this.scope(signal)
    let answer: Response | undefined
    try {
      answer = await deskFetch(`${ROOT}/${observations.reference.id}/events`,{redirect:'error',signal:scope.signal,headers:{Accept:'text/event-stream',...(cursor === undefined ? {} : {'Last-Event-ID':cursor})}})
      await this.current(scope.signal)
      if (!answer.ok) { await discardBody(answer); throw new RunRequestError('refused',answer.status,statusCode(answer.status)) }
      if (answer.headers.get('Content-Type')?.split(';')[0].trim() !== 'text/event-stream' || !answer.body) { await discardBody(answer); return invalid() }
      let reconciled = false
      for await (const frame of readFrames(answer.body,scope.signal)) {
        await this.current(scope.signal)
        const observation = observations.accept(frame)
        if (frame.kind === 'snapshot') reconciled = true
        if (observation) yield observation
      }
      if (!scope.signal.aborted && (!reconciled || !observations.snapshot || ['admitted','running'].includes(observations.snapshot.state))) throw new RunRequestError('uncertain')
    } catch (cause) { if (!scope.signal.aborted) throw cause instanceof AIFailureError ? cause : new AIFailureError(normalizeFailure(cause,'desk')) }
    finally { if (answer) await discardBody(answer); scope.close() }
  }
}
function statusCode(status: number): FailureCode { return status === 409 ? 'stale-state' : status === 400 ? 'invalid-request' : status === 401 ? 'authentication' : status === 403 ? 'permission' : status === 429 ? 'rate-limited' : 'unavailable' }
function validateReference(ref: RunReference): void {
  if (!object(ref) || !object(ref.owner) || !runID(ref.id) || ref.owner.kind !== 'chat' || !ownerID(ref.owner.id) || !digest(ref.owner.revision) || !text(ref.requestId,128)) throw new RunRequestError('not-sent',undefined,'invalid-response')
}
function bind(ref: RunReference): RunReference { validateReference(ref); return {id:ref.id,owner:{...ref.owner},requestId:ref.requestId} }
async function readJSON(answer: Response, signal: AbortSignal): Promise<unknown> {
  if (answer.headers.get('Content-Type')?.split(';')[0].trim() !== 'application/json' || !answer.body) { await discardBody(answer); return invalid() }
  const reader = answer.body.getReader(), decoder = new TextDecoder('utf-8',{fatal:true})
  let bytes = 0, raw = ''
  const abort = () => {void reader.cancel().catch(()=>{})}
  signal.addEventListener('abort',abort,{once:true})
  try {
    while (true) {
      if (signal.aborted) return invalid()
      const part = await reader.read()
      if (signal.aborted) return invalid()
      if (part.done) break
      bytes += part.value.byteLength
      if (bytes > MAX_RUN_BYTES) return invalid()
      raw += decoder.decode(part.value,{stream:true})
    }
    return parseRunJSON(raw + decoder.decode())
  } finally { signal.removeEventListener('abort',abort); try { await reader.cancel() } catch { /* Transport already ended. */ } reader.releaseLock() }
}

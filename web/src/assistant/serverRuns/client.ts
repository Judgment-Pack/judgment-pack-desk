import { readPackCheck, type PackProposalCheck } from '../../chat/packProposal'
import { digestOf } from '../../research/checkCandidate'
import { validQuestionDraft } from '@judgment-pack/harness/questions'
import { AIFailureError, failure, normalizeFailure, type FailureCode } from '@judgment-pack/harness/failures'
import { deskFetch } from '../../files/client'
import { discardBody, sessionBearer, whenSessionEnds } from '../../mcp/session'
import { cursorParts, decodeDiscovery, decodeSnapshot, digest, exact, invalid, MAX_RUN_BYTES, object, ownerID, briefSubject, runOwner, parseRunJSON, pendingQuestion, revision, runID, text } from './decode'
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
  private async json(path: string, expected: RunBinding & {id?:string}, body?: RunStart | RunAction, signal?: AbortSignal, headers:Record<string,string> = {}): Promise<RunSnapshot> {
    const scope = await this.scope(signal)
    let sent = false, answer: Response | undefined
    try {
      sent = true
      answer = await deskFetch(path,{method:body ? 'POST' : 'GET',redirect:'error',signal:scope.signal, headers:{...headers,...(body ? {'Content-Type':'application/json'} : {})}, ...(body ? {body:JSON.stringify(body)} : {})})
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
  async start(start: RunStart, signal?: AbortSignal, target?: {connectionId:string;connectionRevision:string}): Promise<RunSnapshot> {
    if (!object(start) || !exact(start,['admissionId','purpose','ownerId','revision','requestId']) || !text(start.admissionId,128) || !runOwner(start.purpose,start.ownerId) || !digest(start.revision) || !text(start.requestId,128) || start.purpose === 'connection-diagnostic' && (start.admissionId!==`diagnostic-${start.requestId}` || !/^[A-Za-z0-9-]{1,100}$/.test(start.requestId) || !target || target.connectionId!==start.ownerId || !text(target.connectionRevision,128)) || start.purpose === 'brief' && (start.admissionId!==`brief-${start.requestId}` || !/^[a-f0-9]{32}$/.test(start.requestId) || !target || !text(target.connectionId,128) || !text(target.connectionRevision,128))) throw new RunRequestError('not-sent',undefined,'invalid-response')
    const body: RunStart = {admissionId:start.admissionId,purpose:start.purpose,ownerId:start.ownerId,revision:start.revision,requestId:start.requestId}
    const snapshot=await this.json(ROOT,{owner:{kind:body.purpose,id:body.ownerId,revision:body.revision},requestId:body.requestId},body,signal,target ? {'X-Assistant-Connection':target.connectionId,'X-Assistant-Revision':target.connectionRevision} : {})
    if(snapshot.admissionId!==body.admissionId)throw new RunRequestError('uncertain',undefined,'invalid-response')
    return snapshot
  }
  /** Model-free host capture: the caller supplies only one attempt ID and saved connection. */
  async diagnosticReference(requestId:string,target:{connectionId:string;connectionRevision:string}):Promise<RunStart> {
    if(!/^[A-Za-z0-9-]{1,100}$/.test(requestId)||!runOwner('connection-diagnostic',target.connectionId)||!text(target.connectionRevision,128))throw new RunRequestError('not-sent',undefined,'invalid-request')
    const value=await this.briefJSON('/api/assistant-diagnostics/reference',{requestId},undefined,{'X-Assistant-Connection':target.connectionId,'X-Assistant-Revision':target.connectionRevision})
    if(!object(value)||!exact(value,['start'])||!object(value.start)||!exact(value.start,['admissionId','purpose','ownerId','revision','requestId']))return invalid()
    const start=value.start
    if(start.purpose!=='connection-diagnostic'||start.ownerId!==target.connectionId||start.requestId!==requestId||start.admissionId!==`diagnostic-${requestId}`||!digest(start.revision))return invalid()
    return start as unknown as RunStart
  }
  /** Uncertain admission is read once; this method never creates a replacement run. */
  async diagnosticLookup(start:RunStart):Promise<RunSnapshot|undefined>{
    if(start.purpose!=='connection-diagnostic'||!runOwner(start.purpose,start.ownerId)||!digest(start.revision)||!/^[A-Za-z0-9-]{1,100}$/.test(start.requestId)||start.admissionId!==`diagnostic-${start.requestId}`)throw new RunRequestError('not-sent',undefined,'invalid-request')
    const value=await this.briefJSON(`/api/assistant-diagnostics/requests/${start.requestId}`,undefined)
    if(!object(value)||!exact(value,['run']))return invalid()
    if(value.run===undefined)return undefined
    const run=decodeSnapshot(value.run,{owner:{kind:'connection-diagnostic',id:start.ownerId,revision:start.revision},requestId:start.requestId})
    if(run.admissionId!==start.admissionId)return invalid()
    return run
  }
  async diagnosticResult(run:RunSnapshot,signal?:AbortSignal):Promise<boolean>{
    if(run.owner.kind!=='connection-diagnostic'||run.state!=='complete'||run.historical)throw new RunRequestError('not-sent',undefined,'permission')
    const value=await this.briefJSON(`${ROOT}/${bind(run).id}/connection-diagnostic`,undefined,signal)
    if(!object(value)||!exact(value,['succeeded'])||typeof value.succeeded!=='boolean')return invalid()
    return value.succeeded
  }
  /** A model-free exact reservation lookup. A missing run never triggers start. */
  async briefReference(subject:string,token:string,signal?:AbortSignal):Promise<{start:RunStart;run?:RunSnapshot}> {
    if(!briefSubject(subject)||!text(token,128))throw new RunRequestError('not-sent',undefined,'invalid-request')
    const value=await this.briefJSON('/api/assistant-briefs/reference',{subject,token},signal)
    if(!object(value)||!exact(value,['start','run'])||!object(value.start)||!exact(value.start,['admissionId','purpose','ownerId','revision','requestId']))return invalid()
    const start=value.start
    if(start.purpose!=='brief'||start.ownerId!==subject||start.requestId!==token||start.admissionId!==`brief-${token}`||!digest(start.revision))return invalid()
    const run=value.run===undefined?undefined:decodeSnapshot(value.run,{owner:{kind:'brief',id:subject,revision:start.revision},requestId:token})
    if(run&&run.admissionId!==start.admissionId)return invalid()
    return {start:start as unknown as RunStart,...(run?{run}:{})}
  }
  /** Explicit closed authoring reservation. No capture/lookup can dispatch a model. */
  async captureAuthoring(request: import('../useAuthoringRun').AuthoringRequest, target:{connectionId:string;connectionRevision:string}, signal?:AbortSignal):Promise<AuthoringReference> {
    const value=await this.briefJSON('/api/assistant-authoring/captures',{...request},signal,{'X-Assistant-Connection':target.connectionId,'X-Assistant-Revision':target.connectionRevision})
    return authoringReference(value,request.requestId,request.purpose)
  }
  async authoringReference(requestId:string,purpose:AuthoringPurpose,signal?:AbortSignal):Promise<AuthoringReference> {
    if(!/^[A-Za-z0-9_-]{1,100}$/.test(requestId))throw new RunRequestError('not-sent',undefined,'invalid-request')
    return authoringReference(await this.briefJSON(`/api/assistant-authoring/captures/${requestId}`,undefined,signal),requestId,purpose)
  }
  async cancelAuthoring(start:RunStart,signal?:AbortSignal):Promise<AuthoringReference> {
    if(!authoringPurpose(start.purpose)||!runOwner(start.purpose,start.ownerId)||!digest(start.revision)||!/^[A-Za-z0-9_-]{1,100}$/.test(start.requestId))throw new RunRequestError('not-sent',undefined,'invalid-request')
    return authoringReference(await this.briefJSON(`/api/assistant-authoring/captures/${start.requestId}/cancel`,{ownerId:start.ownerId,revision:start.revision},signal),start.requestId,start.purpose)
  }
  commitBrief(snapshot:RunSnapshot,signal?:AbortSignal):Promise<unknown> {
    if(snapshot.owner.kind!=='brief'||snapshot.state!=='complete'||snapshot.historical)throw new RunRequestError('not-sent',undefined,'permission')
    return this.briefJSON(`${ROOT}/${bind(snapshot).id}/brief`,{token:snapshot.requestId},signal)
  }
  briefReceipt(snapshot:RunSnapshot,signal?:AbortSignal):Promise<unknown> {
    if(snapshot.owner.kind!=='brief'||snapshot.state!=='complete'||snapshot.historical)throw new RunRequestError('not-sent',undefined,'permission')
    return this.briefJSON(`${ROOT}/${bind(snapshot).id}/brief`,undefined,signal)
  }
  private async briefJSON(path:string,body:Record<string,unknown>|undefined,signal?:AbortSignal,headers:Record<string,string>={}):Promise<unknown> {
    const scope=await this.scope(signal);let answer:Response|undefined
    try {
      answer=await deskFetch(path,{method:body?'POST':'GET',redirect:'error',signal:scope.signal,headers:{...headers,...(body?{'Content-Type':'application/json'}:{})},...(body?{body:JSON.stringify(body)}:{})})
      await this.current(scope.signal)
      if(!answer.ok){await discardBody(answer);throw new RunRequestError(answer.status>=400&&answer.status<500?'refused':'uncertain',answer.status,statusCode(answer.status))}
      const value=await readJSON(answer,scope.signal);await this.current(scope.signal);return value
    } catch(cause){if(cause instanceof RunRequestError)throw cause;throw new RunRequestError('uncertain',undefined,cause instanceof AIFailureError?'invalid-response':'connection-lost')}
    finally{if(answer)await discardBody(answer);scope.close()}
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
  /** User-owned, model-free recheck. References never authorize a write or model request. */
  async recheckPack(ref: RunReference, request: {id:string;documentDigest:string;suiteDigest:string}, signal?: AbortSignal): Promise<PackProposalCheck> {
    const bound=bind(ref)
    if (!object(request)||!exact(request,['id','documentDigest','suiteDigest'])||!text(request.id,128)||!digest(request.documentDigest)||!digest(request.suiteDigest)) throw new RunRequestError('not-sent',undefined,'invalid-request')
    const scope=await this.scope(signal)
    let sent=false,answer:Response|undefined
    try {
      sent=true
      answer=await deskFetch(`${ROOT}/${bound.id}/pack-checks`,{method:'POST',redirect:'error',signal:scope.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({id:request.id,documentDigest:request.documentDigest,suiteDigest:request.suiteDigest})})
      await this.current(scope.signal)
      if(!answer.ok){await discardBody(answer);throw new RunRequestError(answer.status>=400&&answer.status<500&&answer.status!==408?'refused':'uncertain',answer.status,statusCode(answer.status))}
      const check=readPackCheck(await readJSON(answer,scope.signal))
      if(!check||check.documentDigest!==request.documentDigest||await digestOf(check.suiteKey)!==request.suiteDigest) throw new RunRequestError('uncertain',undefined,'invalid-response')
      await this.current(scope.signal)
      return check
    } catch(cause){if(cause instanceof RunRequestError)throw cause;throw new RunRequestError(sent?'uncertain':'not-sent',undefined,'connection-lost')}
    finally{if(answer)await discardBody(answer);scope.close()}
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
    observations.beginStream()
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
    finally { observations.endStream(); if (answer) await discardBody(answer); scope.close() }
  }
}
function statusCode(status: number): FailureCode { return status === 409 ? 'stale-state' : status === 400 ? 'invalid-request' : status === 401 ? 'authentication' : status === 403 ? 'permission' : status === 429 ? 'rate-limited' : 'unavailable' }
function validateReference(ref: RunReference): void {
  if (!object(ref) || !object(ref.owner) || !runID(ref.id) || !runOwner(ref.owner.kind,ref.owner.id) || !digest(ref.owner.revision) || !text(ref.requestId,128)) throw new RunRequestError('not-sent',undefined,'invalid-response')
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

export type AuthoringPurpose='author-pack'|'describe-pack'|'author-graph'
export interface AuthoringReference {readonly start:RunStart;readonly run?:RunSnapshot;readonly cancelled?:boolean}
export function authoringPurpose(value:unknown):value is AuthoringPurpose{return value==='author-pack'||value==='describe-pack'||value==='author-graph'}
function authoringReference(value:unknown,requestId:string,purpose:AuthoringPurpose):AuthoringReference {
 if(!object(value)||!exact(value,['start','run','cancelled'])||!object(value.start)||!exact(value.start,['admissionId','purpose','ownerId','revision','requestId']))return invalid()
 const start=value.start
 if(start.purpose!==purpose||!authoringPurpose(start.purpose)||!runOwner(start.purpose,start.ownerId)||!digest(start.revision)||start.requestId!==requestId||typeof start.admissionId!=='string'||!/^author-admit-[a-f0-9]{32}$/.test(start.admissionId)||value.cancelled!==undefined&&typeof value.cancelled!=='boolean')return invalid()
 const run=value.run===undefined?undefined:decodeSnapshot(value.run,{owner:{kind:purpose,id:String(start.ownerId),revision:start.revision},requestId})
 if(run&&run.admissionId!==start.admissionId)return invalid()
 return {start:start as unknown as RunStart,...(run?{run}:{}),...(value.cancelled===true?{cancelled:true}:{})}
}

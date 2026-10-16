import {answer,deskFetch} from '../files/client'
import {canonicalize,parseJSON} from './canon'

/** Runtime's offline check, never proof of source binding or permission to run. */
export interface DialectAuthoringReport {
 status:string
 dialectVersion?:number
 formatVersion?:string
 artifactID?:string
 artifactVersion?:string
 document?:string
 digest?:string
 diagnostics:{code:string;instancePath?:string;message:string}[]
 compatibility?:{scope:string;status:string;pack:{sha256:string};valueTypesChecked:boolean;executionChecked:boolean;notes:{code:string;kind:string;target:string;severity:string}[]}
}
export class DialectCheckError extends Error {
 constructor(readonly report:DialectAuthoringReport){
  const findings=report.diagnostics.map(d=>[d.code,d.instancePath,d.message].filter(Boolean).join(' · '))
  const coverage=report.compatibility?.notes.filter(n=>n.severity==='error').map(n=>`${n.code} · ${n.target}`)??[]
  super([...findings,...coverage].slice(0,8).join('\n')||'Runtime dialect checks did not pass.')
  this.name='DialectCheckError'
 }
}
/** Called only on an explicit check/proposal/save path; no polling or retries.
 * The canonical writer previews bytes; Runtime remains the validation authority.
 */
export async function checkDialect(document:unknown,pack?:string,signal?:AbortSignal):Promise<DialectAuthoringReport>{
 const text=typeof document==='string'?document:canonicalize(document)
 signal?.throwIfAborted()
 const report=await answer<DialectAuthoringReport>(await deskFetch('/api/dialect-authoring/check',{
  method:'POST',signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({document:text,...(pack===undefined?{}:{pack})})
 }))
 signal?.throwIfAborted()
 const accepted=pack===undefined?report.status==='document-valid':!!report.compatibility&&['complete','review-required'].includes(report.status)
 if(!accepted||!report.document||!report.digest)throw new DialectCheckError(report)
 if(canonicalize(parseJSON(report.document))!==canonicalize(parseJSON(text)))throw Error('Runtime returned a different dialect document.')
 return report
}

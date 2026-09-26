import { answer, deskFetch } from '../files/client'
import type { DocumentReference, VerifiedDocument } from './client'
import type { ChatAttachment } from '../chat/store'
import type { PackDraft } from '../packs/drafts/model'
import type { TestStore } from '../packs/test-workspace/model'
export interface SourceReview { id:string;name:string;before:DocumentReference;after:DocumentReference;checkedAt:string;reviewedAt?:string }
interface Store { version:1;reviews:SourceReview[] }
interface Reply { project:string;sha256:string;content:Store }
const uuid=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(v)
function validRef(ref:DocumentReference) { return ref && uuid(ref.id) && /^sha256:[a-f0-9]{64}$/.test(ref.digest) && typeof ref.allowPartial==='boolean' && Array.isArray(ref.pages) && ref.pages.length<=500 && ref.pages.every(n=>Number.isSafeInteger(n)&&n>0) && new Set(ref.pages).size===ref.pages.length }
export function decodeReviews(value:Store):Store {
 if(value?.version!==1||!Array.isArray(value.reviews)||value.reviews.length>2048||new Set(value.reviews.map(r=>r?.id)).size!==value.reviews.length||value.reviews.some(r=>!r||!uuid(r.id)||typeof r.name!=='string'||!r.name||r.name.length>2048||!validRef(r.before)||!validRef(r.after)||r.before.id===r.after.id||!Number.isFinite(Date.parse(r.checkedAt))||(r.reviewedAt!==undefined&&!Number.isFinite(Date.parse(r.reviewedAt)))))throw Error('Source review history could not be read. Nothing was changed.')
 return value
}
export async function readReviews(signal?:AbortSignal):Promise<Reply> {
 const reply=await answer<Reply>(await deskFetch('/api/source-reviews',{signal}));return {...reply,content:decodeReviews(reply.content)}
}
export async function saveReview(review:SourceReview, project:string, signal?:AbortSignal) {
 const prior=await readReviews(signal)
 if(prior.project!==project)throw Error('The project changed. Reopen the source before saving.')
 const existing=prior.content.reviews.find(r=>r.id===review.id)
 if(existing&&(JSON.stringify(existing.before)!==JSON.stringify(review.before)||JSON.stringify(existing.after)!==JSON.stringify(review.after)||existing.checkedAt!==review.checkedAt))throw Error('The review changed. Reopen it before saving.')
 const content=decodeReviews({version:1,reviews:[...prior.content.reviews.filter(r=>r.id!==review.id),{...review,reviewedAt:existing?.reviewedAt??review.reviewedAt}]})
 return answer<Reply>(await deskFetch('/api/source-reviews',{method:'PUT',headers:{'Content-Type':'application/json','If-Match':prior.sha256},body:JSON.stringify(content),signal}))
}
export function sameSource(before:VerifiedDocument,after:VerifiedDocument) {
 const a=before.record.provenance.source,b=after.record.provenance.source
 if(a.kind!==b.kind)return false
 switch(a.kind){
  case 'web':return a.requestedUrl===b.requestedUrl
  case 'google-drive':return a.fileId===b.fileId
  case 'gmail':return a.messageId===b.messageId
  case 'connected-source':case 'connection-resource':return a.provider===b.provider&&a.resourceId===b.resourceId
  case 'inline':return true // explicitly selected replacement file
 }
}
export function sourceChanges(before:VerifiedDocument,after:VerifiedDocument) {
 const pages=(d:VerifiedDocument)=>d.record.content.pages
 const changed=[...new Set([...pages(before),...pages(after)].map(p=>p.number))].filter(n=>{
  const a=pages(before).find(p=>p.number===n),b=pages(after).find(p=>p.number===n)
  return a?.text!==b?.text||a?.status!==b?.status
 })
 return {bytesChanged:before.object.original.sha256!==after.object.original.sha256,changedPages:changed,extractionChanged:JSON.stringify(before.record.content)!==JSON.stringify(after.record.content)}
}
export function sourceImpacts(reference:DocumentReference,tests:TestStore,drafts:readonly PackDraft[]) {
 const matches=(s:ChatAttachment)=>s.document?.id===reference.id&&s.document.digest===reference.digest
 return [
  ...Object.entries(tests.suites).flatMap(([owner,suite])=>suite.cases.filter(c=>c.sources.some(matches)).map(c=>({id:`${owner}:${c.id}`,title:c.name,detail:owner,href:drafts.some(d=>d.id===owner)?`/packs/drafts/${encodeURIComponent(owner)}`:`/packs/${encodeURIComponent(owner)}/evaluate`}))),
  ...drafts.filter(d=>d.documents.some(matches)).map(d=>({id:d.id,title:d.title,detail:d.finalized?'Saved pack sources':'Draft sources',href:d.finalized?`/packs/${encodeURIComponent(d.finalized.id)}`:`/packs/drafts/${encodeURIComponent(d.id)}`}))
 ]
}

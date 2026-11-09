import {MEMORY_KEY,memoryRequest as transport,type MemoryEntry} from '@judgment-pack/harness/memory/client'
import {recallMemory,type Memory} from '@judgment-pack/harness/memories'
import {sha256} from '@judgment-pack/harness/records'
import {object,exact,digest,runID,revision,invalid,text} from '../serverRuns/decode'
export {MEMORY_KEY}
export type {MemoryEntry}
export interface MemoryProposal {runId:string;runRevision:number;chatId:string;turnId:string;memory:Memory;reviewable:boolean}
export interface MemoryReviewChange {id:string;runId:string;runRevision:number;proposalId:string;proposalDigest:string;decision:'accept'|'decline'|'edit';text?:string}
export interface MemoryReviewReceipt {id:string;intentDigest:string;principal:string;session:string;policy:string;project:string;runId:string;runRevision:number;proposalId:string;proposalDigest:string;decision:'accept'|'decline'|'edit';entryId?:string;memoryId?:string;memoryDigest?:string;at:string}
export interface MemoryReply {sha256:string;content:{version:1;settings:{use:boolean;suggest:boolean};entries:MemoryEntry[];blocked?:string[];generationAfter?:string;reviews?:MemoryReviewReceipt[]};proposals?:MemoryProposal[]}
const memoryDigest=(value:unknown):value is string=>typeof value==='string'&&/^sha256:[a-f0-9]{64}$/.test(value)
const date=(value:unknown):value is string=>typeof value==='string'&&Number.isFinite(Date.parse(value))
export function readMemoryReceipt(value:unknown):MemoryReviewReceipt {
 if(!object(value)||!exact(value,['id','intentDigest','principal','session','policy','project','runId','runRevision','proposalId','proposalDigest','decision','entryId','memoryId','memoryDigest','at'])||!text(value.id,128)||!runID(value.runId)||!revision(value.runRevision)||!text(value.proposalId,100)||!memoryDigest(value.proposalDigest)||!date(value.at)||!['accept','decline','edit'].includes(String(value.decision)))return invalid()
 for(const key of ['intentDigest','principal','session','policy','project'])if(!digest(value[key]))return invalid()
 if(value.decision==='decline'){if(value.entryId!==undefined||value.memoryId!==undefined||value.memoryDigest!==undefined)return invalid()}
 else if(!text(value.entryId,128)||!text(value.memoryId,100)||!memoryDigest(value.memoryDigest))return invalid()
 if(value.decision==='accept'&&(value.memoryId!==value.proposalId||value.memoryDigest!==value.proposalDigest)||value.decision==='edit'&&value.memoryId===value.proposalId)return invalid()
 return value as unknown as MemoryReviewReceipt
}
/** Server-owned private format. Canonical record validation stays in Agent. */
export async function readMemoryReply(value:unknown):Promise<MemoryReply> {
 if(!object(value)||!exact(value,['sha256','content','proposals'])||value.sha256!=='absent'&&!digest(value.sha256)||!object(value.content))return invalid()
 const d=value.content
 if(!exact(d,['version','settings','entries','blocked','generationAfter','reviews'])||d.version!==1||!object(d.settings)||!exact(d.settings,['use','suggest'])||typeof d.settings.use!=='boolean'||typeof d.settings.suggest!=='boolean'||!Array.isArray(d.entries)||d.entries.length>256||d.blocked!==undefined&&(!Array.isArray(d.blocked)||d.blocked.length>4096||d.blocked.some(v=>typeof v!=='string'))||d.generationAfter!==undefined&&!date(d.generationAfter))return invalid()
 const ids=new Set<string>(),canonical=new Set<string>()
 for(const e of d.entries){
  if(!object(e)||!exact(e,['id','key','text','state','createdAt','updatedAt','source','sourceDigest','dependencies','stale','canonical'])||!text(e.id,128)||ids.has(e.id)||typeof e.key!=='string'||!/^[a-z0-9][a-z0-9-]{0,79}$/.test(e.key)||typeof e.text!=='string'||!e.text.trim()||new TextEncoder().encode(e.text).length>2000||!['active','pending','superseded'].includes(String(e.state))||typeof e.createdAt!=='string'||typeof e.updatedAt!=='string'||e.stale!==undefined&&typeof e.stale!=='boolean')return invalid()
  ids.add(e.id)
  if(e.source!==undefined&&e.source!==null&&(!object(e.source)||!exact(e.source,['chatId','turnId'])||typeof e.source.chatId!=='string'||typeof e.source.turnId!=='string'))return invalid()
  if(e.sourceDigest!==undefined&&typeof e.sourceDigest!=='string')return invalid()
  if(e.dependencies!==undefined&&e.dependencies!==null&&(!Array.isArray(e.dependencies)||e.dependencies.length>8||e.dependencies.some(dep=>!object(dep)||!exact(dep,['path','sha256'])||typeof dep.path!=='string'||typeof dep.sha256!=='string')))return invalid()
  if(e.canonical!==undefined){
   const m=e.canonical
   if(!object(m)||!exact(m,['id','kind','source','digest','expiresAt'])||typeof m.id!=='string'||canonical.has(m.id))return invalid()
   canonical.add(m.id)
   try{await recallMemory([{...m,text:e.text,scope:'project',createdAt:e.createdAt,state:'accepted'}],Number.MAX_SAFE_INTEGER,'')}catch{return invalid()}
  }
 }
 if(d.reviews!==undefined){
  if(!Array.isArray(d.reviews)||d.reviews.length>512)return invalid()
  const ids=new Set<string>(),proposals=new Set<string>()
  for(const raw of d.reviews){const r=readMemoryReceipt(raw),key=r.runId+'/'+r.proposalId;if(ids.has(r.id)||proposals.has(key))return invalid();ids.add(r.id);proposals.add(key)}
 }
 if(value.proposals!==undefined){
  if(!Array.isArray(value.proposals)||value.proposals.length>128*256)return invalid()
  const ids=new Set<string>()
  for(const p of value.proposals){
   if(!object(p)||!exact(p,['runId','runRevision','chatId','turnId','memory','reviewable'])||!runID(p.runId)||!revision(p.runRevision)||!text(p.chatId,160)||!text(p.turnId,100)||typeof p.reviewable!=='boolean'||!object(p.memory)||p.memory.state!=='proposed'||p.memory.scope!=='project'||!object(p.memory.source)||p.memory.source.proposedBy!=='model')return invalid()
   try{await recallMemory([p.memory],0,'')}catch{return invalid()}
   const key=p.runId+'/'+p.memory.id;if(ids.has(key))return invalid();ids.add(key)
  }
 }
 return value as unknown as MemoryReply
}
export async function memoryRequest(action?:Record<string,unknown>,revision?:string):Promise<MemoryReply>{
 const reply=await readMemoryReply(await transport(action,revision))
 if(action?.action==='review'){
  const change=action.review
  if(!object(change))return invalid()
  const receipt=reply.content.reviews?.find(r=>r.id===change.id)
  if(!receipt||receipt.runId!==change.runId||receipt.runRevision!==change.runRevision||receipt.proposalId!==change.proposalId||receipt.proposalDigest!==change.proposalDigest||receipt.decision!==change.decision)return invalid()
  if(change.decision==='edit'&&(typeof change.text!=='string'||receipt.memoryDigest!=='sha256:'+sha256(change.text)))return invalid()
 }
 return reply
}

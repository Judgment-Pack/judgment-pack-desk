import {validInterpretationTime} from '../policies/interpretation'
import type { GraphProposal } from './author'
import {validPolicySources,type PolicyAuthoringSource} from '../policies/authoringSource'
import {queryOptions} from '@tanstack/react-query'
import {answer,deskFetch} from '../files/client'
import {privateQuery} from '../data/privateQuery'
import {sourceMessage} from '../i18n/source'

/** Independent private artifact. Chat copies are related-work history. */
export interface GraphDraft extends GraphProposal {
  draftId: string
  createdAt: string
  saved?: boolean
  writePending?: boolean
  originChatId?: string
  policySources?: PolicyAuthoringSource[]
}
export function validGraphDraft(value: unknown): value is GraphDraft {
  if (!value || typeof value !== 'object') return false
  const draft = value as GraphDraft
  return typeof draft.draftId === 'string' && /^[a-zA-Z0-9-]{1,80}$/.test(draft.draftId)
    && typeof draft.id === 'string' && /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(draft.id) && draft.id.length <= 128
    && typeof draft.path === 'string' && new TextEncoder().encode(draft.path).length >=1 && new TextEncoder().encode(draft.path).length <= 1024
    && typeof draft.content === 'string' && new TextEncoder().encode(draft.content).length <= 1024 * 1024
    && validInterpretationTime(draft.createdAt)
    && (draft.description === undefined || typeof draft.description === 'string' && new TextEncoder().encode(draft.description).length <= 4096)
    && (draft.baseSha256 === undefined || typeof draft.baseSha256==='string' && /^[a-f0-9]{64}$/.test(draft.baseSha256))
    && (draft.saved === undefined || typeof draft.saved === 'boolean')
    && (draft.writePending===undefined || typeof draft.writePending==='boolean') && !(draft.saved&&draft.writePending)
    && (draft.originChatId === undefined || typeof draft.originChatId==='string' && /^[a-zA-Z0-9-]{1,80}$/.test(draft.originChatId))
    && (draft.policySources === undefined || validPolicySources(draft.policySources))
}
export function graphDraftHref(chatId: string, draftId: string) {
  return `/graphs?chat=${encodeURIComponent(chatId)}&draft=${encodeURIComponent(draftId)}`
}
export const validGraphSnapshot=(value:unknown):value is GraphDraft=>validGraphDraft(value)&&!Object.keys(value).some(key=>['policysources','originchatid','writepending'].includes(key.toLowerCase()))
export const GRAPH_DRAFTS_KEY=['graph-drafts'] as const
export interface GraphDraftDocument {version:1;drafts:GraphDraft[];deleted:string[]}
export interface GraphDraftReply {project:string;sha256:string;content:GraphDraftDocument}
export const graphDraftKey=(draft:Pick<GraphDraft,'originChatId'|'draftId'>)=>(draft.originChatId??'')+':'+draft.draftId
export function decodeGraphDrafts(value:unknown):GraphDraftDocument {
  const doc=value as GraphDraftDocument|null,invalid=()=>{throw Error(sourceMessage('Saved graph drafts could not be read. The saved file has not been changed.'))}
  if(!doc||Object.keys(doc).some(k=>!['version','drafts','deleted'].includes(k))||doc.version!==1||!Array.isArray(doc.drafts)||doc.drafts.length>256||!Array.isArray(doc.deleted)||doc.deleted.length>4096||doc.deleted.some(id=>typeof id!=='string'||!/^[a-zA-Z0-9-]{0,80}:[a-zA-Z0-9-]{1,80}$/.test(id)))return invalid()
  const seen=new Set(doc.deleted)
  if(seen.size!==doc.deleted.length)return invalid()
  for(const draft of doc.drafts){if(!validGraphDraft(draft)||!draft.originChatId||Object.keys(draft).some(k=>!['draftId','id','path','content','description','baseSha256','createdAt','saved','writePending','originChatId','policySources'].includes(k))||seen.has(graphDraftKey(draft)))return invalid();seen.add(graphDraftKey(draft))}
  return doc
}
export async function readGraphDrafts(signal?:AbortSignal):Promise<GraphDraftReply>{const reply=await answer<GraphDraftReply>(await deskFetch('/api/draft-graphs',{signal}));return {...reply,content:decodeGraphDrafts(reply.content)}}
export const graphDraftsQuery=()=>queryOptions({...privateQuery,queryKey:GRAPH_DRAFTS_KEY,queryFn:({signal})=>readGraphDrafts(signal)})
async function writeGraphDrafts(before:GraphDraftReply,content:GraphDraftDocument,signal?:AbortSignal){
  const reply=await answer<GraphDraftReply>(await deskFetch('/api/draft-graphs',{method:'PUT',signal,headers:{'Content-Type':'application/json','If-Match':before.sha256},body:JSON.stringify(content)}))
  return {...reply,content:decodeGraphDrafts(reply.content)}
}
/** Compare the selected frozen entry, preserve unrelated entries, and never replay a write. */
export function sameGraphDraft(a:GraphDraft|undefined,b:GraphDraft|undefined):boolean{
  const normalized=(d:GraphDraft|undefined)=>d?JSON.stringify([d.originChatId??'',d.draftId,d.id,d.path,d.content,d.description??'',d.baseSha256??'',d.createdAt,!!d.saved,!!d.writePending,d.policySources??[]]):undefined
  return normalized(a)===normalized(b)
}
export async function retainGraphDraft(draft:GraphDraft,previous?:GraphDraft,signal?:AbortSignal):Promise<GraphDraftReply>{
  const before=await readGraphDrafts(signal),key=graphDraftKey(draft),held=before.content.drafts.find(d=>graphDraftKey(d)===key)
  if(before.content.deleted.includes(key))throw Error(sourceMessage('This graph draft was deleted.'))
  const equal=sameGraphDraft
  if(held&&equal(held,draft))return before
  if(!equal(held,previous))throw Error(sourceMessage('The graph draft changed. Reopen it before saving.'))
  return writeGraphDrafts(before,{...before.content,drafts:[...before.content.drafts.filter(d=>graphDraftKey(d)!==key),draft]},signal)
}
export async function deleteGraphDrafts(items:{originChatId:string;draftId:string}[],signal?:AbortSignal){
  const before=await readGraphDrafts(signal),keys=new Set(items.map(graphDraftKey))
  if(before.content.drafts.some(d=>keys.has(graphDraftKey(d))&&(d.saved||d.writePending)))throw Error(sourceMessage('Saved or pending graph history cannot be deleted as a draft.'))
  return writeGraphDrafts(before,{...before.content,drafts:before.content.drafts.filter(d=>!keys.has(graphDraftKey(d))),deleted:[...new Set([...before.content.deleted,...keys])]},signal)
}
/** Legacy rows remain visible until their next authenticated conversation save. */
export function retainedGraphDrafts(doc:GraphDraftDocument|undefined,chats:readonly {id:string;graphDrafts?:GraphDraft[];policySources?:PolicyAuthoringSource[]}[]):GraphDraft[]{
  const entries=new Map<string,GraphDraft>(),deleted=new Set(doc?.deleted??[])
  for(const chat of chats)for(const d of chat.graphDrafts??[]){const {policySources:_nested,originChatId:_owner,writePending:_pending,...proposal}=d;const draft={...proposal,originChatId:chat.id,...(chat.policySources?.length?{policySources:chat.policySources}:{})};if(!deleted.has(graphDraftKey(draft)))entries.set(graphDraftKey(draft),draft)}
  for(const d of doc?.drafts??[])entries.set(graphDraftKey(d),d)
  return [...entries.values()]
}

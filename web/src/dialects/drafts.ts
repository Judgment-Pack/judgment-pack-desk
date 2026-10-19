import {queryOptions} from '@tanstack/react-query'
import {answer,deskFetch} from '../files/client'
import {privateQuery} from '../data/privateQuery'
import type {DialectRecipe} from './client'
import {recipeFromProposal,type DialectRecipeProposal} from './proposals'

export const DIALECT_DRAFTS_KEY=['dialect-drafts']
export interface DialectDraft {version:1;id:string;name:string;recipe:DialectRecipe;origin?:{chatId:string;draftId:string};createdAt:string;updatedAt:string;savedInputId?:string;discarded?:boolean}
export interface DialectDraftReply {sha256:string;document:DialectDraft}
export interface DialectDraftValues {name:string;recipe:DialectRecipe}
export const dialectDraftAPI=async<T,>(path='',method='GET',body?:unknown,revision?:string,signal?:AbortSignal)=>answer<T>(await deskFetch('/api/dialect-drafts'+path,{method,signal,headers:{'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body===undefined?{}:{body:JSON.stringify(body)})}))
export const dialectDraftQuery=(id:string)=>queryOptions({queryKey:[...DIALECT_DRAFTS_KEY,id],queryFn:({signal})=>dialectDraftAPI<DialectDraftReply>('/'+id,'GET',undefined,undefined,signal),...privateQuery})
export const dialectDraftListQuery=()=>queryOptions({queryKey:DIALECT_DRAFTS_KEY,queryFn:({signal})=>dialectDraftAPI<{items:Pick<DialectDraft,'id'|'name'|'updatedAt'>[]}>('','GET',undefined,undefined,signal),...privateQuery,refetchInterval:5000})
/** Durable before the tool reports success; the chat contains only its reference. */
export const retainDialectProposal=(chatId:string,draft:DialectRecipeProposal)=>dialectDraftAPI<DialectDraftReply>('','POST',{origin:{chatId,draftId:draft.draftId},name:draft.name,recipe:{intent:draft.intent,research:recipeFromProposal(draft)}})
export function dialectDraftHref(draft:DialectDraft){const chat=draft.origin?.chatId;return draft.savedInputId?'/dialects/'+draft.savedInputId+'?tab=recipe'+(chat?'&chat='+encodeURIComponent(chat):''):'/dialects/drafts/'+draft.id+(chat?'?chat='+encodeURIComponent(chat):'')}

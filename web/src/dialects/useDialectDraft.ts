import {useCallback,useEffect,useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {DIALECT_DRAFTS_KEY,dialectDraftAPI,type DialectDraftReply,type DialectDraftValues} from './drafts'
const values=(reply:DialectDraftReply):DialectDraftValues=>({name:reply.document.name,recipe:reply.document.recipe})
const same=(a:DialectDraftValues,b:DialectDraftValues)=>JSON.stringify(a)===JSON.stringify(b)
/** Serialize autosaves against their exact base revision. A late response never
 * replaces newer edits. Conflicts keep the buffer until an explicit reload. */
export function useDialectDraft(initial:DialectDraftReply){
 const cache=useQueryClient(),[value,setValue]=useState(()=>values(initial)),[saved,setSaved]=useState(initial),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>()
 const latest=useRef(value),base=useRef(initial),flight=useRef<Promise<DialectDraftReply>|undefined>(undefined),mounted=useRef(true)
 latest.current=value
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 const persist=useCallback(async():Promise<DialectDraftReply>=>{
  if(flight.current){await flight.current;return persist()}
  if(same(latest.current,values(base.current)))return base.current
  const pending=structuredClone(latest.current)
  setBusy(true);setError(undefined)
  const request=dialectDraftAPI<DialectDraftReply>('/'+base.current.document.id,'PUT',pending,base.current.sha256)
  flight.current=request
  try{
   const next=await request;base.current=next
   // A successful write acknowledges this exact buffer. Adopt the server's
   // representation only if nothing newer was typed while it was in flight;
   // JSON member order/default normalization must not trigger another write.
   if(same(latest.current,pending)){latest.current=values(next);if(mounted.current)setValue(latest.current)}
   cache.setQueryData([...DIALECT_DRAFTS_KEY,next.document.id],next);void cache.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY,exact:true});if(mounted.current)setSaved(next);return next
  }
  catch(e){if(mounted.current)setError(e);throw e}
  finally{flight.current=undefined;if(mounted.current)setBusy(false)}
 },[cache])
 const flush=useCallback(async()=>{do{await persist()}while(!same(latest.current,values(base.current)));return base.current},[persist])
 const dirty=!same(value,values(saved))
 useEffect(()=>{if(!dirty||busy||error)return;const timer=setTimeout(()=>{void persist().catch(()=>{})},500);return()=>clearTimeout(timer)},[value,dirty,busy,error,persist])
 useEffect(()=>{if(!flight.current&&same(latest.current,values(base.current))&&initial.sha256!==base.current.sha256){base.current=initial;latest.current=values(initial);setValue(latest.current);setSaved(initial);setError(undefined)}},[initial.sha256])
 const clear=useRegisteredChanges(dirty,msg('Some draft changes have not been saved yet.'),{kind:'autosave',saveFailed:!!error,busy,saveDraft:async()=>{await flush()}})
 const reload=useCallback(async()=>{const next=await dialectDraftAPI<DialectDraftReply>('/'+base.current.document.id);base.current=next;latest.current=values(next);setValue(latest.current);setSaved(next);setError(undefined);cache.setQueryData([...DIALECT_DRAFTS_KEY,next.document.id],next)},[cache])
 return {value,setValue,saved,busy,error,dirty,flush,reload,clear,current:()=>base.current,settled:()=>flight.current?.catch(()=>{})}
}

import {useRef,useState} from 'react'
import {useNavigate} from 'react-router-dom'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {dialectChatHref} from '../chat/navigation'
import {ChatAction} from '../chat/ChatActions'
import {ArtifactReference} from '../chat/ArtifactReference'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {DIALECT_DRAFTS_KEY,dialectDraftHref,retainDialectProposal} from './drafts'
import type {DialectRecipeProposal} from './proposals'


/** Main chat, the pane and Related work all open the independent draft page. */
export function RecipeReference({chatId,draft,disabled=false,presentation='card'}:{chatId:string;draft:DialectRecipeProposal;disabled?:boolean;presentation?:'card'|'action'}) {
 const navigate=useNavigate(),cache=useQueryClient(),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>(),active=useRef(false)
 const open=async()=>{if(active.current)return;active.current=true;setBusy(true);setError(undefined);try{
  const saved=await retainDialectProposal(chatId,draft)
  cache.setQueryData([...DIALECT_DRAFTS_KEY,saved.document.id],saved);void cache.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY,exact:true})
  navigate(dialectDraftHref(saved.document))
 }catch(e){setError(e)}finally{active.current=false;setBusy(false)}}
 return <>
  <QueryStatusNotice id={'dialect-draft:'+chatId+':'+draft.draftId} label={msg('Dialect draft unavailable')} error={error} retrying={busy} onRetry={open}/>
  {presentation==='action'&&!draft.savedInputId?<ChatAction disabled={disabled||busy} onClick={()=>void open()}>{busy?msg('Opening…'):msg('Review recipe')}</ChatAction>:<ArtifactReference compact title={draft.name} description={<>{draft.savedInputId?msg('Saved to Dialects'):msg('Dialect draft')} · {msg('Records')}</>}
   action={draft.savedInputId?msg('Open dialect'):busy?msg('Opening…'):msg('Review recipe')} href={draft.savedInputId?dialectChatHref(draft.savedInputId,chatId,{pathname:'/dialects/'+draft.savedInputId,search:'?tab=recipe'}):undefined} disabled={disabled||busy} onOpen={()=>void open()}/>}
 </>
}
export function DialectRecipeNotice({chatId,drafts,disabled}:{chatId:string;drafts:DialectRecipeProposal[];disabled?:boolean}) {
 const draft=drafts.at(-1)
 if(draft?.savedInputId)return null
 return draft?<RecipeReference key={draft.draftId} chatId={chatId} draft={draft} disabled={disabled}/>:null
}

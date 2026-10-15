import {useMemo,useRef,useState,type RefObject} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {dialectChatHref} from '../chat/navigation'
import {ChatAction} from '../chat/ChatActions'
import {ArtifactReference} from '../chat/ArtifactReference'
import {useChats} from '../chat/ChatProvider'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Field} from '../ui/Field'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {ResearchRecipe} from './ResearchRecipe'
import {DialectPackEditor} from './DialectPackEditor'
import {dialectAPI,type DialectRecipe,type PreparationReply} from './client'
import {recipeFromProposal,type DialectRecipeProposal} from './proposals'
import styles from './DialectsView.module.css'

const targetLabel=(target:DialectRecipeProposal['target'])=>target==='records'?msg('Records'):target==='companies'?msg('Companies'):target==='people'?msg('People'):msg('People at selected companies')

export function RecipeReference({chatId,draft,disabled=false,presentation='card'}:{chatId:string;draft:DialectRecipeProposal;disabled?:boolean;presentation?:'card'|'action'}) {
 const [review,setReview]=useState(false),opener=useRef<HTMLElement|null>(null)
 return <>
  {presentation==='action'&&!draft.savedInputId?<ChatAction disabled={disabled} onClick={event=>{opener.current=event.currentTarget;setReview(true)}}>{msg('Review recipe')}</ChatAction>:<ArtifactReference compact title={draft.name} description={<>{draft.savedInputId?msg('Saved to Dialects'):msg('Proposed dialect recipe')} · {targetLabel(draft.target)}</>}
   action={draft.savedInputId?msg('Open dialect'):msg('Review recipe')} href={draft.savedInputId?dialectChatHref(draft.savedInputId,chatId,{pathname:'/dialects/'+draft.savedInputId,search:'?tab=recipe'}):undefined} disabled={disabled}
   onOpen={()=>{opener.current=document.activeElement as HTMLElement;setReview(true)}}/>}
  {review&&<DialectRecipeReview chatId={chatId} draft={draft} openerRef={opener} onClose={()=>setReview(false)}/>}
 </>
}

/** One shared artifact and review for main chat, the pane, and draft history. */
export function DialectRecipeNotice({chatId,drafts,disabled}:{chatId:string;drafts:DialectRecipeProposal[];disabled?:boolean}) {
 const draft=drafts.at(-1)
 // Saved work is available from the shared Related work control, in every placement.
 if(draft?.savedInputId)return null
 return draft?<RecipeReference key={draft.draftId} chatId={chatId} draft={draft} disabled={disabled}/>:null
}

export function DialectRecipeReview({chatId,draft,onClose,openerRef}:{chatId:string;draft:DialectRecipeProposal;onClose:()=>void;openerRef:RefObject<HTMLElement|null>}) {
 const initial=useMemo<DialectRecipe>(()=>({intent:draft.intent,research:recipeFromProposal(draft)}),[draft.draftId])
 const [name,setName]=useState(draft.name),[recipe,setRecipe]=useState(initial),[busy,setBusy]=useState(false),[pending,setPending]=useState(false),[error,setError]=useState('')
 const inFlight=useRef(false),{store}=useChats(),queries=useQueryClient(),confirm=useConfirmDiscard()
 const dirty=name!==draft.name||JSON.stringify(recipe)!==JSON.stringify(initial)
 const question=msg('Discard changes to this recipe review? The original proposal stays in chat.')
 const clearChanges=useRegisteredChanges(dirty,question,{busy})
 const close=async()=>{if(!inFlight.current&&!pending&&(!dirty||await confirm(question))){clearChanges();onClose()}}
 const save=async()=>{
  if(inFlight.current||pending||!store)return
  inFlight.current=true;setBusy(true);setError('')
  try {
   const live=store.getSnapshot().chats.find(chat=>chat.id===chatId)
   if(!live?.inputDrafts?.some(item=>item.draftId===draft.draftId))throw Error(msg('This chat proposal is no longer available.'))
   const saved=await dialectAPI<PreparationReply>('','POST',{name:name.trim(),recipe,chatOrigin:{chatId,draftId:draft.draftId}})
   const current=store.getSnapshot().chats.find(chat=>chat.id===chatId)
   if(current)store.update(chatId,{inputDrafts:current.inputDrafts?.map(item=>item.draftId===draft.draftId?{...item,savedInputId:saved.document.id}:item)})
   void queries.invalidateQueries({queryKey:['input-preparations']})
   clearChanges();onClose()
  }catch(e){setError(e instanceof Error?e.message:msg('The recipe could not be saved.'))}
  finally{inFlight.current=false;setBusy(false)}
 }
 const researchReady=!!recipe.research?.ai.connection&&!!recipe.research.ai.model&&!!recipe.research.ai.revision
 const companyReady=recipe.research?.target!=='people-at-companies'||(recipe.research.companyIds?.length??0)+(recipe.research.companyInputs?.length??0)>0
 return <Dialog open title={msg('Review dialect recipe')} size="form" openerRef={openerRef} onOpenChange={open=>{if(!open)void close()}}
  description={msg('Review the proposed instructions, fields and connections. Saving does not start research.')}
  footer={<DialogActions><Button disabled={busy||pending} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||pending||!store||!name.trim()||!recipe.intent.trim()||!researchReady||!companyReady} onClick={()=>void save()}>{busy?msg('Saving…'):msg('Save to Dialects')}</Button></DialogActions>}>
  <div className={styles.stack}>
   {error&&<Notice tone="error">{error}</Notice>}
   <Field label={msg('Name')}>{props=><Input {...props} value={name} maxLength={160} disabled={busy} onChange={event=>setName(event.target.value)}/>}</Field>
   <Field label={msg('What records do you need?')} hint={msg('Describe the records to find and what to research. Set record counts, request budgets and retries in Limits and retries. Qualification rules stay separate.')}>{props=><TextArea {...props} value={recipe.intent} maxLength={12000} rows={4} disabled={busy} onChange={event=>setRecipe({...recipe,intent:event.target.value})}/>}</Field>
   <ResearchRecipe value={recipe.research} records={[]} disabled={busy} onChange={research=>setRecipe({...recipe,research})}/>
   <Disclosure title={msg('Pack (optional)')}><div className={styles.stack}><p className={styles.note}>{msg('Research can run before you choose a pack. Review dialects here or later in Dialects.')}</p><DialectPackEditor value={recipe.qualification} records={[]} research={recipe.research} disabled={busy} onPendingChange={setPending} onChange={qualification=>setRecipe({...recipe,qualification})}/></div></Disclosure>
  </div>
 </Dialog>
}
